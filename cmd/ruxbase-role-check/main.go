// Command ruxbase-role-check inspects a proposed serving identity without
// bootstrapping Ruxbase, modifying its schema, or changing database grants.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/AbdullahZaher/ruxbase/internal/pgrolecheck"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func run() error {
	dsn := os.Getenv("RUXBASE_ROLE_CHECK_URL")
	if dsn == "" {
		return errors.New("set RUXBASE_ROLE_CHECK_URL to an authorized database connection")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return errors.New("unable to open PostgreSQL connection")
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	contract := pgrolecheck.IdentityContract{
		Runtime:      os.Getenv("RUXBASE_ROLE_CHECK_RUNTIME"),
		Migration:    os.Getenv("RUXBASE_ROLE_CHECK_MIGRATION"),
		Provisioning: os.Getenv("RUXBASE_ROLE_CHECK_PROVISIONING"),
		Replication:  os.Getenv("RUXBASE_ROLE_CHECK_REPLICATION"),
	}
	var report pgrolecheck.Report
	if contract != (pgrolecheck.IdentityContract{}) {
		report, err = contract.AdmitRuntime(ctx, db)
	} else {
		report, err = pgrolecheck.Inspect(ctx, db)
	}
	if err != nil {
		return err
	}

	findings := report.Findings()
	var drift *pgrolecheck.GrantDrift
	var scopeErr error
	if path := os.Getenv("RUXBASE_ROLE_CHECK_GRANTS_FILE"); path != "" {
		if contract.Runtime == "" {
			return errors.New("scoped grants require an explicit runtime identity contract")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return errors.New("unable to read scoped grant contract")
		}
		var grants pgrolecheck.GrantContract
		if err := json.Unmarshal(raw, &grants); err != nil {
			return errors.New("invalid scoped grant contract")
		}
		if grants.Role != contract.Runtime {
			return errors.New("scoped grant role does not match runtime contract")
		}
		result, err := grants.Check(ctx, db)
		drift = &result
		scopeErr = err
	}
	output := struct {
		Authority  pgrolecheck.Report      `json:"authority"`
		Findings   []string                `json:"findings"`
		GrantDrift *pgrolecheck.GrantDrift `json:"grantDrift,omitempty"`
		ScreenOK   bool                    `json:"screenOK"`
	}{report, findings, drift, len(findings) == 0 && scopeErr == nil}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		return err
	}
	if scopeErr != nil {
		return scopeErr
	}
	if !output.ScreenOK {
		return errors.New("proposed runtime identity failed initial authority screen")
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
