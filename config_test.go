package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAPITokenFromEnv(t *testing.T) {
	t.Setenv("API_TOKEN", "dev-token")
	t.Setenv("API_TOKEN_FILE", "")
	t.Setenv("KUBE_DEPLOY_API_TOKEN", "")

	if got := resolveAPIToken(); got != "dev-token" {
		t.Fatalf("resolveAPIToken() = %q, want dev-token", got)
	}
}

func TestResolveAPITokenFromFile(t *testing.T) {
	t.Setenv("API_TOKEN", "")
	t.Setenv("KUBE_DEPLOY_API_TOKEN", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("API_TOKEN_FILE", path)

	if got := resolveAPIToken(); got != "file-token" {
		t.Fatalf("resolveAPIToken() = %q, want file-token", got)
	}
}
