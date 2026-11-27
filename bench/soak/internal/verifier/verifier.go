// Package verifier implements:
//  1. RLS-aware independent data & security integrity reconciliation (Phase 5),
//  2. Pilot capacity calibration & SLO baseline derivation using post-GC live heap (Phase 6),
//  3. Final 168-hour qualification reconciliation & immutable SHA-256 manifest verification (Phase 11),
//  4. Strictly read-only A11 Independent Operational Certification Audit (Section 14).
package verifier

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/journeys"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/state"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/telemetry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const VerifierVersion = "w11-05-verifier-v2.0.0"

// CheckOptions configures an independent integrity check pass.
type CheckOptions struct {
	Client        *http.Client
	BaseURL       string
	SuperToken    string
	Fixtures      *journeys.Fixtures // optional: enables live HTTP & strict-RLS SQL tenant verification
	Pool          *pgxpool.Pool
	EvidenceDir   string
	CampaignID    string
	FullReconcile bool // when true, checks 100% of ledger entities instead of bounded recent window
}

// ReconstructedEntity represents the reconstructed lifecycle of one synthetic record from ledger.jsonl.
type ReconstructedEntity struct {
	EntityID        string `json:"entityId"`
	TenantID        string `json:"tenantId"`
	OpID            string `json:"opId"`
	ExpectedPresent bool   `json:"expectedPresent"`
	Deleted         bool   `json:"deleted"`
	ExpectedText    string `json:"expectedText,omitempty"`
	ExpectedHash    string `json:"expectedHash,omitempty"`
	InFlight        bool   `json:"inFlight,omitempty"`
	InFlightKind    string `json:"inFlightKind,omitempty"`
}

// LedgerReconstruction summarizes reconstructed committed, deleted, and in-flight operations.
type LedgerReconstruction struct {
	TotalEntries      int
	OrderedEntities   []*ReconstructedEntity
	InFlightCreateOps []state.LedgerEntry
	Malformations     []string
	UnackedIntents    int
	CommittedCreates  int
	CommittedUpdates  int
	CommittedDeletes  int
	RejectedMutations int
}

// IntegrityCheckReport captures one independent reconciliation pass with explicit
// failure attribution (never conflating RLS visibility/permissions/errors with missing data).
type IntegrityCheckReport struct {
	TimestampUTC           string   `json:"timestampUtc"`
	CampaignID             string   `json:"campaignId,omitempty"`
	Passed                 bool     `json:"passed"`
	VerificationIncomplete bool     `json:"verificationIncomplete"`
	DatabaseName           string   `json:"databaseName,omitempty"`
	SQLRole                string   `json:"sqlRole,omitempty"`
	RoleSuperuser          bool     `json:"roleSuperuser"`
	RoleBypassRLS          bool     `json:"roleBypassRls"`
	TableOwner             string   `json:"tableOwner,omitempty"`
	RLSEnabled             bool     `json:"rlsEnabled"`
	DirectSQLVisibility    string   `json:"directSqlVisibility"` // OWNER_VISIBLE, HIDDEN_BY_RLS, NO_SELECT_PRIVILEGE, UNAVAILABLE
	RuntimeRoleRestricted  bool     `json:"runtimeRoleRestricted"`
	ReleaseBlockersPassed  bool     `json:"releaseBlockersPassed"`
	WorkerCampaignMatch    bool     `json:"workerCampaignMatch"`
	LedgerChecked          int      `json:"ledgerChecked"`
	PhysicallyPresent      int      `json:"physicallyPresent"`
	IntentionallyDeleted   int      `json:"intentionallyDeleted"`
	ConfirmedMissingRows   int      `json:"confirmedMissingRows"`
	ConfirmedResurrected   int      `json:"confirmedResurrectedRows"`
	CorruptedChecksumRows  int      `json:"corruptedChecksumRows"`
	HiddenByRLS            int      `json:"hiddenByRls"`
	PermissionErrors       int      `json:"permissionErrors"`
	ConnectionErrors       int      `json:"connectionErrors"`
	QueryErrors            int      `json:"queryErrors"`
	LedgerInconsistencies  int      `json:"ledgerInconsistencies"`
	InFlightIntents        int      `json:"inFlightIntents"`
	HTTPVerifiedRows       int      `json:"httpVerifiedRows"`
	HTTPCrossTenantDenied  int      `json:"httpCrossTenantDenied"`
	HTTPFailures           int      `json:"httpFailures"`
	RLSSQLVerifiedRows     int      `json:"rlsSqlVerifiedRows"`
	RLSSQLDeniedChecks     int      `json:"rlsSqlDeniedChecks"`
	NullTenantRows         int64    `json:"nullTenantRows"`
	TamperedRows           int64    `json:"tamperedRows"`
	DeadJobs               int64    `json:"deadJobs"`
	FailedOutbox           int64    `json:"failedOutbox"`
	Failures               []string `json:"failures,omitempty"`
}

