// Command soak is the operational W11-05 168-hour staging qualification harness for Ruxbase.
//
// Subcommands:
//
//	preflight  - Verify host isolation, disk headroom, PG role restrictions, and Ruxbase readiness.
//	worker     - Run the supervised Durable Jobs, Workflows, Outbox, and Multipart Storage worker.
//	pilot      - Execute a minimum 60-minute continuous pilot and derive calibrated capacity & SLOs.
//	freeze     - Freeze candidate Git/binary/deployed-binary/image/PG/schema provenance into candidate-manifest.json.
//	run        - Execute the continuous 168-hour customer-journey workload, telemetry, and verifier.
//	verify     - Run an on-demand independent integrity reconciliation pass.
//	reconcile  - Execute Phase 11 final qualification evaluation and write an immutable SHA256SUMS.txt.
//	audit      - Strictly read-only A11 independent evidence examination and verdict issuance.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/journeys"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/safety"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/schedule"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/state"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/telemetry"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/verifier"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	sub := os.Args[1]
	args := os.Args[2:]

	var err error
	switch sub {
	case "preflight":
		err = cmdPreflight(args)
	case "worker":
		err = cmdWorker(args)
	case "pilot":
		err = cmdRunCampaign(args, true)
	case "run":
		err = cmdRunCampaign(args, false)
	case "freeze":
		err = cmdFreeze(args)
	case "verify":
		err = cmdVerify(args)
	case "reconcile":
		err = cmdReconcile(args)
	case "audit":
		err = cmdAudit(args)
	case "-h", "--help", "help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", sub)
		printUsage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("soak %s failed: %v", sub, err)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: soak <preflight|worker|pilot|freeze|run|verify|reconcile|audit> [flags]")
}

func cmdPreflight(args []string) error {
	fs := flag.NewFlagSet("preflight", flag.ExitOnError)
	baseURL := fs.String("base-url", "http://127.0.0.1:18090", "Staging Ruxbase base URL")
	monitorDSN := fs.String("monitor-db", "", "PostgreSQL monitoring DSN (ruxbase_monitor)")
	evidenceDir := fs.String("evidence-dir", "/srv/ruxbase-soak/evidence", "Evidence directory")
	checkIsolation := fs.Bool("check-isolation", true, "Fail closed if unrelated or production Docker containers exist on host")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inventory := map[string]any{
		"checkedAtUtc": time.Now().UTC().Format(time.RFC3339),
		"goVersion":    runtime.Version(),
		"goos":         runtime.GOOS,
		"goarch":       runtime.GOARCH,
		"numCpu":       runtime.NumCPU(),
	}
	if host, err := os.Hostname(); err == nil {
		inventory["hostname"] = host
	}

	if *checkIsolation {
		out, err := exec.CommandContext(ctx, "docker", "ps", "--format", "{{.Names}}").Output()
		if err != nil {
			inventory["isolationPassed"] = false
			inventory["isolationError"] = fmt.Sprintf("docker ps failed: %v", err)
			if *evidenceDir != "" {
				_ = state.WriteJSON(filepath.Join(*evidenceDir, "infrastructure-inventory.json"), inventory)
			}
			b, _ := json.MarshalIndent(inventory, "", "  ")
			fmt.Println(string(b))
			return fmt.Errorf("staging host isolation check failed (docker ps): %w", err)
		}
		names := strings.Split(strings.TrimSpace(string(out)), "\n")
		if isoErr := safety.CheckHostIsolation(names, "ruxbase-soak-"); isoErr != nil {
			inventory["isolationPassed"] = false
			inventory["isolationError"] = isoErr.Error()
			if *evidenceDir != "" {
				_ = state.WriteJSON(filepath.Join(*evidenceDir, "infrastructure-inventory.json"), inventory)
			}
			b, _ := json.MarshalIndent(inventory, "", "  ")
			fmt.Println(string(b))
			return isoErr
		}
		inventory["isolationPassed"] = true
	}

	client := journeys.NewHTTPClient(10 * time.Second)
	col, err := telemetry.NewCollector(ctx, client, *baseURL, "", *monitorDSN, *evidenceDir, time.Now().UTC())
	if err != nil {
		return err
	}
	defer col.Close()

	sample := col.Collect(ctx)
	inventory["healthReadyOk"] = sample.HealthReadyOK
	inventory["healthReadyStatus"] = sample.HealthReadyStatus
	inventory["host"] = sample.Host
	if sample.Postgres != nil {
		inventory["postgresVersion"] = sample.Postgres.ServerVersion
		inventory["replicationSlotsStatus"] = sample.Postgres.ReplicationSlotsStatus
		inventory["logicalWalStatus"] = sample.Postgres.LogicalWALStatus
		inventory["standbyStatus"] = sample.Postgres.StandbyStatus
	}

	if *evidenceDir != "" {
		if err := state.WriteJSON(filepath.Join(*evidenceDir, "infrastructure-inventory.json"), inventory); err != nil {
			return fmt.Errorf("write infrastructure-inventory.json: %w", err)
		}
	}
	b, _ := json.MarshalIndent(inventory, "", "  ")
	fmt.Println(string(b))

	if !sample.HealthReadyOK {
		return fmt.Errorf("staging Ruxbase /api/health?ready not OK (status=%d)", sample.HealthReadyStatus)
	}
	if sample.Host.DiskFreePct > 0 && sample.Host.DiskFreePct < 20.0 {
		return fmt.Errorf("insufficient disk headroom: %.2f%% free (< 20%% required before launch)", sample.Host.DiskFreePct)
	}
	return nil
}

