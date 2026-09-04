package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	version        = "dev"
	commit         = "unknown"
	releaseBaseURL = "https://github.com/Lunden-Labs/ardvi-harness/releases"
)

var immutableImage = regexp.MustCompile(`^[a-z0-9][a-z0-9./_-]*@sha256:[0-9a-f]{64}$`)
var projectUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type releaseManifest struct {
	Schema    int               `json:"schema"`
	Version   string            `json:"version"`
	Image     string            `json:"image"`
	Commit    string            `json:"commit,omitempty"`
	Upstreams map[string]string `json:"upstreams,omitempty"`
	Binaries  map[string]struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
	} `json:"binaries,omitempty"`
}

func composeFile(image string) (string, error) {
	if !immutableImage.MatchString(image) {
		return "", errors.New("release image must be pinned by sha256 digest")
	}
	return fmt.Sprintf(`name: ardvi
services:
  mcp:
    image: %q
    container_name: ardvi-mcp
    labels:
      io.ardvi.service: mcp
    environment:
      ARDVI_CONTAINER: "1"
    command: ["serve", "--listen", "0.0.0.0:8765", "--allow-non-loopback", "--data", "/var/lib/ardvi", "--catalog", "/opt/ardvi/catalog.json"]
    ports:
      - "127.0.0.1:8765:8765"
    restart: unless-stopped
    read_only: true
    tmpfs:
      - /tmp
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    volumes:
      - ardvi-data:/var/lib/ardvi
    healthcheck:
      test: ["CMD", "/usr/local/bin/ardvi", "healthcheck"]
      interval: 10s
      timeout: 3s
      retries: 5
volumes:
  ardvi-data:
    name: ardvi-data
`, image), nil
}

func loadManifest(location string) (releaseManifest, error) {
	var reader io.ReadCloser
	if strings.HasPrefix(location, "https://") {
		client := &http.Client{Timeout: 30 * time.Second}
		response, err := client.Get(location)
		if err != nil {
			return releaseManifest{}, err
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return releaseManifest{}, fmt.Errorf("release manifest returned %s", response.Status)
		}
		reader = response.Body
	} else if strings.Contains(location, "://") {
		return releaseManifest{}, errors.New("release manifest URL must use HTTPS")
	} else {
		file, err := os.Open(location)
		if err != nil {
			return releaseManifest{}, err
		}
		reader = file
	}
	defer reader.Close()
	var manifest releaseManifest
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return releaseManifest{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return releaseManifest{}, errors.New("release manifest contains trailing data")
	}
	if manifest.Schema != 1 || manifest.Version == "" || !immutableImage.MatchString(manifest.Image) {
		return releaseManifest{}, errors.New("invalid release manifest")
	}
	return manifest, nil
}