// ReconstructExpectedLedger replays intent, ack, and legacy ledger entries into canonical
// per-entity expected state.
func ReconstructExpectedLedger(entries []state.LedgerEntry) LedgerReconstruction {
	var out LedgerReconstruction
	out.TotalEntries = len(entries)

	byEntity := make(map[string]*ReconstructedEntity)
	var order []string
	pendingIntents := make(map[string]state.LedgerEntry) // opId -> intent

	ensureEntity := func(id string) *ReconstructedEntity {
		ent, ok := byEntity[id]
		if !ok {
			ent = &ReconstructedEntity{EntityID: id}
			byEntity[id] = ent
			order = append(order, id)
		}
		return ent
	}

	for idx, e := range entries {
		if e.OpID == "" || e.Kind == "" || e.TenantID == "" {
			out.Malformations = append(out.Malformations, fmt.Sprintf("ledger entry %d missing opId/kind/tenantId", idx+1))
			continue
		}

		switch e.Phase {
		case "intent":
			pendingIntents[e.OpID] = e
			if e.EntityID != "" {
				ent := ensureEntity(e.EntityID)
				ent.InFlight = true
				ent.InFlightKind = e.Kind
			}
		case "ack":
			delete(pendingIntents, e.OpID)
			committed := e.Committed != nil && *e.Committed
			if !committed {
				out.RejectedMutations++
				if e.EntityID != "" {
					if ent, ok := byEntity[e.EntityID]; ok {
						ent.InFlight = false
						ent.InFlightKind = ""
					}
				}
				continue
			}
			if e.EntityID == "" {
				out.Malformations = append(out.Malformations, fmt.Sprintf("committed ack %s missing entityId", e.OpID))
				continue
			}
			ent := ensureEntity(e.EntityID)
			ent.InFlight = false
			ent.InFlightKind = ""
			ent.TenantID = e.TenantID
			ent.OpID = e.OpID
			switch e.Kind {
			case "record_create":
				out.CommittedCreates++
				ent.ExpectedPresent = true
				ent.Deleted = false
				ent.ExpectedText = e.ExpectedText
				ent.ExpectedHash = e.ExpectedHash
			case "record_update":
				out.CommittedUpdates++
				ent.ExpectedPresent = true
				ent.Deleted = false
				ent.ExpectedText = e.ExpectedText
				ent.ExpectedHash = e.ExpectedHash
			case "record_delete":
				out.CommittedDeletes++
				ent.ExpectedPresent = false
				ent.Deleted = true
			default:
				out.Malformations = append(out.Malformations, fmt.Sprintf("unknown ledger kind %q on op %s", e.Kind, e.OpID))
			}
		case "":
			// Legacy single-entry format
			if e.EntityID == "" {
				out.Malformations = append(out.Malformations, fmt.Sprintf("legacy ledger entry %s missing entityId", e.OpID))
				continue
			}
			ent := ensureEntity(e.EntityID)
			ent.TenantID = e.TenantID
			ent.OpID = e.OpID
			if e.Kind == "record_delete" || e.Deleted {
				out.CommittedDeletes++
				ent.ExpectedPresent = false
				ent.Deleted = true
			} else if e.Kind == "record_create" || e.Kind == "record_update" {
				if e.Kind == "record_create" {
					out.CommittedCreates++
				} else {
					out.CommittedUpdates++
				}
				ent.ExpectedPresent = true
				ent.Deleted = false
				ent.ExpectedText = e.ExpectedText
				ent.ExpectedHash = e.ExpectedHash
			}
		default:
			out.Malformations = append(out.Malformations, fmt.Sprintf("unknown ledger phase %q on op %s", e.Phase, e.OpID))
		}
	}

	out.UnackedIntents = len(pendingIntents)
	for _, intent := range pendingIntents {
		if intent.Kind == "record_create" && intent.EntityID == "" {
			out.InFlightCreateOps = append(out.InFlightCreateOps, intent)
		}
	}

	out.OrderedEntities = make([]*ReconstructedEntity, 0, len(order))
	for _, id := range order {
		out.OrderedEntities = append(out.OrderedEntities, byEntity[id])
	}
	return out
}

