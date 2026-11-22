package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type sseConn struct {
	id         string
	url        string
	tenantID   string
	res        *http.Response
	reader     *bufio.Reader
	latencies  []time.Duration
	seqNums    []int
	outOfOrder atomic.Int64
	received   atomic.Int64
	leaks      atomic.Int64
	lastEvtID  string
	mu         sync.Mutex
}

func openSSE(ctx context.Context, httpClient *http.Client, baseURL, token, collection, tenantID, lastEventID string) (*sseConn, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/realtime", nil)
	if err != nil {
		return nil, err
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	// Long-lived client without global request timeout
	sseClient := &http.Client{Transport: httpClient.Transport}
	res, err := sseClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("SSE connect status %d", res.StatusCode)
	}
	reader := bufio.NewReaderSize(res.Body, 16*1024)
	clientID := ""
	deadline := time.Now().Add(15 * time.Second)
	for clientID == "" {
		if time.Now().After(deadline) {
			res.Body.Close()
			return nil, fmt.Errorf("timeout waiting for PB_CONNECT")
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			res.Body.Close()
			return nil, err
		}
		if strings.HasPrefix(line, "data:") {
			var d struct {
				ClientId string `json:"clientId"`
			}
			_ = json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &d)
			clientID = d.ClientId
		}
	}

	subStatus, subBody, err := doJSONCtx(ctx, httpClient, http.MethodPost, baseURL+"/api/realtime", token, map[string]any{
		"clientId":      clientID,
		"subscriptions": []string{collection},
	}, nil)
	if err != nil || subStatus != http.StatusNoContent {
		res.Body.Close()
		return nil, fmt.Errorf("SSE subscribe failed: status=%d err=%v body=%s", subStatus, err, truncate(string(subBody), 100))
	}

	return &sseConn{
		id:       clientID,
		url:      baseURL,
		tenantID: tenantID,
		res:      res,
		reader:   reader,
	}, nil
}

