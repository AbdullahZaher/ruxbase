package main

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

type enduranceSample struct {
	ElapsedSec     float64 `json:"elapsedSec"`
	WindowRPS      float64 `json:"windowRps"`
	WindowErrors   int64   `json:"windowErrors"`
	WindowP95Ms    float64 `json:"windowP95ms"`
	WindowP99Ms    float64 `json:"windowP99ms"`
	HeapAllocBytes uint64  `json:"heapAllocBytes"`
	HeapSysBytes   uint64  `json:"heapSysBytes"`
	NumGC          uint32  `json:"numGC"`
	Goroutines     int     `json:"goroutines"`
	PoolOpen       int     `json:"poolOpen"`
	PoolInUse      int     `json:"poolInUse"`
	PoolIdle       int     `json:"poolIdle"`
	PoolWaitCount  int64   `json:"poolWaitCount"`
	PoolWaitMs     int64   `json:"poolWaitMs"`
	PGActiveBack   int     `json:"pgActiveBackends"`
	PGWALBytesCum  int64   `json:"pgWalBytesCum"`
	PGDeadTuples   int64   `json:"pgDeadTuples"`
	AlertsCount    int     `json:"alertsCount"`
}

// runTestH executes a sustained continuous mixed workload (authenticated RLS reads, writes,
// SSE subscribers, and metrics scraping) while sampling Go runtime heap, goroutines, GC,
// DB pool stats, PG WAL growth, and throughput/latency drift every 5 seconds.
func runTestH(ctx context.Context, baseURL string, st *FixtureState, pg *pgTelemetry, concurrency int, dur time.Duration, sampleInterval time.Duration) error {
	if sampleInterval <= 0 {
		sampleInterval = 5 * time.Second
	}
	log.Printf("[Test H] starting sustained endurance run: concurrency=%d duration=%s sampleInterval=%s", concurrency, dur, sampleInterval)
	client := newStressHTTPClient(10 * time.Second)
	var writeSeq atomic.Int64
	var committedCreates atomic.Int64
	var totalOK, totalErr, totalAuthViol atomic.Int64

	// Keep 20 live SSE subscribers open throughout the endurance run to detect SSE goroutine/memory leaks
	sseConns := make([]*sseConn, 0, 20)
	sseDone := make(chan struct{})
	var sseWg sync.WaitGroup
	for i := range 20 {
		tu := st.Tenants[i%len(st.Tenants)]
		c, err := openSSE(ctx, client, baseURL, tu.Token, stressItemsCol, tu.TenantID, "")
		if err == nil {
			sseConns = append(sseConns, c)
			sseWg.Add(1)
			go func(sc *sseConn) {
				defer sseWg.Done()
				sc.listen(sseDone, "")
			}(c)
		}
	}
	log.Printf("[Test H] attached %d persistent authenticated SSE subscribers", len(sseConns))

	pgStart, _ := pg.snapshot(ctx)
	start := time.Now()
	eCtx, eCancel := context.WithTimeout(ctx, dur)
	defer eCancel()

	var winOK, winErr atomic.Int64
	var winLatMu sync.Mutex
	var winLats []time.Duration

	var wg sync.WaitGroup
	for w := range concurrency {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(uint64(800+id), 13))
			for eCtx.Err() == nil {
				tIdx := id % len(st.Tenants)
				t0 := time.Now()
				var out reqOutcome
				if rng.IntN(5) == 0 {
					out = authWriteOp(eCtx, client, baseURL, st, tIdx, writeSeq.Add(1), &committedCreates)
				} else {
					out = authReadOp(eCtx, client, baseURL, st, tIdx, rng, true)
				}
				d := time.Since(t0)
				if out.authViolation {
					totalAuthViol.Add(1)
				}
				if out.err == nil && !out.authViolation && !out.integrityError {
					winOK.Add(1)
					totalOK.Add(1)
					winLatMu.Lock()
					winLats = append(winLats, d)
					winLatMu.Unlock()
				} else {
					if eCtx.Err() != nil {
						return
					}
					winErr.Add(1)
					totalErr.Add(1)
				}
			}
		}(w)
	}

	samples := make([]enduranceSample, 0, int(dur/sampleInterval)+2)
	tk := time.NewTicker(sampleInterval)