// RunIntegrityCheck executes an independent, fail-closed integrity reconciliation.
// It distinguishes missing records from RLS visibility restrictions, permission denials,
// query errors, and connection failures, and validates canonical content checksums.
func RunIntegrityCheck(ctx context.Context, opts CheckOptions) IntegrityCheckReport {
	rep := IntegrityCheckReport{
		TimestampUTC:        time.Now().UTC().Format(time.RFC3339),
		CampaignID:          opts.CampaignID,
		Passed:              true,
		DirectSQLVisibility: "UNAVAILABLE",
	}

	failIncomplete := func(msg string) {
		rep.Passed = false
		rep.VerificationIncomplete = true
		rep.Failures = append(rep.Failures, msg)
	}
	failCritical := func(msg string) {
		rep.Passed = false
		rep.Failures = append(rep.Failures, msg)
	}

	if opts.Pool == nil {
		failIncomplete("mandatory PostgreSQL verification pool is nil")
	}
	if opts.Client == nil || opts.BaseURL == "" || opts.SuperToken == "" {
		failIncomplete("mandatory Ruxbase HTTP verification client/baseURL/superToken is missing")
	}

	// 0. Validate worker heartbeat campaign binding when evidence directory & campaign ID exist
	if opts.EvidenceDir != "" && opts.CampaignID != "" {
		hbPath := filepath.Join(opts.EvidenceDir, "worker-heartbeat.json")
		if hbRaw, err := os.ReadFile(hbPath); err == nil {
			var hb state.WorkerHeartbeat
			if uErr := json.Unmarshal(hbRaw, &hb); uErr != nil {
				failCritical(fmt.Sprintf("corrupt worker-heartbeat.json: %v", uErr))
			} else if hb.CampaignID != opts.CampaignID {
				failCritical(fmt.Sprintf("campaign ID mismatch on soak-worker: heartbeat=%q active=%q", hb.CampaignID, opts.CampaignID))
			} else {
				rep.WorkerCampaignMatch = true
			}
		} else if !os.IsNotExist(err) {
			failIncomplete(fmt.Sprintf("unable to read worker-heartbeat.json: %v", err))
		}
	}

	var hasDirectVisibility bool
	var canSetRLSRole bool

	if opts.Pool != nil {
		// 1. Inspect database identity, role privileges, and ruxbase_app restrictions
		var appSuper, appBypass bool
		if err := opts.Pool.QueryRow(ctx, "SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'ruxbase_app'").Scan(&appSuper, &appBypass); err != nil {
			classifySQLError(&rep, "verify ruxbase_app role", err)
		} else if appSuper || appBypass {
			failCritical(fmt.Sprintf("SECURITY VIOLATION: ruxbase_app super=%v bypassrls=%v", appSuper, appBypass))
		} else {
			rep.RuntimeRoleRestricted = true
		}

		if err := opts.Pool.QueryRow(ctx, `
			SELECT current_database(), current_user, r.rolsuper, r.rolbypassrls,
			       pg_has_role(current_user, 'ruxbase_api', 'MEMBER')
			FROM pg_roles r WHERE r.rolname = current_user
		`).Scan(&rep.DatabaseName, &rep.SQLRole, &rep.RoleSuperuser, &rep.RoleBypassRLS, &canSetRLSRole); err != nil {
			classifySQLError(&rep, "inspect verifier SQL role", err)
		}

		// 2. Inspect RLS & ownership visibility on soak_items BEFORE running any data queries
		var forceRLS, hasSelect bool
		err := opts.Pool.QueryRow(ctx, `
			SELECT c.relrowsecurity, c.relforcerowsecurity,
			       pg_get_userbyid(c.relowner),
			       has_table_privilege(current_user, c.oid, 'SELECT')
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public' AND c.relname = $1
		`, journeys.SoakItemsCollection).Scan(&rep.RLSEnabled, &forceRLS, &rep.TableOwner, &hasSelect)
		if err != nil {
			classifySQLError(&rep, "inspect soak_items pg_class", err)
		} else {
			if !rep.RLSEnabled {
				failCritical(fmt.Sprintf("relrowsecurity is FALSE on %s", journeys.SoakItemsCollection))
			}
			switch {
			case !hasSelect:
				rep.DirectSQLVisibility = "NO_SELECT_PRIVILEGE"
			case !rep.RLSEnabled || rep.RoleSuperuser || rep.RoleBypassRLS || (rep.SQLRole == rep.TableOwner && !forceRLS):
				rep.DirectSQLVisibility = "OWNER_VISIBLE"
				hasDirectVisibility = true
			default:
				rep.DirectSQLVisibility = "HIDDEN_BY_RLS"
			}
		}

		// 3. Check system tables without RLS (_jobs, _outbox_events)
		if err := opts.Pool.QueryRow(ctx, "SELECT count(*) FROM _jobs WHERE status = 'dead'").Scan(&rep.DeadJobs); err != nil {
			classifySQLError(&rep, "query _jobs dead count", err)
		}
		if err := opts.Pool.QueryRow(ctx, "SELECT count(*) FROM _outbox_events WHERE status = 'failed'").Scan(&rep.FailedOutbox); err != nil {
			classifySQLError(&rep, "query _outbox_events failed count", err)
		}
		if rep.FailedOutbox > 0 {
			failCritical(fmt.Sprintf("found %d failed outbox events", rep.FailedOutbox))
		}

		// 4. Null-tenant & forged-row checks ONLY when visibility is actually proven
		if hasDirectVisibility {
			if err := opts.Pool.QueryRow(ctx, "SELECT count(*) FROM soak_items WHERE tenant_id IS NULL OR tenant_id = ''").Scan(&rep.NullTenantRows); err != nil {
				classifySQLError(&rep, "query null tenant rows", err)
			} else if rep.NullTenantRows > 0 {
				failCritical(fmt.Sprintf("found %d soak_items rows with null/empty tenant_id", rep.NullTenantRows))
			}

			if err := opts.Pool.QueryRow(ctx, "SELECT count(*) FROM soak_items WHERE title = 'FORGED_CROSS_TENANT_SOAK'").Scan(&rep.TamperedRows); err != nil {
				classifySQLError(&rep, "query forged cross-tenant rows", err)
			} else if rep.TamperedRows > 0 {
				failCritical(fmt.Sprintf("found %d forged cross-tenant rows in soak_items", rep.TamperedRows))
			}
		} else if rep.DirectSQLVisibility != "UNAVAILABLE" {
			failIncomplete(fmt.Sprintf("SQL connection role %q has %s on RLS-protected %s (cannot perform unmasked physical reconciliation; use non-superuser table owner ruxbase_app for verifier DB)", rep.SQLRole, rep.DirectSQLVisibility, journeys.SoakItemsCollection))
		}

		// 5. Reconstruct expected mutation lifecycles from durable ledger.jsonl
		entries, err := state.ReadLedger(opts.EvidenceDir)
		if err != nil {
			rep.LedgerInconsistencies++
			failIncomplete(fmt.Sprintf("failed reading ledger.jsonl: %v", err))
		} else if len(entries) > 0 {
			rec := ReconstructExpectedLedger(entries)
			if len(rec.Malformations) > 0 {
				rep.LedgerInconsistencies += len(rec.Malformations)
				failCritical(fmt.Sprintf("ledger contains %d malformed entry/entries (first: %s)", len(rec.Malformations), rec.Malformations[0]))
			}
			rep.InFlightIntents = rec.UnackedIntents

			targets := rec.OrderedEntities
			if !opts.FullReconcile && len(targets) > 250 {
				targets = targets[len(targets)-250:]
			}

			var activeSamples []*ReconstructedEntity
			for _, ent := range targets {
				if ent.InFlight {
					continue
				}
				rep.LedgerChecked++

				if !hasDirectVisibility {
					if rep.DirectSQLVisibility == "NO_SELECT_PRIVILEGE" {
						rep.PermissionErrors++
					} else {
						rep.HiddenByRLS++
					}
					continue
				}

				var dbTenant, dbOpID, dbTitle, dbChecksum string
				qErr := opts.Pool.QueryRow(ctx,
					"SELECT tenant_id, op_id, title, checksum FROM soak_items WHERE id = $1",
					ent.EntityID,
				).Scan(&dbTenant, &dbOpID, &dbTitle, &dbChecksum)

				if !ent.ExpectedPresent || ent.Deleted {
					if qErr == nil {
						rep.ConfirmedResurrected++
						failCritical(fmt.Sprintf("committed deleted row %s still exists in PostgreSQL", ent.EntityID))
					} else if errors.Is(qErr, pgx.ErrNoRows) {
						rep.IntentionallyDeleted++
					} else {
						classifySQLError(&rep, fmt.Sprintf("check deleted row %s", ent.EntityID), qErr)
					}
					continue
				}

				if qErr != nil {
					if errors.Is(qErr, pgx.ErrNoRows) {
						rep.ConfirmedMissingRows++
					} else {
						classifySQLError(&rep, fmt.Sprintf("reconcile row %s", ent.EntityID), qErr)
					}
					continue
				}

				// Verify tenant ownership AND canonical expected checksum/title
				expectedSum := ent.ExpectedHash
				if expectedSum == "" && ent.ExpectedText != "" && ent.OpID != "" {
					expectedSum = journeys.CanonicalRecordChecksum(ent.OpID, ent.ExpectedText)
				}
				actualComputed := expectedChecksumForRow(dbOpID, ent.EntityID, dbTitle)
				if dbTenant != ent.TenantID ||
					(ent.ExpectedText != "" && dbTitle != ent.ExpectedText) ||
					(expectedSum != "" && dbChecksum != expectedSum) ||
					(dbChecksum != "" && dbChecksum != actualComputed && dbChecksum != sha256Hex(ent.EntityID+"|"+dbTitle)) {
					rep.CorruptedChecksumRows++
					failCritical(fmt.Sprintf("content/checksum/tenant mismatch on %s: db=(tenant=%s,title=%q,chk=%s) expected=(tenant=%s,title=%q,chk=%s)",
						ent.EntityID, dbTenant, dbTitle, dbChecksum, ent.TenantID, ent.ExpectedText, expectedSum))
					continue
				}
				rep.PhysicallyPresent++
				if len(activeSamples) < 10 {
					activeSamples = append(activeSamples, ent)
				}
			}

			if rep.ConfirmedMissingRows > 0 {
				failCritical(fmt.Sprintf("%d committed ledger row(s) confirmed missing from PostgreSQL", rep.ConfirmedMissingRows))
			}

			// 6. Independent RLS-aware tenant verification (HTTP REST + Strict-RLS SQL role)
			if opts.Fixtures != nil && opts.Client != nil && opts.BaseURL != "" {
				verifyRLSAccessSamples(ctx, opts, activeSamples, canSetRLSRole, &rep, failCritical)
			}
		}
	}

	// 7. Verify /api/studio/overview CheckReleaseBlockers (migrations, non-superuser, storage SHA-256)
	if opts.Client != nil && opts.BaseURL != "" && opts.SuperToken != "" {
		st, raw, err := journeys.DoJSON(ctx, opts.Client, http.MethodGet, strings.TrimRight(opts.BaseURL, "/")+"/api/studio/overview", opts.SuperToken, nil)
		if err != nil || st != http.StatusOK {
			failIncomplete(fmt.Sprintf("GET /api/studio/overview failed: status=%d err=%v", st, err))
		} else {
			var ov struct {
				ReleaseBlockers struct {
					Passed   bool `json:"passed"`
					Findings []struct {
						Code    string `json:"code"`
						Detail  string `json:"detail"`
						Blocked bool   `json:"blocked"`
					} `json:"findings"`
				} `json:"releaseBlockers"`
			}
			if json.Unmarshal(raw, &ov) != nil {
				failIncomplete("failed parsing /api/studio/overview JSON")
			} else {
				rep.ReleaseBlockersPassed = ov.ReleaseBlockers.Passed
				if !ov.ReleaseBlockers.Passed {
					for _, f := range ov.ReleaseBlockers.Findings {
						if f.Blocked {
							failCritical(fmt.Sprintf("releaseBlocker[%s]: %s", f.Code, f.Detail))
						}
					}
				}
			}
		}
	}

	if rep.PermissionErrors > 0 || rep.ConnectionErrors > 0 || rep.QueryErrors > 0 || rep.HiddenByRLS > 0 {
		rep.VerificationIncomplete = true
		rep.Passed = false
	}

	return rep
}

