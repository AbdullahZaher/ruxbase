package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// StageResult captures complete, machine-readable telemetry for a single test stage.
type StageResult struct {
	Timestamp        string         `json:"timestamp"`
	TestID           string         `json:"testId"`
	Scenario         string         `json:"scenario"`
	Mode             string         `json:"mode"` // closed-loop, open-loop, chaos, pg, realtime, endurance
	PGVersion        string         `json:"pgVersion,omitempty"`
	RLSStrict        bool           `json:"rlsStrict"`
	PoolMax          int            `json:"poolMax,omitempty"`
	Nodes            int            `json:"nodes"`
	Concurrency      int            `json:"concurrency"`
	OfferedRPS       float64        `json:"offeredRps,omitempty"`
	Trial            int            `json:"trial"`
	WarmupSec        float64        `json:"warmupSeconds"`
	DurationSec      float64        `json:"durationSeconds"`
	TotalRequests    int64          `json:"totalRequests"`
	SuccessRequests  int64          `json:"successRequests"`
	Errors           int64          `json:"errors"`
	Timeouts         int64          `json:"timeouts"`
	AuthViolations   int64          `json:"authViolations"`
	IntegrityErrors  int64          `json:"integrityErrors"`
	ErrorRatePct     float64        `json:"errorRatePct"`
	AchievedRPS      float64        `json:"achievedRps"`
	P50Ms            float64        `json:"p50ms"`
	P95Ms            float64        `json:"p95ms"`
	P99Ms            float64        `json:"p99ms"`
	MaxMs            float64        `json:"maxMs"`
	StatusCodes      map[int]int64  `json:"statusCodes,omitempty"`
	ErrorSamples     []string       `json:"errorSamples,omitempty"`
	EmergencyStopped bool           `json:"emergencyStopped,omitempty"`
	StopReason       string         `json:"stopReason,omitempty"`
	PGDelta          *PGDeltaStats  `json:"pgDelta,omitempty"`
	OpsAfter         *OpsSnap       `json:"opsAfter,omitempty"`
	Extras           map[string]any `json:"extras,omitempty"`
}

func newStressHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	tr := &http.Transport{
		DialContext:         dialer.DialContext,
		MaxIdleConns:        10000,
		MaxIdleConnsPerHost: 10000,
		MaxConnsPerHost:     0,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
		ForceAttemptHTTP2:   false,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func doJSON(client *http.Client, method, target, token string, body any) (int, []byte, error) {
	return doJSONCtx(context.Background(), client, method, target, token, body, nil)
}

func doJSONCtx(ctx context.Context, client *http.Client, method, target, token string, body any, extraHeaders map[string]string) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

// reqOutcome returned by a single workload iteration.
type reqOutcome struct {
	statusCode     int
	err            error
	timeout        bool
	authViolation  bool
	integrityError bool
}

type metricsRecorder struct {
	mu           sync.Mutex
	latencies    []time.Duration
	maxSamples   int
	statusCodes  map[int]int64
	errorSamples []string
	sampleSeen   uint64

	total       atomic.Int64
	success     atomic.Int64
	errs        atomic.Int64
	timeouts    atomic.Int64
	authViol    atomic.Int64
	integrityEr atomic.Int64
}

func newMetricsRecorder(maxSamples int) *metricsRecorder {
	if maxSamples <= 0 {
		maxSamples = 500000
	}
	return &metricsRecorder{
		latencies:   make([]time.Duration, 0, min(maxSamples, 65536)),
		maxSamples:  maxSamples,
		statusCodes: make(map[int]int64, 8),
	}
}

func (m *metricsRecorder) record(d time.Duration, out reqOutcome) {
	m.total.Add(1)
	if out.authViolation {
		m.authViol.Add(1)
	}
	if out.integrityError {
		m.integrityEr.Add(1)
	}
	if out.timeout {
		m.timeouts.Add(1)
	}
	ok := out.err == nil && !out.authViolation && !out.integrityError
	if ok {
		m.success.Add(1)
	} else {
		m.errs.Add(1)
	}

	m.mu.Lock()
	if out.statusCode > 0 {
		m.statusCodes[out.statusCode]++
	}
	if !ok && out.err != nil && len(m.errorSamples) < 10 {
		m.errorSamples = append(m.errorSamples, out.err.Error())
	}
	if ok {
		m.sampleSeen++
		if len(m.latencies) < m.maxSamples {
			m.latencies = append(m.latencies, d)
		} else {
			// Deterministic stride replacement (reservoir-style) to stay bounded in RAM
			idx := int(m.sampleSeen % uint64(m.maxSamples))
			m.latencies[idx] = d
		}
	}
	m.mu.Unlock()
}

func (m *metricsRecorder) finalize(res *StageResult, elapsed time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	res.DurationSec = round3(elapsed.Seconds())
	res.TotalRequests = m.total.Load()
	res.SuccessRequests = m.success.Load()
	res.Errors = m.errs.Load()
	res.Timeouts = m.timeouts.Load()
	res.AuthViolations = m.authViol.Load()
	res.IntegrityErrors = m.integrityEr.Load()
	if res.TotalRequests > 0 {
		res.ErrorRatePct = round3(float64(res.Errors) * 100.0 / float64(res.TotalRequests))
	}
	if elapsed > 0 {
		res.AchievedRPS = round2(float64(res.SuccessRequests) / elapsed.Seconds())
	}
	res.StatusCodes = m.statusCodes
	res.ErrorSamples = m.errorSamples

	if len(m.latencies) > 0 {
		slices.Sort(m.latencies)
		res.P50Ms = percentileMs(m.latencies, 50)
		res.P95Ms = percentileMs(m.latencies, 95)
		res.P99Ms = percentileMs(m.latencies, 99)
		res.MaxMs = round3(float64(m.latencies[len(m.latencies)-1].Microseconds()) / 1000.0)
	}
}

func percentileMs(sorted []time.Duration, p float64) float64 {
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
	return round3(float64(sorted[idx].Microseconds()) / 1000.0)
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if err == context.DeadlineExceeded {
		return true
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	return false
}

type loadOpts struct {
	concurrency       int
	warmup            time.Duration
	duration          time.Duration
	emergencyErrPct   float64 // 0 disables
	emergencyMinReqs  int64
	emergencyCheckSec float64
}

// runClosedLoop runs a warmup period followed by a measured closed-loop concurrency stage.
func runClosedLoop(ctx context.Context, opts loadOpts, fn func(ctx context.Context, worker int, measuring bool) reqOutcome) (*metricsRecorder, time.Duration, bool, string) {
	if opts.emergencyMinReqs == 0 {
		opts.emergencyMinReqs = 200
	}
	if opts.emergencyCheckSec == 0 {
		opts.emergencyCheckSec = 3.0
	}

	// Warmup phase (not recorded in measurement stats)
	if opts.warmup > 0 {
		wCtx, wCancel := context.WithTimeout(ctx, opts.warmup)
		var wg sync.WaitGroup
		warmWorkers := min(opts.concurrency, 250)
		for w := range warmWorkers {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for wCtx.Err() == nil {
					_ = fn(wCtx, id, false)
				}
			}(w)
		}
		wg.Wait()
		wCancel()
	}

	// Measurement phase
	rec := newMetricsRecorder(500000)
	mCtx, mCancel := context.WithTimeout(ctx, opts.duration)
	defer mCancel()

	var emergencyStopped atomic.Bool
	var stopReason atomic.Value

	start := time.Now()

	// Emergency guardrail monitor
	if opts.emergencyErrPct > 0 {
		go func() {
			tk := time.NewTicker(time.Second)
			defer tk.Stop()
			for {
				select {
				case <-mCtx.Done():
					return
				case <-tk.C:
					if time.Since(start).Seconds() < opts.emergencyCheckSec {
						continue
					}
					tot := rec.total.Load()
					errs := rec.errs.Load()
					if tot >= opts.emergencyMinReqs {
						pct := float64(errs) * 100.0 / float64(tot)
						if pct >= opts.emergencyErrPct {
							emergencyStopped.Store(true)
							stopReason.Store(fmt.Sprintf("emergency stop: error rate %.1f%% >= %.1f%% (%d/%d)", pct, opts.emergencyErrPct, errs, tot))
							mCancel()
							return
						}
					}
					if rec.authViol.Load() > 0 {
						emergencyStopped.Store(true)
						stopReason.Store(fmt.Sprintf("emergency stop: %d cross-tenant/auth violation(s) detected", rec.authViol.Load()))
						mCancel()
						return
					}
				}
			}
		}()
	}

	var wg sync.WaitGroup
	for w := range opts.concurrency {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for mCtx.Err() == nil {
				t0 := time.Now()
				out := fn(mCtx, id, true)
				if mCtx.Err() != nil && out.err != nil && !out.authViolation && !out.integrityError {
					// Ignore clean cancellations right at deadline boundary
					if out.err == context.Canceled || mCtx.Err() == context.DeadlineExceeded {
						return
					}
				}
				rec.record(time.Since(t0), out)
			}
		}(w)
	}
	wg.Wait()
	elapsed := time.Since(start)
	reason, _ := stopReason.Load().(string)
	return rec, elapsed, emergencyStopped.Load(), reason
}

