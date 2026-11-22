// Command corestress exercises Ruxbase's Go-level core subsystems against a real
// isolated PostgreSQL database (restricted ruxbase_app role) for Test F:
//   - Durable Jobs: high-volume enqueue, idempotency collisions, concurrent worker claiming,
//     lease expiration & worker crash reclamation, retries + DLQ + replay, transactional outbox.
//   - StorageManager: concurrent uploads/downloads, 10MB large object, signed URLs,
//     sequential & concurrent multipart upload race probe, cross-tenant denial, SHA-256 integrity.
//   - FunctionManager: concurrent Goja JS executions, timeout enforcement, sandbox isolation,
//     parent context cancellation probe, and failure recovery.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	_ "github.com/AbdullahZaher/ruxbase/migrations"
	"github.com/AbdullahZaher/ruxbase/tools/types"
)

func emit(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

func pctl(sorted []time.Duration, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted))*p/100.0)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return math.Round(float64(sorted[idx].Microseconds())/1000.0*1000) / 1000
}

func main() {
	log.SetOutput(os.Stderr)

	dsn := flag.String("db", os.Getenv("DATABASE_URL"), "restricted ruxbase_app PostgreSQL DSN")
	dataDir := flag.String("dir", "/tmp/corestress_data", "disposable storage directory")
	poolMax := flag.Int("pool", 30, "DB max open connections")
	jobCount := flag.Int("jobs", 3000, "durable jobs count for high-volume enqueue/drain test")
	workers := flag.Int("workers", 20, "concurrent workers for jobs/storage/functions")
	fnCalls := flag.Int("fn-calls", 1000, "function invocations for concurrent JS test")
	flag.Parse()

	if *dsn == "" {
		log.Fatal("-db or DATABASE_URL is required")
	}
	_ = os.RemoveAll(*dataDir)
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("mkdir dataDir: %v", err)
	}
	defer os.RemoveAll(*dataDir)

	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:          *dataDir,
		DatabaseURL:      *dsn,
		DataMaxOpenConns: *poolMax,
		DataMaxIdleConns: *poolMax,
		AuxMaxOpenConns:  5,
		AuxMaxIdleConns:  5,
	})
	if err := app.Bootstrap(); err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}
	defer app.ResetBootstrapState()

	ctx := context.Background()

	// Verify restricted role (no SUPERUSER / no BYPASSRLS)
	blockersPre, err := core.CheckReleaseBlockers(ctx, app)
	if err != nil {
		log.Fatalf("CheckReleaseBlockers pre-check error: %v", err)
	}
	log.Printf("[corestress] pre-check releaseBlockers passed=%v", blockersPre.Passed)

	runJobsStress(ctx, app, *jobCount, *workers)
	runStorageStress(ctx, app, *workers)
	runFunctionsStress(ctx, app, *fnCalls, *workers)

	blockersPost, _ := core.CheckReleaseBlockers(ctx, app)
	emit(map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"testId":    "F-CORE-INTEGRITY",
		"scenario":  "core-release-blockers-and-storage-sha256",
		"passed":    blockersPost != nil && blockersPost.Passed,
		"findings":  blockersPost.Findings,
	})
}

