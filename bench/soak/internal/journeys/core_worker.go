package journeys

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/state"
	"github.com/AbdullahZaher/ruxbase/core"
	_ "github.com/AbdullahZaher/ruxbase/migrations"
	"github.com/AbdullahZaher/ruxbase/tools/types"
)

const (
	SoakJobQueue      = "soak_durable_jobs"
	SoakRetryQueue    = "soak_retry_dlq"
	SoakStorageBucket = "soak_multipart_bucket"
)

// CoreWorkerConfig configures the supervised Durable Jobs, Workflows, Outbox,
// and Multipart Storage worker container.
type CoreWorkerConfig struct {
	DatabaseURL string
	DataDir     string
	EvidenceDir string
	CampaignID  string
	Workers     int
	MetricsAddr string
}

// CoreWorkerStats tracks background processing invariants for Prometheus and logs.
type CoreWorkerStats struct {
	JobsEnqueued         atomic.Int64
	JobsAttempted        atomic.Int64
	JobsCompleted        atomic.Int64
	JobsRetried          atomic.Int64
	JobsReclaimed        atomic.Int64
	WorkflowsCompleted   atomic.Int64
	OutboxPublished      atomic.Int64
	OutboxDupAtLeastOnce atomic.Int64
	MultipartCompleted   atomic.Int64
	MultipartErrors      atomic.Int64
	OrphanGCCleaned      atomic.Int64
	IntegrityErrors      atomic.Int64
}

// boundedIDSet keeps O(1) bounded memory across 7+ days instead of leaking unbounded sync.Map keys.
type boundedIDSet struct {
	mu    sync.Mutex
	max   int
	ring  []string
	idx   int
	items map[string]struct{}
}

func newBoundedIDSet(maxItems int) *boundedIDSet {
	if maxItems <= 0 {
		maxItems = 10000
	}
	return &boundedIDSet{
		max:   maxItems,
		ring:  make([]string, maxItems),
		items: make(map[string]struct{}, maxItems),
	}
}

func (b *boundedIDSet) add(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.items[id]; exists {
		return false
	}
	if old := b.ring[b.idx]; old != "" {
		delete(b.items, old)
	}
	b.ring[b.idx] = id
	b.items[id] = struct{}{}
	b.idx = (b.idx + 1) % b.max
	return true
}