func verifyRLSAccessSamples(
	ctx context.Context,
	opts CheckOptions,
	samples []*ReconstructedEntity,
	canSetRLSRole bool,
	rep *IntegrityCheckReport,
	failCritical func(string),
) {
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	for _, ent := range samples {
		owner, ok := opts.Fixtures.UserForTenant(ent.TenantID)
		if !ok {
			continue
		}
		// 6a. Authorized tenant HTTP GET must succeed and match checksum
		st, raw, err := journeys.DoJSON(ctx, opts.Client, http.MethodGet, baseURL+"/api/collections/"+journeys.SoakItemsCollection+"/records/"+ent.EntityID, owner.Token, nil)
		if err != nil || st != http.StatusOK {
			rep.HTTPFailures++
			failCritical(fmt.Sprintf("authorized tenant HTTP GET %s failed: status=%d err=%v", ent.EntityID, st, err))
			continue
		}
		var rec struct {
			ID       string `json:"id"`
			TenantID string `json:"tenant_id"`
			Checksum string `json:"checksum"`
		}
		if json.Unmarshal(raw, &rec) != nil || rec.TenantID != ent.TenantID || (ent.ExpectedHash != "" && rec.Checksum != ent.ExpectedHash) {
			rep.HTTPFailures++
			failCritical(fmt.Sprintf("HTTP record checksum/tenant mismatch on %s", ent.EntityID))
			continue
		}
		rep.HTTPVerifiedRows++

		// 6b. Foreign tenant HTTP GET must return 404/403
		foreign, hasForeign := opts.Fixtures.ForeignUserForTenant(ent.TenantID)
		if hasForeign {
			fst, _, ferr := journeys.DoJSON(ctx, opts.Client, http.MethodGet, baseURL+"/api/collections/"+journeys.SoakItemsCollection+"/records/"+ent.EntityID, foreign.Token, nil)
			if ferr == nil && (fst == http.StatusNotFound || fst == http.StatusForbidden) {
				rep.HTTPCrossTenantDenied++
			} else {
				failCritical(fmt.Sprintf("CROSS-TENANT HTTP EXPOSURE on %s by %s: status=%d err=%v", ent.EntityID, foreign.TenantID, fst, ferr))
			}
		}

		// 6c. Direct PostgreSQL strict-RLS check via SET LOCAL ROLE ruxbase_api + ruxbase_rls token
		if canSetRLSRole && opts.Pool != nil {
			rlsTok, tokErr := journeys.FetchTenantRLSToken(ctx, opts.Client, baseURL, owner.Token, 60)
			if tokErr != nil {
				failCritical(fmt.Sprintf("fetch owner RLS token failed for %s: %v", ent.EntityID, tokErr))
			} else {
				var gotTenant string
				if qErr := queryAsRLSRole(ctx, opts.Pool, rlsTok, ent.EntityID, &gotTenant); qErr == nil && gotTenant == ent.TenantID {
					rep.RLSSQLVerifiedRows++
				} else {
					failCritical(fmt.Sprintf("strict-RLS SQL owner query failed for %s: tenant=%q err=%v", ent.EntityID, gotTenant, qErr))
				}
			}
			if hasForeign {
				fTok, tokErr := journeys.FetchTenantRLSToken(ctx, opts.Client, baseURL, foreign.Token, 60)
				if tokErr != nil {
					failCritical(fmt.Sprintf("fetch foreign RLS token failed for %s: %v", ent.EntityID, tokErr))
				} else {
					var leaked string
					qErr := queryAsRLSRole(ctx, opts.Pool, fTok, ent.EntityID, &leaked)
					if errors.Is(qErr, pgx.ErrNoRows) {
						rep.RLSSQLDeniedChecks++
					} else {
						failCritical(fmt.Sprintf("STRICT-RLS SQL CROSS-TENANT EXPOSURE on %s for %s: got=%q err=%v", ent.EntityID, foreign.TenantID, leaked, qErr))
					}
				}
			}
		}
	}
}

func queryAsRLSRole(ctx context.Context, pool *pgxpool.Pool, rlsToken, entityID string, tenantOut *string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		"SELECT set_config('role', 'ruxbase_api', true), set_config('ruxbase.token', $1, true), set_config('ruxbase.rule', 'view', true)",
		rlsToken,
	); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT tenant_id FROM soak_items WHERE id = $1", entityID).Scan(tenantOut); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func classifySQLError(rep *IntegrityCheckReport, op string, err error) {
	if err == nil {
		return
	}
	rep.Passed = false
	rep.VerificationIncomplete = true
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42501" {
		rep.PermissionErrors++
		rep.Failures = append(rep.Failures, fmt.Sprintf("permission denied (%s): %v", op, err))
		return
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.As(err, &netErr) || strings.Contains(strings.ToLower(err.Error()), "connection") {
		rep.ConnectionErrors++
		rep.Failures = append(rep.Failures, fmt.Sprintf("connection error (%s): %v", op, err))
		return
	}
	rep.QueryErrors++
	rep.Failures = append(rep.Failures, fmt.Sprintf("query error (%s): %v", op, err))
}

