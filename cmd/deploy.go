package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// DeployGuidance describes recommended production deployment practices.
type DeployGuidance struct {
	PlatformRequirements []string          `json:"platformRequirements"`
	SecurityChecklist    []string          `json:"securityChecklist"`
	DatabaseRoles        map[string]string `json:"databaseRoles"`
	RecommendedEnv       map[string]string `json:"recommendedEnv"`
}

// NewDeployCommand returns a command displaying production deployment guidance,
// security preflight advice, and architectural configuration rules.
func NewDeployCommand(app core.App) *cobra.Command {
	var jsonOutput bool

	command := &cobra.Command{
		Use:          "deploy",
		Short:        "Display production deployment guidelines, security preflight, and role separation rules",
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			guidance := DeployGuidance{
				PlatformRequirements: []string{
					"External PostgreSQL 17 or 18 instance with SSL enabled (dev embedded Postgres is for local dev only)",
					"pgvector extension enabled for semantic search and embedding vectors",
					"Linux AMD64 or ARM64 container runtime with non-root execution user (UID 10001)",
					"Reverse proxy (Nginx, Caddy, Traefik, or AWS ALB) terminating TLS with HTTP/2 or HTTP/3",
				},
				SecurityChecklist: []string{
					"Set --dev=false in all production environments",
					"Provide a dedicated 32-character RUXBASE_ENCRYPTION_KEY",
					"Never connect the runtime as superuser 'postgres' (use least-privilege 'ruxbase_app')",
					"Run 'ruxbase-role-check' before admitting new serving instances",
					"Keep backup storage on encrypted S3 or local volume with daily snapshot schedules",
				},
				DatabaseRoles: map[string]string{
					"ruxbase_app":         "Runtime pool identity: DML only (SELECT, INSERT, UPDATE, DELETE); cannot alter schema or grant roles",
					"ruxbase_migrator":    "Offline migration identity: DDL authority (CREATE, ALTER, DROP) executed strictly during deploy gates",
					"ruxbase_provisioner": "Database DBA bootstrap identity: creates database, schemas, and extensions",
				},
				RecommendedEnv: map[string]string{
					"DATABASE_URL":           "postgres://ruxbase_app:***@postgres-host:5432/ruxbase?sslmode=verify-full",
					"RUXBASE_ENCRYPTION_KEY": "<32_character_random_hex_secret>",
					"RUXBASE_MAX_OPEN_CONNS": "50",
					"RUXBASE_MAX_IDLE_CONNS": "10",
				},
			}

			if jsonOutput {
				bytes, _ := json.MarshalIndent(guidance, "", "  ")
				fmt.Println(string(bytes))
				return nil
			}

			fmt.Println(color.CyanString("Ruxbase Production Deployment & Architecture Guidance"))
			fmt.Println(strings.Repeat("=", 60))

			fmt.Println(color.YellowString("\n1. Platform & Infrastructure Requirements:"))
			for _, req := range guidance.PlatformRequirements {
				fmt.Printf("  • %s\n", req)
			}

			fmt.Println(color.YellowString("\n2. Mandatory Security Checklist:"))
			for _, sec := range guidance.SecurityChecklist {
				fmt.Printf("  • %s\n", sec)
			}

			fmt.Println(color.YellowString("\n3. Least-Privilege Database Role Separation:"))
			for role, desc := range guidance.DatabaseRoles {
				fmt.Printf("  • %s: %s\n", color.GreenString(role), desc)
			}

			fmt.Println(color.YellowString("\n4. Recommended Runtime Environment Variables:"))
			for k, v := range guidance.RecommendedEnv {
				fmt.Printf("  • %s=%s\n", k, v)
			}

			fmt.Println(strings.Repeat("=", 60))
			return nil
		},
	}

	command.Flags().BoolVar(&jsonOutput, "json", false, "output deployment guidance as machine-readable JSON")
	return command
}
