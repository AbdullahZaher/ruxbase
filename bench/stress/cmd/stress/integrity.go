package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// runIntegrityVerification performs Section 12 mandatory post-test data integrity,
// cross-tenant isolation, unauthorized write rejection, RLS policy presence, and
// CheckReleaseBlockers verification via /api/studio/overview.
func runIntegrityVerification(ctx context.Context, baseURL string, st *FixtureState, pg *pgTelemetry, label string) error {
	client := newStressHTTPClient(15 * time.Second)
	failures := []string{}
	checks := map[string]any{}

	// Wait up to 10s for server to quiesce / drain any prior load
	for attempt := 0; attempt < 10; attempt++ {
		stCode, _, _ := doJSONCtx(ctx, client, http.MethodGet, baseURL+"/api/health?ready", "", nil, nil)
		if stCode == http.StatusOK {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// 1. Cross-tenant negative read, update, and delete checks across 5 tenant pairs
	crossTenantPass := 0
	crossTenantFail := 0
	for i := range 5 {
		caller := st.Tenants[i]
		victimIdx := (i + 1) % len(st.Tenants)
		victimID := st.ItemIDsByTen[victimIdx][0]

		// View foreign record -> MUST be 404
		sView, _, err := doJSONCtx(ctx, client, http.MethodGet, baseURL+"/api/collections/"+stressItemsCol+"/records/"+victimID, caller.Token, nil, nil)
		if err != nil || sView != http.StatusNotFound {
			crossTenantFail++
			failures = append(failures, fmt.Sprintf("cross-tenant GET %s by %s returned %d (%v)", victimID, caller.TenantID, sView, err))
		} else {
			crossTenantPass++
		}

		// Update foreign record -> MUST be 404/403
		sPatch, _, err := doJSONCtx(ctx, client, http.MethodPatch, baseURL+"/api/collections/"+stressItemsCol+"/records/"+victimID, caller.Token, map[string]any{"title": "TAMPERED"}, nil)
		if err != nil || (sPatch != http.StatusNotFound && sPatch != http.StatusForbidden) {
			crossTenantFail++
			failures = append(failures, fmt.Sprintf("cross-tenant PATCH %s by %s returned %d (%v)", victimID, caller.TenantID, sPatch, err))
		} else {
			crossTenantPass++
		}

		// Delete foreign record -> MUST be 404/403
		sDel, _, err := doJSONCtx(ctx, client, http.MethodDelete, baseURL+"/api/collections/"+stressItemsCol+"/records/"+victimID, caller.Token, nil, nil)
		if err != nil || (sDel != http.StatusNotFound && sDel != http.StatusForbidden) {
			crossTenantFail++
			failures = append(failures, fmt.Sprintf("cross-tenant DELETE %s by %s returned %d (%v)", victimID, caller.TenantID, sDel, err))
		} else {
			crossTenantPass++
		}

		// Forged create with foreign tenant_id -> MUST be 403/400/404
		sCreate, _, err := doJSONCtx(ctx, client, http.MethodPost, baseURL+"/api/collections/"+stressItemsCol+"/records", caller.Token, map[string]any{
			"tenant_id": st.Tenants[victimIdx].TenantID,
			"title":     "FORGED_TENANT_WRITE",
		}, nil)
		if err != nil || sCreate == http.StatusOK || sCreate == http.StatusCreated {
			crossTenantFail++
			failures = append(failures, fmt.Sprintf("forged cross-tenant POST by %s into %s returned %d (%v)", caller.TenantID, st.Tenants[victimIdx].TenantID, sCreate, err))
		} else {
			crossTenantPass++
		}
	}
	checks["crossTenantChecksPassed"] = crossTenantPass
	checks["crossTenantChecksFailed"] = crossTenantFail

	// 2. Unauthenticated access to stress_items MUST fail closed (0 items or 403/401)
	sAnonList, rawAnon, err := doJSONCtx(ctx, client, http.MethodGet, baseURL+"/api/collections/"+stressItemsCol+"/records?perPage=5", "", nil, nil)
	anonLeak := false
	if err == nil && sAnonList == http.StatusOK {
		var lr stressListResp
		if json.Unmarshal(rawAnon, &lr) == nil && len(lr.Items) > 0 {
			anonLeak = true
			failures = append(failures, fmt.Sprintf("anonymous GET stress_items returned %d items", len(lr.Items)))
		}
	}
	checks["anonymousReadBlockedOrEmpty"] = !anonLeak

	// 3. Database reconciliation (row counts, zero tampered rows, non-superuser role, RLS enabled in pg_class)
	if pg != nil {
		var totalRows, tamperedRows, nullTenantRows int64
		_ = pg.pool.QueryRow(ctx, "SELECT count(*) FROM stress_items").Scan(&totalRows)
		_ = pg.pool.QueryRow(ctx, "SELECT count(*) FROM stress_items WHERE title IN ('TAMPERED', 'FORGED_TENANT_WRITE')").Scan(&tamperedRows)
		_ = pg.pool.QueryRow(ctx, "SELECT count(*) FROM stress_items WHERE tenant_id = '' OR tenant_id IS NULL").Scan(&nullTenantRows)

		var rlsEnabled bool
		_ = pg.pool.QueryRow(ctx, "SELECT relrowsecurity FROM pg_class WHERE relname = 'stress_items' AND relnamespace = 'public'::regnamespace").Scan(&rlsEnabled)

		var appSuper, appBypass bool
		_ = pg.pool.QueryRow(ctx, "SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'ruxbase_app'").Scan(&appSuper, &appBypass)

		var deadJobs, failedOutbox int64
		_ = pg.pool.QueryRow(ctx, "SELECT count(*) FROM _jobs WHERE status = 'dead'").Scan(&deadJobs)
		_ = pg.pool.QueryRow(ctx, "SELECT count(*) FROM _outbox_events WHERE status = 'failed'").Scan(&failedOutbox)

		checks["pgStressItemsCount"] = totalRows
		checks["pgTamperedRows"] = tamperedRows
		checks["pgNullTenantRows"] = nullTenantRows
		checks["pgRLSEnabled"] = rlsEnabled
		checks["pgAppRoleRestricted"] = !appSuper && !appBypass
		checks["pgDeadJobs"] = deadJobs
		checks["pgFailedOutbox"] = failedOutbox

		if tamperedRows > 0 {
			failures = append(failures, fmt.Sprintf("found %d tampered rows in stress_items", tamperedRows))
		}
		if nullTenantRows > 0 {
			failures = append(failures, fmt.Sprintf("found %d null/empty tenant_id rows", nullTenantRows))
		}
		if !rlsEnabled {
			failures = append(failures, "relrowsecurity is FALSE on stress_items")
		}
		if appSuper || appBypass {
			failures = append(failures, fmt.Sprintf("ruxbase_app has elevated privileges (super=%v bypass=%v)", appSuper, appBypass))
		}
	}

	// 4. Ruxbase /api/studio/overview CheckReleaseBlockers & /api/health?ready
	readyStatus, _, _ := doJSONCtx(ctx, client, http.MethodGet, baseURL+"/api/health?ready", "", nil, nil)
	checks["healthReadyStatus"] = readyStatus
	if readyStatus != http.StatusOK {
		failures = append(failures, fmt.Sprintf("/api/health?ready returned %d", readyStatus))
	}

	overview := fetchOpsJSON(client, baseURL, "/api/studio/overview", st.SuperuserToken)
	checks["studioOverviewStatus"] = overview["status"]
	if bm, ok := overview["body"].(map[string]any); ok {
		if rb, ok := bm["releaseBlockers"].(map[string]any); ok {
			checks["releaseBlockersPassed"] = rb["passed"]
			if passed, ok := rb["passed"].(bool); ok && !passed {
				failures = append(failures, fmt.Sprintf("CheckReleaseBlockers failed: %v", rb["findings"]))
			}
		}
	}

	checks["verifiedPassed"] = len(failures) == 0
	checks["failures"] = failures

	emitJSONL(&StageResult{
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		TestID:          "INTEGRITY-" + label,
		Scenario:        "mandatory-data-integrity-verification",
		Mode:            "verify",
		Nodes:           1,
		AuthViolations:  int64(crossTenantFail),
		IntegrityErrors: int64(len(failures)),
		Extras:          checks,
	})

	if len(failures) > 0 {
		return fmt.Errorf("integrity verification failed (%d issue(s)): %v", len(failures), failures)
	}
	return nil
}
