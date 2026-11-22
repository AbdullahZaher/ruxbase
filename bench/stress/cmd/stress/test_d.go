package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// runTestD executes API and Authentication Stress:
// - Public reads, Authenticated RLS reads, CRUD, Filtered/sorted queries
// - Concurrent password auth (bcrypt storm), token refresh
// - Mid-load session invalidation (tokenKey rotation -> zero stale-token acceptance)
// - Mixed cross-tenant negative access probes (must return 100% 404/403, 0 leaks)
// - Workload mixes: 80R/20W, 50R/50W, 20R/80W (write-heavy), Auth-heavy
func runTestD(ctx context.Context, urls []string, st *FixtureState, pg *pgTelemetry, rlsStrict bool, poolMax int, concurrency int, dur time.Duration) error {
	client := newStressHTTPClient(15 * time.Second)
	var writeSeq atomic.Int64

	runMix := func(testID, scenario string, conc int, fn func(rctx context.Context, w int, rng *rand.Rand) reqOutcome) {
		log.Printf("[Test D] %s (%s) c=%d dur=%s", testID, scenario, conc, dur)
		pgBefore, _ := pg.snapshot(ctx)
		rngs := make([]*rand.Rand, conc)
		for i := range conc {
			rngs[i] = rand.New(rand.NewPCG(uint64(200+i), uint64(conc)))
		}
		rec, elapsed, stopped, reason := runClosedLoop(ctx, loadOpts{
			concurrency:     conc,
			warmup:          3 * time.Second,
			duration:        dur,
			emergencyErrPct: 50.0,
		}, func(rctx context.Context, w int, _ bool) reqOutcome {
			return fn(rctx, w, rngs[w])
		})
		pgAfter, _ := pg.snapshot(ctx)
		res := &StageResult{
			Timestamp:        time.Now().UTC().Format(time.RFC3339),
			TestID:           testID,
			Scenario:         scenario,
			Mode:             "closed-loop",
			RLSStrict:        rlsStrict,
			PoolMax:          poolMax,
			Nodes:            len(urls),
			Concurrency:      conc,
			Trial:            1,
			WarmupSec:        3,
			EmergencyStopped: stopped,
			StopReason:       reason,
			PGDelta:          pg.diff(ctx, pgBefore, pgAfter),
			OpsAfter:         fetchOpsMetrics(client, urls[0], st.SuperuserToken),
		}
		if res.PGDelta != nil {
			res.PGVersion = res.PGDelta.ServerVersion
		}
		rec.finalize(res, elapsed)
		emitJSONL(res)
		time.Sleep(1 * time.Second)
	}

	// D1: Public reads (unauthenticated baseline)
	runMix("D1", "public-reads", concurrency, func(rctx context.Context, w int, rng *rand.Rand) reqOutcome {
		target := urls[w%len(urls)]
		if rng.IntN(2) == 0 && len(st.PublicIDs) > 0 {
			id := st.PublicIDs[rng.IntN(len(st.PublicIDs))]
			status, _, err := doJSONCtx(rctx, client, http.MethodGet, target+"/api/collections/"+stressPublicCol+"/records/"+id, "", nil, nil)
			if err != nil {
				return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
			}
			if status != http.StatusOK {
				return reqOutcome{statusCode: status, err: fmt.Errorf("status %d", status)}
			}
			return reqOutcome{statusCode: status}
		}
		status, _, err := doJSONCtx(rctx, client, http.MethodGet, target+"/api/collections/"+stressPublicCol+"/records?perPage=20&sort=-n&skipTotal=1&filter=published%3Dtrue", "", nil, nil)
		if err != nil {
			return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
		}
		if status != http.StatusOK {
			return reqOutcome{statusCode: status, err: fmt.Errorf("status %d", status)}
		}
		return reqOutcome{statusCode: status}
	})

	// D2: Authenticated RLS reads + 10% adversarial cross-tenant probes (must all fail closed!)
	var deniedProbes atomic.Int64
	runMix("D2", "auth-reads-with-cross-tenant-probes", concurrency, func(rctx context.Context, w int, rng *rand.Rand) reqOutcome {
		target := urls[w%len(urls)]
		tIdx := w % len(st.Tenants)
		if rng.IntN(10) == 0 {
			// Adversarial probe: attempt to view/modify another tenant's record
			otherIdx := (tIdx + 1) % len(st.Tenants)
			foreignID := st.ItemIDsByTen[otherIdx][0]
			status, _, err := doJSONCtx(rctx, client, http.MethodGet, target+"/api/collections/"+stressItemsCol+"/records/"+foreignID, st.Tenants[tIdx].Token, nil, nil)
			if err != nil {
				return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
			}
			if status == http.StatusNotFound || status == http.StatusForbidden {
				deniedProbes.Add(1)
				return reqOutcome{statusCode: status}
			}
			return reqOutcome{statusCode: status, authViolation: true, err: fmt.Errorf("CROSS-TENANT PROBE SUCCEEDED status=%d", status)}
		}
		return authReadOp(rctx, client, target, st, tIdx, rng, true)
	})
	log.Printf("[Test D2] cross-tenant negative probes properly denied: %d", deniedProbes.Load())

	// D3: 80% reads / 20% writes (authenticated RLS)
	runMix("D3", "mix-80r-20w", concurrency, func(rctx context.Context, w int, rng *rand.Rand) reqOutcome {
		target := urls[w%len(urls)]
		tIdx := w % len(st.Tenants)
		if rng.IntN(5) == 0 {
			return authWriteOp(rctx, client, target, st, tIdx, writeSeq.Add(1), nil)
		}
		return authReadOp(rctx, client, target, st, tIdx, rng, true)
	})

	// D4: 50% reads / 50% writes (authenticated RLS)
	runMix("D4", "mix-50r-50w", concurrency, func(rctx context.Context, w int, rng *rand.Rand) reqOutcome {
		target := urls[w%len(urls)]
		tIdx := w % len(st.Tenants)
		if rng.IntN(2) == 0 {
			return authWriteOp(rctx, client, target, st, tIdx, writeSeq.Add(1), nil)
		}
		return authReadOp(rctx, client, target, st, tIdx, rng, true)
	})

	// D5: Write-heavy (20% reads / 80% writes — authenticated RLS create+patch+delete)
	runMix("D5", "mix-20r-80w-write-heavy", concurrency, func(rctx context.Context, w int, rng *rand.Rand) reqOutcome {
		target := urls[w%len(urls)]
		tIdx := w % len(st.Tenants)
		if rng.IntN(5) != 0 {
			return authWriteOp(rctx, client, target, st, tIdx, writeSeq.Add(1), nil)
		}
		return authReadOp(rctx, client, target, st, tIdx, rng, true)
	})

	// D6: Token Refresh stress (JWT signing + auth-table lookup)
	runMix("D6", "auth-token-refresh", concurrency, func(rctx context.Context, w int, _ *rand.Rand) reqOutcome {
		target := urls[w%len(urls)]
		tu := st.Tenants[w%len(st.Tenants)]
		status, raw, err := doJSONCtx(rctx, client, http.MethodPost, target+"/api/collections/users/auth-refresh", tu.Token, nil, nil)
		if err != nil {
			return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
		}
		if status != http.StatusOK {
			return reqOutcome{statusCode: status, err: fmt.Errorf("refresh status %d", status)}
		}
		var r struct {
			Token string `json:"token"`
		}
		if json.Unmarshal(raw, &r) != nil || r.Token == "" {
			return reqOutcome{statusCode: status, integrityError: true, err: fmt.Errorf("missing refreshed token")}
		}
		return reqOutcome{statusCode: status}
	})

	// D7: Concurrent Password Authentication (bcrypt CPU stress)
	runMix("D7", "auth-password-bcrypt-storm", min(concurrency, 100), func(rctx context.Context, w int, _ *rand.Rand) reqOutcome {
		target := urls[w%len(urls)]
		tu := st.Tenants[w%len(st.Tenants)]
		status, _, err := doJSONCtx(rctx, client, http.MethodPost, target+"/api/collections/users/auth-with-password", "", map[string]string{
			"identity": tu.Email,
			"password": tu.Password,
		}, nil)
		if err != nil {
			return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
		}
		if status != http.StatusOK {
			return reqOutcome{statusCode: status, err: fmt.Errorf("auth status %d", status)}
		}
		return reqOutcome{statusCode: status}
	})

	// D8: Auth-heavy realistic mix (20% password login, 30% token refresh, 50% authenticated RLS read)
	runMix("D8", "mix-auth-heavy", min(concurrency, 100), func(rctx context.Context, w int, rng *rand.Rand) reqOutcome {
		target := urls[w%len(urls)]
		tIdx := w % len(st.Tenants)
		tu := st.Tenants[tIdx]
		p := rng.IntN(10)
		if p < 2 {
			status, _, err := doJSONCtx(rctx, client, http.MethodPost, target+"/api/collections/users/auth-with-password", "", map[string]string{
				"identity": tu.Email,
				"password": tu.Password,
			}, nil)
			if err != nil || status != http.StatusOK {
				return reqOutcome{statusCode: status, err: fmt.Errorf("login %d %v", status, err), timeout: isTimeoutErr(err)}
			}
			return reqOutcome{statusCode: status}
		}
		if p < 5 {
			status, _, err := doJSONCtx(rctx, client, http.MethodPost, target+"/api/collections/users/auth-refresh", tu.Token, nil, nil)
			if err != nil || status != http.StatusOK {
				return reqOutcome{statusCode: status, err: fmt.Errorf("refresh %d %v", status, err), timeout: isTimeoutErr(err)}
			}
			return reqOutcome{statusCode: status}
		}
		return authReadOp(rctx, client, target, st, tIdx, rng, true)
	})

	// D9: Mid-Load Session Invalidation Test (rotate user_19's tokenKey under active concurrent reads)
	log.Printf("[Test D9] running session invalidation under concurrent traffic")
	victim := st.Tenants[len(st.Tenants)-1]
	oldToken := victim.Token
	var preRevokeOK, postRevokeRejected, postRevokeLeaked atomic.Int64
	var revoked atomic.Bool

	d9Ctx, d9Cancel := context.WithTimeout(ctx, 6*time.Second)
	var wg sync.WaitGroup
	for w := range 20 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for d9Ctx.Err() == nil {
				wasRevokedBeforeCall := revoked.Load()
				status, raw, err := doJSONCtx(d9Ctx, client, http.MethodGet, urls[id%len(urls)]+"/api/collections/"+stressItemsCol+"/records?perPage=5&skipTotal=1", oldToken, nil, nil)
				if err != nil {
					continue
				}
				if wasRevokedBeforeCall {
					// After revocation completed, oldToken MUST NEVER return tenant records!
					if status == http.StatusOK {
						var lr stressListResp
						_ = json.Unmarshal(raw, &lr)
						if len(lr.Items) > 0 {
							postRevokeLeaked.Add(1)
						} else {
							postRevokeRejected.Add(1)
						}
					} else {
						postRevokeRejected.Add(1)
					}
				} else if status == http.StatusOK {
					preRevokeOK.Add(1)
				}
			}
		}(w)
	}

	time.Sleep(1500 * time.Millisecond)
	// Rotate victim's password/tokenKey via superuser PATCH (triggers immediate tokenKey invalidation)
	revStart := time.Now()
	stCode, revBody, revErr := doJSONCtx(ctx, client, http.MethodPatch, urls[0]+"/api/collections/users/records/"+victim.UserID, st.SuperuserToken, map[string]any{
		"tokenKey": fmt.Sprintf("rotated_key_%d", time.Now().UnixNano()),
	}, nil)
	revDur := time.Since(revStart)
	if revErr == nil && stCode == http.StatusOK {
		revoked.Store(true)
	} else {
		log.Printf("warning: tokenKey rotation status=%d err=%v body=%s", stCode, revErr, string(revBody))
	}
	wg.Wait()
	d9Cancel()

	// Re-authenticate victim so fixture state remains usable for subsequent tests
	if stCode == http.StatusOK {
		if aSt, aBody, aErr := doJSON(client, http.MethodPost, urls[0]+"/api/collections/users/auth-with-password", "", map[string]string{
			"identity": victim.Email,
			"password": victim.Password,
		}); aErr == nil && aSt == http.StatusOK {
			var ar struct {
				Token string `json:"token"`
			}
			if json.Unmarshal(aBody, &ar) == nil && ar.Token != "" {
				st.Tenants[len(st.Tenants)-1].Token = ar.Token
			}
		}
	}

	emitJSONL(&StageResult{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		TestID:         "D9",
		Scenario:       "session-invalidation-under-load",
		Mode:           "closed-loop",
		RLSStrict:      rlsStrict,
		PoolMax:        poolMax,
		Nodes:          len(urls),
		Concurrency:    20,
		DurationSec:    6,
		AuthViolations: postRevokeLeaked.Load(),
		Extras: map[string]any{
			"revokePatchStatus":    stCode,
			"revokePatchMs":        round3(float64(revDur.Microseconds()) / 1000.0),
			"preRevokeSucceeded":   preRevokeOK.Load(),
			"postRevokeDenied":     postRevokeRejected.Load(),
			"postRevokeStaleLeaks": postRevokeLeaked.Load(),
		},
	})
	return nil
}
