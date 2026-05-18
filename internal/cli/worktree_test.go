package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/git"
)

func TestLoadWorktreeConfig_UsesLoadedCLIConfig(t *testing.T) {
	oldCfg, oldCfgFile := cfg, cfgFile
	t.Cleanup(func() {
		cfg = oldCfg
		cfgFile = oldCfgFile
	})

	cfg = &config.Config{ProjectsBase: "/from-loaded-config"}
	cfgFile = filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgFile, []byte(`projects_base = "/from-file"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	loaded, err := loadWorktreeConfig()
	if err != nil {
		t.Fatalf("loadWorktreeConfig() error = %v", err)
	}
	if loaded != cfg {
		t.Fatal("loadWorktreeConfig() should reuse already loaded CLI config")
	}
	if loaded.ProjectsBase != "/from-loaded-config" {
		t.Fatalf("ProjectsBase = %q, want /from-loaded-config", loaded.ProjectsBase)
	}
}

func TestLoadWorktreeConfig_UsesCfgFileNotLocalConfigToml(t *testing.T) {
	oldCfg, oldCfgFile := cfg, cfgFile
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	t.Cleanup(func() {
		cfg = oldCfg
		cfgFile = oldCfgFile
		_ = os.Chdir(wd)
	})

	cfg = nil
	tmpDir := t.TempDir()
	cfgFile = filepath.Join(tmpDir, "user-config.toml")
	if err := os.WriteFile(cfgFile, []byte(`projects_base = "/from-cfg-file"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(cfgFile) failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "config.toml"), []byte(`projects_base = "/wrong-local"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(local config.toml) failed: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	loaded, err := loadWorktreeConfig()
	if err != nil {
		t.Fatalf("loadWorktreeConfig() error = %v", err)
	}
	if loaded.ProjectsBase != "/from-cfg-file" {
		t.Fatalf("ProjectsBase = %q, want /from-cfg-file", loaded.ProjectsBase)
	}
}

func TestLoadWorktreeConfig_InvalidCfgFile(t *testing.T) {
	oldCfg, oldCfgFile := cfg, cfgFile
	t.Cleanup(func() {
		cfg = oldCfg
		cfgFile = oldCfgFile
	})

	cfg = nil
	cfgFile = filepath.Join(t.TempDir(), "bad-config.toml")
	if err := os.WriteFile(cfgFile, []byte("not valid toml {{{"), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	_, err := loadWorktreeConfig()
	if err == nil {
		t.Fatal("expected error for invalid cfgFile")
	}
}

func TestResolveWorktreeSyncRootAcceptsProvisionedSiblingWorktree(t *testing.T) {
	repo := setupCLIWorktreeGitRepo(t)
	wm, err := git.NewWorktreeManager(repo)
	if err != nil {
		t.Fatalf("NewWorktreeManager() error = %v", err)
	}
	info, err := wm.ProvisionWorktree(context.Background(), "cod", "session-one")
	if err != nil {
		t.Fatalf("ProvisionWorktree() error = %v", err)
	}

	nested := filepath.Join(info.Path, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", nested, err)
	}

	got, err := resolveWorktreeSyncRoot(nested)
	if err != nil {
		t.Fatalf("resolveWorktreeSyncRoot(%q) error = %v", nested, err)
	}
	if got != info.Path {
		t.Fatalf("resolveWorktreeSyncRoot() = %q, want %q", got, info.Path)
	}
}

func setupCLIWorktreeGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"git", "init"},
		{"git", "symbolic-ref", "HEAD", "refs/heads/main"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test User"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("%v failed: %v\n%s", args, err, string(out))
		}
	}
	return dir
}
