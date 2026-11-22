package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type stressRecord struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	N        int    `json:"n"`
}

type stressListResp struct {
	Items []stressRecord `json:"items"`
}

// authReadOp performs an authenticated RLS read (50% PK view, 50% filtered sorted list)
// and verifies that EVERY returned record belongs strictly to the caller's tenant.
func authReadOp(ctx context.Context, client *http.Client, baseURL string, st *FixtureState, tIdx int, rng *rand.Rand, skipTotal bool) reqOutcome {
	tu := st.Tenants[tIdx]
	ids := st.ItemIDsByTen[tIdx]
	if rng.IntN(2) == 0 && len(ids) > 0 {
		id := ids[rng.IntN(len(ids))]
		status, raw, err := doJSONCtx(ctx, client, http.MethodGet, baseURL+"/api/collections/"+stressItemsCol+"/records/"+id+"?fields=id,tenant_id,n,title", tu.Token, nil, nil)
		if err != nil {
			return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
		}
		if status != http.StatusOK {
			return reqOutcome{statusCode: status, err: fmt.Errorf("view status %d", status)}
		}
		var rec stressRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return reqOutcome{statusCode: status, err: err, integrityError: true}
		}
		if rec.TenantID != tu.TenantID || rec.ID != id {
			return reqOutcome{statusCode: status, err: fmt.Errorf("CROSS-TENANT LEAK: caller=%s got tenant=%s id=%s", tu.TenantID, rec.TenantID, rec.ID), authViolation: true}
		}
		return reqOutcome{statusCode: status}
	}

	q := url.Values{}
	q.Set("perPage", "20")
	q.Set("sort", "-n")
	q.Set("fields", "id,tenant_id,n,title")
	if skipTotal {
		q.Set("skipTotal", "1")
	}
	threshold := rng.IntN(max(st.InitialItems, 1000)) + 100
	q.Set("filter", fmt.Sprintf("n < %d && published = true", threshold))
	status, raw, err := doJSONCtx(ctx, client, http.MethodGet, baseURL+"/api/collections/"+stressItemsCol+"/records?"+q.Encode(), tu.Token, nil, nil)
	if err != nil {
		return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
	}
	if status != http.StatusOK {
		return reqOutcome{statusCode: status, err: fmt.Errorf("list status %d", status)}
	}
	var lr stressListResp
	if err := json.Unmarshal(raw, &lr); err != nil {
		return reqOutcome{statusCode: status, err: err, integrityError: true}
	}
	for _, item := range lr.Items {
		if item.TenantID != tu.TenantID {
			return reqOutcome{statusCode: status, err: fmt.Errorf("CROSS-TENANT LEAK in list: caller=%s got tenant=%s id=%s", tu.TenantID, item.TenantID, item.ID), authViolation: true}
		}
	}
	return reqOutcome{statusCode: status}
}