// RunCoreWorker connects via the restricted ruxbase_app role and continuously executes
// Journey E (Durable Jobs, Delayed Jobs, Retries, DLQ Replay, DAG Workflows, Outbox leasing)
// and Journey F (StorageManager concurrent multipart uploads + SHA-256 verification + orphan GC).
func RunCoreWorker(ctx context.Context, cfg CoreWorkerConfig) error {
	if cfg.DatabaseURL == "" {
		return errors.New("database URL is required for soak worker")
	}
	cid := strings.TrimSpace(cfg.CampaignID)
	if cid == "" || cid == "pilot-pending" || cid == "staging" {
		return fmt.Errorf("soak worker requires a valid active --campaign-id (got %q)", cfg.CampaignID)
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return fmt.Errorf("mkdir data dir: %w", err)
	}

	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:          cfg.DataDir,
		DatabaseURL:      cfg.DatabaseURL,
		DataMaxOpenConns: cfg.Workers + 6,
		DataMaxIdleConns: cfg.Workers + 2,
		AuxMaxOpenConns:  2,
		AuxMaxIdleConns:  2,
	})
	if err := app.Bootstrap(); err != nil {
		return fmt.Errorf("bootstrap worker BaseApp: %w", err)
	}
	defer app.ClearBootstrap()

	// Fail closed if runtime role is privileged
	blockers, err := core.CheckReleaseBlockers(ctx, app)
	if err != nil {
		return fmt.Errorf("worker preflight CheckReleaseBlockers: %w", err)
	}
	if !blockers.Passed {
		return fmt.Errorf("worker preflight CheckReleaseBlockers FAILED: %+v", blockers.Findings)
	}

	stats := &CoreWorkerStats{}
	startedAt := time.Now().UTC()
	jm := app.Jobs()
	sm := app.Storage()

	if _, bErr := sm.GetBucket(SoakStorageBucket); errors.Is(bErr, core.ErrStorageBucketNotFound) {
		if _, cErr := sm.CreateBucket(&core.StorageBucket{
			Name:          SoakStorageBucket,
			Public:        false,
			FileSizeLimit: 25 * 1024 * 1024,
		}); cErr != nil {
			if _, checkErr := sm.GetBucket(SoakStorageBucket); checkErr != nil {
				return fmt.Errorf("create storage bucket %s: %w", SoakStorageBucket, cErr)
			}
		}
	} else if bErr != nil {
		return fmt.Errorf("inspect storage bucket %s: %w", SoakStorageBucket, bErr)
	}

	// Register idempotent job & workflow handlers with bounded memory tracking
	seenJobEffects := newBoundedIDSet(10000)
	jm.RegisterHandler("soak.compute", func(_ context.Context, job *core.Job) error {
		time.Sleep(15 * time.Millisecond)
		seenJobEffects.add(job.Id)
		stats.JobsCompleted.Add(1)
		return nil
	})
	jm.RegisterHandler("soak.flaky_once", func(_ context.Context, job *core.Job) error {
		if job.Attempts <= 1 {
			stats.JobsRetried.Add(1)
			return errors.New("intentional transient retry exercise")
		}
		stats.JobsCompleted.Add(1)
		return nil
	})
	jm.RegisterWorkflowHandler("soak.wf.validate", func(_ context.Context, _ *core.WorkflowStep, _ map[string]any) (map[string]any, error) {
		return map[string]any{"validated": true}, nil
	})
	jm.RegisterWorkflowHandler("soak.wf.finalize", func(_ context.Context, _ *core.WorkflowStep, execCtx map[string]any) (map[string]any, error) {
		if execCtx["validated"] != true {
			return nil, errors.New("workflow context lost between steps")
		}
		return map[string]any{"finalized": true}, nil
	})

	if cfg.MetricsAddr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok\n"))
		})
		mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			fmt.Fprintf(w, "ruxbase_soak_worker_jobs_enqueued_total %d\n", stats.JobsEnqueued.Load())
			fmt.Fprintf(w, "ruxbase_soak_worker_jobs_attempted_total %d\n", stats.JobsAttempted.Load())
			fmt.Fprintf(w, "ruxbase_soak_worker_jobs_completed_total %d\n", stats.JobsCompleted.Load())
			fmt.Fprintf(w, "ruxbase_soak_worker_jobs_retried_total %d\n", stats.JobsRetried.Load())
			fmt.Fprintf(w, "ruxbase_soak_worker_jobs_reclaimed_total %d\n", stats.JobsReclaimed.Load())
			fmt.Fprintf(w, "ruxbase_soak_worker_workflows_completed_total %d\n", stats.WorkflowsCompleted.Load())
			fmt.Fprintf(w, "ruxbase_soak_worker_outbox_published_total %d\n", stats.OutboxPublished.Load())
			fmt.Fprintf(w, "ruxbase_soak_worker_multipart_completed_total %d\n", stats.MultipartCompleted.Load())
			fmt.Fprintf(w, "ruxbase_soak_worker_multipart_errors_total %d\n", stats.MultipartErrors.Load())
			fmt.Fprintf(w, "ruxbase_soak_worker_orphan_gc_cleaned_total %d\n", stats.OrphanGCCleaned.Load())
			fmt.Fprintf(w, "ruxbase_soak_worker_integrity_errors_total %d\n", stats.IntegrityErrors.Load())
		})
		srv := &http.Server{Addr: cfg.MetricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			<-ctx.Done()
			shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shCtx)
		}()
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("[soak-worker] metrics server error: %v", err)
			}
		}()
	}

	var wg sync.WaitGroup

	// 1. Producer loop: enqueues immediate jobs, delayed jobs, retry jobs, workflows, outbox events
	wg.Add(1)
	go func() {
		defer wg.Done()
		tk := time.NewTicker(2 * time.Second)
		defer tk.Stop()
		var seq int64
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				seq++
				if _, err := jm.Enqueue(&core.Job{
					Queue:          SoakJobQueue,
					Task:           "soak.compute",
					Payload:        types.JSONMap[any]{"campaignId": cid, "seq": seq},
					MaxRetries:     3,
					BackoffSeconds: 2,
					IdempotencyKey: fmt.Sprintf("soak-%s-job-%d-%d", cid, time.Now().Unix(), seq),
				}); err != nil {
					log.Printf("[soak-worker] enqueue compute job error: %v", err)
				} else {
					stats.JobsEnqueued.Add(1)
				}
				if seq%5 == 0 {
					futureRun, _ := types.ParseDateTime(time.Now().UTC().Add(5 * time.Second))
					if _, err := jm.Enqueue(&core.Job{
						Queue:          SoakJobQueue,
						Task:           "soak.flaky_once",
						Payload:        types.JSONMap[any]{"campaignId": cid, "seq": seq},
						MaxRetries:     3,
						BackoffSeconds: 2,
						RunAt:          futureRun,
					}); err != nil {
						log.Printf("[soak-worker] enqueue flaky_once job error: %v", err)
					} else {
						stats.JobsEnqueued.Add(1)
					}
				}
				if _, err := jm.PublishOutbox("soak.record.processed", "soak.events", map[string]any{
					"campaignId": cid,
					"seq":        seq,
					"ts":         time.Now().UTC().Format(time.RFC3339),
				}); err != nil {
					log.Printf("[soak-worker] publish outbox error: %v", err)
				}
				if seq%15 == 0 {
					wf, wErr := jm.CreateWorkflow(&core.Workflow{
						Name: fmt.Sprintf("soak-wf-%s-%d", cid, seq),
						Steps: types.JSONArray[core.WorkflowStep]{
							{Name: "step1", Task: "soak.wf.validate", OnSuccess: "step2"},
							{Name: "step2", Task: "soak.wf.finalize"},
						},
					})
					if wErr != nil {
						log.Printf("[soak-worker] create workflow error: %v", wErr)
					} else if wf != nil {
						for range 4 {
							cont, stepErr := jm.ExecuteNextWorkflowStep(ctx, wf.Id)
							if stepErr != nil || !cont {
								break
							}
						}
						if doneWf, gErr := jm.GetWorkflow(wf.Id); gErr == nil && doneWf.Status == core.WorkflowStatusCompleted {
							stats.WorkflowsCompleted.Add(1)
						}
					}
				}
			}
		}
	}()

	// 2. Durable Job Consumer workers + stale lease reclamation
	for w := range cfg.Workers {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			workerID := fmt.Sprintf("soak-worker-%d-%02d", os.Getpid(), wid)
			for ctx.Err() == nil {
				didWork, err := jm.ProcessNext(ctx, SoakJobQueue, workerID, 20*time.Second)
				if err != nil && !strings.Contains(err.Error(), "intentional transient retry exercise") {
					log.Printf("[soak-worker] ProcessNext error (%s): %v", workerID, err)
				}
				if didWork {
					stats.JobsAttempted.Add(1)
					continue
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(250 * time.Millisecond):
				}
			}
		}(w)
	}

	// 3. Stale lease reclaimer & transactional outbox publisher (with bounded dedup tracking)
	wg.Add(1)
	go func() {
		defer wg.Done()
		seenOutbox := newBoundedIDSet(10000)
		reclaimTk := time.NewTicker(10 * time.Second)
		outboxTk := time.NewTicker(3 * time.Second)
		defer reclaimTk.Stop()
		defer outboxTk.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-reclaimTk.C:
				n, err := jm.ReclaimStaleLeases()
				if err != nil && ctx.Err() == nil {
					log.Printf("[soak-worker] ReclaimStaleLeases error: %v", err)
				} else if n > 0 {
					stats.JobsReclaimed.Add(n)
					log.Printf("[soak-worker] reclaimed %d stale job lease(s)", n)
				}
			case <-outboxTk.C:
				_, err := jm.ProcessPendingOutboxWithOptions(ctx, core.OutboxProcessOptions{
					Limit:          25,
					LeaseDuration:  30 * time.Second,
					RenewInterval:  10 * time.Second,
					PublishTimeout: 5 * time.Second,
				}, func(_ context.Context, evt *core.OutboxEvent) error {
					if !seenOutbox.add(evt.Id) {
						stats.OutboxDupAtLeastOnce.Add(1)
					}
					stats.OutboxPublished.Add(1)
					return nil
				})
				if err != nil && ctx.Err() == nil {
					log.Printf("[soak-worker] outbox processing error: %v", err)
				}
			}
		}
	}()

	// 4. Concurrent Multipart Storage Upload + SHA-256 verification + Two-Stage Orphan GC
	wg.Add(1)
	go func() {
		defer wg.Done()
		mpTk := time.NewTicker(45 * time.Second)
		gcTk := time.NewTicker(5 * time.Minute)
		defer mpTk.Stop()
		defer gcTk.Stop()
		var mpSeq int64

		for {
			select {
			case <-ctx.Done():
				return
			case <-mpTk.C:
				mpSeq++
				if err := runMultipartCycle(ctx, sm, cid, mpSeq); err != nil {
					stats.MultipartErrors.Add(1)
					if errors.Is(err, core.ErrStorageIntegrityMismatch) {
						stats.IntegrityErrors.Add(1)
					}
					log.Printf("[soak-worker] multipart cycle %d error: %v", mpSeq, err)
				} else {
					stats.MultipartCompleted.Add(1)
				}
			case <-gcTk.C:
				cleaned, err := sm.CleanupOrphanedUploads(ctx, 10*time.Minute)
				if err != nil && ctx.Err() == nil {
					log.Printf("[soak-worker] orphan GC error: %v", err)
				} else {
					stats.OrphanGCCleaned.Add(int64(cleaned))
				}
			}
		}
	}()

	// 5. Campaign-scoped structured worker heartbeat
	if cfg.EvidenceDir != "" {
		campDir := filepath.Join(cfg.EvidenceDir, cid)
		if err := os.MkdirAll(campDir, 0o750); err != nil {
			return fmt.Errorf("mkdir campaign evidence dir %s: %w", campDir, err)
		}
		hbJSONPath := filepath.Join(campDir, "worker-heartbeat.json")
		writeHB := func() error {
			return state.WriteJSON(hbJSONPath, state.WorkerHeartbeat{
				CampaignID:         cid,
				PID:                os.Getpid(),
				StartedAtUTC:       startedAt.Format(time.RFC3339),
				LastHeartbeatUTC:   time.Now().UTC().Format(time.RFC3339),
				JobsEnqueued:       stats.JobsEnqueued.Load(),
				JobsAttempted:      stats.JobsAttempted.Load(),
				JobsCompleted:      stats.JobsCompleted.Load(),
				JobsRetried:        stats.JobsRetried.Load(),
				JobsReclaimed:      stats.JobsReclaimed.Load(),
				WorkflowsCompleted: stats.WorkflowsCompleted.Load(),
				OutboxPublished:    stats.OutboxPublished.Load(),
				MultipartCompleted: stats.MultipartCompleted.Load(),
				MultipartErrors:    stats.MultipartErrors.Load(),
				OrphanGCCleaned:    stats.OrphanGCCleaned.Load(),
				IntegrityErrors:    stats.IntegrityErrors.Load(),
			})
		}
		if err := writeHB(); err != nil {
			return fmt.Errorf("write initial worker heartbeat: %w", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			hbTk := time.NewTicker(30 * time.Second)
			defer hbTk.Stop()
			for {
				select {
				case <-ctx.Done():
					_ = writeHB()
					return
				case <-hbTk.C:
					if err := writeHB(); err != nil {
						log.Printf("[soak-worker] heartbeat write error: %v", err)
					}
				}
			}
		}()
	}

	wg.Wait()
	return nil
}

