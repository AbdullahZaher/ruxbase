package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// InitSummary describes the initialized project workspace.
type InitSummary struct {
	Directory   string   `json:"directory"`
	DataDir     string   `json:"dataDir"`
	Created     []string `json:"created"`
	Starters    []string `json:"starters"`
	NextActions []string `json:"nextActions"`
}

// NewInitCommand returns a command for initializing a safe local Ruxbase workspace.
func NewInitCommand(app core.App) *cobra.Command {
	var jsonOutput bool
	var listStarters bool

	command := &cobra.Command{
		Use:          "init [target-dir]",
		Short:        "Initialize a safe local Ruxbase workspace, data directory, and starter instructions",
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			starters := []string{
				"react: React + Vite + Tailwind CSS starter with PocketBase SDK client",
				"nextjs: Next.js App Router (React 19) starter with server/client auth session helpers",
			}

			if listStarters {
				if jsonOutput {
					b, _ := json.MarshalIndent(map[string]any{"starters": starters}, "", "  ")
					fmt.Println(string(b))
					return nil
				}
				fmt.Println(color.CyanString("Available Frontend Starters:"))
				for _, s := range starters {
					fmt.Printf("  • %s\n", s)
				}
				return nil
			}

			targetDir := "."
			if len(args) > 0 {
				targetDir = args[0]
			}

			absTarget, err := filepath.Abs(targetDir)
			if err != nil {
				return fmt.Errorf("invalid target directory: %w", err)
			}

			dataDir := filepath.Join(absTarget, "ruxbase_data")
			createdFiles := []string{}

			if err := os.MkdirAll(dataDir, 0755); err != nil {
				return fmt.Errorf("failed to create data directory: %w", err)
			}
			createdFiles = append(createdFiles, dataDir)

			// Generate safe .env.example if not present
			envExample := filepath.Join(absTarget, ".env.example")
			if _, err := os.Stat(envExample); os.IsNotExist(err) {
				content := "# Ruxbase Local Development Configuration\nDATABASE_URL=postgres://ruxbase_app:ruxbasepassword@127.0.0.1:5432/ruxbase?sslmode=disable\nRUXBASE_TEST_URL=postgres://ruxbase_app:ruxbasepassword@127.0.0.1:5433/ruxbase_test?sslmode=disable\n"
				if err := os.WriteFile(envExample, []byte(content), 0644); err == nil {
					createdFiles = append(createdFiles, envExample)
				}
			}

			summary := InitSummary{
				Directory: absTarget,
				DataDir:   dataDir,
				Created:   createdFiles,
				Starters:  starters,
				NextActions: []string{
					"Start local PostgreSQL: docker compose -f docker-compose.dev.yml up -d",
					"Run preflight checks: ruxbase check",
					"Create frontend starter: node scripts/create-ruxbase-app.mjs react my-app",
					"Start server in dev mode: ruxbase serve --dev",
				},
			}

			if jsonOutput {
				b, _ := json.MarshalIndent(summary, "", "  ")
				fmt.Println(string(b))
				return nil
			}

			fmt.Println(color.GreenString("✓ Initialized Ruxbase Workspace"))
			fmt.Printf("  Target Directory: %s\n", absTarget)
			fmt.Printf("  Data Directory:   %s\n\n", dataDir)

			fmt.Println(color.CyanString("Next Steps:"))
			for i, action := range summary.NextActions {
				fmt.Printf("  %d. %s\n", i+1, action)
			}

			return nil
		},
	}

	command.Flags().BoolVar(&jsonOutput, "json", false, "format summary as machine-readable JSON")
	command.Flags().BoolVar(&listStarters, "list", false, "list available frontend starter templates")
	return command
}