func authWriteOp(ctx context.Context, client *http.Client, baseURL string, st *FixtureState, tIdx int, seq int64, createdCounter *atomic.Int64) reqOutcome {
	tu := st.Tenants[tIdx]
	payload := map[string]any{
		"tenant_id":     tu.TenantID,
		"owner":         tu.UserID,
		"category":      fmt.Sprintf("cat_%d", seq%10),
		"n":             st.InitialItems + int(seq),
		"amount":        float64(seq%500) + 2.25,
		"published":     seq%2 == 0,
		"title":         fmt.Sprintf("Write #%d", seq),
		"body":          "stress write payload body for transaction durability verification",
		"unindexed_tag": fmt.Sprintf("tag_%d", seq%500),
	}
	status, raw, err := doJSONCtx(ctx, client, http.MethodPost, baseURL+"/api/collections/"+stressItemsCol+"/records", tu.Token, payload, nil)
	if err != nil {
		return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
	}
	if status != http.StatusOK {
		return reqOutcome{statusCode: status, err: fmt.Errorf("create status %d: %s", status, truncate(string(raw), 120))}
	}
	var rec stressRecord
	if err := json.Unmarshal(raw, &rec); err != nil || rec.TenantID != tu.TenantID {
		return reqOutcome{statusCode: status, err: fmt.Errorf("create tenant mismatch: want=%s got=%s", tu.TenantID, rec.TenantID), authViolation: true}
	}
	if createdCounter != nil {
		createdCounter.Add(1)
	}
	return reqOutcome{statusCode: status}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// runTestA executes Progressive Load Testing across A1..A8 (closed-loop) plus open-loop arrival-rate stages.
func runTestA(ctx context.Context, urls []string, st *FixtureState, pg *pgTelemetry, rlsStrict bool, poolMax int, stagesCSV, openRatesCSV string, warmup, dur time.Duration, repeatStages map[int]int) error {
	client := newStressHTTPClient(10 * time.Second)
	var writeSeq atomic.Int64
	var committedCreates atomic.Int64

	// 1. Closed-loop progressive concurrency stages (A1..A8)
	stages := parseIntCSV(stagesCSV, []int{10, 50, 100, 250, 500, 1000, 2000, 5000})
	for idx, vus := range stages {
		trials := 1
		if r, ok := repeatStages[vus]; ok && r > 1 {
			trials = r
		}
		stageLabel := fmt.Sprintf("A%d", idx+1)
		stoppedHighErr := false

		for trial := 1; trial <= trials; trial++ {
			log.Printf("[Test A] stage=%s vus=%d trial=%d/%d rlsStrict=%v poolMax=%d", stageLabel, vus, trial, trials, rlsStrict, poolMax)
			pgBefore, _ := pg.snapshot(ctx)

			rngs := make([]*rand.Rand, vus)
			for i := range vus {
				rngs[i] = rand.New(rand.NewPCG(uint64(100+trial), uint64(i+1)))
			}

			rec, elapsed, stopped, stopReason := runClosedLoop(ctx, loadOpts{
				concurrency:     vus,
				warmup:          warmup,
				duration:        dur,
				emergencyErrPct: 50.0,
			}, func(rctx context.Context, w int, measuring bool) reqOutcome {
				target := urls[w%len(urls)]
				tIdx := w % len(st.Tenants)
				rng := rngs[w]
				// Realistic 80% authenticated RLS read / 20% authenticated RLS write
				if rng.IntN(5) == 0 {
					var cnt *atomic.Int64
					if measuring {
						cnt = &committedCreates
					}
					return authWriteOp(rctx, client, target, st, tIdx, writeSeq.Add(1), cnt)
				}
				return authReadOp(rctx, client, target, st, tIdx, rng, true)
			})

			pgAfter, _ := pg.snapshot(ctx)
			res := &StageResult{
				Timestamp:        time.Now().UTC().Format(time.RFC3339),
				TestID:           fmt.Sprintf("A-%s", stageLabel),
				Scenario:         fmt.Sprintf("progressive-80r20w-vu%d", vus),
				Mode:             "closed-loop",
				RLSStrict:        rlsStrict,
				PoolMax:          poolMax,
				Nodes:            len(urls),
				Concurrency:      vus,
				Trial:            trial,
				WarmupSec:        warmup.Seconds(),
				EmergencyStopped: stopped,
				StopReason:       stopReason,
				PGDelta:          pg.diff(ctx, pgBefore, pgAfter),
				OpsAfter:         fetchOpsMetrics(client, urls[0], st.SuperuserToken),
			}
			if res.PGDelta != nil {
				res.PGVersion = res.PGDelta.ServerVersion
			}
			rec.finalize(res, elapsed)
			emitJSONL(res)

			if stopped || res.ErrorRatePct >= 50.0 {
				stoppedHighErr = true
				break
			}
			time.Sleep(1 * time.Second)
		}
		if stoppedHighErr && vus >= 1000 {
			log.Printf("[Test A] stopping escalation after %s (vus=%d) due to emergency stop / >=50%% errors", stageLabel, vus)
			break
		}
	}

	// 2. Open-loop constant arrival-rate sweep (prevents coordinated omission)
	if openRatesCSV != "" {
		openRates := parseFloatCSV(openRatesCSV, []float64{250, 500, 1000, 2000, 3500, 5000})
		for idx, rate := range openRates {
			log.Printf("[Test A Open-Loop] rate=%.0f req/s rlsStrict=%v poolMax=%d", rate, rlsStrict, poolMax)
			pgBefore, _ := pg.snapshot(ctx)
			rec, elapsed, stopped, stopReason := runOpenLoop(ctx, rate, 4000, 3*time.Second, dur, 35.0, func(rctx context.Context, seq int64) reqOutcome {
				target := urls[int(seq)%len(urls)]
				tIdx := int(seq) % len(st.Tenants)
				rng := rand.New(rand.NewPCG(uint64(seq+1), 42))
				if seq%5 == 0 {
					return authWriteOp(rctx, client, target, st, tIdx, writeSeq.Add(1), &committedCreates)
				}
				return authReadOp(rctx, client, target, st, tIdx, rng, true)
			})
			pgAfter, _ := pg.snapshot(ctx)
			res := &StageResult{
				Timestamp:        time.Now().UTC().Format(time.RFC3339),
				TestID:           fmt.Sprintf("A-OL-%d", idx+1),
				Scenario:         fmt.Sprintf("openloop-80r20w-rps%.0f", rate),
				Mode:             "open-loop",
				RLSStrict:        rlsStrict,
				PoolMax:          poolMax,
				Nodes:            len(urls),
				OfferedRPS:       rate,
				Trial:            1,
				WarmupSec:        3,
				EmergencyStopped: stopped,
				StopReason:       stopReason,
				PGDelta:          pg.diff(ctx, pgBefore, pgAfter),
				OpsAfter:         fetchOpsMetrics(client, urls[0], st.SuperuserToken),
			}
			if res.PGDelta != nil {
				res.PGVersion = res.PGDelta.ServerVersion
			}
			rec.finalize(res, elapsed)
			emitJSONL(res)
			if stopped || res.ErrorRatePct >= 30.0 || res.P99Ms > 5000 {
				log.Printf("[Test A Open-Loop] breaking saturation reached at offered=%.0f req/s (err=%.2f%%, p99=%.1fms)", rate, res.ErrorRatePct, res.P99Ms)
				break
			}
			time.Sleep(1500 * time.Millisecond)
		}
	}

	log.Printf("[Test A] completed; tracked committed creates=%d", committedCreates.Load())
	return nil
}

// runTestB escalates offered rate and concurrency until provisional SLOs break or saturation hits,
// then removes load and measures recovery back to healthy baseline.
func runTestB(ctx context.Context, urls []string, st *FixtureState, pg *pgTelemetry, rlsStrict bool, poolMax int, stepDur time.Duration) error {
	client := newStressHTTPClient(8 * time.Second)
	var writeSeq atomic.Int64

	// Baseline single-probe latency before overload
	baselineP95 := probeRecoveryLatency(ctx, client, urls[0], st, 30)
	log.Printf("[Test B] pre-overload baseline p95=%.2fms", baselineP95)

	// Open-loop ramp: 200 -> 500 -> 1000 -> 1500 -> 2000 -> 3000 -> 4500 -> 6000 -> 8000 req/s
	// For strict RLS (which incurs heavy pgcrypto HMAC & PL/pgSQL auth checks per txn), scale proportionately
	rates := []float64{200, 500, 1000, 1500, 2000, 3000, 4500, 6000, 8000}
	var peakRPS float64
	var maxSustainedRPS float64
	sloP95 := 100.0
	if rlsStrict {
		rates = []float64{5, 10, 15, 25, 40, 75, 120}
		sloP95 = 200.0
	}
	firstDegradationRate := 0.0
	breakingRate := 0.0

	for i, rate := range rates {
		log.Printf("[Test B] escalating open-loop step %d: offered=%.0f req/s", i+1, rate)
		pgBefore, _ := pg.snapshot(ctx)
		rec, elapsed, stopped, stopReason := runOpenLoop(ctx, rate, 4000, 2*time.Second, stepDur, 25.0, func(rctx context.Context, seq int64) reqOutcome {
			target := urls[int(seq)%len(urls)]
			tIdx := int(seq) % len(st.Tenants)
			rng := rand.New(rand.NewPCG(uint64(seq+77), 99))
			if seq%5 == 0 {
				return authWriteOp(rctx, client, target, st, tIdx, writeSeq.Add(1), nil)
			}
			return authReadOp(rctx, client, target, st, tIdx, rng, true)
		})
		pgAfter, _ := pg.snapshot(ctx)
		res := &StageResult{
			Timestamp:        time.Now().UTC().Format(time.RFC3339),
			TestID:           fmt.Sprintf("B-STEP-%d", i+1),
			Scenario:         fmt.Sprintf("breaking-point-rps%.0f", rate),
			Mode:             "open-loop",
			RLSStrict:        rlsStrict,
			PoolMax:          poolMax,
			Nodes:            len(urls),
			OfferedRPS:       rate,
			Trial:            1,
			WarmupSec:        2,
			EmergencyStopped: stopped,
			StopReason:       stopReason,
			PGDelta:          pg.diff(ctx, pgBefore, pgAfter),
			OpsAfter:         fetchOpsMetrics(client, urls[0], st.SuperuserToken),
		}
		if res.PGDelta != nil {
			res.PGVersion = res.PGDelta.ServerVersion
		}
		rec.finalize(res, elapsed)
		if res.AchievedRPS > peakRPS {
			peakRPS = res.AchievedRPS
		}
		withinSLO := res.ErrorRatePct < 0.5 && res.P95Ms <= sloP95 && !stopped && res.AchievedRPS >= rate*0.92
		if withinSLO && res.AchievedRPS > maxSustainedRPS {
			maxSustainedRPS = res.AchievedRPS
		}
		if !withinSLO && firstDegradationRate == 0 {
			firstDegradationRate = rate
		}
		emitJSONL(res)

		// Stop safely once hard saturation/SLO break is clearly demonstrated
		if stopped || res.ErrorRatePct >= 10.0 || res.P99Ms > 3000 || res.AchievedRPS < rate*0.65 {
			breakingRate = rate
			log.Printf("[Test B] breaking point identified at offered=%.0f req/s (achieved=%.1f, p95=%.1fms, p99=%.1fms, err=%.2f%%)", rate, res.AchievedRPS, res.P95Ms, res.P99Ms, res.ErrorRatePct)
			break
		}
	}

	// Measure recovery after load removal!
	recoveryStart := time.Now()
	recoverySamples := []map[string]any{}
	recoveredAtSec := -1.0
	for tick := 0; tick < 30; tick++ {
		elapsed := time.Since(recoveryStart).Seconds()
		p95 := probeRecoveryLatency(ctx, client, urls[0], st, 15)
		readyStatus, _, _ := doJSONCtx(ctx, client, http.MethodGet, urls[0]+"/api/health?ready", "", nil, nil)
		ops := fetchOpsMetrics(client, urls[0], st.SuperuserToken)
		sample := map[string]any{
			"sec":         round2(elapsed),
			"probeP95Ms":  round3(p95),
			"readyStatus": readyStatus,
		}
		if ops != nil {
			sample["poolInUse"] = ops.DBPool.InUse
			sample["goroutines"] = ops.Runtime.Goroutines
			sample["activeBackends"] = ops.Database.ActiveBackends
		}
		recoverySamples = append(recoverySamples, sample)
		if readyStatus == http.StatusOK && p95 <= max(baselineP95*2.0, 25.0) && recoveredAtSec < 0 {
			recoveredAtSec = round2(elapsed)
			if tick >= 5 {
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	emitJSONL(&StageResult{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TestID:    "B-SUMMARY",
		Scenario:  "breaking-point-and-recovery-summary",
		Mode:      "open-loop",
		RLSStrict: rlsStrict,
		PoolMax:   poolMax,
		Nodes:     len(urls),
		Extras: map[string]any{
			"provisionalSloP95Ms":  sloP95,
			"baselineP95Ms":        round3(baselineP95),
			"maxSustainableRps":    round2(maxSustainedRPS),
			"peakObservedRps":      round2(peakRPS),
			"firstDegradationRate": firstDegradationRate,
			"breakingOfferedRate":  breakingRate,
			"recoverySeconds":      recoveredAtSec,
			"recoverySamples":      recoverySamples,
		},
	})
	return nil
}

func probeRecoveryLatency(ctx context.Context, client *http.Client, baseURL string, st *FixtureState, n int) float64 {
	rec := newMetricsRecorder(n)
	rng := rand.New(rand.NewPCG(7, 11))
	for i := range n {
		t0 := time.Now()
		out := authReadOp(ctx, client, baseURL, st, i%len(st.Tenants), rng, true)
		rec.record(time.Since(t0), out)
	}
	tmp := &StageResult{}
	rec.finalize(tmp, time.Second)
	if tmp.SuccessRequests == 0 {
		return 9999.0
	}
	return tmp.P95Ms
}

func parseIntCSV(s string, def []int) []int {
	if strings.TrimSpace(s) == "" {
		return def
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if v, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && v > 0 {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

func parseFloatCSV(s string, def []float64) []float64 {
	if strings.TrimSpace(s) == "" {
		return def
	}
	parts := strings.Split(s, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		if v, err := strconv.ParseFloat(strings.TrimSpace(p), 64); err == nil && v > 0 {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}