func cmdWorker(args []string) error {
	fs := flag.NewFlagSet("worker", flag.ExitOnError)
	dbURL := fs.String("db", os.Getenv("DATABASE_URL"), "Restricted ruxbase_app PostgreSQL DSN")
	dataDir := fs.String("data-dir", "/app/ruxbase_data", "Shared Ruxbase storage data directory")
	evidenceDir := fs.String("evidence-dir", "/srv/ruxbase-soak/evidence", "Evidence directory")
	campaignID := fs.String("campaign-id", "", "Active campaign identifier (required)")
	workers := fs.Int("workers", 6, "Number of concurrent durable job workers")
	metricsAddr := fs.String("metrics-addr", ":9109", "Worker Prometheus metrics & /healthz listen address")
	_ = fs.Parse(args)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return journeys.RunCoreWorker(ctx, journeys.CoreWorkerConfig{
		DatabaseURL: *dbURL,
		DataDir:     *dataDir,
		EvidenceDir: *evidenceDir,
		CampaignID:  *campaignID,
		Workers:     *workers,
		MetricsAddr: *metricsAddr,
	})
}

func cmdFreeze(args []string) error {
	fs := flag.NewFlagSet("freeze", flag.ExitOnError)
	campaignID := fs.String("campaign-id", "", "Official campaign ID (default W11-05-168H-<UTC>)")
	evidenceDir := fs.String("evidence-dir", "/srv/ruxbase-soak/evidence", "Evidence root directory")
	repoDir := fs.String("repo-dir", ".", "Git repository checkout path")
	binaryPath := fs.String("binary", "", "Path to candidate Linux ruxbase binary for SHA-256")
	deployedContainer := fs.String("deployed-container", "ruxbase-soak-app", "Deployed Ruxbase container name to verify live binary SHA-256")
	deployedBinaryPath := fs.String("deployed-binary-path", "/usr/local/bin/ruxbase", "Binary path inside deployed container")
	pgContainer := fs.String("postgres-container", "ruxbase-soak-postgres", "PostgreSQL container fallback when --monitor-db is not reachable from host")
	pgDatabase := fs.String("postgres-db", "ruxbase_staging", "PostgreSQL database name for container fallback")
	monitorDSN := fs.String("monitor-db", "", "PostgreSQL monitoring DSN")
	capacityRPS := fs.Float64("capacity-rps", 40.0, "Pilot-qualified sustainable capacity RPS")
	pilotDir := fs.String("pilot-dir", "", "Optional completed pilot evidence dir; refuses freeze unless pilot reconciled PASS")
	allowUnisolated := fs.Bool("allow-unisolated", false, "Record isolation note without failing freeze (for user-authorized dev tests only)")
	_ = fs.Parse(args)

	if *pilotDir != "" {
		pRep, err := verifier.Reconcile(*pilotDir, 0.98)
		if err != nil {
			return fmt.Errorf("evaluate prerequisite pilot %s: %w", *pilotDir, err)
		}
		if pRep.Verdict != "PASS" {
			return fmt.Errorf("REFUSING FREEZE: prerequisite pilot %s verdict is %s (failures=%v missing=%v)", *pilotDir, pRep.Verdict, pRep.FailureReasons, pRep.MissingEvidence)
		}
	}

	cid := *campaignID
	if cid == "" {
		cid = "W11-05-168H-" + time.Now().UTC().Format("20060102T150405Z")
	}
	campDir := filepath.Join(*evidenceDir, cid)
	if _, err := os.Stat(filepath.Join(campDir, "state.json")); err == nil {
		return fmt.Errorf("REFUSING FREEZE: campaign %s already exists in %s", cid, campDir)
	}
	if err := os.MkdirAll(campDir, 0o750); err != nil {
		return err
	}

	gitOut := func(gitArgs ...string) string {
		cmd := exec.Command("git", append([]string{"-C", *repoDir}, gitArgs...)...)
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}

	manifest := state.CandidateManifest{
		CampaignID:              cid,
		CreatedAtUTC:            time.Now().UTC().Format(time.RFC3339),
		GitCommitSHA:            gitOut("rev-parse", "HEAD"),
		GitTreeSHA:              gitOut("rev-parse", "HEAD^{tree}"),
		GoVersion:               runtime.Version(),
		ContainerImageDigests:   map[string]string{},
		ExtensionVersions:       map[string]string{},
		ConfigHashes:            map[string]string{},
		IntegrityVerifierVer:    verifier.VerifierVersion,
		TrafficScheduleCapacity: *capacityRPS,
	}

	if *binaryPath != "" {
		sum, err := state.FileSHA256(*binaryPath)
		if err != nil {
			return fmt.Errorf("hash candidate binary %s: %w", *binaryPath, err)
		}
		manifest.LinuxBinarySHA256 = sum
	}

	// Verify live deployed container binary SHA-256 and container image digests
	if *deployedContainer != "" {
		shaOut, err := exec.Command("docker", "exec", *deployedContainer, "sha256sum", *deployedBinaryPath).Output()
		if err == nil {
			fields := strings.Fields(string(shaOut))
			if len(fields) >= 1 {
				manifest.DeployedBinarySHA256 = fields[0]
				manifest.DeployedBinaryVerified = manifest.LinuxBinarySHA256 != "" && manifest.DeployedBinarySHA256 == manifest.LinuxBinarySHA256
			}
		}
		if manifest.LinuxBinarySHA256 != "" && manifest.DeployedBinarySHA256 != "" && !manifest.DeployedBinaryVerified {
			return fmt.Errorf("REFUSING FREEZE: deployed container binary %s != candidate binary %s", manifest.DeployedBinarySHA256, manifest.LinuxBinarySHA256)
		}
		for _, cname := range []string{"ruxbase-soak-app", "ruxbase-soak-postgres", "ruxbase-soak-worker", "ruxbase-soak-prometheus", "ruxbase-soak-grafana"} {
			if idOut, iErr := exec.Command("docker", "inspect", "-f", "{{.Image}}", cname).Output(); iErr == nil {
				manifest.ContainerImageDigests[cname] = strings.TrimSpace(string(idOut))
			}
		}
	}

	// Verify host isolation status (fail-closed when docker ps fails unless --allow-unisolated)
	psOut, err := exec.Command("docker", "ps", "--format", "{{.Names}}").Output()
	if err != nil {
		manifest.IsolationPassed = false
		manifest.IsolationNote = fmt.Sprintf("docker ps failed: %v", err)
		if !*allowUnisolated {
			return fmt.Errorf("staging host isolation check failed (docker ps): %w", err)
		}
	} else {
		names := strings.Split(strings.TrimSpace(string(psOut)), "\n")
		if isoErr := safety.CheckHostIsolation(names, "ruxbase-soak-"); isoErr != nil {
			manifest.IsolationPassed = false
			manifest.IsolationNote = isoErr.Error()
			if !*allowUnisolated {
				return isoErr
			}
		} else {
			manifest.IsolationPassed = true
		}
	}

	for _, rel := range []string{
		"bench/soak/deploy/docker-compose.staging.yml",
		"bench/soak/deploy/postgres.conf",
		"bench/soak/deploy/init-staging-roles.sql",
		"bench/soak/monitoring/prometheus.yml",
		"bench/soak/monitoring/alerts.yml",
	} {
		if sum, err := state.FileSHA256(filepath.Join(*repoDir, rel)); err == nil {
			manifest.ConfigHashes[rel] = sum
		}
	}

	if *monitorDSN != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		pool, err := pgxpool.New(ctx, *monitorDSN)
		if err != nil {
			return fmt.Errorf("connect monitor DSN during freeze: %w", err)
		}
		defer pool.Close()
		if err := pool.QueryRow(ctx, "SHOW server_version").Scan(&manifest.PostgresVersion); err != nil {
			return fmt.Errorf("query PG server_version during freeze: %w", err)
		}
		if rows, err := pool.Query(ctx, "SELECT extname, extversion FROM pg_extension ORDER BY extname"); err == nil {
			for rows.Next() {
				var name, ver string
				if rows.Scan(&name, &ver) == nil {
					manifest.ExtensionVersions[name] = ver
				}
			}
			rows.Close()
		}
		var migCount int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM _migrations").Scan(&migCount); err != nil {
			return fmt.Errorf("query _migrations during freeze: %w", err)
		}
		manifest.AppliedMigrationsCount = migCount
	} else if *pgContainer != "" {
		psql := func(q string) string {
			out, err := exec.Command("docker", "exec", *pgContainer, "psql", "-U", "postgres", "-d", *pgDatabase, "-Atc", q).Output()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		}
		manifest.PostgresVersion = psql("SHOW server_version;")
		for _, line := range strings.Split(psql("SELECT extname || '=' || extversion FROM pg_extension ORDER BY extname;"), "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
				manifest.ExtensionVersions[k] = v
			}
		}
		if migStr := psql("SELECT count(*) FROM _migrations;"); migStr != "" {
			_, _ = fmt.Sscanf(migStr, "%d", &manifest.AppliedMigrationsCount)
		}
	}

	outPath := filepath.Join(campDir, "candidate-manifest.json")
	if err := state.WriteJSON(outPath, manifest); err != nil {
		return err
	}
	fmt.Println(outPath)
	return nil
}

