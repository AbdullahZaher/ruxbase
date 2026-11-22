package main

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// runTestG executes Traffic Spikes (100->1000 VUs, 250->2000 VUs, read-heavy->write-heavy transitions)
// and continuous chaos-probe mode (when an external script injects container restart / PG pause / LISTEN kill / netproxy latency).
func runTestG(ctx context.Context, urls []string, st *FixtureState, pg *pgTelemetry, subMode string, dur time.Duration) error {
	client := newStressHTTPClient(10 * time.Second)
	var writeSeq atomic.Int64

	switch subMode {
	case "spikes":
		// G1: Sudden spike 100 -> 1,000 -> 100 VUs
		runSpike := func(testID string, baseVU, spikeVU int) {
			log.Printf("[Test G] %s spike %d -> %d -> %d VUs", testID, baseVU, spikeVU, baseVU)
			phases := []struct {
				name string
				vus  int
				d    time.Duration
			}{
				{"pre-baseline", baseVU, 8 * time.Second},
				{"spike-burst", spikeVU, 12 * time.Second},
				{"post-recovery", baseVU, 12 * time.Second},
			}
			for _, ph := range phases {
				pgBefore, _ := pg.snapshot(ctx)
				rngs := make([]*rand.Rand, ph.vus)
				for i := range ph.vus {
					rngs[i] = rand.New(rand.NewPCG(uint64(400+i), uint64(ph.vus)))
				}
				rec, elapsed, stopped, reason := runClosedLoop(ctx, loadOpts{
					concurrency:     ph.vus,
					warmup:          0,
					duration:        ph.d,
					emergencyErrPct: 60.0,
				}, func(rctx context.Context, w int, _ bool) reqOutcome {
					target := urls[w%len(urls)]
					tIdx := w % len(st.Tenants)
					if rngs[w].IntN(5) == 0 {
						return authWriteOp(rctx, client, target, st, tIdx, writeSeq.Add(1), nil)
					}
					return authReadOp(rctx, client, target, st, tIdx, rngs[w], true)
				})
				pgAfter, _ := pg.snapshot(ctx)
				res := &StageResult{
					Timestamp:        time.Now().UTC().Format(time.RFC3339),
					TestID:           fmt.Sprintf("%s-%s", testID, ph.name),
					Scenario:         fmt.Sprintf("spike-%dto%d-%s", baseVU, spikeVU, ph.name),
					Mode:             "chaos",
					Nodes:            len(urls),
					Concurrency:      ph.vus,
					Trial:            1,
					EmergencyStopped: stopped,
					StopReason:       reason,
					PGDelta:          pg.diff(ctx, pgBefore, pgAfter),
					OpsAfter:         fetchOpsMetrics(client, urls[0], st.SuperuserToken),
				}
				rec.finalize(res, elapsed)
				emitJSONL(res)
			}
		}

		runSpike("G1-SPIKE-100-1000", 100, 1000)
		time.Sleep(2 * time.Second)
		runSpike("G2-SPIKE-250-2000", 250, 2000)
		time.Sleep(2 * time.Second)

		// G3: Rapid Read-Heavy (90R/10W) -> Write-Heavy (10R/90W) -> Read-Heavy (90R/10W) transition at 150 VUs
		log.Printf("[Test G3] rapid read-heavy -> write-heavy -> read-heavy transition (150 VUs)")
		mixPhases := []struct {
			name     string
			writePct int
		}{
			{"read-heavy-pre", 10},
			{"write-heavy-burst", 90},
			{"read-heavy-post", 10},
		}
		for _, ph := range mixPhases {
			pgBefore, _ := pg.snapshot(ctx)
			rngs := make([]*rand.Rand, 150)
			for i := range 150 {
				rngs[i] = rand.New(rand.NewPCG(uint64(500+i), uint64(ph.writePct)))
			}
			rec, elapsed, stopped, reason := runClosedLoop(ctx, loadOpts{
				concurrency:     150,
				warmup:          0,
				duration:        10 * time.Second,
				emergencyErrPct: 50.0,
			}, func(rctx context.Context, w int, _ bool) reqOutcome {
				target := urls[w%len(urls)]
				tIdx := w % len(st.Tenants)
				if rngs[w].IntN(100) < ph.writePct {
					return authWriteOp(rctx, client, target, st, tIdx, writeSeq.Add(1), nil)
				}
				return authReadOp(rctx, client, target, st, tIdx, rngs[w], true)
			})
			pgAfter, _ := pg.snapshot(ctx)
			res := &StageResult{
				Timestamp:        time.Now().UTC().Format(time.RFC3339),
				TestID:           "G3-" + ph.name,
				Scenario:         fmt.Sprintf("workload-shift-%s", ph.name),
				Mode:             "chaos",
				Nodes:            len(urls),
				Concurrency:      150,
				Trial:            1,
				EmergencyStopped: stopped,
				StopReason:       reason,
				PGDelta:          pg.diff(ctx, pgBefore, pgAfter),
				OpsAfter:         fetchOpsMetrics(client, urls[0], st.SuperuserToken),
				Extras:           map[string]any{"writePct": ph.writePct},
			}
			rec.finalize(res, elapsed)
			emitJSONL(res)
		}

	case "chaos-probe":
		// Continuous 1-second windowed telemetry probe while external chaos script injects faults
		// (node restart, LISTEN kill, PG pause/unpause, netproxy latency).
		log.Printf("[Test G chaos-probe] running continuous 1s-window probe for %s across %d url(s)", dur, len(urls))
		type secBucket struct {
			Second      int     `json:"sec"`
			RPS         float64 `json:"rps"`
			Errors      int64   `json:"errors"`
			Timeouts    int64   `json:"timeouts"`
			P95Ms       float64 `json:"p95ms"`
			ReadyStatus int     `json:"readyStatus"`
		}
		var buckets []secBucket
		var curOK, curErr, curTO atomic.Int64
		var latMu sync.Mutex
		var curLats []time.Duration
		var totalOK, totalErr, totalAuthViol atomic.Int64

		pCtx, pCancel := context.WithTimeout(ctx, dur)
		defer pCancel()

		var wg sync.WaitGroup
		const probeConns = 40
		for w := range probeConns {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				rng := rand.New(rand.NewPCG(uint64(600+id), 7))
				probeClient := newStressHTTPClient(3 * time.Second)
				for pCtx.Err() == nil {
					target := urls[id%len(urls)]
					tIdx := id % len(st.Tenants)
					t0 := time.Now()
					var out reqOutcome
					if rng.IntN(5) == 0 {
						out = authWriteOp(pCtx, probeClient, target, st, tIdx, writeSeq.Add(1), nil)
					} else {
						out = authReadOp(pCtx, probeClient, target, st, tIdx, rng, true)
					}
					d := time.Since(t0)
					if out.authViolation {
						totalAuthViol.Add(1)
					}
					if out.err == nil && !out.authViolation && !out.integrityError {
						curOK.Add(1)
						totalOK.Add(1)
						latMu.Lock()
						curLats = append(curLats, d)
						latMu.Unlock()
					} else {
						if pCtx.Err() != nil {
							return
						}
						curErr.Add(1)
						totalErr.Add(1)
						if out.timeout {
							curTO.Add(1)
						}
					}
				}
			}(w)
		}

		readyClient := newStressHTTPClient(2 * time.Second)
		tk := time.NewTicker(1 * time.Second)
		sec := 0
	loop:
		for {
			select {
			case <-pCtx.Done():
				break loop
			case <-tk.C:
				sec++
				ok := curOK.Swap(0)
				er := curErr.Swap(0)
				to := curTO.Swap(0)
				latMu.Lock()
				lats := curLats
				curLats = nil
				latMu.Unlock()
				tmpRec := newMetricsRecorder(len(lats) + 1)
				for _, l := range lats {
					tmpRec.record(l, reqOutcome{statusCode: 200})
				}
				tmpRes := &StageResult{}
				tmpRec.finalize(tmpRes, time.Second)
				rStatus, _, _ := doJSONCtx(ctx, readyClient, http.MethodGet, urls[0]+"/api/health?ready", "", nil, nil)
				buckets = append(buckets, secBucket{
					Second:      sec,
					RPS:         float64(ok),
					Errors:      er,
					Timeouts:    to,
					P95Ms:       tmpRes.P95Ms,
					ReadyStatus: rStatus,
				})
			}
		}
		tk.Stop()
		wg.Wait()

		emitJSONL(&StageResult{
			Timestamp:       time.Now().UTC().Format(time.RFC3339),
			TestID:          "G-CHAOS-PROBE",
			Scenario:        "chaos-timeline-probe",
			Mode:            "chaos",
			Nodes:           len(urls),
			Concurrency:     probeConns,
			DurationSec:     dur.Seconds(),
			TotalRequests:   totalOK.Load() + totalErr.Load(),
			SuccessRequests: totalOK.Load(),
			Errors:          totalErr.Load(),
			AuthViolations:  totalAuthViol.Load(),
			AchievedRPS:     round2(float64(totalOK.Load()) / max(dur.Seconds(), 1)),
			Extras: map[string]any{
				"timeline": buckets,
			},
		})
	}
	return nil
}
