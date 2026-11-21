package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStarters_DryRunGeneration(t *testing.T) {
	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to resolve root path: %v", err)
	}

	scriptPath := filepath.Join(rootPath, "scripts/create-ruxbase-app.mjs")

	frameworks := []string{"react", "nextjs"}
	for _, fw := range frameworks {
		cmd := exec.Command("node", scriptPath, fw, "test-starter-app", "--dry-run", "--json")
		cmd.Dir = rootPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Dry run for %s failed: %v, output: %s", fw, err, string(out))
		}

		var res struct {
			Ok        bool   `json:"ok"`
			DryRun    bool   `json:"dryRun"`
			Framework string `json:"framework"`
			Name      string `json:"name"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			t.Fatalf("Failed to parse JSON output: %v, raw: %s", err, string(out))
		}

		if !res.Ok || !res.DryRun || res.Framework != fw || res.Name != "test-starter-app" {
			t.Errorf("Unexpected dry-run result for %s: %+v", fw, res)
		}
	}
}

func TestStarters_RealGenerationAndArtifacts(t *testing.T) {
	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to resolve root path: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "ruxbase_starter_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	scriptPath := filepath.Join(rootPath, "scripts/create-ruxbase-app.mjs")
	cmd := exec.Command("node", scriptPath, "react", "my-test-app", "--output", tempDir, "--json")
	cmd.Dir = rootPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to generate react starter: %v, output: %s", err, string(out))
	}

	projectDir := filepath.Join(tempDir, "my-test-app")

	requiredFiles := []string{
		"package.json",
		"package-lock.json",
		".env.example",
		"RUXBASE_AI_CONTEXT.md",
		"index.html",
	}

	for _, file := range requiredFiles {
		p := filepath.Join(projectDir, file)
		if stat, err := os.Stat(p); err != nil || stat.IsDir() {
			t.Errorf("Missing expected file %s in generated starter", file)
		}
	}

	// Verify package.json name replacement
	pkgBytes, err := os.ReadFile(filepath.Join(projectDir, "package.json"))
	if err != nil {
		t.Fatalf("Failed to read package.json: %v", err)
	}
	var pkg struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(pkgBytes, &pkg); err != nil {
		t.Fatalf("Failed to parse package.json: %v", err)
	}
	if pkg.Name != "my-test-app" {
		t.Errorf("Expected package name 'my-test-app', got %q", pkg.Name)
	}
}

func TestStarters_DockerComposeConfig(t *testing.T) {
	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to resolve root path: %v", err)
	}

	composeFile := filepath.Join(rootPath, "docker-compose.dev.yml")
	content, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatalf("Failed to read docker-compose.dev.yml: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "services:") {
		t.Errorf("docker-compose.dev.yml missing services definition")
	}
	if !strings.Contains(s, "ruxbase_db") && !strings.Contains(s, "db:") {
		t.Errorf("docker-compose.dev.yml missing db service")
	}
	if !strings.Contains(s, "pgvector") && !strings.Contains(s, "postgres") {
		t.Errorf("docker-compose.dev.yml missing postgres/pgvector image")
	}
}

func TestStarters_AITaskSchema(t *testing.T) {
	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to resolve root path: %v", err)
	}

	taskFile := filepath.Join(rootPath, "starters/ai-tasks.json")
	data, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatalf("Failed to read ai-tasks.json: %v", err)
	}

	var tasks struct {
		SchemaVersion int    `json:"schemaVersion"`
		Objective     string `json:"objective"`
		Steps         []struct {
			Id      string   `json:"id"`
			Command string   `json:"command"`
			Expect  []string `json:"expect"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(data, &tasks); err != nil {
		t.Fatalf("Failed to parse ai-tasks.json: %v", err)
	}

	if tasks.SchemaVersion != 1 {
		t.Errorf("Expected schemaVersion 1, got %d", tasks.SchemaVersion)
	}
	if len(tasks.Steps) == 0 {
		t.Errorf("Expected non-empty steps list in ai-tasks.json")
	}
}