func expectedChecksumForRow(opID, id, title string) string {
	if opID != "" {
		return journeys.CanonicalRecordChecksum(opID, title)
	}
	return sha256Hex(id + "|" + title)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TrendMetrics summarizes steady-state memory, goroutine, WAL, workload, and telemetry coverage.
type TrendMetrics struct {
	SampleCount            int     `json:"sampleCount"`
	ExpectedSamples        int     `json:"expectedSamples"`
	TelemetryCoveragePct   float64 `json:"telemetryCoveragePct"`
	MaxSampleGapSec        float64 `json:"maxSampleGapSec"`
	WorkloadWindows        int     `json:"workloadWindows"`
	ActiveWorkloadWindows  int     `json:"activeWorkloadWindows"`
	WorkloadCoveragePct    float64 `json:"workloadCoveragePct"`
	MemoryMetricUsed       string  `json:"memoryMetricUsed"`
	HeapBaselineMiB        float64 `json:"heapBaselineMiB"`
	HeapFinalMiB           float64 `json:"heapFinalMiB"`
	HeapDriftPct           float64 `json:"heapDriftPct"`
	HeapSlopeMiBPerHour    float64 `json:"heapSlopeMiBPerHour"`
	RSSBaselineMiB         float64 `json:"rssBaselineMiB,omitempty"`
	RSSFinalMiB            float64 `json:"rssFinalMiB,omitempty"`
	RSSDriftPct            float64 `json:"rssDriftPct,omitempty"`
	GoroutinesBaseline     float64 `json:"goroutinesBaseline"`
	GoroutinesFinal        float64 `json:"goroutinesFinal"`
	GoroutinesDriftPct     float64 `json:"goroutinesDriftPct"`
	MaxPoolWaitCount       int64   `json:"maxPoolWaitCount"`
	MaxRetainedWALBytes    int64   `json:"maxRetainedWalBytes"`
	MinDiskFreePct         float64 `json:"minDiskFreePct"`
	ReplicationSlotsStatus string  `json:"replicationSlotsStatus"`
	LogicalWALStatus       string  `json:"logicalWalStatus"`
	StandbyStatus          string  `json:"standbyStatus"`
}

// ReconciliationReport is the Phase 11 formal qualification evaluation.
type ReconciliationReport struct {
	CampaignID              string       `json:"campaignId"`
	GeneratedAtUTC          string       `json:"generatedAtUtc"`
	VerifierVersion         string       `json:"verifierVersion"`
	Verdict                 string       `json:"verdict"` // PASS, FAIL, INCOMPLETE
	OfficialStartUTC        string       `json:"officialStartUtc"`
	FinalTimestampUTC       string       `json:"finalTimestampUtc"`
	ElapsedHours            float64      `json:"elapsedHours"`
	RequiredHours           float64      `json:"requiredHours"`
	CandidateFrozen         bool         `json:"candidateFrozen"`
	DeployedBinaryVerified  bool         `json:"deployedBinaryVerified"`
	IsolationPassed         bool         `json:"isolationPassed"`
	WorkerHeartbeatVerified bool         `json:"workerHeartbeatVerified"`
	IntegrityChecksPassed   int          `json:"integrityChecksPassed"`
	IntegrityChecksFailed   int          `json:"integrityChecksFailed"`
	Trends                  TrendMetrics `json:"trends"`
	CrossTenantLeaks        int64        `json:"crossTenantLeaks"`
	IntegrityViolations     int64        `json:"integrityViolations"`
	LedgerPersistErrors     int64        `json:"ledgerPersistErrors"`
	DisqualifyingInterrupts int          `json:"disqualifyingInterruptions"`
	BackupVerificationsOK   int          `json:"backupVerificationsOk"`
	BackupVerificationsFail int          `json:"backupVerificationsFail"`
	FaultDrillsCompleted    []string     `json:"faultDrillsCompleted"`
	MissingEvidence         []string     `json:"missingEvidence,omitempty"`
	FailureReasons          []string     `json:"failureReasons,omitempty"`
}

// Reconcile evaluates a completed campaign directory against the formal W11-05 acceptance criteria.
// Any missing mandatory evidence produces INCOMPLETE; any critical integrity defect produces FAIL.
func Reconcile(dir string, requiredHours float64) (*ReconciliationReport, error) {
	if requiredHours <= 0 {
		requiredHours = 168.0
	}
	rep := &ReconciliationReport{
		GeneratedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		VerifierVersion: VerifierVersion,
		RequiredHours:   requiredHours,
		Verdict:         "PASS",
	}

	// 1. Load checkpoint
	cpRaw, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		rep.Verdict = "INCOMPLETE"
		rep.MissingEvidence = append(rep.MissingEvidence, "state.json")
		return rep, nil
	}
	var cp state.Checkpoint
	if err := json.Unmarshal(cpRaw, &cp); err != nil {
		return nil, fmt.Errorf("parse state.json: %w", err)
	}
	rep.CampaignID = cp.CampaignID
	rep.OfficialStartUTC = cp.OfficialStartUTC.Format(time.RFC3339)
	rep.FinalTimestampUTC = cp.LastHeartbeatUTC.Format(time.RFC3339)
	rep.ElapsedHours = math.Round(cp.LastHeartbeatUTC.Sub(cp.OfficialStartUTC).Hours()*1000) / 1000
	rep.CrossTenantLeaks = cp.CrossTenantLeaks
	rep.IntegrityViolations = cp.IntegrityViolations
	rep.LedgerPersistErrors = cp.LedgerPersistErrors

	// 2. Verify candidate provenance manifest & deployed executable digest
	manPath := filepath.Join(dir, "candidate-manifest.json")
	if manRaw, err := os.ReadFile(manPath); err == nil {
		var man state.CandidateManifest
		if uErr := json.Unmarshal(manRaw, &man); uErr != nil {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("corrupt candidate-manifest.json: %v", uErr))
		} else {
			rep.CandidateFrozen = man.GitCommitSHA != "" && man.LinuxBinarySHA256 != "" && man.PostgresVersion != "" && man.AppliedMigrationsCount > 0
			rep.DeployedBinaryVerified = man.DeployedBinaryVerified && man.DeployedBinarySHA256 != "" && man.DeployedBinarySHA256 == man.LinuxBinarySHA256
			rep.IsolationPassed = man.IsolationPassed
			if !rep.CandidateFrozen {
				rep.MissingEvidence = append(rep.MissingEvidence, "candidate-manifest.json incomplete (missing git/binary/postgres/migrations provenance)")
			}
			if !rep.DeployedBinaryVerified {
				rep.MissingEvidence = append(rep.MissingEvidence, "deployed binary digest verification missing or mismatched")
			}
			if !rep.IsolationPassed {
				rep.FailureReasons = append(rep.FailureReasons, "mandatory staging host isolation not satisfied ("+man.IsolationNote+")")
			}
		}
	} else {
		rep.MissingEvidence = append(rep.MissingEvidence, "candidate-manifest.json")
	}

	// 3. Verify worker heartbeat campaign binding
	hbPath := filepath.Join(dir, "worker-heartbeat.json")
	if hbRaw, err := os.ReadFile(hbPath); err == nil {
		var hb state.WorkerHeartbeat
		if uErr := json.Unmarshal(hbRaw, &hb); uErr != nil {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("corrupt worker-heartbeat.json: %v", uErr))
		} else if hb.CampaignID != cp.CampaignID {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("worker campaign ID mismatch: %q != %q", hb.CampaignID, cp.CampaignID))
		} else if hb.IntegrityErrors > 0 || hb.MultipartErrors > 0 {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("worker reported integrityErrors=%d multipartErrors=%d", hb.IntegrityErrors, hb.MultipartErrors))
		} else {
			rep.WorkerHeartbeatVerified = true
		}
	} else {
		rep.MissingEvidence = append(rep.MissingEvidence, "worker-heartbeat.json (campaign-bound worker heartbeat required)")
	}

	// 4. Analyze 60-second telemetry samples & workload continuity
	samples, err := loadTelemetrySamples(filepath.Join(dir, "metrics.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			rep.MissingEvidence = append(rep.MissingEvidence, "metrics.jsonl")
		} else {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("unreadable/corrupt metrics.jsonl: %v", err))
		}
	} else if len(samples) == 0 {
		rep.MissingEvidence = append(rep.MissingEvidence, "metrics.jsonl")
	} else {
		rep.Trends = computeTrends(samples, rep.ElapsedHours)
	}
	if wErr := loadWorkloadContinuity(filepath.Join(dir, "workload.jsonl"), &rep.Trends); wErr != nil {
		if os.IsNotExist(wErr) {
			rep.MissingEvidence = append(rep.MissingEvidence, "workload.jsonl")
		} else {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("unreadable/corrupt workload.jsonl: %v", wErr))
		}
	} else if rep.Trends.WorkloadWindows == 0 {
		rep.MissingEvidence = append(rep.MissingEvidence, "workload.jsonl (zero workload windows)")
	}

	// 5. Inspect integrity verification ledger (must have >= 1 verified check, 0 failed/incomplete checks)
	intChecks, intErr := loadIntegrityChecks(filepath.Join(dir, "integrity.jsonl"))
	if intErr != nil {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("unreadable integrity.jsonl: %v", intErr))
	} else if len(intChecks) == 0 {
		rep.MissingEvidence = append(rep.MissingEvidence, "integrity.jsonl (at least 1 verified integrity check required)")
	} else {
		for _, ic := range intChecks {
			if ic.Passed && !ic.VerificationIncomplete {
				rep.IntegrityChecksPassed++
			} else {
				rep.IntegrityChecksFailed++
			}
		}
		lastIC := intChecks[len(intChecks)-1]
		if !lastIC.Passed || lastIC.VerificationIncomplete {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("final integrity check failed/incomplete: %v", lastIC.Failures))
		}
	}

	// 6. Inspect interruptions
	interrupts, ivErr := loadInterruptions(filepath.Join(dir, "interruptions.jsonl"))
	if ivErr != nil && !os.IsNotExist(ivErr) {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("unreadable interruptions.jsonl: %v", ivErr))
	}
	for _, iv := range interrupts {
		if iv.Disqualifying {
			rep.DisqualifyingInterrupts++
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("disqualifying interruption (%s): %.1fs — %s", iv.Kind, iv.GapSeconds, iv.Detail))
		}
	}

	// 7. Inspect backup/restore verification logs (must match campaignId when present)
	bOK, bFail, bErr := countBackupResults(filepath.Join(dir, "backups.jsonl"), cp.CampaignID)
	rep.BackupVerificationsOK = bOK
	rep.BackupVerificationsFail = bFail
	if bErr != nil {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("backups.jsonl validation error: %v", bErr))
	} else if bOK == 0 {
		rep.MissingEvidence = append(rep.MissingEvidence, "backups.jsonl (at least 1 verified online backup + isolated restore required)")
	}
	if bFail > 0 {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("%d backup/restore verification failure(s)", bFail))
	}

	// 8. Inspect scheduled fault drills (H24, H72, H120) for 168h campaigns
	faults, fErr := loadFaultResults(filepath.Join(dir, "faults.jsonl"))
	if fErr != nil && !os.IsNotExist(fErr) {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("unreadable/corrupt faults.jsonl: %v", fErr))
	}
	for _, f := range faults {
		if f.Passed {
			rep.FaultDrillsCompleted = append(rep.FaultDrillsCompleted, f.Drill)
		} else {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("fault drill %s failed: %s", f.Drill, f.Detail))
		}
	}
	if requiredHours >= 168.0 {
		for _, needed := range []string{"hour24_worker_kill", "hour72_pg_disconnect", "hour120_backup_restore"} {
			if !contains(rep.FaultDrillsCompleted, needed) {
				rep.MissingEvidence = append(rep.MissingEvidence, "fault drill "+needed)
			}
		}
	}

	// 9. Evaluate formal acceptance thresholds
	if rep.ElapsedHours < requiredHours {
		rep.MissingEvidence = append(rep.MissingEvidence, fmt.Sprintf("elapsed wall-clock hours %.3fh < required %.1fh", rep.ElapsedHours, requiredHours))
	}
	if cp.AttemptedOps == 0 || cp.SuccessfulOps == 0 || cp.HTTPRequestsSucceeded == 0 {
		rep.FailureReasons = append(rep.FailureReasons, "zero successful HTTP workload operations recorded")
	}
	if cp.LedgerPersistErrors > 0 {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("%d durable ledger persistence failure(s)", cp.LedgerPersistErrors))
	}
	if cp.CrossTenantLeaks > 0 {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("CRITICAL: %d cross-tenant leak(s)", cp.CrossTenantLeaks))
	}
	if cp.IntegrityViolations > 0 {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("CRITICAL: %d integrity violation(s)", cp.IntegrityViolations))
	}
	if cp.EmergencyStopped {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("emergency stop triggered: %s", cp.EmergencyStopReason))
	}
	if rep.Trends.SampleCount > 0 {
		if rep.Trends.HeapBaselineMiB <= 0 {
			rep.FailureReasons = append(rep.FailureReasons, "missing /api/ops/metrics runtime heap telemetry across collected samples")
		}
		if rep.Trends.TelemetryCoveragePct < 95.0 {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("telemetry coverage %.2f%% < 95.0%%", rep.Trends.TelemetryCoveragePct))
		}
		if rep.Trends.MaxSampleGapSec > 300.0 {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("telemetry collection gap %.1fs > 300s ceiling", rep.Trends.MaxSampleGapSec))
		}
		if rep.Trends.WorkloadWindows > 0 && rep.Trends.WorkloadCoveragePct < 95.0 {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("active HTTP workload coverage %.2f%% < 95.0%% (idle worker cannot satisfy load continuity)", rep.Trends.WorkloadCoveragePct))
		}
		if rep.Trends.HeapDriftPct > 5.0 {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("sustained memory drift (%s) %.2f%% > 5.0%% criterion", rep.Trends.MemoryMetricUsed, rep.Trends.HeapDriftPct))
		}
		if rep.Trends.GoroutinesDriftPct > 25.0 {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("sustained goroutine growth %.2f%% > 25.0%% threshold", rep.Trends.GoroutinesDriftPct))
		}
	}

	switch {
	case len(rep.FailureReasons) > 0:
		rep.Verdict = "FAIL"
	case len(rep.MissingEvidence) > 0:
		rep.Verdict = "INCOMPLETE"
	default:
		rep.Verdict = "PASS"
	}

	return rep, nil
}