func ardviConfigDir(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	if value := os.Getenv("ARDVI_CONFIG_DIR"); value != "" {
		return filepath.Abs(value)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ardvi"), nil
}

func writeAtomic(path, value string) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symlink: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".compose-*.yaml")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.WriteString(value)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func writeCandidate(dir, value string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, ".compose-candidate-*.yaml")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err = file.Chmod(0600); err == nil {
		_, err = file.WriteString(value)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func runDocker(compose string, args ...string) error {
	base := []string{"compose", "-p", "ardvi", "-f", compose}
	command := exec.Command("docker", append(base, args...)...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command.Run()
}

func dockerOutput(compose string, args ...string) ([]byte, error) {
	base := []string{"compose", "-p", "ardvi", "-f", compose}
	return exec.Command("docker", append(base, args...)...).Output()
}

func dockerCommand(compose string, args ...string) *exec.Cmd {
	base := []string{"compose", "-p", "ardvi", "-f", compose}
	return exec.Command("docker", append(base, args...)...)
}

func defaultManifest(latest bool) (string, error) {
	if value := os.Getenv("ARDVI_RELEASE_MANIFEST_URL"); value != "" {
		return value, nil
	}
	if latest {
		return strings.TrimRight(releaseBaseURL, "/") + "/latest/download/release-manifest.json", nil
	}
	if version == "dev" {
		return "", errors.New("development build requires --manifest")
	}
	return strings.TrimRight(releaseBaseURL, "/") + "/download/v" + strings.TrimPrefix(version, "v") + "/release-manifest.json", nil
}

func installRuntime(args []string, latest bool) error {
	f := flag.NewFlagSet("install", flag.ContinueOnError)
	manifestLocation := f.String("manifest", "", "release manifest file or HTTPS URL")
	configOverride := f.String("config-dir", "", "global Ardvi configuration directory")
	noStart := f.Bool("no-start", false, "write configuration without starting Docker")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *manifestLocation == "" {
		var err error
		*manifestLocation, err = defaultManifest(latest)
		if err != nil {
			return err
		}
	}
	manifest, err := loadManifest(*manifestLocation)
	if err != nil {
		return fmt.Errorf("load release manifest: %w", err)
	}
	if !latest && version != "dev" && manifest.Version != strings.TrimPrefix(version, "v") {
		return fmt.Errorf("release manifest version %s does not match CLI %s", manifest.Version, version)
	}
	compose, err := composeFile(manifest.Image)
	if err != nil {
		return err
	}
	configDir, err := ardviConfigDir(*configOverride)
	if err != nil {
		return err
	}
	composePath := filepath.Join(configDir, "compose.yaml")
	if info, statErr := os.Lstat(composePath); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symlink: %s", composePath)
	}
	candidate, err := writeCandidate(configDir, compose)
	if err != nil {
		return err
	}
	defer os.Remove(candidate)
	metadata, _ := json.MarshalIndent(manifest, "", "  ")
	if *noStart {
		if err = os.Rename(candidate, composePath); err != nil {
			return err
		}
		return writeAtomic(filepath.Join(configDir, "installed-release.json"), string(metadata)+"\n")
	}
	if latest {
		if err = runDocker(candidate, "pull"); err != nil {
			return err
		}
	}
	if err = runDocker(candidate, "up", "-d", "--remove-orphans"); err != nil {
		if _, statErr := os.Stat(composePath); statErr == nil {
			_ = runDocker(composePath, "up", "-d", "--remove-orphans")
		} else {
			_ = runDocker(candidate, "down", "--remove-orphans")
		}
		return err
	}
	for attempt := 0; attempt < 50; attempt++ {
		if healthcheck(nil) == nil {
			if err = os.Rename(candidate, composePath); err != nil {
				return err
			}
			if err = writeAtomic(filepath.Join(configDir, "installed-release.json"), string(metadata)+"\n"); err != nil {
				return err
			}
			fmt.Printf("Ardvi runtime %s configured with %s\n", manifest.Version, manifest.Image)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, statErr := os.Stat(composePath); statErr == nil {
		_ = runDocker(composePath, "up", "-d", "--remove-orphans")
	} else {
		_ = runDocker(candidate, "down", "--remove-orphans")
	}
	return errors.New("new Ardvi runtime did not become healthy; previous configuration retained")
}

func serviceCommand(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: ardvi service ensure|status|stop")
	}
	configDir, err := ardviConfigDir("")
	if err != nil {
		return err
	}
	compose := filepath.Join(configDir, "compose.yaml")
	if _, err = os.Stat(compose); err != nil {
		return errors.New("Ardvi is not installed; run ardvi install")
	}
	switch args[0] {
	case "ensure":
		return runDocker(compose, "up", "-d", "--remove-orphans")
	case "status":
		return runDocker(compose, "ps")
	case "stop":
		return runDocker(compose, "stop")
	default:
		return errors.New("usage: ardvi service ensure|status|stop")
	}
}

func memoryCommand(args []string) error {
	if len(args) == 0 || (args[0] != "export" && args[0] != "import") {
		return errors.New("usage: ardvi memory export|import --project UUID --file FILE")
	}
	action := args[0]
	f := flag.NewFlagSet("memory "+action, flag.ContinueOnError)
	project := f.String("project", "", "project UUID")
	file := f.String("file", "", "JSONL file")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if !projectUUID.MatchString(*project) || *file == "" {
		return errors.New("valid --project and --file are required")
	}
	absolute, err := filepath.Abs(*file)
	if err != nil {
		return err
	}
	if action == "import" {
		if _, err = os.Stat(absolute); err != nil {
			return err
		}
	} else if err = os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return err
	}
	configDir, err := ardviConfigDir("")
	if err != nil {
		return err
	}
	compose := filepath.Join(configDir, "compose.yaml")
	running, err := dockerOutput(compose, "ps", "--status", "running", "-q", "mcp")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(running)) != "" {
		return errors.New("stop the machine-wide service before memory export or import")
	}
	command := dockerCommand(compose, "run", "--rm", "--no-deps", "-T", "mcp", "memory-"+action,
		"--data", "/var/lib/ardvi", "--project", *project, "--file", "-")
	command.Stderr = os.Stderr
	if action == "import" {
		input, err := os.Open(absolute)
		if err != nil {
			return err
		}
		defer input.Close()
		command.Stdin = input
		command.Stdout = os.Stdout
		return command.Run()
	}
	var output bytes.Buffer
	command.Stdout = &output
	if err = command.Run(); err != nil {
		return err
	}
	return writeAtomic(absolute, output.String())
}

func healthcheck(args []string) error {
	f := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	endpoint := f.String("url", "http://127.0.0.1:8765/healthz", "health endpoint")
	if err := f.Parse(args); err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(*endpoint)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", response.Status)
	}
	return nil
}