func runMultipartCycle(ctx context.Context, sm *core.StorageManager, campaignID string, seq int64) error {
	owner := fmt.Sprintf("soak_tenant_%02d", seq%DefaultTenantCount)
	objName := fmt.Sprintf("%s_mp_%08d.bin", campaignID, seq)
	sess, err := sm.StartMultipartUpload(SoakStorageBucket, objName, owner)
	if err != nil {
		return err
	}

	const parts = 4
	partBufs := make([][]byte, parts)
	var expected bytes.Buffer
	for p := range parts {
		chunk := bytes.Repeat([]byte{byte('A' + (int(seq)+p)%26)}, 64*1024)
		partBufs[p] = chunk
		expected.Write(chunk)
	}
	wantSum := sha256.Sum256(expected.Bytes())
	wantHex := hex.EncodeToString(wantSum[:])

	var pwg sync.WaitGroup
	errCh := make(chan error, parts)
	for p := range parts {
		pwg.Add(1)
		go func(idx int) {
			defer pwg.Done()
			if _, pErr := sm.UploadPart(sess.Id, idx+1, partBufs[idx]); pErr != nil {
				errCh <- pErr
			}
		}(p)
	}
	pwg.Wait()
	close(errCh)
	for pErr := range errCh {
		_ = sm.AbortMultipartUpload(sess.Id)
		return pErr
	}

	obj, err := sm.CompleteMultipartUpload(ctx, sess.Id)
	if err != nil {
		return err
	}
	if obj.Etag != wantHex || obj.Size != int64(expected.Len()) {
		return fmt.Errorf("%w: multipart etag/size mismatch want=%s(%d) got=%s(%d)", core.ErrStorageIntegrityMismatch, wantHex, expected.Len(), obj.Etag, obj.Size)
	}

	_, data, err := sm.GetObject(ctx, SoakStorageBucket, objName, owner, false)
	if err != nil {
		return err
	}
	gotSum := sha256.Sum256(data)
	if hex.EncodeToString(gotSum[:]) != wantHex {
		return fmt.Errorf("%w: downloaded bytes SHA-256 mismatch", core.ErrStorageIntegrityMismatch)
	}
	otherOwner := fmt.Sprintf("soak_tenant_%02d", (seq+1)%DefaultTenantCount)
	if _, _, denyErr := sm.GetObject(ctx, SoakStorageBucket, objName, otherOwner, false); !errors.Is(denyErr, core.ErrStorageAccessDenied) {
		return fmt.Errorf("storage cross-tenant denial failed: expected ErrStorageAccessDenied, got %v", denyErr)
	}

	if seq%20 != 0 {
		if delErr := sm.DeleteObject(ctx, SoakStorageBucket, objName, owner, false); delErr != nil {
			return fmt.Errorf("delete ephemeral multipart object %s: %w", objName, delErr)
		}
	}
	return nil
}