// AuditVerdict is the strictly read-only A11 operational certification verdict.
type AuditVerdict struct {
	AuditedAtUTC       string                `json:"auditedAtUtc"`
	AuditorRole        string                `json:"auditorRole"`
	EvidenceDir        string                `json:"evidenceDir"`
	Verdict            string                `json:"verdict"` // W11-05_VERIFIED, W11-05_CONDITIONALLY_ACCEPTABLE, W11-05_FAILED, W11-05_INCOMPLETE_EVIDENCE
	ManifestVerified   bool                  `json:"manifestVerified"`
	ManifestMismatches []string              `json:"manifestMismatches,omitempty"`
	Dimensions         map[string]string     `json:"dimensions"` // VERIFIED, FAILED, NOT_VERIFIED
	Reconciliation     *ReconciliationReport `json:"reconciliation,omitempty"`
}

// AuditCampaign performs strictly read-only A11 independent examination of a frozen evidence directory.
func AuditCampaign(dir string, requiredHours float64) (*AuditVerdict, error) {
	av := &AuditVerdict{
		AuditedAtUTC: time.Now().UTC().Format(time.RFC3339),
		AuditorRole:  "A11_INDEPENDENT_READ_ONLY_AUDITOR",
		EvidenceDir:  dir,
		Dimensions:   make(map[string]string),
	}

	verified, mismatches, _, err := state.VerifySHA256Manifest(dir)
	if err != nil {
		av.Dimensions["sha256_manifest"] = "NOT_VERIFIED"
	} else {
		av.ManifestVerified = verified
		av.ManifestMismatches = mismatches
		if verified {
			av.Dimensions["sha256_manifest"] = "VERIFIED"
		} else {
			av.Dimensions["sha256_manifest"] = "FAILED"
		}
	}

	rec, err := Reconcile(dir, requiredHours)
	if err != nil {
		return nil, err
	}
	av.Reconciliation = rec

	if rec.CandidateFrozen && rec.DeployedBinaryVerified {
		av.Dimensions["candidate_provenance"] = "VERIFIED"
	} else {
		av.Dimensions["candidate_provenance"] = "NOT_VERIFIED"
	}
	if rec.IsolationPassed {
		av.Dimensions["staging_isolation"] = "VERIFIED"
	} else {
		av.Dimensions["staging_isolation"] = "FAILED"
	}
	if rec.ElapsedHours >= requiredHours && rec.DisqualifyingInterrupts == 0 && rec.Trends.WorkloadCoveragePct >= 95.0 {
		av.Dimensions["timeline_and_workload"] = "VERIFIED"
	} else if rec.DisqualifyingInterrupts > 0 {
		av.Dimensions["timeline_and_workload"] = "FAILED"
	} else {
		av.Dimensions["timeline_and_workload"] = "NOT_VERIFIED"
	}
	if rec.CrossTenantLeaks == 0 && rec.IntegrityViolations == 0 && rec.IntegrityChecksPassed > 0 && rec.IntegrityChecksFailed == 0 {
		av.Dimensions["security_and_integrity"] = "VERIFIED"
	} else if rec.CrossTenantLeaks > 0 || rec.IntegrityViolations > 0 || rec.IntegrityChecksFailed > 0 {
		av.Dimensions["security_and_integrity"] = "FAILED"
	} else {
		av.Dimensions["security_and_integrity"] = "NOT_VERIFIED"
	}
	if rec.BackupVerificationsOK > 0 && rec.BackupVerificationsFail == 0 {
		av.Dimensions["backup_and_restore"] = "VERIFIED"
	} else if rec.BackupVerificationsFail > 0 {
		av.Dimensions["backup_and_restore"] = "FAILED"
	} else {
		av.Dimensions["backup_and_restore"] = "NOT_VERIFIED"
	}

	switch {
	case len(av.ManifestMismatches) > 0 || rec.Verdict == "FAIL":
		av.Verdict = "W11-05_FAILED"
	case rec.Verdict == "INCOMPLETE" || !av.ManifestVerified:
		av.Verdict = "W11-05_INCOMPLETE_EVIDENCE"
	default:
		av.Verdict = "W11-05_VERIFIED"
	}

	return av, nil
}