func cmdRunCampaign(args []string, isPilot bool) error {
	mode := "soak"
	defaultDur := 168 * time.Hour
	if isPilot {
		mode = "pilot"
		defaultDur = 60 * time.Minute
	}

	fs := flag.NewFlagSet(mode, flag.ExitOnError)
	campaignID := fs.String("campaign-id", "", "Campaign ID")
	baseURL := fs.String("base-url", "http://127.0.0.1:18090", "Staging Ruxbase base URL")
	dbURL := fs.String("db", "", "Staging restricted app PostgreSQL DSN (ruxbase_app, used for EXPLAIN and integrity reconciliation)")
	monitorDSN := fs.String("monitor-db", "", "Staging monitor PostgreSQL DSN (ruxbase_monitor, used for pg_stat_* telemetry)")
	dataDir := fs.String("data-dir", "/app/ruxbase_data", "Ruxbase data directory (for disk telemetry)")
	evidenceRoot := fs.String("evidence-dir", "/srv/ruxbase-soak/evidence", "Persistent evidence root directory")
	superEmail := fs.String("superuser-email", "", "Staging superuser email")
	superPass := fs.String("superuser-password", "", "Staging superuser password")
	promTokenFile := fs.String("prom-token-file", "", "Optional Prometheus bearer token file to refresh alongside JWT rotation")
	capacityRPS := fs.Float64("capacity-rps", 40.0, "Pilot-qualified sustainable capacity RPS")
	duration := fs.Duration("duration", defaultDur, "Target consecutive wall-clock campaign duration")
	sampleInterval := fs.Duration("sample-interval", 60*time.Second, "Telemetry collection interval")
	verifyInterval := fs.Duration("verify-interval", 15*time.Minute, "Periodic independent integrity verification interval")
	sseClients := fs.Int("sse-clients", 12, "Persistent multi-tenant SSE subscribers")
	tenants := fs.Int("tenants", journeys.DefaultTenantCount, "Number of synthetic tenants")
	metricsAddr := fs.String("metrics-addr", ":9108", "Soak engine Prometheus /metrics & /healthz address")
	_ = fs.Parse(args)

	if isPilot && *duration < 60*time.Minute {
		return fmt.Errorf("pilot minimum duration is 60m per Phase 6 requirements (got %s)", *duration)
	}
	if !isPilot && *duration < 168*time.Hour {
		return fmt.Errorf("official W11-05 soak minimum duration is 168h per Phase 7 requirements (got %s)", *duration)
	}
	if *superEmail == "" || *superPass == "" {
		return errors.New("--superuser-email and --superuser-password are required")
	}
	if *dbURL == "" {
		return errors.New("--db (non-superuser ruxbase_app DSN) is required for independent integrity reconciliation")
	}
	cid := *campaignID
	if cid == "" {
		prefix := "W11-05-168H-"
		if isPilot {
			prefix = "W11-05-PILOT-"
		}
		cid = prefix + time.Now().UTC().Format("20060102T150405Z")
	}

	schedCfg := schedule.DefaultConfig(*capacityRPS)
	if err := schedCfg.Validate(); err != nil {
		return fmt.Errorf("invalid traffic schedule: %w", err)
	}

	campDir := filepath.Join(*evidenceRoot, cid)
	store, err := state.Open(campDir, cid, mode, *duration)
	if err != nil {
		return err
	}
	if err := state.WriteJSON(filepath.Join(campDir, "schedule.json"), schedCfg); err != nil {
		return fmt.Errorf("write schedule.json: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if *metricsAddr != "" {
		srv := &http.Server{Addr: *metricsAddr, Handler: store.MetricsHandler(), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			<-ctx.Done()
			shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shCtx)
		}()
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("[soak] metrics server error: %v", err)
			}
		}()
	}

	client := journeys.NewHTTPClient(15 * time.Second)
	fixtures, err := journeys.EnsureFixtures(ctx, client, *baseURL, *superEmail, *superPass, *tenants)
	if err != nil {
		return fmt.Errorf("initialize staging fixtures: %w", err)
	}
	syncPromTokenFile(*promTokenFile, fixtures.SuperToken())

	// Telemetry collector uses ruxbase_monitor when configured, falling back to dbURL.
	monDSN := *monitorDSN
	if monDSN == "" {
		monDSN = *dbURL
	}
	cp := store.Snapshot()
	col, err := telemetry.NewCollector(ctx, client, *baseURL, fixtures.SuperToken(), monDSN, *dataDir, cp.OfficialStartUTC)
	if err != nil {
		return fmt.Errorf("initialize telemetry collector: %w", err)
	}
	defer col.Close()

	// Verifier pool MUST connect via --db (non-superuser ruxbase_app table owner), NEVER ruxbase_monitor,
	// so RLS-protected tables are reconcilable while still verifying strict-RLS via ruxbase_api.
	verifyPool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		return fmt.Errorf("open verifier PostgreSQL pool: %w", err)
	}
	defer verifyPool.Close()

	remaining := time.Until(cp.EarliestValidEndUTC)
	if remaining <= 0 {
		log.Printf("[soak] campaign %s already reached earliest valid end %s", cid, cp.EarliestValidEndUTC.Format(time.RFC3339))
		return nil
	}
	runCtx, runCancel := context.WithTimeout(ctx, remaining)
	defer runCancel()

	runner := journeys.NewRunner(client, *baseURL, fixtures, store)
	ssePool := journeys.NewSSEPool(*baseURL, fixtures, *sseClients)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ssePool.Run(runCtx)
	}()

	var winAttempt, winOK, winHTTPAtt, winHTTPOK, winMutAtt, winMutCom, winNoOp, winLedgerErr, winAppErr, winGenErr, winTimeout, winCTChecks, winCTLeaks, winIntegErr atomic.Int64
	var latMu sync.Mutex
	var winLats []time.Duration

	// Open-loop diurnal arrival-rate workload pacer
	wg.Add(1)
	go func() {
		defer wg.Done()
		rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 42))
		sem := make(chan struct{}, max(int(*capacityRPS*4), 64))

		for runCtx.Err() == nil {
			tgt := schedCfg.Evaluate(time.Now().UTC(), rng)
			_ = store.Update(func(c *state.Checkpoint) {
				c.CurrentTargetRPS = tgt.TargetRPS
				c.CurrentWindow = tgt.WindowName
			})

			interval := time.Duration(float64(time.Second) / tgt.TargetRPS)
			sliceEnd := time.Now().Add(30 * time.Second)
			scheduled := time.Now()
			for runCtx.Err() == nil && time.Now().Before(sliceEnd) {
				scheduled = scheduled.Add(interval)
				if wait := time.Until(scheduled); wait > 0 {
					select {
					case <-runCtx.Done():
						return
					case <-time.After(wait):
					}
				}
				select {
				case sem <- struct{}{}:
					schedTime := scheduled
					seed := rng.Uint64()
					writeRatio := tgt.WriteRatio
					wg.Add(1)
					go func() {
						defer wg.Done()
						defer func() { <-sem }()
						localRng := rand.New(rand.NewPCG(seed, 99))
						out := runner.ExecuteMixed(runCtx, localRng, writeRatio)
						coLatency := time.Since(schedTime)
						winAttempt.Add(1)
						winHTTPAtt.Add(int64(out.HTTPAttempted))
						winHTTPOK.Add(int64(out.HTTPSucceeded))
						winMutAtt.Add(int64(out.MutationsAttempted))
						winMutCom.Add(int64(out.MutationsCommitted))
						winNoOp.Add(int64(out.NoOpSkipped))
						if out.LedgerPersistErr {
							winLedgerErr.Add(1)
						}
						if out.CrossTenantChk {
							winCTChecks.Add(1)
						}
						if out.CrossTenantLk {
							winCTLeaks.Add(1)
						}
						if out.IntegrityErr {
							winIntegErr.Add(1)
						}
						if out.Timeout {
							winTimeout.Add(1)
						}
						switch {
						case out.Err == nil && !out.AppFailure && !out.GenFailure && !out.CrossTenantLk && !out.IntegrityErr:
							winOK.Add(1)
							latMu.Lock()
							if len(winLats) < 50000 {
								winLats = append(winLats, coLatency)
							}
							latMu.Unlock()
						case out.GenFailure:
							winGenErr.Add(1)
						default:
							winAppErr.Add(1)
						}
					}()
				default:
					winAttempt.Add(1)
					winGenErr.Add(1)
					winTimeout.Add(1)
				}
			}
		}
	}()

	thresholds := safety.DefaultThresholds()
	wg.Add(1)
	go func() {
		defer wg.Done()
		tk := time.NewTicker(*sampleInterval)
		refreshTk := time.NewTicker(4 * time.Hour)
		verifyTk := time.NewTicker(*verifyInterval)
		defer tk.Stop()
		defer refreshTk.Stop()
		defer verifyTk.Stop()

		consecUnavail := 0
		tickNum := 0

		runAndRecordIntegrity := func(full bool) bool {
			rep := verifier.RunIntegrityCheck(runCtx, verifier.CheckOptions{
				Client:        client,
				BaseURL:       *baseURL,
				SuperToken:    fixtures.SuperToken(),
				Fixtures:      fixtures,
				Pool:          verifyPool,
				EvidenceDir:   campDir,
				CampaignID:    cid,
				FullReconcile: full,
			})
			if appendErr := store.AppendJSONL("integrity.jsonl", rep); appendErr != nil {
				triggerEmergencyStop(store, runCancel, fmt.Sprintf("integrity_ledger_write_error: %v", appendErr))
				return false
			}
			if !rep.Passed {
				_ = store.AppendAlert(state.AlertEvent{
					TimestampUTC: rep.TimestampUTC,
					Severity:     "critical",
					Code:         "integrity_verification_failed",
					Component:    "verifier",
					Message:      strings.Join(rep.Failures, "; "),
				})
				_ = store.Update(func(c *state.Checkpoint) {
					c.IntegrityViolations += int64(len(rep.Failures))
				})
				triggerEmergencyStop(store, runCancel, "integrity_verification_failed: "+strings.Join(rep.Failures, "; "))
				return false
			}
			if plan, err := journeys.RunExplainProbe(runCtx, verifyPool, "soak_tenant_00"); err == nil && len(plan) > 0 {
				_ = store.AppendJSONL("explain.jsonl", map[string]any{
					"timestampUtc": time.Now().UTC().Format(time.RFC3339),
					"plan":         plan,
				})
			}
			return true
		}

		for {
			select {
			case <-runCtx.Done():
				// Perform a final pre-shutdown integrity check if campaign exited cleanly
				if !store.Snapshot().EmergencyStopped {
					finCtx, finCancel := context.WithTimeout(context.Background(), 30*time.Second)
					rep := verifier.RunIntegrityCheck(finCtx, verifier.CheckOptions{
						Client:        client,
						BaseURL:       *baseURL,
						SuperToken:    fixtures.SuperToken(),
						Fixtures:      fixtures,
						Pool:          verifyPool,
						EvidenceDir:   campDir,
						CampaignID:    cid,
						FullReconcile: true,
					})
					finCancel()
					_ = store.AppendJSONL("integrity.jsonl", rep)
					if !rep.Passed {
						_ = store.Update(func(c *state.Checkpoint) {
							c.IntegrityViolations += int64(len(rep.Failures))
						})
					}
				}
				return
			case <-refreshTk.C:
				if rErr := fixtures.RefreshTokens(runCtx, client, *baseURL); rErr != nil {
					log.Printf("[%s] token refresh warning: %v", mode, rErr)
				} else {
					col.SetSuperuserToken(fixtures.SuperToken())
					syncPromTokenFile(*promTokenFile, fixtures.SuperToken())
				}
			case <-verifyTk.C:
				if !runAndRecordIntegrity(false) {
					return
				}
			case <-tk.C:
				tickNum++
				att := winAttempt.Swap(0)
				ok := winOK.Swap(0)
				httpAtt := winHTTPAtt.Swap(0)
				httpOK := winHTTPOK.Swap(0)
				mutAtt := winMutAtt.Swap(0)
				mutCom := winMutCom.Swap(0)
				noOp := winNoOp.Swap(0)
				lErr := winLedgerErr.Swap(0)
				appErr := winAppErr.Swap(0)
				genErr := winGenErr.Swap(0)
				to := winTimeout.Swap(0)
				ctChk := winCTChecks.Swap(0)
				ctLk := winCTLeaks.Swap(0) + ssePool.Leaks.Swap(0)
				intErr := winIntegErr.Swap(0)

				latMu.Lock()
				lats := winLats
				winLats = nil
				latMu.Unlock()

				p50 := journeys.PercentileMs(lats, 50)
				p95 := journeys.PercentileMs(lats, 95)
				p99 := journeys.PercentileMs(lats, 99)
				achievedRPS := float64(ok) / sampleInterval.Seconds()

				sample := col.Collect(runCtx)
				if err := store.AppendJSONL("metrics.jsonl", sample); err != nil {
					triggerEmergencyStop(store, runCancel, fmt.Sprintf("metrics_write_error: %v", err))
					return
				}
				if err := store.AppendJSONL("workload.jsonl", map[string]any{
					"timestampUtc":          sample.TimestampUTC,
					"attempted":             att,
					"success":               ok,
					"httpRequestsAttempted": httpAtt,
					"httpRequestsSuccess":   httpOK,
					"mutationsAttempted":    mutAtt,
					"mutationsCommitted":    mutCom,
					"noOpSkipped":           noOp,
					"ledgerPersistErrors":   lErr,
					"failedApp":             appErr,
					"failedGen":             genErr,
					"timeouts":              to,
					"achievedRps":           achievedRPS,
					"p50Ms":                 p50,
					"p95Ms":                 p95,
					"p99Ms":                 p99,
					"activeSseClients":      ssePool.ActiveClients.Load(),
					"sseEventsRecv":         ssePool.EventsRecv.Load(),
					"sseReconnects":         ssePool.Reconnects.Load(),
					"crossTenantLeaks":      ctLk,
				}); err != nil {
					triggerEmergencyStop(store, runCancel, fmt.Sprintf("workload_write_error: %v", err))
					return
				}

				if !sample.HealthReadyOK {
					consecUnavail++
				} else {
					consecUnavail = 0
				}

				bStatus, bTS := state.LatestBackupStatus(campDir, cid)
				if err := store.Update(func(c *state.Checkpoint) {
					c.AttemptedOps += att
					c.SuccessfulOps += ok
					c.HTTPRequestsAttempted += httpAtt
					c.HTTPRequestsSucceeded += httpOK
					c.MutationsAttempted += mutAtt
					c.MutationsCommitted += mutCom
					c.JourneysCompleted += ok
					c.NoOpSkipped += noOp
					c.FailedAppOps += appErr
					c.FailedGenOps += genErr
					c.TimedOutOps += to
					c.CrossTenantChecks += ctChk
					c.CrossTenantLeaks += ctLk
					c.IntegrityViolations += intErr
					c.CurrentAchievedRPS = achievedRPS
					c.LastP50Ms = p50
					c.LastP95Ms = p95
					c.LastP99Ms = p99
					c.ActiveSSEClients = int(ssePool.ActiveClients.Load())
					if bStatus != "" {
						c.LastBackupStatus = bStatus
						c.LastBackupUTC = bTS
					}
				}); err != nil {
					triggerEmergencyStop(store, runCancel, fmt.Sprintf("state_checkpoint_write_error: %v", err))
					return
				}

				if lErr > 0 {
					triggerEmergencyStop(store, runCancel, fmt.Sprintf("durable_ledger_persistence_failure: %d write error(s)", lErr))
					return
				}

				errRate := 0.0
				if att > 0 {
					errRate = float64(appErr) * 100.0 / float64(att)
				}
				var walRetained int64
				if sample.Postgres != nil {
					walRetained = sample.Postgres.MaxRetainedWALBytes
				}
				dec := safety.Evaluate(thresholds, safety.Observation{
					CrossTenantLeaks:       ctLk,
					StorageChecksumErrors:  intErr,
					DiskFreePct:            sample.Host.DiskFreePct,
					RetainedWALBytes:       walRetained,
					ConsecutiveUnavailTick: consecUnavail,
					WindowErrorRatePct:     errRate,
					WindowTotalOps:         att,
				})
				if dec.Stop {
					_ = store.AppendAlert(state.AlertEvent{
						TimestampUTC: sample.TimestampUTC,
						Severity:     dec.Severity,
						Code:         dec.Code,
						Component:    "safety_controller",
						Message:      dec.Reason,
					})
					triggerEmergencyStop(store, runCancel, dec.Reason)
					return
				}

				log.Printf("[%s] tick=%d rps=%.1f http=%d/%d mut=%d/%d p95=%.1fms p99=%.1fms appErr=%d genErr=%d sse=%d ready=%v diskFree=%.1f%%",
					mode, tickNum, achievedRPS, httpOK, httpAtt, mutCom, mutAtt, p95, p99, appErr, genErr, ssePool.ActiveClients.Load(), sample.HealthReadyOK, sample.Host.DiskFreePct)
			}
		}
	}()

	wg.Wait()

	finalCP := store.Snapshot()
	if !finalCP.EmergencyStopped {
		if time.Now().UTC().After(finalCP.EarliestValidEndUTC) || time.Now().UTC().Equal(finalCP.EarliestValidEndUTC) {
			nextStatus := state.StatusSoakCompletedPendingAudit
			if isPilot {
				nextStatus = state.StatusPilotPassed
			}
			_ = store.Update(func(c *state.Checkpoint) {
				c.Status = nextStatus
			})
		}
	}
	return nil
}

