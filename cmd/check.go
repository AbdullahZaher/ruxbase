package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// CheckItem represents a single system diagnostic check.
type CheckItem struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Detail  string `json:"detail"`
	Warning bool   `json:"warning,omitempty"`
}

// CheckReport summarizes the full preflight check execution.
type CheckReport struct {
	Passed bool        `json:"passed"`
	Checks []CheckItem `json:"checks"`
}

// NewCheckCommand returns a new command for performing non-destructive
// diagnostics on the database connection, PostgreSQL version, and schema integrity.
func NewCheckCommand(app core.App) *cobra.Command {
	var jsonOutput bool

	command := &cobra.Command{
		Use:          "check",
		Short:        "Inspect database connectivity, PostgreSQL version, extensions, and migration status",
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			report := CheckReport{
				Passed: true,
				Checks: []CheckItem{},
			}

			// 1. Database Connectivity
			var pingRes int
			err := app.DB().NewQuery("SELECT 1").Row(&pingRes)
			if err != nil {
				report.Passed = false
				report.Checks = append(report.Checks, CheckItem{
					Name:   "Database Connectivity",
					Passed: false,
					Detail: fmt.Sprintf("Failed to query database: %v", err),
				})
			} else {
				report.Checks = append(report.Checks, CheckItem{
					Name:   "Database Connectivity",
					Passed: true,
					Detail: "Connection alive and responsive",
				})

				// 2. PostgreSQL Server Version
				var verNumStr string
				if err := app.DB().NewQuery("SHOW server_version_num").Row(&verNumStr); err == nil {
					verNum, _ := strconv.Atoi(strings.TrimSpace(verNumStr))
					if verNum >= 170000 {
						report.Checks = append(report.Checks, CheckItem{
							Name:   "PostgreSQL Compatibility (>= 17.0)",
							Passed: true,
							Detail: fmt.Sprintf("Server version num is %d (PostgreSQL %d+)", verNum, verNum/10000),
						})
					} else {
						report.Passed = false
						report.Checks = append(report.Checks, CheckItem{
							Name:   "PostgreSQL Compatibility (>= 17.0)",
							Passed: false,
							Detail: fmt.Sprintf("Unsupported PostgreSQL version num %d; PostgreSQL 17 or higher is required", verNum),
						})
					}
				}

				// 3. Vector extension check
				var hasVector int
				if err := app.DB().NewQuery("SELECT COUNT(*) FROM pg_extension WHERE extname = 'vector'").Row(&hasVector); err == nil {
					if hasVector > 0 {
						report.Checks = append(report.Checks, CheckItem{
							Name:   "pgvector Extension",
							Passed: true,
							Detail: "pgvector extension is installed and available",
						})
					} else {
						report.Checks = append(report.Checks, CheckItem{
							Name:    "pgvector Extension",
							Passed:  true,
							Warning: true,
							Detail:  "pgvector extension not enabled in current database (vector search will fall back to exact or disabled)",
						})
					}
				}

				// 4. Migration status
				var appliedCount int
				if err := app.DB().NewQuery("SELECT COUNT(*) FROM _migrations").Row(&appliedCount); err == nil {
					report.Checks = append(report.Checks, CheckItem{
						Name:   "Schema Migrations",
						Passed: true,
						Detail: fmt.Sprintf("%d migrations recorded in _migrations", appliedCount),
					})
				} else {
					report.Checks = append(report.Checks, CheckItem{
						Name:    "Schema Migrations",
						Passed:  true,
						Warning: true,
						Detail:  "_migrations table not initialized yet (run migrate up or serve to initialize)",
					})
				}
			}

			// 5. Data Directory check
			if app.DataDir() != "" {
				report.Checks = append(report.Checks, CheckItem{
					Name:   "Data Directory",
					Passed: true,
					Detail: fmt.Sprintf("Configured data directory: %s", app.DataDir()),
				})
			}

			if jsonOutput {
				bytes, _ := json.MarshalIndent(report, "", "  ")
				fmt.Println(string(bytes))
				if !report.Passed {
					return fmt.Errorf("diagnostic checks failed")
				}
				return nil
			}

			// Console formatted output
			fmt.Println(color.CyanString("Ruxbase System Diagnostics Preflight"))
			fmt.Println(strings.Repeat("-", 50))
			for _, check := range report.Checks {
				if check.Passed && !check.Warning {
					fmt.Printf("%s %s: %s\n", color.GreenString("[PASS]"), check.Name, check.Detail)
				} else if check.Warning {
					fmt.Printf("%s %s: %s\n", color.YellowString("[WARN]"), check.Name, check.Detail)
				} else {
					fmt.Printf("%s %s: %s\n", color.RedString("[FAIL]"), check.Name, check.Detail)
				}
			}
			fmt.Println(strings.Repeat("-", 50))

			if !report.Passed {
				return fmt.Errorf("one or more critical preflight checks failed")
			}

			fmt.Println(color.GreenString("All critical diagnostics passed."))
			return nil
		},
	}

	command.Flags().BoolVar(&jsonOutput, "json", false, "format report as machine-readable JSON")
	return command
}
