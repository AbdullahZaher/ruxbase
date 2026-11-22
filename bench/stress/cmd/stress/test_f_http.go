package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// runTestFHTTP stresses HTTP-exposed Storage (record file uploads/downloads + SHA-256 verification),
// pgvector tenant-isolated vector search (/vector-search and @vectorDistance), and /api/studio/overview.
func runTestFHTTP(ctx context.Context, baseURL string, st *FixtureState, pg *pgTelemetry, dur time.Duration) error {
	client := newStressHTTPClient(20 * time.Second)

	// F-HTTP-1: Concurrent record file uploads (128 KB synthetic blobs) & SHA-256 verified downloads
	log.Printf("[Test F-HTTP] concurrent file uploads & SHA-256 verified downloads")
	type uploadedFile struct {
		recordID string
		filename string
		sha256   string
		token    string
	}
	var uploadsMu sync.Mutex
	uploaded := make([]uploadedFile, 0, 200)
	var upSeq atomic.Int64

	pgBefore, _ := pg.snapshot(ctx)
	recUp, elapsedUp, stoppedUp, reasonUp := runClosedLoop(ctx, loadOpts{
		concurrency:     20,
		warmup:          1 * time.Second,
		duration:        dur,
		emergencyErrPct: 50.0,
	}, func(rctx context.Context, w int, measuring bool) reqOutcome {
		tu := st.Tenants[w%len(st.Tenants)]
		s := upSeq.Add(1)
		// Deterministic 128 KB synthetic payload
		payload := bytes.Repeat([]byte(fmt.Sprintf("ruxbase-storage-stress-chunk-%08d-", s)), 3500)
		sum := sha256.Sum256(payload)
		wantHash := hex.EncodeToString(sum[:])

		var bodyBuf bytes.Buffer
		mw := multipart.NewWriter(&bodyBuf)
		_ = mw.WriteField("tenant_id", tu.TenantID)
		_ = mw.WriteField("owner", tu.UserID)
		_ = mw.WriteField("category", "cat_file")
		_ = mw.WriteField("n", fmt.Sprintf("%d", 900000+s))
		_ = mw.WriteField("title", fmt.Sprintf("File #%d", s))
		part, err := mw.CreateFormFile("attachment", fmt.Sprintf("blob_%d.bin", s))
		if err != nil {
			return reqOutcome{err: err}
		}
		_, _ = part.Write(payload)
		_ = mw.Close()

		req, err := http.NewRequestWithContext(rctx, http.MethodPost, baseURL+"/api/collections/"+stressItemsCol+"/records", &bodyBuf)
		if err != nil {
			return reqOutcome{err: err}
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", tu.Token)
		resp, err := client.Do(req)
		if err != nil {
			return reqOutcome{err: err, timeout: isTimeoutErr(err)}
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return reqOutcome{statusCode: resp.StatusCode, err: fmt.Errorf("upload status %d: %s", resp.StatusCode, truncate(string(raw), 100))}
		}
		var rec struct {
			ID         string `json:"id"`
			Attachment string `json:"attachment"`
		}
		if json.Unmarshal(raw, &rec) != nil || rec.ID == "" || rec.Attachment == "" {
			return reqOutcome{statusCode: resp.StatusCode, integrityError: true, err: fmt.Errorf("missing uploaded filename")}
		}
		if measuring {
			uploadsMu.Lock()
			if len(uploaded) < 200 {
				uploaded = append(uploaded, uploadedFile{recordID: rec.ID, filename: rec.Attachment, sha256: wantHash, token: tu.Token})
			}
			uploadsMu.Unlock()
		}
		return reqOutcome{statusCode: resp.StatusCode}
	})
	pgAfter, _ := pg.snapshot(ctx)
	resUp := &StageResult{
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		TestID:           "F-STORAGE-HTTP-UPLOAD",
		Scenario:         "concurrent-record-file-upload-128kb",
		Mode:             "closed-loop",
		Nodes:            1,
		Concurrency:      20,
		Trial:            1,
		WarmupSec:        1,
		EmergencyStopped: stoppedUp,
		StopReason:       reasonUp,
		PGDelta:          pg.diff(ctx, pgBefore, pgAfter),
		OpsAfter:         fetchOpsMetrics(client, baseURL, st.SuperuserToken),
	}
	recUp.finalize(resUp, elapsedUp)
	emitJSONL(resUp)

	// Concurrent downloads + SHA-256 checksum verification of every downloaded blob
	if len(uploaded) > 0 {
		pgBefore, _ = pg.snapshot(ctx)
		recDl, elapsedDl, stoppedDl, reasonDl := runClosedLoop(ctx, loadOpts{
			concurrency:     30,
			warmup:          1 * time.Second,
			duration:        dur,
			emergencyErrPct: 50.0,
		}, func(rctx context.Context, w int, _ bool) reqOutcome {
			f := uploaded[rand.IntN(len(uploaded))]
			url := fmt.Sprintf("%s/api/files/%s/%s/%s", baseURL, stressItemsCol, f.recordID, f.filename)
			req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
			if err != nil {
				return reqOutcome{err: err}
			}
			req.Header.Set("Authorization", f.token)
			resp, err := client.Do(req)
			if err != nil {
				return reqOutcome{err: err, timeout: isTimeoutErr(err)}
			}
			h := sha256.New()
			_, copyErr := io.Copy(h, resp.Body)
			resp.Body.Close()
			if copyErr != nil || resp.StatusCode != http.StatusOK {
				return reqOutcome{statusCode: resp.StatusCode, err: fmt.Errorf("download status=%d err=%v", resp.StatusCode, copyErr)}
			}
			got := hex.EncodeToString(h.Sum(nil))
			if got != f.sha256 {
				return reqOutcome{statusCode: resp.StatusCode, integrityError: true, err: fmt.Errorf("SHA-256 mismatch: want=%s got=%s", f.sha256, got)}
			}
			return reqOutcome{statusCode: resp.StatusCode}
		})
		pgAfter, _ = pg.snapshot(ctx)
		resDl := &StageResult{
			Timestamp:        time.Now().UTC().Format(time.RFC3339),
			TestID:           "F-STORAGE-HTTP-DOWNLOAD",
			Scenario:         "concurrent-record-file-download-sha256-verified",
			Mode:             "closed-loop",
			Nodes:            1,
			Concurrency:      30,
			Trial:            1,
			WarmupSec:        1,
			EmergencyStopped: stoppedDl,
			StopReason:       reasonDl,
			PGDelta:          pg.diff(ctx, pgBefore, pgAfter),
			OpsAfter:         fetchOpsMetrics(client, baseURL, st.SuperuserToken),
		}
		recDl.finalize(resDl, elapsedDl)
		emitJSONL(resDl)
	}

	// F-HTTP-2: pgvector Tenant-Isolated Vector Search (HNSW 384-dim cosine)
	if st.InitialVectors > 0 {
		log.Printf("[Test F-HTTP] concurrent tenant-isolated pgvector HNSW search (%d vectors, %d dims)", st.InitialVectors, st.VectorDims)
		// Pre-build 20 deterministic query vectors
		qVecs := make([][]float64, 20)
		for i := range 20 {
			qVecs[i] = makeQueryVector(i*17+3, st.VectorDims)
		}

		for _, conc := range []int{10, 50, 100} {
			var underfilled atomic.Int64
			pgBefore, _ := pg.snapshot(ctx)
			recVec, elapsedVec, stoppedVec, reasonVec := runClosedLoop(ctx, loadOpts{
				concurrency:     conc,
				warmup:          2 * time.Second,
				duration:        dur,
				emergencyErrPct: 50.0,
			}, func(rctx context.Context, w int, _ bool) reqOutcome {
				tIdx := w % 10 // vectors span tenant_00..tenant_09
				tu := st.Tenants[tIdx]
				status, raw, err := doJSONCtx(rctx, client, http.MethodPost, baseURL+"/api/collections/"+stressVectorsCol+"/vector-search", tu.Token, map[string]any{
					"field":  "embedding",
					"vector": qVecs[rand.IntN(len(qVecs))],
					"k":      10,
				}, nil)
				if err != nil {
					return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
				}
				if status != http.StatusOK {
					return reqOutcome{statusCode: status, err: fmt.Errorf("vector-search status %d: %s", status, truncate(string(raw), 100))}
				}
				var vr struct {
					Items []struct {
						ID       string  `json:"id"`
						TenantID string  `json:"tenant_id"`
						Score    float64 `json:"score"`
					} `json:"items"`
				}
				if err := json.Unmarshal(raw, &vr); err != nil {
					return reqOutcome{statusCode: status, integrityError: true, err: err}
				}
				for _, item := range vr.Items {
					if item.TenantID != tu.TenantID {
						return reqOutcome{statusCode: status, authViolation: true, err: fmt.Errorf("VECTOR CROSS-TENANT LEAK: caller=%s got=%s", tu.TenantID, item.TenantID)}
					}
				}
				if len(vr.Items) < 10 {
					underfilled.Add(1)
				}
				return reqOutcome{statusCode: status}
			})
			pgAfter, _ := pg.snapshot(ctx)
			resVec := &StageResult{
				Timestamp:        time.Now().UTC().Format(time.RFC3339),
				TestID:           fmt.Sprintf("F-VECTOR-C%d", conc),
				Scenario:         fmt.Sprintf("pgvector-hnsw-tenant-search-384d-c%d", conc),
				Mode:             "closed-loop",
				Nodes:            1,
				Concurrency:      conc,
				Trial:            1,
				WarmupSec:        2,
				EmergencyStopped: stoppedVec,
				StopReason:       reasonVec,
				PGDelta:          pg.diff(ctx, pgBefore, pgAfter),
				OpsAfter:         fetchOpsMetrics(client, baseURL, st.SuperuserToken),
				Extras: map[string]any{
					"vectorCount":      st.InitialVectors,
					"vectorDims":       st.VectorDims,
					"requestedK":       10,
					"underfilledCount": underfilled.Load(),
				},
			}
			recVec.finalize(resVec, elapsedVec)
			emitJSONL(resVec)
		}
	}

	return nil
}
