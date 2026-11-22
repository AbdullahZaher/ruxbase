package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// runTestC executes PostgreSQL Extreme Concurrency testing across 10k, 100k, and 1M records,
// enforcing restricted roles + real RLS (never disabling RLS or granting superuser access to app queries).
func runTestC(ctx context.Context, baseURL string, st *FixtureState, pgAdmin *pgTelemetry, appDSN string, sizesCSV string, dur time.Duration) error {
	if pgAdmin == nil || appDSN == "" {
		return errors.New("test C requires both -pg-admin-url (for telemetry/seeding) and -pg-app-url (restricted ruxbase_app role)")
	}
	if err := pgAdmin.verifyAppRoleRestricted(ctx, "ruxbase_app"); err != nil {
		return err
	}

	httpClient := newStressHTTPClient(15 * time.Second)

	// Open restricted app pool (configured at 40 conns for high-concurrency PG stress)
	appCfg, err := pgxpool.ParseConfig(appDSN)
	if err != nil {
		return err
	}
	appCfg.MaxConns = 40
	appPool, err := pgxpool.NewWithConfig(ctx, appCfg)
	if err != nil {
		return err
	}
	defer appPool.Close()

	// Verify the restricted connection itself is NOT superuser and NOT bypassrls
	var curRole string
	var isSuper, isBypass bool
	if err := appPool.QueryRow(ctx, "SELECT current_user, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user").Scan(&curRole, &isSuper, &isBypass); err != nil {
		return err
	}
	if isSuper || isBypass {
		return fmt.Errorf("test C refused: app DSN connected as privileged role %q (super=%v bypassrls=%v)", curRole, isSuper, isBypass)
	}
	log.Printf("[Test C] verified restricted PG connection role=%s (rolsuper=false, rolbypassrls=false)", curRole)

	// Mint real RLS tokens for all 20 tenants via /api/rls/token so direct PG scoped tx tests exercise authentic ruxbase_rls.auth()!
	rlsTokens := make([]string, len(st.Tenants))
	for i, tu := range st.Tenants {
		status, raw, err := doJSON(httpClient, http.MethodPost, baseURL+"/api/rls/token", tu.Token, map[string]any{"duration": 3600})
		if err != nil || status != http.StatusOK {
			return fmt.Errorf("mint RLS token for %s: status=%d err=%v body=%s", tu.TenantID, status, err, string(raw))
		}
		var resp struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil || resp.Token == "" {
			return fmt.Errorf("decode RLS token for %s", tu.TenantID)
		}
		rlsTokens[i] = resp.Token
	}

	sizes := parseIntCSV(sizesCSV, []int{10000, 100000, 1000000})

	for _, targetRows := range sizes {
		log.Printf("[Test C] === Scaling stress_items dataset to %d rows ===", targetRows)
		tSeed := time.Now()
		var currentCount int64
		_ = pgAdmin.pool.QueryRow(ctx, "SELECT count(*) FROM stress_items").Scan(&currentCount)
		if int(currentCount) < targetRows {
			startG := int(currentCount) + 1
			// Seed in chunks of 100k to keep WAL/memory predictable
			for chunkStart := startG; chunkStart <= targetRows; chunkStart += 100000 {
				chunkEnd := min(chunkStart+99999, targetRows)
				_, err := pgAdmin.pool.Exec(ctx, `
					INSERT INTO stress_items (id, tenant_id, owner, category, n, amount, published, title, body, unindexed_tag, sent, attachment, created, updated)
					SELECT
						'si' || lpad(g::text, 13, '0'),
						'tenant_' || lpad((g % $3)::text, 2, '0'),
						'owner_' || lpad((g % $3)::text, 2, '0'),
						'cat_' || (g % 10)::text,
						g::double precision,
						((g % 1000) + 1.5)::double precision,
						(g % 2 = 0),
						'Stress Item #' || g::text,
						repeat('enterprise workload payload data ', 4),
						'tag_' || (g % 500)::text,
						0,
						'',
						now(),
						now()
					FROM generate_series($1::int, $2::int) AS g
					ON CONFLICT (id) DO NOTHING
				`, chunkStart, chunkEnd, stressNumTenants)
				if err != nil {
					return fmt.Errorf("seed chunk %d..%d: %w", chunkStart, chunkEnd, err)
				}
			}
			_, _ = pgAdmin.pool.Exec(ctx, "ANALYZE stress_items")
		}
		var tableBytes int64
		_ = pgAdmin.pool.QueryRow(ctx, "SELECT pg_total_relation_size('stress_items')").Scan(&tableBytes)
		log.Printf("[Test C] dataset=%d rows ready in %.2fs (table+indexes=%.1f MiB)", targetRows, time.Since(tSeed).Seconds(), float64(tableBytes)/(1024*1024))

		pgAdmin.resetStatements(ctx)

		// C1: Indexed filtered+sorted RLS reads over HTTP (with & without skipTotal)
		runPGScenario := func(id, name string, conc int, fn func(rctx context.Context, w int, measuring bool) reqOutcome, extras map[string]any) {
			pgBefore, _ := pgAdmin.snapshot(ctx)
			rec, elapsed, stopped, reason := runClosedLoop(ctx, loadOpts{
				concurrency:     conc,
				warmup:          2 * time.Second,
				duration:        dur,
				emergencyErrPct: 60.0,
			}, fn)
			pgAfter, _ := pgAdmin.snapshot(ctx)
			if extras == nil {
				extras = map[string]any{}
			}
			extras["datasetRows"] = targetRows
			extras["tableTotalMiB"] = round2(float64(tableBytes) / (1024 * 1024))
			res := &StageResult{
				Timestamp:        time.Now().UTC().Format(time.RFC3339),
				TestID:           fmt.Sprintf("C-%d-%s", targetRows, id),
				Scenario:         name,
				Mode:             "pg",
				Nodes:            1,
				Concurrency:      conc,
				Trial:            1,
				WarmupSec:        2,
				EmergencyStopped: stopped,
				StopReason:       reason,
				PGDelta:          pgAdmin.diff(ctx, pgBefore, pgAfter),
				OpsAfter:         fetchOpsMetrics(httpClient, baseURL, st.SuperuserToken),
				Extras:           extras,
			}
			if res.PGDelta != nil {
				res.PGVersion = res.PGDelta.ServerVersion
			}
			rec.finalize(res, elapsed)
			emitJSONL(res)
		}

		// C1: Indexed read (tenant_id + n DESC, skipTotal=1)
		runPGScenario("IDX-READ", "indexed-filtered-sorted-rls-read", 50, func(rctx context.Context, w int, _ bool) reqOutcome {
			tu := st.Tenants[w%len(st.Tenants)]
			q := url.Values{}
			q.Set("perPage", "20")
			q.Set("sort", "-n")
			q.Set("skipTotal", "1")
			q.Set("filter", fmt.Sprintf("n < %d && published = true", rand.IntN(targetRows)+1))
			status, raw, err := doJSONCtx(rctx, httpClient, http.MethodGet, baseURL+"/api/collections/"+stressItemsCol+"/records?"+q.Encode(), tu.Token, nil, nil)
			if err != nil {
				return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
			}
			if status != http.StatusOK {
				return reqOutcome{statusCode: status, err: fmt.Errorf("status %d", status)}
			}
			var lr stressListResp
			if json.Unmarshal(raw, &lr) == nil {
				for _, it := range lr.Items {
					if it.TenantID != tu.TenantID {
						return reqOutcome{statusCode: status, authViolation: true, err: fmt.Errorf("cross-tenant leak")}
					}
				}
			}
			return reqOutcome{statusCode: status}
		}, nil)

		// C2: Non-indexed filter query (`unindexed_tag`)
		runPGScenario("UNIDX-READ", "unindexed-filter-rls-read", 25, func(rctx context.Context, w int, _ bool) reqOutcome {
			tu := st.Tenants[w%len(st.Tenants)]
			q := url.Values{}
			q.Set("perPage", "20")
			q.Set("skipTotal", "1")
			q.Set("filter", fmt.Sprintf(`unindexed_tag = "tag_%d"`, rand.IntN(500)))
			status, _, err := doJSONCtx(rctx, httpClient, http.MethodGet, baseURL+"/api/collections/"+stressItemsCol+"/records?"+q.Encode(), tu.Token, nil, nil)
			if err != nil {
				return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
			}
			if status != http.StatusOK {
				return reqOutcome{statusCode: status, err: fmt.Errorf("status %d", status)}
			}
			return reqOutcome{statusCode: status}
		}, nil)

		// C3: Deep offset pagination (page = 50% into tenant's records, WITH total count)
		deepPage := max((targetRows/stressNumTenants)/2/20, 10)
		runPGScenario("DEEP-PAGE", fmt.Sprintf("deep-pagination-page%d", deepPage), 25, func(rctx context.Context, w int, _ bool) reqOutcome {
			tu := st.Tenants[w%len(st.Tenants)]
			q := url.Values{}
			q.Set("page", fmt.Sprintf("%d", deepPage))
			q.Set("perPage", "20")
			q.Set("sort", "-n")
			status, _, err := doJSONCtx(rctx, httpClient, http.MethodGet, baseURL+"/api/collections/"+stressItemsCol+"/records?"+q.Encode(), tu.Token, nil, nil)
			if err != nil {
				return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
			}
			if status != http.StatusOK {
				return reqOutcome{statusCode: status, err: fmt.Errorf("status %d", status)}
			}
			return reqOutcome{statusCode: status}
		}, map[string]any{"page": deepPage, "offset": (deepPage - 1) * 20})

		// C4: Concurrent HTTP SQL RPC (`POST /api/ruxbase/rpc/stress_tenant_summary`)
		runPGScenario("SQL-RPC", "http-sql-rpc-concurrency", 50, func(rctx context.Context, w int, _ bool) reqOutcome {
			tu := st.Tenants[w%len(st.Tenants)]
			status, raw, err := doJSONCtx(rctx, httpClient, http.MethodPost, baseURL+"/api/ruxbase/rpc/stress_tenant_summary", tu.Token, map[string]any{
				"p_tenant": tu.TenantID,
			}, nil)
			if err != nil {
				return reqOutcome{statusCode: status, err: err, timeout: isTimeoutErr(err)}
			}
			if status != http.StatusOK {
				return reqOutcome{statusCode: status, err: fmt.Errorf("rpc status %d: %s", status, truncate(string(raw), 100))}
			}
			return reqOutcome{statusCode: status}
		}, nil)

		// C5: Concurrent tenant-scoped PG RLS transactions (direct restricted role + SET LOCAL ROLE ruxbase_api + ruxbase.token)
		// Proves zero tenant leakage and measures native PG RLS evaluation latency at 10k / 100k / 1M rows!
		runPGScenario("SCOPED-RLS-TX", "direct-scoped-rls-transactions", 40, func(rctx context.Context, w int, _ bool) reqOutcome {
			tIdx := w % len(st.Tenants)
			tu := st.Tenants[tIdx]
			tx, err := appPool.Begin(rctx)
			if err != nil {
				return reqOutcome{err: err, timeout: isTimeoutErr(err)}
			}
			defer tx.Rollback(rctx)

			if _, err := tx.Exec(rctx,
				"SELECT set_config('role', 'ruxbase_api', true), set_config('ruxbase.token', $1, true), set_config('ruxbase.rule', 'list', true)",
				rlsTokens[tIdx],
			); err != nil {
				return reqOutcome{err: err}
			}

			rows, err := tx.Query(rctx, "SELECT id, tenant_id, n FROM stress_items WHERE published = true ORDER BY n DESC LIMIT 20")
			if err != nil {
				return reqOutcome{err: err, timeout: isTimeoutErr(err)}
			}
			for rows.Next() {
				var id, gotTenant string
				var n float64
				if err := rows.Scan(&id, &gotTenant, &n); err != nil {
					rows.Close()
					return reqOutcome{err: err, integrityError: true}
				}
				if gotTenant != tu.TenantID {
					rows.Close()
					return reqOutcome{authViolation: true, err: fmt.Errorf("DIRECT RLS LEAK: expected %s got %s", tu.TenantID, gotTenant)}
				}
			}
			rows.Close()
			if err := tx.Commit(rctx); err != nil {
				return reqOutcome{err: err}
			}
			return reqOutcome{statusCode: 200}
		}, nil)

		// C6: Concurrent inserts / updates / deletes mix over HTTP
		var crudSeq atomic.Int64
		runPGScenario("CRUD-MIX", "concurrent-insert-update-delete-http", 50, func(rctx context.Context, w int, _ bool) reqOutcome {
			tIdx := w % len(st.Tenants)
			tu := st.Tenants[tIdx]
			s := crudSeq.Add(1)
			// Create -> Update -> Delete lifecycle on an isolated synthetic record
			status, raw, err := doJSONCtx(rctx, httpClient, http.MethodPost, baseURL+"/api/collections/"+stressItemsCol+"/records", tu.Token, map[string]any{
				"tenant_id":     tu.TenantID,
				"owner":         tu.UserID,
				"category":      "cat_crud",
				"n":             targetRows + int(s),
				"amount":        42.5,
				"published":     true,
				"title":         fmt.Sprintf("CRUD #%d", s),
				"body":          "crud stress record",
				"unindexed_tag": "tag_crud",
			}, nil)
			if err != nil || status != http.StatusOK {
				return reqOutcome{statusCode: status, err: fmt.Errorf("create status=%d err=%v", status, err), timeout: isTimeoutErr(err)}
			}
			var created stressRecord
			if err := json.Unmarshal(raw, &created); err != nil || created.ID == "" {
				return reqOutcome{statusCode: status, integrityError: true, err: fmt.Errorf("invalid create body")}
			}
			// Update
			status, _, err = doJSONCtx(rctx, httpClient, http.MethodPatch, baseURL+"/api/collections/"+stressItemsCol+"/records/"+created.ID, tu.Token, map[string]any{
				"title":  fmt.Sprintf("CRUD Updated #%d", s),
				"amount": 99.9,
			}, nil)
			if err != nil || status != http.StatusOK {
				return reqOutcome{statusCode: status, err: fmt.Errorf("update status=%d err=%v", status, err), timeout: isTimeoutErr(err)}
			}
			// Delete
			status, _, err = doJSONCtx(rctx, httpClient, http.MethodDelete, baseURL+"/api/collections/"+stressItemsCol+"/records/"+created.ID, tu.Token, nil, nil)
			if err != nil || status != http.StatusNoContent {
				return reqOutcome{statusCode: status, err: fmt.Errorf("delete status=%d err=%v", status, err), timeout: isTimeoutErr(err)}
			}
			return reqOutcome{statusCode: 200}
		}, nil)

		// Capture EXPLAIN (ANALYZE, BUFFERS) plans at this dataset scale (executed under restricted ruxbase_api role!)
		plans := map[string][]string{}
		explainUnderRLS := func(label, query string, args ...any) {
			tx, err := appPool.Begin(ctx)
			if err != nil {
				plans[label] = []string{"error: " + err.Error()}
				return
			}
			defer tx.Rollback(ctx)
			_, _ = tx.Exec(ctx, "SELECT set_config('role', 'ruxbase_api', true), set_config('ruxbase.token', $1, true), set_config('ruxbase.rule', 'list', true)", rlsTokens[0])
			rows, err := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+query, args...)
			if err != nil {
				plans[label] = []string{"error: " + err.Error()}
				return
			}
			defer rows.Close()
			var lines []string
			for rows.Next() {
				var line string
				if rows.Scan(&line) == nil {
					lines = append(lines, line)
				}
			}
			plans[label] = lines
		}
		explainUnderRLS("rls_indexed_top20", "SELECT id, tenant_id, n FROM stress_items WHERE published = true ORDER BY n DESC LIMIT 20")
		explainUnderRLS("rls_unindexed_tag", "SELECT id, tenant_id, n FROM stress_items WHERE unindexed_tag = 'tag_42' LIMIT 20")
		explainUnderRLS("rls_deep_offset", fmt.Sprintf("SELECT id, tenant_id, n FROM stress_items ORDER BY n DESC LIMIT 20 OFFSET %d", (deepPage-1)*20))
		explainUnderRLS("rls_tenant_join_agg", "SELECT s.category, count(*), avg(s.amount) FROM stress_items s JOIN stress_public p ON p.n = (s.n::int % 2000) + 1 GROUP BY s.category")

		topSQL := pgAdmin.topStatements(ctx, 12)
		emitJSONL(&StageResult{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			TestID:    fmt.Sprintf("C-%d-PLANS", targetRows),
			Scenario:  "pg-explain-and-stat-statements",
			Mode:      "pg",
			Nodes:     1,
			Extras: map[string]any{
				"datasetRows":   targetRows,
				"tableTotalMiB": round2(float64(tableBytes) / (1024 * 1024)),
				"explainPlans":  plans,
				"topStatements": topSQL,
			},
		})
	}

	// C7: Deliberate Lock Contention & Deadlock Detection Test (SQLSTATE 40P01) + Pool Saturation
	log.Printf("[Test C] running lock contention & deadlock detection probe")
	pgBefore, _ := pgAdmin.snapshot(ctx)
	var deadlocksObserved atomic.Int64
	var lockTimeouts atomic.Int64
	var txSucceeded atomic.Int64

	// Pick 2 hot rows owned by tenant_00
	rowA := st.ItemIDsByTen[0][0]
	rowB := st.ItemIDsByTen[0][1]

	dCtx, dCancel := context.WithTimeout(ctx, 8*time.Second)
	var wg sync.WaitGroup
	for w := range 16 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			first, second := rowA, rowB
			if id%2 == 1 {
				first, second = rowB, rowA
			}
			for dCtx.Err() == nil {
				tx, err := appPool.Begin(dCtx)
				if err != nil {
					continue
				}
				_, _ = tx.Exec(dCtx, "SET LOCAL lock_timeout = '2s'")
				_, err = tx.Exec(dCtx, "UPDATE stress_items SET amount = amount + 1 WHERE id = $1", first)
				if err == nil {
					time.Sleep(5 * time.Millisecond)
					_, err = tx.Exec(dCtx, "UPDATE stress_items SET amount = amount + 1 WHERE id = $1", second)
				}
				if err != nil {
					_ = tx.Rollback(dCtx)
					var pgErr *pgconn.PgError
					if errors.As(err, &pgErr) {
						switch pgErr.Code {
						case "40P01":
							deadlocksObserved.Add(1)
						case "55P03":
							lockTimeouts.Add(1)
						}
					}
					continue
				}
				if err := tx.Commit(dCtx); err == nil {
					txSucceeded.Add(1)
				}
			}
		}(w)
	}
	wg.Wait()
	dCancel()
	pgAfter, _ := pgAdmin.snapshot(ctx)

	emitJSONL(&StageResult{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		TestID:      "C-DEADLOCK",
		Scenario:    "lock-contention-and-deadlock-detection",
		Mode:        "pg",
		Nodes:       1,
		Concurrency: 16,
		DurationSec: 8,
		PGDelta:     pgAdmin.diff(ctx, pgBefore, pgAfter),
		Extras: map[string]any{
			"txSucceeded":       txSucceeded.Load(),
			"deadlocks40P01":    deadlocksObserved.Load(),
			"lockTimeouts55P03": lockTimeouts.Load(),
		},
	})

	return nil
}