func runJobsStress(ctx context.Context, app *core.BaseApp, totalJobs, workers int) {
	jm := app.Jobs()
	queue := fmt.Sprintf("stress_q_%d", time.Now().UnixNano())

	// 1. High-volume concurrent enqueue + idempotency collision storm
	log.Printf("[Test F-Jobs] enqueuing %d unique jobs + 500 idempotency collisions with %d goroutines", totalJobs, workers)
	var enqOK, enqErr atomic.Int64
	var enqLatMu sync.Mutex
	enqLats := make([]time.Duration, 0, totalJobs)

	tEnq := time.Now()
	jobsCh := make(chan int, totalJobs)
	for i := range totalJobs {
		jobsCh <- i
	}
	close(jobsCh)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobsCh {
				t0 := time.Now()
				_, err := jm.Enqueue(&core.Job{
					Queue:          queue,
					Task:           "stress_work",
					Payload:        types.JSONMap[any]{"seq": idx},
					MaxRetries:     3,
					BackoffSeconds: 1,
					IdempotencyKey: fmt.Sprintf("%s_key_%d", queue, idx),
				})
				d := time.Since(t0)
				if err != nil {
					enqErr.Add(1)
					continue
				}
				enqOK.Add(1)
				enqLatMu.Lock()
				enqLats = append(enqLats, d)
				enqLatMu.Unlock()
			}
		}()
	}
	wg.Wait()
	enqElapsed := time.Since(tEnq)
	slices.Sort(enqLats)

	// Idempotency collision storm: re-enqueue first 500 keys concurrently from 20 goroutines
	var dedupReturnedExisting, dedupErr atomic.Int64
	var dwg sync.WaitGroup
	for w := range 20 {
		dwg.Add(1)
		go func(id int) {
			defer dwg.Done()
			for k := id; k < 500; k += 20 {
				j, err := jm.Enqueue(&core.Job{
					Queue:          queue,
					Task:           "stress_work",
					Payload:        types.JSONMap[any]{"seq": k, "dup": true},
					IdempotencyKey: fmt.Sprintf("%s_key_%d", queue, k),
				})
				if err != nil {
					dedupErr.Add(1)
				} else if j != nil {
					dedupReturnedExisting.Add(1)
				}
			}
		}(w)
	}
	dwg.Wait()

	// Verify exact row count in _jobs for this queue (must equal totalJobs, zero duplicates!)
	var dbEnqueuedCount int64
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_jobs}} WHERE [[queue]] = {:q}").Bind(map[string]any{"q": queue}).Row(&dbEnqueuedCount)

	// 2. Concurrent worker claiming & processing (verify exact-once execution, 0 lost jobs)
	log.Printf("[Test F-Jobs] draining %d jobs across %d concurrent workers", dbEnqueuedCount, workers)
	var execCounts sync.Map // jobId -> *atomic.Int64
	var processedTotal, duplicateExecs, procErrors atomic.Int64
	var procLatMu sync.Mutex
	procLats := make([]time.Duration, 0, totalJobs)

	jm.RegisterHandler("stress_work", func(hctx context.Context, job *core.Job) error {
		val, _ := execCounts.LoadOrStore(job.Id, &atomic.Int64{})
		if val.(*atomic.Int64).Add(1) > 1 {
			duplicateExecs.Add(1)
		}
		return nil
	})

	tDrain := time.Now()
	var pwg sync.WaitGroup
	for w := range workers {
		pwg.Add(1)
		go func(wid int) {
			defer pwg.Done()
			wName := fmt.Sprintf("worker-%02d", wid)
			emptyStreak := 0
			for {
				t0 := time.Now()
				didWork, err := jm.ProcessNext(ctx, queue, wName, 30*time.Second)
				d := time.Since(t0)
				if err != nil {
					procErrors.Add(1)
				}
				if didWork {
					emptyStreak = 0
					processedTotal.Add(1)
					procLatMu.Lock()
					procLats = append(procLats, d)
					procLatMu.Unlock()
					continue
				}
				emptyStreak++
				if emptyStreak >= 3 {
					return
				}
				time.Sleep(15 * time.Millisecond)
			}
		}(w)
	}
	pwg.Wait()
	drainElapsed := time.Since(tDrain)
	slices.Sort(procLats)

	var completedCount, pendingLeft int64
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_jobs}} WHERE [[queue]] = {:q} AND [[status]] = 'completed'").Bind(map[string]any{"q": queue}).Row(&completedCount)
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_jobs}} WHERE [[queue]] = {:q} AND [[status]] != 'completed'").Bind(map[string]any{"q": queue}).Row(&pendingLeft)

	emit(map[string]any{
		"timestamp":            time.Now().UTC().Format(time.RFC3339),
		"testId":               "F-JOBS-THROUGHPUT",
		"scenario":             "durable-jobs-enqueue-idempotency-drain",
		"workers":              workers,
		"targetJobs":           totalJobs,
		"enqueuedOk":           enqOK.Load(),
		"enqueueErrors":        enqErr.Load(),
		"enqueueRPS":           math.Round(float64(enqOK.Load())/enqElapsed.Seconds()*100) / 100,
		"enqueueP50ms":         pctl(enqLats, 50),
		"enqueueP95ms":         pctl(enqLats, 95),
		"enqueueP99ms":         pctl(enqLats, 99),
		"dedupCollisionCalls":  dedupReturnedExisting.Load(),
		"dedupErrors":          dedupErr.Load(),
		"dbRowCountAfterDedup": dbEnqueuedCount,
		"idempotencyVerified":  dbEnqueuedCount == int64(totalJobs),
		"drainSeconds":         math.Round(drainElapsed.Seconds()*1000) / 1000,
		"drainJobsPerSec":      math.Round(float64(processedTotal.Load())/drainElapsed.Seconds()*100) / 100,
		"claimProcessP50ms":    pctl(procLats, 50),
		"claimProcessP95ms":    pctl(procLats, 95),
		"claimProcessP99ms":    pctl(procLats, 99),
		"completedInDB":        completedCount,
		"uncompletedLeft":      pendingLeft,
		"duplicateExecutions":  duplicateExecs.Load(),
		"lostJobs":             int64(totalJobs) - completedCount,
	})

	// 3. Lease expiration + worker crash reclamation + retry/DLQ + ReplayDeadJob
	crashQ := queue + "_resilience"
	crashJob, _ := jm.Enqueue(&core.Job{Queue: crashQ, Task: "crash_task", MaxRetries: 3, BackoffSeconds: 1})
	// Simulate worker claiming with 1s lease and crashing (never calling Complete/Fail)
	claimed, _ := jm.ClaimNext(crashQ, "crashed-worker", 1*time.Second)
	time.Sleep(1300 * time.Millisecond)
	reclaimed, _ := jm.ReclaimStaleLeases()
	reclaimedJob, _ := jm.ClaimNext(crashQ, "recovery-worker", 10*time.Second)
	if reclaimedJob != nil {
		_ = jm.Complete(reclaimedJob.Id, map[string]any{"recovered": true})
	}
	finalCrashJob, _ := jm.GetJob(crashJob.Id)

	// Poison job -> retries -> DLQ -> ReplayDeadJob
	poisonQ := queue + "_poison"
	poisonJob, _ := jm.Enqueue(&core.Job{Queue: poisonQ, Task: "poison_task", MaxRetries: 2, BackoffSeconds: 1})
	for attempt := 0; attempt < 2; attempt++ {
		if j, _ := jm.ClaimNext(poisonQ, "poison-w", 5*time.Second); j != nil {
			_ = jm.Fail(j.Id, errors.New("simulated downstream failure"))
			// Force run_at to now for immediate second retry test
			_, _ = app.DB().NewQuery("UPDATE {{_jobs}} SET [[run_at]] = now() WHERE [[id]] = {:id}").Bind(map[string]any{"id": j.Id}).Execute()
		}
	}
	deadState, _ := jm.GetJob(poisonJob.Id)
	replayErr := jm.ReplayDeadJob(poisonJob.Id)
	replayedState, _ := jm.GetJob(poisonJob.Id)

	// 4. Transactional Outbox concurrent publication probe (tests whether concurrent workers double-publish!)
	outboxTopic := fmt.Sprintf("stress_outbox_%d", time.Now().UnixNano())
	const outboxN = 100
	for i := range outboxN {
		_, _ = jm.PublishOutbox("order.created", outboxTopic, map[string]any{"seq": i})
	}
	var outboxPublishedCalls atomic.Int64
	var outboxDupDeliveries atomic.Int64
	var seenOutbox sync.Map
	var owg sync.WaitGroup
	for range 8 {
		owg.Add(1)
		go func() {
			defer owg.Done()
			for {
				n, _ := jm.ProcessPendingOutbox(20, func(evt *core.OutboxEvent) error {
					// Simulate 5ms webhook/broker publish latency
					time.Sleep(5 * time.Millisecond)
					outboxPublishedCalls.Add(1)
					val, _ := seenOutbox.LoadOrStore(evt.Id, &atomic.Int64{})
					if val.(*atomic.Int64).Add(1) > 1 {
						outboxDupDeliveries.Add(1)
					}
					return nil
				})
				if n == 0 {
					return
				}
			}
		}()
	}
	owg.Wait()

	emit(map[string]any{
		"timestamp":                 time.Now().UTC().Format(time.RFC3339),
		"testId":                    "F-JOBS-RESILIENCE-OUTBOX",
		"scenario":                  "lease-crash-dlq-replay-and-outbox-concurrency",
		"crashJobClaimed":           claimed != nil,
		"staleLeasesReclaimed":      reclaimed,
		"crashJobRecoveredStatus":   finalCrashJob.Status,
		"poisonJobDLQStatus":        deadState.Status,
		"poisonReplayErr":           fmt.Sprint(replayErr),
		"poisonReplayedStatus":      replayedState.Status,
		"outboxEventsEnqueued":      outboxN,
		"outboxPublisherCalls":      outboxPublishedCalls.Load(),
		"outboxDuplicateDeliveries": outboxDupDeliveries.Load(),
	})
}