func syncPromTokenFile(path, token string) {
	if path == "" || token == "" {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(token), 0o644); err == nil {
		_ = os.Rename(tmp, path)
	}
}

func triggerEmergencyStop(store *state.Store, cancel context.CancelFunc, reason string) {
	log.Printf("[EMERGENCY STOP] %s", reason)
	_ = store.AppendInterruption(state.InterruptionEvent{
		TimestampUTC:  time.Now().UTC().Format(time.RFC3339),
		Kind:          "emergency_stop",
		Detail:        reason,
		Controlled:    false,
		Disqualifying: true,
	})
	_ = store.Update(func(c *state.Checkpoint) {
		c.EmergencyStopped = true
		c.EmergencyStopReason = reason
		if c.Mode == "pilot" {
			c.Status = state.StatusPilotFailed
		} else {
			c.Status = state.StatusFailed
		}
	})
	cancel()
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	baseURL := fs.String("base-url", "http://127.0.0.1:18090", "Staging Ruxbase base URL")
	dbDSN := fs.String("db", "", "PostgreSQL DSN (ruxbase_app non-superuser table owner)")
	evidenceDir := fs.String("evidence-dir", "", "Campaign evidence directory")
	campaignID := fs.String("campaign-id", "", "Expected campaign ID")
	superEmail := fs.String("superuser-email", "", "Superuser email")
	superPass := fs.String("superuser-password", "", "Superuser password")
	full := fs.Bool("full", false, "Reconcile 100% of ledger entries")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := journeys.NewHTTPClient(15 * time.Second)
	token := ""
	if *superEmail != "" && *superPass != "" {
		var err error
		token, err = journeys.AuthSuperuser(ctx, client, *baseURL, *superEmail, *superPass)
		if err != nil {
			return err
		}
	}
	var pool *pgxpool.Pool
	if *dbDSN != "" {
		var err error
		pool, err = pgxpool.New(ctx, *dbDSN)
		if err != nil {
			return err
		}
		defer pool.Close()
	}
	rep := verifier.RunIntegrityCheck(ctx, verifier.CheckOptions{
		Client:        client,
		BaseURL:       *baseURL,
		SuperToken:    token,
		Pool:          pool,
		EvidenceDir:   *evidenceDir,
		CampaignID:    *campaignID,
		FullReconcile: *full,
	})
	b, _ := json.MarshalIndent(rep, "", "  ")
	fmt.Println(string(b))
	if !rep.Passed {
		return fmt.Errorf("integrity check failed (%d failures, incomplete=%v)", len(rep.Failures), rep.VerificationIncomplete)
	}
	return nil
}

