// Command stress is the Ruxbase Enterprise Stress Testing, Scalability Limits
// & Independent Performance Certification driver (Tests A through H + Integrity).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

func main() {
	log.SetOutput(os.Stderr)

	var (
		scenario       = flag.String("scenario", "", "setup, test-a, test-b, test-c, test-d, test-e, test-f-http, test-g, test-h, verify, ops-dump")
		urlsFlag       = flag.String("url", "http://ruxbase:8090", "comma-separated Ruxbase base URL(s)")
		pgAdminURL     = flag.String("pg-admin-url", "", "PostgreSQL diagnostic/seeding superuser DSN (never used by Ruxbase)")
		pgAppURL       = flag.String("pg-app-url", "", "PostgreSQL restricted non-superuser ruxbase_app DSN (for Test C)")
		email          = flag.String("email", "stress-admin@example.com", "superuser email")
		password       = flag.String("password", "StressAdminPass!2026", "superuser password")
		stateFile      = flag.String("state", "/tmp/stress-state.json", "fixture state JSON path")
		records        = flag.Int("records", 10000, "initial stress_items record count")
		vectors        = flag.Int("vectors", 5000, "initial stress_vectors count (384-dim HNSW)")
		rlsStrict      = flag.Bool("rls-strict", false, "metadata tag indicating RUXBASE_RLS_STRICT=true on server")
		poolMax        = flag.Int("pool-max", 20, "metadata tag indicating RUXBASE_DB_MAX_CONNS on server")
		stages         = flag.String("stages", "10,50,100,250,500,1000,2000,5000", "Test A closed-loop VU stages")
		openRates      = flag.String("open-rates", "250,500,1000,2000,3500,5000", "Test A open-loop offered req/s rates")
		repeatCSV      = flag.String("repeat-stages", "50,100,250,500", "Test A VU stages repeated 3x")
		sizesCSV       = flag.String("sizes", "10000,100000,1000000", "Test C dataset sizes")
		concurrency    = flag.Int("c", 100, "concurrency for Test D / Test H")
		warmup         = flag.Duration("warmup", 4*time.Second, "warmup duration per stage")
		duration       = flag.Duration("d", 12*time.Second, "measurement duration per stage")
		sampleInterval = flag.Duration("sample", 5*time.Second, "sampling interval for Test H")
		clientsCSV     = flag.String("clients", "50,200,500,1000", "Test E SSE client counts")
		events         = flag.Int("events", 100, "Test E realtime events per stage")
		rate           = flag.Int("rate", 20, "Test E realtime event rate/sec")
		authMode       = flag.Bool("auth-sse", false, "Test E use authenticated RLS SSE subscriptions instead of public")
		apiDur         = flag.Duration("api-dur", 12*time.Second, "Test E horizontal API scaling duration (0 skips)")
		subMode        = flag.String("submode", "spikes", "Test G submode: spikes or chaos-probe")
		label          = flag.String("label", "post", "label for verify or ops-dump")
	)
	flag.Parse()

	ctx := context.Background()
	urls := strings.Split(*urlsFlag, ",")
	for i := range urls {
		urls[i] = strings.TrimRight(strings.TrimSpace(urls[i]), "/")
	}

	pg, err := openPGTelemetry(ctx, *pgAdminURL)
	if err != nil {
		log.Fatalf("open pg telemetry: %v", err)
	}
	defer pg.Close()

	httpClient := newStressHTTPClient(15 * time.Second)

	switch *scenario {
	case "setup":
		st, err := setupStressFixtures(ctx, httpClient, pg, urls[0], *email, *password, *records, *vectors, *stateFile)
		if err != nil {
			log.Fatalf("setup failed: %v", err)
		}
		emitJSONL(map[string]any{
			"timestamp":      time.Now().UTC().Format(time.RFC3339),
			"testId":         "SETUP",
			"tenants":        len(st.Tenants),
			"initialItems":   st.InitialItems,
			"initialVectors": st.InitialVectors,
			"rlsApplied":     st.RLSApplied,
		})

	case "test-a":
		st, err := loadFixtureState(*stateFile)
		if err != nil {
			log.Fatalf("load state: %v", err)
		}
		repMap := map[int]int{}
		for _, v := range parseIntCSV(*repeatCSV, nil) {
			repMap[v] = 3
		}
		if err := runTestA(ctx, urls, st, pg, *rlsStrict, *poolMax, *stages, *openRates, *warmup, *duration, repMap); err != nil {
			log.Fatalf("test-a failed: %v", err)
		}

	case "test-b":
		st, err := loadFixtureState(*stateFile)
		if err != nil {
			log.Fatalf("load state: %v", err)
		}
		if err := runTestB(ctx, urls, st, pg, *rlsStrict, *poolMax, *duration); err != nil {
			log.Fatalf("test-b failed: %v", err)
		}

	case "test-c":
		st, err := loadFixtureState(*stateFile)
		if err != nil {
			log.Fatalf("load state: %v", err)
		}
		if err := runTestC(ctx, urls[0], st, pg, *pgAppURL, *sizesCSV, *duration); err != nil {
			log.Fatalf("test-c failed: %v", err)
		}

	case "test-d":
		st, err := loadFixtureState(*stateFile)
		if err != nil {
			log.Fatalf("load state: %v", err)
		}
		if err := runTestD(ctx, urls, st, pg, *rlsStrict, *poolMax, *concurrency, *duration); err != nil {
			log.Fatalf("test-d failed: %v", err)
		}

	case "test-e":
		st, err := loadFixtureState(*stateFile)
		if err != nil {
			log.Fatalf("load state: %v", err)
		}
		if err := runTestE(ctx, urls, st, pg, *clientsCSV, *events, *rate, *authMode, *apiDur); err != nil {
			log.Fatalf("test-e failed: %v", err)
		}

	case "test-f-http":
		st, err := loadFixtureState(*stateFile)
		if err != nil {
			log.Fatalf("load state: %v", err)
		}
		if err := runTestFHTTP(ctx, urls[0], st, pg, *duration); err != nil {
			log.Fatalf("test-f-http failed: %v", err)
		}

	case "test-g":
		st, err := loadFixtureState(*stateFile)
		if err != nil {
			log.Fatalf("load state: %v", err)
		}
		if err := runTestG(ctx, urls, st, pg, *subMode, *duration); err != nil {
			log.Fatalf("test-g failed: %v", err)
		}

	case "test-h":
		st, err := loadFixtureState(*stateFile)
		if err != nil {
			log.Fatalf("load state: %v", err)
		}
		if err := runTestH(ctx, urls[0], st, pg, *concurrency, *duration, *sampleInterval); err != nil {
			log.Fatalf("test-h failed: %v", err)
		}

	case "verify":
		st, err := loadFixtureState(*stateFile)
		if err != nil {
			log.Fatalf("load state: %v", err)
		}
		if err := runIntegrityVerification(ctx, urls[0], st, pg, *label); err != nil {
			log.Fatalf("verify failed: %v", err)
		}

	case "ops-dump":
		st, err := loadFixtureState(*stateFile)
		if err != nil {
			log.Fatalf("load state: %v", err)
		}
		emitJSONL(map[string]any{
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"testId":    "OPS-DUMP-" + *label,
			"metrics":   fetchOpsMetrics(httpClient, urls[0], st.SuperuserToken),
			"capacity":  fetchOpsJSON(httpClient, urls[0], "/api/ops/capacity", st.SuperuserToken),
			"alerts":    fetchOpsJSON(httpClient, urls[0], "/api/ops/alerts", st.SuperuserToken),
			"health":    fetchOpsJSON(httpClient, urls[0], "/api/health?ready", st.SuperuserToken),
		})

	default:
		fmt.Fprintf(os.Stderr, "unknown scenario %q\n", *scenario)
		os.Exit(2)
	}
}