func loadTelemetrySamples(path string) ([]telemetry.Sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []telemetry.Sample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s telemetry.Sample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("parse metrics.jsonl: %w", err)
		}
		out = append(out, s)
	}
	return out, sc.Err()
}

func loadWorkloadContinuity(path string, tm *TrendMetrics) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var w struct {
			Success int64 `json:"success"`
		}
		if err := json.Unmarshal([]byte(line), &w); err != nil {
			return fmt.Errorf("parse workload.jsonl: %w", err)
		}
		tm.WorkloadWindows++
		if w.Success > 0 {
			tm.ActiveWorkloadWindows++
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if tm.WorkloadWindows > 0 {
		tm.WorkloadCoveragePct = round2(float64(tm.ActiveWorkloadWindows) * 100.0 / float64(tm.WorkloadWindows))
	}
	return nil
}

func loadIntegrityChecks(path string) ([]IntegrityCheckReport, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []IntegrityCheckReport
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ic IntegrityCheckReport
		if err := json.Unmarshal([]byte(line), &ic); err != nil {
			return nil, err
		}
		out = append(out, ic)
	}
	return out, sc.Err()
}

func computeTrends(samples []telemetry.Sample, elapsedHours float64) TrendMetrics {
	tm := TrendMetrics{
		SampleCount:            len(samples),
		ExpectedSamples:        max(int(math.Floor(elapsedHours*60.0)), 1),
		MinDiskFreePct:         100.0,
		ReplicationSlotsStatus: telemetry.StatusNotConfigured,
		LogicalWALStatus:       telemetry.StatusNotConfigured,
		StandbyStatus:          telemetry.StatusNotConfigured,
		MemoryMetricUsed:       "live_heap_post_gc",
	}
	tm.TelemetryCoveragePct = math.Min(100.0, math.Round(float64(tm.SampleCount)*100.0/float64(tm.ExpectedSamples)*100)/100)

	var liveHeaps, allocHeaps, rssVals, goros, hours []float64
	var prevTS time.Time
	for _, s := range samples {
		if ts, err := time.Parse(time.RFC3339, s.TimestampUTC); err == nil {
			if !prevTS.IsZero() {
				gap := ts.Sub(prevTS).Seconds()
				if gap > tm.MaxSampleGapSec {
					tm.MaxSampleGapSec = round2(gap)
				}
			}
			prevTS = ts
		}
		if s.Host.DiskFreePct > 0 && s.Host.DiskFreePct < tm.MinDiskFreePct {
			tm.MinDiskFreePct = round2(s.Host.DiskFreePct)
		}
		if s.RuxbaseOps != nil {
			if s.RuxbaseOps.Runtime.LiveHeapBytes > 0 {
				liveHeaps = append(liveHeaps, float64(s.RuxbaseOps.Runtime.LiveHeapBytes)/(1024*1024))
			}
			allocHeaps = append(allocHeaps, float64(s.RuxbaseOps.Runtime.HeapAllocBytes)/(1024*1024))
			if s.RuxbaseOps.Runtime.RSSBytes > 0 {
				rssVals = append(rssVals, float64(s.RuxbaseOps.Runtime.RSSBytes)/(1024*1024))
			}
			goros = append(goros, float64(s.RuxbaseOps.Runtime.Goroutines))
			hours = append(hours, s.ElapsedSec/3600.0)
			if s.RuxbaseOps.DBPool.WaitCount > tm.MaxPoolWaitCount {
				tm.MaxPoolWaitCount = s.RuxbaseOps.DBPool.WaitCount
			}
		}
		if s.Postgres != nil {
			if s.Postgres.MaxRetainedWALBytes > tm.MaxRetainedWALBytes {
				tm.MaxRetainedWALBytes = s.Postgres.MaxRetainedWALBytes
			}
			if s.Postgres.ReplicationSlotsStatus != "" {
				tm.ReplicationSlotsStatus = s.Postgres.ReplicationSlotsStatus
			}
			if s.Postgres.LogicalWALStatus != "" {
				tm.LogicalWALStatus = s.Postgres.LogicalWALStatus
			}
			if s.Postgres.StandbyStatus != "" {
				tm.StandbyStatus = s.Postgres.StandbyStatus
			}
		}
	}

	heaps := liveHeaps
	if len(heaps) < len(allocHeaps) {
		// Fall back only for legacy snapshots without LiveHeapBytes
		heaps = allocHeaps
		tm.MemoryMetricUsed = "heap_alloc_fallback"
	}

	if len(heaps) >= 2 {
		// Discard initial startup warm-up (first 10% of samples, up to 15 samples)
		warm := min(max(len(heaps)/10, 0), 15)
		if len(heaps) < 10 {
			warm = 0
		}
		steady := heaps[warm:]
		steadyHours := hours[warm:]
		steadyGoros := goros[warm:]

		win := max(len(steady)/4, min(3, len(steady)/2))
		if win < 1 {
			win = 1
		}
		tm.HeapBaselineMiB = round2(avg(steady[:win]))
		tm.HeapFinalMiB = round2(avg(steady[len(steady)-win:]))
		if tm.HeapBaselineMiB > 0 {
			tm.HeapDriftPct = round2(((tm.HeapFinalMiB - tm.HeapBaselineMiB) / tm.HeapBaselineMiB) * 100.0)
		}
		tm.HeapSlopeMiBPerHour = round2(linearSlope(steadyHours, steady))

		tm.GoroutinesBaseline = round2(avg(steadyGoros[:win]))
		tm.GoroutinesFinal = round2(avg(steadyGoros[len(steadyGoros)-win:]))
		if tm.GoroutinesBaseline > 0 {
			tm.GoroutinesDriftPct = round2(((tm.GoroutinesFinal - tm.GoroutinesBaseline) / tm.GoroutinesBaseline) * 100.0)
		}
	}

	if len(rssVals) >= 6 {
		warm := min(len(rssVals)/10, 15)
		steady := rssVals[warm:]
		win := max(len(steady)/4, 3)
		tm.RSSBaselineMiB = round2(avg(steady[:win]))
		tm.RSSFinalMiB = round2(avg(steady[len(steady)-win:]))
		if tm.RSSBaselineMiB > 0 {
			tm.RSSDriftPct = round2(((tm.RSSFinalMiB - tm.RSSBaselineMiB) / tm.RSSBaselineMiB) * 100.0)
		}
	}
	return tm
}