func cmdReconcile(args []string) error {
	fs := flag.NewFlagSet("reconcile", flag.ExitOnError)
	evidenceDir := fs.String("evidence-dir", "", "Campaign evidence directory")
	requiredHours := fs.Float64("required-hours", 168.0, "Required consecutive wall-clock hours")
	_ = fs.Parse(args)
	if *evidenceDir == "" {
		return errors.New("--evidence-dir is required")
	}

	rep, err := verifier.Reconcile(*evidenceDir, *requiredHours)
	if err != nil {
		return err
	}
	outPath := filepath.Join(*evidenceDir, "final-reconciliation.json")
	if err := state.WriteJSON(outPath, rep); err != nil {
		return err
	}
	// IMPORTANT: WriteSHA256Manifest MUST be the final write inside evidenceDir so
	// SHA256SUMS.txt matches final-reconciliation.json and all artifacts identically.
	manifestPath, count, err := state.WriteSHA256Manifest(*evidenceDir)
	if err != nil {
		return err
	}
	verified, mismatches, _, vErr := state.VerifySHA256Manifest(*evidenceDir)
	if vErr != nil || !verified {
		return fmt.Errorf("post-write SHA-256 manifest verification failed: err=%v mismatches=%v", vErr, mismatches)
	}

	b, _ := json.MarshalIndent(map[string]any{
		"reconciliation":      rep,
		"sha256ManifestPath":  manifestPath,
		"sha256ManifestFiles": count,
		"manifestVerified":    verified,
	}, "", "  ")
	fmt.Println(string(b))
	if rep.Verdict != "PASS" {
		return fmt.Errorf("campaign reconciliation verdict: %s", rep.Verdict)
	}
	return nil
}

func cmdAudit(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	evidenceDir := fs.String("evidence-dir", "", "Frozen campaign evidence directory (read-only)")
	requiredHours := fs.Float64("required-hours", 168.0, "Required consecutive wall-clock hours")
	_ = fs.Parse(args)
	if *evidenceDir == "" {
		return errors.New("--evidence-dir is required")
	}

	verdict, err := verifier.AuditCampaign(*evidenceDir, *requiredHours)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(verdict, "", "  ")
	fmt.Println(string(b))
	if verdict.Verdict != "W11-05_VERIFIED" {
		return fmt.Errorf("A11 independent certification verdict: %s", verdict.Verdict)
	}
	return nil
}