func runStorageStress(ctx context.Context, app *core.BaseApp, workers int) {
	sm := app.Storage()
	bucketName := fmt.Sprintf("stress_bkt_%d", time.Now().UnixNano())
	_, err := sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 25 * 1024 * 1024, // 25 MiB
	})
	if err != nil {
		log.Printf("[Test F-Storage] CreateBucket error: %v", err)
		return
	}

	// 1. Concurrent uploads & downloads (100 objects × 256 KB) + cross-tenant access denial + SHA-256 check
	const numObjs = 100
	payload256K := bytes.Repeat([]byte("RUXBASE-OBJECT-STORAGE-256KB-BLOB"), 7900)
	h256 := sha256.Sum256(payload256K)
	want256 := hex.EncodeToString(h256[:])

	var upOK, upErr, dlOK, dlErr, hashMismatch, crossDenied, crossLeaked atomic.Int64
	var upLatMu, dlLatMu sync.Mutex
	upLats := make([]time.Duration, 0, numObjs)
	dlLats := make([]time.Duration, 0, numObjs)

	tUp := time.Now()
	ch := make(chan int, numObjs)
	for i := range numObjs {
		ch <- i
	}
	close(ch)
	var wg sync.WaitGroup
	for range min(workers, 16) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range ch {
				owner := fmt.Sprintf("tenant_%02d", idx%5)
				name := fmt.Sprintf("obj_%04d.bin", idx)
				t0 := time.Now()
				obj, err := sm.UploadObject(ctx, bucketName, name, owner, "application/octet-stream", payload256K, map[string]any{"idx": idx})
				d := time.Since(t0)
				if err != nil || obj == nil || obj.Etag != want256 {
					upErr.Add(1)
					continue
				}
				upOK.Add(1)
				upLatMu.Lock()
				upLats = append(upLats, d)
				upLatMu.Unlock()
			}
		}()
	}
	wg.Wait()
	upElapsed := time.Since(tUp)
	slices.Sort(upLats)

	// Concurrent reads + cross-tenant checks
	tDl := time.Now()
	ch2 := make(chan int, numObjs)
	for i := range numObjs {
		ch2 <- i
	}
	close(ch2)
	for range min(workers, 16) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range ch2 {
				owner := fmt.Sprintf("tenant_%02d", idx%5)
				other := fmt.Sprintf("tenant_%02d", (idx+1)%5)
				name := fmt.Sprintf("obj_%04d.bin", idx)
				t0 := time.Now()
				_, data, err := sm.GetObject(ctx, bucketName, name, owner, false)
				d := time.Since(t0)
				if err != nil {
					dlErr.Add(1)
				} else {
					sum := sha256.Sum256(data)
					if hex.EncodeToString(sum[:]) != want256 {
						hashMismatch.Add(1)
					} else {
						dlOK.Add(1)
						dlLatMu.Lock()
						dlLats = append(dlLats, d)
						dlLatMu.Unlock()
					}
				}
				// Cross-tenant probe
				if _, _, cErr := sm.GetObject(ctx, bucketName, name, other, false); errors.Is(cErr, core.ErrStorageAccessDenied) {
					crossDenied.Add(1)
				} else {
					crossLeaked.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	dlElapsed := time.Since(tDl)
	slices.Sort(dlLats)

	// 2. Large file (10 MiB) upload & SHA-256 download
	largePayload := bytes.Repeat([]byte("0123456789ABCDEF"), 655360) // 10 MiB
	largeSum := sha256.Sum256(largePayload)
	tLarge := time.Now()
	largeObj, largeErr := sm.UploadObject(ctx, bucketName, "large_10mb.bin", "tenant_00", "application/octet-stream", largePayload, nil)
	largeUpMs := float64(time.Since(tLarge).Microseconds()) / 1000.0
	tLargeDl := time.Now()
	_, largeData, largeDlErr := sm.GetObject(ctx, bucketName, "large_10mb.bin", "tenant_00", false)
	largeDlMs := float64(time.Since(tLargeDl).Microseconds()) / 1000.0
	largeHashOK := largeErr == nil && largeDlErr == nil && largeObj != nil && sha256.Sum256(largeData) == largeSum

	// 3. Signed URL issuance & verification (valid + tampered + expired)
	secret := "stress-signing-secret-2026"
	signedTok, sErr := sm.CreateSignedURL(bucketName, "large_10mb.bin", 60*time.Second, secret)
	vb, vo, vErr := sm.VerifySignedToken(signedTok, secret)
	_, _, badSigErr := sm.VerifySignedToken(signedTok+"tamper", secret)
	expTok, _ := sm.CreateSignedURL(bucketName, "large_10mb.bin", -2*time.Second, secret)
	_, _, expErr := sm.VerifySignedToken(expTok, secret)
	signedOK := sErr == nil && vErr == nil && vb == bucketName && vo == "large_10mb.bin" && badSigErr != nil && expErr != nil

	// 4. Multipart uploads: sequential (4 parts × 512KB) AND concurrent (8 parts uploaded in parallel to test JSONB race!)
	seqSession, _ := sm.StartMultipartUpload(bucketName, "multipart_seq.bin", "tenant_00")
	var seqExpected bytes.Buffer
	for p := 1; p <= 4; p++ {
		chunk := bytes.Repeat([]byte{byte('A' + p)}, 512*1024)
		seqExpected.Write(chunk)
		_, _ = sm.UploadPart(seqSession.Id, p, chunk)
	}
	seqObj, seqCompErr := sm.CompleteMultipartUpload(ctx, seqSession.Id)
	seqSum := sha256.Sum256(seqExpected.Bytes())
	seqIntegrityOK := seqCompErr == nil && seqObj != nil && seqObj.Etag == hex.EncodeToString(seqSum[:]) && seqObj.Size == int64(seqExpected.Len())

	// Concurrent multipart UploadPart race probe (8 goroutines uploading parts 1..8 simultaneously)
	concSession, _ := sm.StartMultipartUpload(bucketName, "multipart_concurrent.bin", "tenant_00")
	var concExpectedLen int64 = 8 * 256 * 1024
	var mpWg sync.WaitGroup
	for p := 1; p <= 8; p++ {
		mpWg.Add(1)
		go func(partNum int) {
			defer mpWg.Done()
			chunk := bytes.Repeat([]byte{byte('0' + partNum)}, 256*1024)
			_, _ = sm.UploadPart(concSession.Id, partNum, chunk)
		}(p)
	}
	mpWg.Wait()
	concObj, concCompErr := sm.CompleteMultipartUpload(ctx, concSession.Id)
	concGotSize := int64(0)
	if concObj != nil {
		concGotSize = concObj.Size
	}

	emit(map[string]any{
		"timestamp":                    time.Now().UTC().Format(time.RFC3339),
		"testId":                       "F-STORAGE-CORE",
		"scenario":                     "storage-manager-concurrency-large-signed-multipart",
		"uploadsOk":                    upOK.Load(),
		"uploadErrors":                 upErr.Load(),
		"uploadObjsPerSec":             math.Round(float64(upOK.Load())/upElapsed.Seconds()*100) / 100,
		"uploadP95ms":                  pctl(upLats, 95),
		"downloadsOk":                  dlOK.Load(),
		"downloadErrors":               dlErr.Load(),
		"downloadObjsPerSec":           math.Round(float64(dlOK.Load())/dlElapsed.Seconds()*100) / 100,
		"downloadP95ms":                pctl(dlLats, 95),
		"sha256Mismatches":             hashMismatch.Load(),
		"crossTenantDenied":            crossDenied.Load(),
		"crossTenantLeaked":            crossLeaked.Load(),
		"large10MiBUploadMs":           math.Round(largeUpMs*100) / 100,
		"large10MiBDownloadMs":         math.Round(largeDlMs*100) / 100,
		"large10MiBSha256Ok":           largeHashOK,
		"signedUrlValidationOk":        signedOK,
		"multipartSequentialOk":        seqIntegrityOK,
		"multipartConcurrentWantBytes": concExpectedLen,
		"multipartConcurrentGotBytes":  concGotSize,
		"multipartConcurrentLostBytes": concExpectedLen - concGotSize,
		"multipartConcurrentErr":       fmt.Sprint(concCompErr),
	})
}

func runFunctionsStress(ctx context.Context, app *core.BaseApp, totalCalls, workers int) {
	fm := app.Functions()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// 1. Compute function
	fnCompute := "stress_compute_" + suffix
	_, err := fm.CreateFunction(&core.Function{
		Name:       fnCompute,
		Entrypoint: "main",
		Runtime:    "js",
		Code: `function main(payload) {
			let sum = 0;
			for (let i = 1; i <= (payload.n || 100); i++) sum += i;
			return { sum: sum, secret: env.APP_ENV || "none" };
		}`,
		EnvVars:    types.JSONMap[string]{"APP_ENV": "isolated_ok"},
		TimeoutSec: 5,
	})
	if err != nil {
		log.Printf("[Test F-Functions] CreateFunction error: %v", err)
		return
	}

	// 2. Infinite-loop timeout function (1s timeout)
	fnTimeout := "stress_timeout_" + suffix
	_, _ = fm.CreateFunction(&core.Function{
		Name:       fnTimeout,
		Entrypoint: "main",
		Runtime:    "js",
		Code:       `function main(p) { while (true) {} }`,
		TimeoutSec: 1,
	})

	// 3. Throwing/error function
	fnFail := "stress_fail_" + suffix
	_, _ = fm.CreateFunction(&core.Function{
		Name:       fnFail,
		Entrypoint: "main",
		Runtime:    "js",
		Code:       `function main(p) { throw new Error("intentional boom"); }`,
		TimeoutSec: 5,
	})

	// 4. Sandbox escape probe (check process/require/os/global host env leakage)
	fnIso := "stress_iso_" + suffix
	_, _ = fm.CreateFunction(&core.Function{
		Name:       fnIso,
		Entrypoint: "main",
		Runtime:    "js",
		Code: `function main(p) {
			return {
				hasProcess: typeof process !== "undefined",
				hasRequire: typeof require !== "undefined",
				hasFetch: typeof fetch !== "undefined",
				hasDatabaseUrl: typeof env.DATABASE_URL !== "undefined"
			};
		}`,
		EnvVars:    types.JSONMap[string]{"SAFE_VAR": "1"},
		TimeoutSec: 3,
	})

	// Concurrent compute invocations
	var okCnt, errCnt atomic.Int64
	var latMu sync.Mutex
	lats := make([]time.Duration, 0, totalCalls)
	ch := make(chan int, totalCalls)
	for i := range totalCalls {
		ch <- i
	}
	close(ch)

	t0 := time.Now()
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range ch {
				s := time.Now()
				out, invErr := fm.Invoke(ctx, fnCompute, map[string]any{"n": 200 + (idx % 50)})
				d := time.Since(s)
				if invErr != nil || out == nil {
					errCnt.Add(1)
					continue
				}
				okCnt.Add(1)
				latMu.Lock()
				lats = append(lats, d)
				latMu.Unlock()
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(t0)
	slices.Sort(lats)

	// Concurrent timeout enforcement (8 simultaneous infinite loops with 1s timeout)
	var timeoutEnforced, timeoutUnexpected atomic.Int64
	tTo := time.Now()
	var twg sync.WaitGroup
	for range 8 {
		twg.Add(1)
		go func() {
			defer twg.Done()
			_, tErr := fm.Invoke(ctx, fnTimeout, nil)
			if errors.Is(tErr, core.ErrFunctionTimeout) {
				timeoutEnforced.Add(1)
			} else {
				timeoutUnexpected.Add(1)
			}
		}()
	}
	twg.Wait()
	timeoutWallMs := float64(time.Since(tTo).Microseconds()) / 1000.0

	// Parent context cancellation probe (50ms caller cancel vs 5s TimeoutSec)
	cancelCtx, cancelFn := context.WithTimeout(ctx, 100*time.Millisecond)
	tCancel := time.Now()
	_, cancelErr := fm.Invoke(cancelCtx, fnTimeout, nil)
	cancelWallMs := float64(time.Since(tCancel).Microseconds()) / 1000.0
	cancelFn()

	// Mixed error + healthy recovery verification
	var failCaught, postFailHealthy int
	for range 10 {
		if _, fErr := fm.Invoke(ctx, fnFail, nil); fErr != nil {
			failCaught++
		}
		if out, hErr := fm.Invoke(ctx, fnCompute, map[string]any{"n": 10}); hErr == nil && out != nil {
			postFailHealthy++
		}
	}

	// Isolation verification
	isoOut, isoErr := fm.Invoke(ctx, fnIso, nil)

	emit(map[string]any{
		"timestamp":                time.Now().UTC().Format(time.RFC3339),
		"testId":                   "F-FUNCTIONS-CORE",
		"scenario":                 "goja-functions-concurrency-timeout-isolation-recovery",
		"workers":                  workers,
		"invocationsOk":            okCnt.Load(),
		"invocationsErr":           errCnt.Load(),
		"invocationsPerSec":        math.Round(float64(okCnt.Load())/elapsed.Seconds()*100) / 100,
		"p50ms":                    pctl(lats, 50),
		"p95ms":                    pctl(lats, 95),
		"p99ms":                    pctl(lats, 99),
		"concurrentTimeoutsHit":    timeoutEnforced.Load(),
		"concurrentTimeoutsMissed": timeoutUnexpected.Load(),
		"timeoutWallMs":            math.Round(timeoutWallMs*100) / 100,
		"parentCancel100msWallMs":  math.Round(cancelWallMs*100) / 100,
		"parentCancelErr":          fmt.Sprint(cancelErr),
		"exceptionsCaught":         failCaught,
		"postFailureHealthyCalls":  postFailHealthy,
		"isolationProbe":           isoOut,
		"isolationProbeErr":        fmt.Sprint(isoErr),
	})
}