func linearSlope(xs, ys []float64) float64 {
	n := float64(min(len(xs), len(ys)))
	if n < 2 {
		return 0
	}
	var sumX, sumY, sumXY, sumXX float64
	for i := range int(n) {
		sumX += xs[i]
		sumY += ys[i]
		sumXY += xs[i] * ys[i]
		sumXX += xs[i] * xs[i]
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}

func loadInterruptions(path string) ([]state.InterruptionEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []state.InterruptionEvent
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var iv state.InterruptionEvent
		if err := json.Unmarshal([]byte(line), &iv); err != nil {
			return nil, err
		}
		out = append(out, iv)
	}
	return out, sc.Err()
}

func countBackupResults(path, expectedCampaignID string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	defer f.Close()
	var okCnt, failCnt int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec struct {
			CampaignID string `json:"campaignId"`
			Passed     bool   `json:"passed"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return okCnt, failCnt, fmt.Errorf("parse backups.jsonl: %w", err)
		}
		if expectedCampaignID != "" && rec.CampaignID != "" && rec.CampaignID != expectedCampaignID {
			return okCnt, failCnt, fmt.Errorf("backup campaign ID mismatch: got %q want %q", rec.CampaignID, expectedCampaignID)
		}
		if rec.Passed {
			okCnt++
		} else {
			failCnt++
		}
	}
	return okCnt, failCnt, sc.Err()
}

type faultResult struct {
	Drill  string `json:"drill"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

func loadFaultResults(path string) ([]faultResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []faultResult
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var fr faultResult
		if err := json.Unmarshal([]byte(line), &fr); err != nil {
			return nil, fmt.Errorf("parse faults.jsonl: %w", err)
		}
		if fr.Drill != "" {
			out = append(out, fr)
		}
	}
	return out, sc.Err()
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func contains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}
