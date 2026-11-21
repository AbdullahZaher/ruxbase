package e2e_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/AbdullahZaher/ruxbase/cmd"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestCLI_CheckCommand(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	checkCmd := cmd.NewCheckCommand(app)
	buf := new(bytes.Buffer)
	checkCmd.SetOut(buf)
	checkCmd.SetErr(buf)
	checkCmd.SetArgs([]string{"--json"})

	if err := checkCmd.Execute(); err != nil {
		t.Fatalf("check command returned error: %v", err)
	}

	var pingRes int
	if err := app.DB().NewQuery("SELECT 1").Row(&pingRes); err != nil {
		t.Fatalf("DB connection failed: %v", err)
	}
}

func TestCLI_DeployCommand(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	deployCmd := cmd.NewDeployCommand(app)
	deployCmd.SetArgs([]string{"--json"})

	if err := deployCmd.Execute(); err != nil {
		t.Fatalf("deploy command returned error: %v", err)
	}
}

func TestCLI_InitCommand(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	tempDir, err := os.MkdirTemp("", "ruxbase_init_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	initCmd := cmd.NewInitCommand(app)
	initCmd.SetArgs([]string{tempDir, "--json"})

	if err := initCmd.Execute(); err != nil {
		t.Fatalf("init command returned error: %v", err)
	}

	// Verify data directory was created
	dataDir := filepath.Join(tempDir, "ruxbase_data")
	if stat, err := os.Stat(dataDir); err != nil || !stat.IsDir() {
		t.Errorf("Expected ruxbase_data directory to be created at %s", dataDir)
	}

	// Verify .env.example was created
	envFile := filepath.Join(tempDir, ".env.example")
	if _, err := os.Stat(envFile); err != nil {
		t.Errorf("Expected .env.example to be created at %s", envFile)
	}
}
