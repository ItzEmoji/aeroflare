package push

import (
	"os"
	"strings"
	"testing"
)

func TestParseConfig_NoPaths(t *testing.T) {
	_, err := ParseConfig([]string{}, "", "", nil)
	if err == nil {
		t.Fatal("expected error when no paths provided")
	}
}

func TestParseConfig_Args(t *testing.T) {
	cfg, err := ParseConfig([]string{"/path/1", "/path/2"}, "", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TargetPaths) != 2 || cfg.TargetPaths[0] != "/path/1" || cfg.TargetPaths[1] != "/path/2" {
		t.Fatalf("unexpected paths: %v", cfg.TargetPaths)
	}
}

func TestParseConfig_StorePath(t *testing.T) {
	cfg, err := ParseConfig([]string{}, "/store/path", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TargetPaths) != 1 || cfg.TargetPaths[0] != "/store/path" {
		t.Fatalf("unexpected paths: %v", cfg.TargetPaths)
	}
}

func TestParseConfig_Stdin(t *testing.T) {
	stdin := strings.NewReader("/stdin/path1\n/stdin/path2\n# comment\n")
	cfg, err := ParseConfig([]string{}, "", "", stdin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TargetPaths) != 2 || cfg.TargetPaths[0] != "/stdin/path1" || cfg.TargetPaths[1] != "/stdin/path2" {
		t.Fatalf("unexpected paths: %v", cfg.TargetPaths)
	}
}

func TestParseConfig_InputFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "aeroflare-push-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	if _, err := tmpFile.WriteString("/file/path1\n/file/path2\n"); err != nil {
		t.Fatal(err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig([]string{}, "", tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TargetPaths) != 2 || cfg.TargetPaths[0] != "/file/path1" || cfg.TargetPaths[1] != "/file/path2" {
		t.Fatalf("unexpected paths: %v", cfg.TargetPaths)
	}
}

func TestParseConfig_Combined(t *testing.T) {
	stdin := strings.NewReader("/stdin/path")
	cfg, err := ParseConfig([]string{"/arg/path"}, "/store/path", "", stdin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TargetPaths) != 3 {
		t.Fatalf("unexpected paths: %v", cfg.TargetPaths)
	}
	expected := map[string]bool{"/store/path": true, "/stdin/path": true, "/arg/path": true}
	for _, p := range cfg.TargetPaths {
		if !expected[p] {
			t.Errorf("unexpected path in combined test: %s", p)
		}
	}
}

func TestPreflight(t *testing.T) {
	cfg := &PushConfig{
		TargetPaths: []string{"/path/1", "/path/2"},
	}

	plan, err := Preflight(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Config != cfg {
		t.Errorf("expected plan.Config to be %v, got %v", cfg, plan.Config)
	}

	if len(plan.FilteredPaths) != 2 || plan.FilteredPaths[0] != "/path/1" || plan.FilteredPaths[1] != "/path/2" {
		t.Errorf("unexpected FilteredPaths: %v", plan.FilteredPaths)
	}

	if plan.SkippedCount != 0 {
		t.Errorf("expected SkippedCount 0, got %d", plan.SkippedCount)
	}
}