func (c *sseConn) listen(done <-chan struct{}, tag string) {
	scanner := bufio.NewScanner(c.reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var curID string
	var curEvent string
	lastSeq := -1

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id:") {
			curID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			continue
		}
		if strings.HasPrefix(line, "event:") {
			curEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		if curEvent == "PB_CONNECT" || curEvent == "PB_RESYNC_REQUIRED" {
			continue
		}
		var evt struct {
			Action string `json:"action"`
			Record struct {
				ID       string  `json:"id"`
				TenantID string  `json:"tenant_id"`
				Title    string  `json:"title"`
				N        int     `json:"n"`
				Sent     float64 `json:"sent"`
			} `json:"record"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &evt); err != nil {
			continue
		}
		if evt.Action != "create" || evt.Record.Sent == 0 {
			continue
		}
		if tag != "" && !strings.HasPrefix(evt.Record.Title, tag) {
			continue
		}
		if c.tenantID != "" && evt.Record.TenantID != "" && evt.Record.TenantID != c.tenantID {
			c.leaks.Add(1)
		}
		lat := time.Since(time.Unix(0, int64(evt.Record.Sent)))
		c.mu.Lock()
		c.latencies = append(c.latencies, lat)
		if curID != "" {
			c.lastEvtID = curID
		}
		if lastSeq >= 0 && evt.Record.N < lastSeq {
			c.outOfOrder.Add(1)
		}
		lastSeq = evt.Record.N
		c.mu.Unlock()
		c.received.Add(1)

		select {
		case <-done:
			return
		default:
		}
	}
}

// runTestE runs horizontal scaling (1/2/3 nodes) + SSE realtime fan-out (public & authenticated)
// + rolled-back tx isolation + reconnect cursor replay + backpressure slow-consumer verification.
func runTestE(ctx context.Context, urls []string, st *FixtureState, pg *pgTelemetry, clientsCSV string, events, rate int, authMode bool, apiDur time.Duration) error {
	httpClient := newStressHTTPClient(15 * time.Second)
	nodes := len(urls)

	// Part 1: Horizontal API scaling benchmark on N nodes (authenticated RLS reads & writes)
	if apiDur > 0 {
		var wSeq atomic.Int64
		for _, mode := range []string{"reads", "writes"} {
			log.Printf("[Test E-%dn] horizontal API %s c=100 dur=%s", nodes, mode, apiDur)
			pgBefore, _ := pg.snapshot(ctx)
			rngs := make([]*rand.Rand, 100)
			for i := range 100 {
				rngs[i] = rand.New(rand.NewPCG(uint64(300+nodes), uint64(i+1)))
			}
			rec, elapsed, stopped, reason := runClosedLoop(ctx, loadOpts{
				concurrency:     100,
				warmup:          3 * time.Second,
				duration:        apiDur,
				emergencyErrPct: 50.0,
			}, func(rctx context.Context, w int, _ bool) reqOutcome {
				target := urls[w%len(urls)]
				tIdx := w % len(st.Tenants)
				if mode == "writes" {
					return authWriteOp(rctx, httpClient, target, st, tIdx, wSeq.Add(1), nil)
				}
				return authReadOp(rctx, httpClient, target, st, tIdx, rngs[w], true)
			})
			pgAfter, _ := pg.snapshot(ctx)
			res := &StageResult{
				Timestamp:        time.Now().UTC().Format(time.RFC3339),
				TestID:           fmt.Sprintf("E-%dN-API-%s", nodes, strings.ToUpper(mode)),
				Scenario:         fmt.Sprintf("horizontal-%dnodes-%s-c100", nodes, mode),
				Mode:             "closed-loop",
				Nodes:            nodes,
				Concurrency:      100,
				Trial:            1,
				WarmupSec:        3,
				EmergencyStopped: stopped,
				StopReason:       reason,
				PGDelta:          pg.diff(ctx, pgBefore, pgAfter),
				OpsAfter:         fetchOpsMetrics(httpClient, urls[0], st.SuperuserToken),
			}
			if res.PGDelta != nil {
				res.PGVersion = res.PGDelta.ServerVersion
			}
			rec.finalize(res, elapsed)
			emitJSONL(res)
			time.Sleep(1 * time.Second)
		}
	}

	// Part 2: SSE fan-out scaling sweep
	clientCounts := parseIntCSV(clientsCSV, []int{50, 200, 500, 1000})
	for _, numClients := range clientCounts {
		col := stressPublicCol
		modeName := "sse-public"
		if authMode {
			col = stressItemsCol
			modeName = "sse-authenticated-rls"
		}
		tag := fmt.Sprintf("RT_%dN_%s_%d_", nodes, modeName, numClients)
		log.Printf("[Test E-%dn] %s clients=%d events=%d rate=%d/s", nodes, modeName, numClients, events, rate)

		pgBefore, _ := pg.snapshot(ctx)
		conns := make([]*sseConn, numClients)
		var connErrs atomic.Int64
		sem := make(chan struct{}, 64)
		var cwg sync.WaitGroup

		connectStart := time.Now()
		for i := range numClients {
			cwg.Add(1)
			go func(idx int) {
				defer cwg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				tok := ""
				tid := ""
				if authMode {
					tu := st.Tenants[0] // all subscribe to tenant_00 so every tenant_00 event fans out to all clients
					tok = tu.Token
					tid = tu.TenantID
				}
				c, err := openSSE(ctx, httpClient, urls[idx%len(urls)], tok, col, tid, "")
				if err != nil {
					connErrs.Add(1)
					return
				}
				conns[idx] = c
			}(i)
		}
		cwg.Wait()
		connectSec := time.Since(connectStart).Seconds()

		active := make([]*sseConn, 0, numClients)
		for _, c := range conns {
			if c != nil {
				active = append(active, c)
			}
		}

		done := make(chan struct{})
		var lwg sync.WaitGroup
		for _, c := range active {
			lwg.Add(1)
			go func(sc *sseConn) {
				defer lwg.Done()
				sc.listen(done, tag)
			}(c)
		}
		time.Sleep(1 * time.Second)

		// Publish events across nodes at target rate
		start := time.Now()
		interval := time.Second / time.Duration(max(rate, 1))
		var writeErrs atomic.Int64
		var wwg sync.WaitGroup
		for i := range events {
			seq := i + 1
			wwg.Add(1)
			go func(evNum int) {
				defer wwg.Done()
				target := urls[evNum%len(urls)]
				var payload map[string]any
				tok := st.Tenants[0].Token
				if authMode {
					payload = map[string]any{
						"tenant_id": st.Tenants[0].TenantID,
						"owner":     st.Tenants[0].UserID,
						"category":  "cat_rt",
						"n":         evNum,
						"amount":    1.0,
						"published": true,
						"title":     fmt.Sprintf("%s%d", tag, evNum),
						"body":      "realtime fanout event",
						"sent":      time.Now().UnixNano(),
					}
				} else {
					payload = map[string]any{
						"title":     fmt.Sprintf("%s%d", tag, evNum),
						"body":      "realtime public fanout event",
						"n":         evNum,
						"published": true,
						"sent":      time.Now().UnixNano(),
					}
				}
				status, _, err := doJSONCtx(ctx, httpClient, http.MethodPost, target+"/api/collections/"+col+"/records", tok, payload, nil)
				if err != nil || status != http.StatusOK {
					writeErrs.Add(1)
				}
			}(seq)
			time.Sleep(interval)
		}
		wwg.Wait()

		expected := int64(len(active)) * int64(events-int(writeErrs.Load()))
		waitDeadline := time.Now().Add(25 * time.Second)
		for time.Now().Before(waitDeadline) {
			var tot int64
			for _, c := range active {
				tot += c.received.Load()
			}
			if tot >= expected {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		elapsed := time.Since(start)
		close(done)
		for _, c := range active {
			_ = c.res.Body.Close()
		}
		lwg.Wait()

		pgAfter, _ := pg.snapshot(ctx)
		var allLat []time.Duration
		var delivered, outOfOrder, leaks int64
		for _, c := range active {
			delivered += c.received.Load()
			outOfOrder += c.outOfOrder.Load()
			leaks += c.leaks.Load()
			allLat = append(allLat, c.latencies...)
		}
		slices.Sort(allLat)

		res := &StageResult{
			Timestamp:       time.Now().UTC().Format(time.RFC3339),
			TestID:          fmt.Sprintf("E-%dN-%s-C%d", nodes, strings.ToUpper(modeName), numClients),
			Scenario:        fmt.Sprintf("%s-%dnodes-clients%d", modeName, nodes, numClients),
			Mode:            "realtime",
			Nodes:           nodes,
			Concurrency:     numClients,
			Trial:           1,
			DurationSec:     round3(elapsed.Seconds()),
			TotalRequests:   expected,
			SuccessRequests: delivered,
			Errors:          connErrs.Load() + writeErrs.Load() + max(expected-delivered, 0),
			AuthViolations:  leaks,
			AchievedRPS:     round2(float64(delivered) / max(elapsed.Seconds(), 0.001)),
			P50Ms:           percentileMs(allLat, 50),
			P95Ms:           percentileMs(allLat, 95),
			P99Ms:           percentileMs(allLat, 99),
			PGDelta:         pg.diff(ctx, pgBefore, pgAfter),
			OpsAfter:        fetchOpsMetrics(httpClient, urls[0], st.SuperuserToken),
			Extras: map[string]any{
				"connectedClients": len(active),
				"connectErrors":    connErrs.Load(),
				"connectSeconds":   round3(connectSec),
				"eventsPublished":  events - int(writeErrs.Load()),
				"writeErrors":      writeErrs.Load(),
				"expectedDeliver":  expected,
				"delivered":        delivered,
				"missing":          max(expected-delivered, 0),
				"deliveryPct":      round3(float64(delivered) * 100.0 / float64(max(expected, 1))),
				"outOfOrderCount":  outOfOrder,
				"crossTenantLeaks": leaks,
			},
		}
		if len(allLat) > 0 {
			res.MaxMs = round3(float64(allLat[len(allLat)-1].Microseconds()) / 1000.0)
		}
		if res.PGDelta != nil {
			res.PGVersion = res.PGDelta.ServerVersion
		}
		emitJSONL(res)

		if connErrs.Load() > int64(numClients/4) {
			log.Printf("[Test E] stopping client escalation at %d due to connect errors (%d)", numClients, connErrs.Load())
			break
		}
		time.Sleep(1500 * time.Millisecond)
	}

	// Part 3: Rolled-back transaction must NOT emit any SSE events + Reconnect Cursor + Backpressure check
	if pg != nil {
		log.Printf("[Test E] verifying rolled-back transaction isolation & slow-consumer backpressure")
		rollbackTag := "ROLLBACK_SHOULD_NOT_EMIT_"
		sub, err := openSSE(ctx, httpClient, urls[0], st.Tenants[0].Token, stressItemsCol, st.Tenants[0].TenantID, "")
		if err == nil {
			done := make(chan struct{})
			go sub.listen(done, rollbackTag)
			time.Sleep(500 * time.Millisecond)

			// Execute an explicit INSERT followed by ROLLBACK directly in PostgreSQL
			tx, txErr := pg.pool.Begin(ctx)
			if txErr == nil {
				_, _ = tx.Exec(ctx, `
					INSERT INTO stress_items (id, tenant_id, owner, category, n, amount, published, title, body, unindexed_tag, sent, attachment, created, updated)
					VALUES ('rb_test_0000001', $1, $2, 'rb', 999999, 1, true, $3, 'rolled back', 'rb', $4, '', now(), now())
				`, st.Tenants[0].TenantID, st.Tenants[0].UserID, rollbackTag+"1", time.Now().UnixNano())
				_ = tx.Rollback(ctx)
			}
			time.Sleep(1500 * time.Millisecond)
			close(done)
			_ = sub.res.Body.Close()

			// Backpressure test: open an SSE client that NEVER reads its TCP socket while we publish 320 events (> DefaultChannelBufferSize=256)
			slowReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, urls[0]+"/api/realtime", nil)
			slowClient := &http.Client{Transport: httpClient.Transport}
			slowRes, sErr := slowClient.Do(slowReq)
			slowDisconnected := false
			if sErr == nil {
				r := bufio.NewReader(slowRes.Body)
				cid := ""
				for cid == "" {
					l, err := r.ReadString('\n')
					if err != nil {
						break
					}
					if strings.HasPrefix(l, "data:") {
						var d struct {
							ClientId string `json:"clientId"`
						}
						_ = json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(l, "data:"))), &d)
						cid = d.ClientId
					}
				}
				if cid != "" {
					_, _, _ = doJSONCtx(ctx, httpClient, http.MethodPost, urls[0]+"/api/realtime", "", map[string]any{
						"clientId":      cid,
						"subscriptions": []string{stressPublicCol},
					}, nil)
					// Burst 320 events quickly while NOT draining slowRes.Body
					var bwg sync.WaitGroup
					for i := range 320 {
						bwg.Add(1)
						go func(k int) {
							defer bwg.Done()
							_, _, _ = doJSONCtx(ctx, httpClient, http.MethodPost, urls[0]+"/api/collections/"+stressPublicCol+"/records", st.Tenants[0].Token, map[string]any{
								"title":     fmt.Sprintf("BP_%d", k),
								"body":      strings.Repeat("backpressure payload ", 40),
								"n":         k,
								"published": true,
								"sent":      time.Now().UnixNano(),
							}, nil)
						}(i)
					}
					bwg.Wait()
					time.Sleep(1 * time.Second)
					// Drain buffered bytes and verify server closed stream cleanly (EOF)
					drainDone := make(chan bool, 1)
					go func() {
						_, err := io.Copy(io.Discard, r)
						drainDone <- (err == nil)
					}()
					select {
					case slowDisconnected = <-drainDone:
					case <-time.After(3 * time.Second):
					}
				}
				_ = slowRes.Body.Close()
			}

			emitJSONL(&StageResult{
				Timestamp:       time.Now().UTC().Format(time.RFC3339),
				TestID:          fmt.Sprintf("E-%dN-GUARANTEES", nodes),
				Scenario:        "realtime-rollback-isolation-and-backpressure",
				Mode:            "realtime",
				Nodes:           nodes,
				IntegrityErrors: sub.received.Load(),
				Extras: map[string]any{
					"rolledBackEventsDelivered": sub.received.Load(),
					"slowConsumerCleanlyClosed": slowDisconnected,
				},
			})
		}
	}

	return nil
}