loop:
	for {
		select {
		case <-eCtx.Done():
			break loop
		case <-tk.C:
			ok := winOK.Swap(0)
			er := winErr.Swap(0)
			winLatMu.Lock()
			lats := winLats
			winLats = nil
			winLatMu.Unlock()
			tmpRec := newMetricsRecorder(len(lats) + 1)
			for _, l := range lats {
				tmpRec.record(l, reqOutcome{statusCode: 200})
			}
			tmpRes := &StageResult{}
			tmpRec.finalize(tmpRes, sampleInterval)

			ops := fetchOpsMetrics(client, baseURL, st.SuperuserToken)
			alerts := fetchOpsJSON(client, baseURL, "/api/ops/alerts", st.SuperuserToken)
			alertCount := 0
			if bm, ok := alerts["body"].(map[string]any); ok {
				if cnt, ok := bm["count"].(float64); ok {
					alertCount = int(cnt)
				}
			}

			var walCum, deadTup int64
			if pg != nil && pgStart != nil && pgStart.WALLSN != "" {
				_ = pg.pool.QueryRow(ctx, "SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), $1::pg_lsn)::bigint", pgStart.WALLSN).Scan(&walCum)
				_ = pg.pool.QueryRow(ctx, "SELECT COALESCE(n_dead_tup, 0) FROM pg_stat_user_tables WHERE relname = 'stress_items'").Scan(&deadTup)
			}

			s := enduranceSample{
				ElapsedSec:    round2(time.Since(start).Seconds()),
				WindowRPS:     round2(float64(ok) / sampleInterval.Seconds()),
				WindowErrors:  er,
				WindowP95Ms:   tmpRes.P95Ms,
				WindowP99Ms:   tmpRes.P99Ms,
				PGWALBytesCum: walCum,
				PGDeadTuples:  deadTup,
				AlertsCount:   alertCount,
			}
			if ops != nil {
				s.HeapAllocBytes = ops.Runtime.HeapAllocBytes
				s.HeapSysBytes = ops.Runtime.HeapSysBytes
				s.NumGC = ops.Runtime.NumGC
				s.Goroutines = ops.Runtime.Goroutines
				s.PoolOpen = ops.DBPool.OpenConnections
				s.PoolInUse = ops.DBPool.InUse
				s.PoolIdle = ops.DBPool.Idle
				s.PoolWaitCount = ops.DBPool.WaitCount
				s.PoolWaitMs = ops.DBPool.WaitDurationMs
				s.PGActiveBack = ops.Database.ActiveBackends
			}
			samples = append(samples, s)
			log.Printf("[Test H] t=%.0fs rps=%.1f p95=%.1fms err=%d heap=%.1fMiB goroutines=%d poolWait=%d wal=%.1fMiB",
				s.ElapsedSec, s.WindowRPS, s.WindowP95Ms, s.WindowErrors,
				float64(s.HeapAllocBytes)/(1024*1024), s.Goroutines, s.PoolWaitCount, float64(s.PGWALBytesCum)/(1024*1024))
		}
	}
	tk.Stop()
	wg.Wait()

	// Tear down SSE connections and check post-drain goroutines
	close(sseDone)
	var sseLeaks int64
	for _, c := range sseConns {
		sseLeaks += c.leaks.Load()
		_ = c.res.Body.Close()
	}
	sseWg.Wait()
	time.Sleep(2 * time.Second)
	opsPostDrain := fetchOpsMetrics(client, baseURL, st.SuperuserToken)
	pgEnd, _ := pg.snapshot(ctx)

	// Linear regression slopes over steady-state samples (skipping first 2 warmup samples if >=6)
	startIdx := 0
	if len(samples) >= 6 {
		startIdx = 2
	}
	steady := samples[startIdx:]
	heapSlopeMBPerMin := linearSlope(steady, func(s enduranceSample) float64 { return s.ElapsedSec / 60.0 }, func(s enduranceSample) float64 { return float64(s.HeapAllocBytes) / (1024 * 1024) })
	goroSlopePerMin := linearSlope(steady, func(s enduranceSample) float64 { return s.ElapsedSec / 60.0 }, func(s enduranceSample) float64 { return float64(s.Goroutines) })
	rpsSlopePerMin := linearSlope(steady, func(s enduranceSample) float64 { return s.ElapsedSec / 60.0 }, func(s enduranceSample) float64 { return s.WindowRPS })
	p95SlopeMsPerMin := linearSlope(steady, func(s enduranceSample) float64 { return s.ElapsedSec / 60.0 }, func(s enduranceSample) float64 { return s.WindowP95Ms })

	elapsed := time.Since(start)
	res := &StageResult{
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		TestID:          "H-ENDURANCE",
		Scenario:        fmt.Sprintf("endurance-mixed-c%d-dur%.0fs", concurrency, dur.Seconds()),
		Mode:            "endurance",
		Nodes:           1,
		Concurrency:     concurrency,
		Trial:           1,
		DurationSec:     round2(elapsed.Seconds()),
		TotalRequests:   totalOK.Load() + totalErr.Load(),
		SuccessRequests: totalOK.Load(),
		Errors:          totalErr.Load(),
		AuthViolations:  totalAuthViol.Load() + sseLeaks,
		AchievedRPS:     round2(float64(totalOK.Load()) / max(elapsed.Seconds(), 1)),
		PGDelta:         pg.diff(ctx, pgStart, pgEnd),
		OpsAfter:        opsPostDrain,
		Extras: map[string]any{
			"committedCreates":     committedCreates.Load(),
			"persistentSseClients": len(sseConns),
			"sampleCount":          len(samples),
			"heapSlopeMiBPerMin":   round3(heapSlopeMBPerMin),
			"goroutineSlopePerMin": round3(goroSlopePerMin),
			"rpsSlopePerMin":       round3(rpsSlopePerMin),
			"p95SlopeMsPerMin":     round3(p95SlopeMsPerMin),
			"postDrainGoroutines":  safeGoroutines(opsPostDrain),
			"postDrainHeapMiB":     safeHeapMiB(opsPostDrain),
			"noteW1105":            "Bounded local endurance run; does NOT substitute for mandatory W11-05 168-hour staging soak.",
			"samples":              samples,
		},
	}
	if res.TotalRequests > 0 {
		res.ErrorRatePct = round3(float64(res.Errors) * 100.0 / float64(res.TotalRequests))
	}
	emitJSONL(res)
	return nil
}

func safeGoroutines(o *OpsSnap) int {
	if o == nil {
		return -1
	}
	return o.Runtime.Goroutines
}

func safeHeapMiB(o *OpsSnap) float64 {
	if o == nil {
		return -1
	}
	return round3(float64(o.Runtime.HeapAllocBytes) / (1024 * 1024))
}

func linearSlope[T any](items []T, xFn, yFn func(T) float64) float64 {
	n := float64(len(items))
	if n < 2 {
		return 0
	}
	var sumX, sumY, sumXY, sumXX float64
	for _, it := range items {
		x := xFn(it)
		y := yFn(it)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}