// runOpenLoop dispatches requests at a constant target arrival rate (offeredRPS),
// measuring latency from the scheduled arrival timestamp to prevent coordinated omission.
func runOpenLoop(ctx context.Context, offeredRPS float64, maxInflight int, warmup, duration time.Duration, emergencyErrPct float64, fn func(ctx context.Context, seq int64) reqOutcome) (*metricsRecorder, time.Duration, bool, string) {
	if maxInflight <= 0 {
		maxInflight = max(2000, int(offeredRPS*2))
		if maxInflight > 10000 {
			maxInflight = 10000
		}
	}

	// Brief warmup
	if warmup > 0 {
		wCtx, wCancel := context.WithTimeout(ctx, warmup)
		var wg sync.WaitGroup
		warmRate := min(offeredRPS, 500)
		warmInterval := time.Duration(float64(time.Second) / warmRate)
		var seq atomic.Int64
		sem := make(chan struct{}, min(maxInflight, 256))
		startW := time.Now()
		for i := int64(0); wCtx.Err() == nil; i++ {
			target := startW.Add(time.Duration(i) * warmInterval)
			if wait := time.Until(target); wait > 0 {
				select {
				case <-wCtx.Done():
					break
				case <-time.After(wait):
				}
			}
			select {
			case sem <- struct{}{}:
				wg.Add(1)
				s := seq.Add(1)
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					_ = fn(wCtx, s)
				}()
			default:
			}
		}
		wg.Wait()
		wCancel()
	}

	rec := newMetricsRecorder(500000)
	mCtx, mCancel := context.WithTimeout(ctx, duration)
	defer mCancel()

	var emergencyStopped atomic.Bool
	var stopReason atomic.Value

	sem := make(chan struct{}, maxInflight)
	intervalNs := float64(time.Second) / offeredRPS
	start := time.Now()

	if emergencyErrPct > 0 {
		go func() {
			tk := time.NewTicker(time.Second)
			defer tk.Stop()
			for {
				select {
				case <-mCtx.Done():
					return
				case <-tk.C:
					if time.Since(start).Seconds() < 3.0 {
						continue
					}
					tot := rec.total.Load()
					errs := rec.errs.Load()
					if tot >= 200 {
						pct := float64(errs) * 100.0 / float64(tot)
						if pct >= emergencyErrPct {
							emergencyStopped.Store(true)
							stopReason.Store(fmt.Sprintf("emergency stop: error rate %.1f%% >= %.1f%% (%d/%d)", pct, emergencyErrPct, errs, tot))
							mCancel()
							return
						}
					}
					if rec.authViol.Load() > 0 {
						emergencyStopped.Store(true)
						stopReason.Store(fmt.Sprintf("emergency stop: %d cross-tenant/auth violation(s)", rec.authViol.Load()))
						mCancel()
						return
					}
				}
			}
		}()
	}

	var wg sync.WaitGroup
	var droppedDispatch atomic.Int64
	for i := int64(0); ; i++ {
		scheduled := start.Add(time.Duration(float64(i) * intervalNs))
		if time.Since(start) >= duration || mCtx.Err() != nil {
			break
		}
		if wait := time.Until(scheduled); wait > 0 {
			if wait > 500*time.Microsecond {
				select {
				case <-mCtx.Done():
					goto done
				case <-time.After(wait):
				}
			} else {
				// Short sub-ms spin/yield for high-rate pacing accuracy
				for time.Now().Before(scheduled) {
					if mCtx.Err() != nil {
						goto done
					}
				}
			}
		}
		select {
		case sem <- struct{}{}:
			wg.Add(1)
			seq := i
			sched := scheduled
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				out := fn(mCtx, seq)
				if mCtx.Err() != nil && out.err != nil && !out.authViolation && !out.integrityError {
					if out.err == context.Canceled {
						return
					}
				}
				// Coordinated-omission-aware latency from scheduled arrival time
				rec.record(time.Since(sched), out)
			}()
		default:
			droppedDispatch.Add(1)
			rec.record(time.Since(scheduled), reqOutcome{
				err:     fmt.Errorf("open-loop in-flight cap reached (%d)", maxInflight),
				timeout: true,
			})
		}
	}

done:
	wg.Wait()
	elapsed := time.Since(start)
	reason, _ := stopReason.Load().(string)
	if droppedDispatch.Load() > 0 && reason == "" {
		reason = fmt.Sprintf("in-flight cap saturated: %d requests shed by open-loop pacer", droppedDispatch.Load())
	}
	return rec, elapsed, emergencyStopped.Load(), reason
}

func emitJSONL(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}
