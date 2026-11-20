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
	report, err := pgrolecheck.Inspect(ctx, db)
	if err != nil {
		return err
	}

	findings := report.Findings()
	output := struct {
		Authority pgrolecheck.Report `json:"authority"`
		Findings  []string           `json:"findings"`
		ScreenOK  bool               `json:"screenOK"`
	}{report, findings, len(findings) == 0}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		return err
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
