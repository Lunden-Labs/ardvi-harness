package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposeUsesOneLoopbackOnlyPersistentService(t *testing.T) {
	text, err := composeFile("ghcr.io/lunden-labs/ardvi-mcp@sha256:" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"name: ardvi", "container_name: ardvi-mcp", "127.0.0.1:8765:8765",
		"restart: unless-stopped", "ardvi-data:/var/lib/ardvi", "read_only: true",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("compose is missing %q:\n%s", want, text)
		}
	}
}

func TestReleaseManifestRequiresImmutableImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "release-manifest.json")
	if err := os.WriteFile(path, []byte(`{"schema":1,"version":"1.2.3","image":"ghcr.io/lunden-labs/ardvi-mcp:latest"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManifest(path); err == nil {
		t.Fatal("mutable image tag accepted")
	}
	good := `{"schema":1,"version":"1.2.3","image":"ghcr.io/lunden-labs/ardvi-mcp@sha256:` + strings.Repeat("b", 64) + `"}`
	if err := os.WriteFile(path, []byte(good), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManifest(path); err != nil {
		t.Fatal(err)
	}
}

func TestInstallNoStartWritesGlobalComposeAndMetadata(t *testing.T) {
	dir := t.TempDir()
	digest := "ghcr.io/lunden-labs/ardvi-mcp@sha256:" + strings.Repeat("c", 64)
	manifest := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifest, []byte(`{"schema":1,"version":"1.2.3","image":"`+digest+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config")
	if err := installRuntime([]string{"--manifest", manifest, "--config-dir", config, "--no-start"}, false); err != nil {
		t.Fatal(err)
	}
	compose, err := os.ReadFile(filepath.Join(config, "compose.yaml"))
	if err != nil || !strings.Contains(string(compose), digest) {
		t.Fatalf("compose not installed: %v %s", err, compose)
	}
	if _, err = os.Stat(filepath.Join(config, "installed-release.json")); err != nil {
		t.Fatal(err)
	}
}
