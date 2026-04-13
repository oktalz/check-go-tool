// Package main implements check-go-tool, a tool verifier and installer.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oktalz/check-go-tool/version"
)

func main() {
	_ = version.Set() //nolint:errcheck
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("check-go-tool", flag.ContinueOnError)
	check := fs.Bool("check", false, "check if a newer version is available (no install)")

	const progName = "check-go-tool"
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [--check] <package@version>\n", progName)
		fmt.Fprintf(os.Stderr, "       %s version | tag\n\n", progName)
		fmt.Fprintf(os.Stderr, "Resolves a Go tool: prefers system PATH (if version+Go match),\n")
		fmt.Fprintf(os.Stderr, "falls back to $TMPDIR/<name>/<version>, or installs if needed.\n")
		fmt.Fprintf(os.Stderr, "Outputs path to tool on success.\n")
		fmt.Fprintf(os.Stderr, "With --check, reports whether a newer version is available.\n")
		fmt.Fprintf(os.Stderr, "With @latest, installs to default GOBIN and auto-upgrades.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  %s mvdan.cc/gofumpt@v0.9.2\n", progName)
		fmt.Fprintf(os.Stderr, "  %s mvdan.cc/gofumpt@latest\n", progName)
		fmt.Fprintf(os.Stderr, "  %s --check mvdan.cc/gofumpt@v0.9.2\n", progName)
		fmt.Fprintf(os.Stderr, "  %s version\n", progName)
		fmt.Fprintf(os.Stderr, "  %s tag\n\n", progName)
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Print(version.Logo)
			fmt.Println(progName, version.Version)
			if version.Repo != "" {
				fmt.Println("built-from", version.Repo)
			}
			if version.CommitDate != "" {
				fmt.Println("commit-date", version.CommitDate)
			}
			return 0
		case "tag":
			fmt.Println(version.Version)
			return 0
		}
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "error: expected exactly one argument: package@version\n\n")
		fs.Usage()
		return 1
	}

	importPath, version := parseURL(fs.Arg(0))
	if version == "" {
		fmt.Fprintf(os.Stderr, "error: missing version: use package@version (e.g. mvdan.cc/gofumpt@v0.9.2)\n")
		return 1
	}

	name := toolName(importPath)

	tmpDir := os.Getenv("TMPDIR")
	if tmpDir == "" {
		tmpDir = "/tmp"
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = tmpDir
	}
	cachePath := filepath.Join(cacheDir, "check-go-tool", name, ".latest-version")

	// Handle --check flag (version check mode)
	if *check {
		return handleCheck(name, version, importPath, cachePath)
	}

	// Handle @latest: install to default GOBIN, keep up-to-date
	if version == "latest" {
		return handleLatest(name, importPath, cachePath)
	}

	localPath := filepath.Join(tmpDir, name, version, name)

	// Try system PATH
	if path, ok := checkSystemBinary(name, version); ok {
		notifyIfOutdated(name, version, importPath, cachePath)
		fmt.Println(path)
		return 0
	}

	// Try local install
	if checkLocalBinary(localPath, version) {
		notifyIfOutdated(name, version, importPath, cachePath)
		fmt.Println(localPath)
		return 0
	}

	// Install locally with current Go version
	if err := installTool(importPath, version, localPath); err != nil {
		fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
		return 1
	}

	notifyIfOutdated(name, version, importPath, cachePath)
	fmt.Println(localPath)
	return 0
}

// checkSystemBinary checks if tool is on PATH with matching version and Go version.
func checkSystemBinary(name, expectedVersion string) (string, bool) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}

	version := getGoModVersion(path)
	if version != expectedVersion {
		return "", false
	}

	buildGoVer := getBuildGoVersion(path)
	currentGoVer := getCurrentGoVersion()
	if buildGoVer == currentGoVer {
		return path, true
	}

	return "", false
}

// checkLocalBinary checks if tool exists locally with matching version and Go version.
func checkLocalBinary(path, expectedVersion string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}

	version := getGoModVersion(path)
	if version != expectedVersion {
		return false
	}

	buildGoVer := getBuildGoVersion(path)
	currentGoVer := getCurrentGoVersion()
	return buildGoVer == currentGoVer
}

// installTool runs `go install` with GOBIN set to install location.
func installTool(importPath, version, targetPath string) error {
	dir := filepath.Dir(targetPath)
	os.MkdirAll(dir, 0o755)

	cmd := exec.Command("go", "install", importPath+"@"+version)
	cmd.Env = append(os.Environ(), "GOBIN="+dir)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr

	return cmd.Run()
}

// getGoModVersion extracts module version from `go version -m <binary>` output.
// Output format: mod <module-path> <version> ...
func getGoModVersion(path string) string {
	cmd := exec.Command("go", "version", "-m", path)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.SplitSeq(string(out), "\n")
	for line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 3 && parts[0] == "mod" {
			return parts[2]
		}
	}
	return ""
}

// getCurrentGoVersion extracts Go version from `go version` output.
// Output format: go version go1.XX.Y platform
func getCurrentGoVersion() string {
	cmd := exec.Command("go", "version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	parts := strings.Fields(string(out))
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

// getBuildGoVersion extracts Go version tool was built with from `go version <binary>`.
// Output format: /path/to/binary go1.XX.Y
func getBuildGoVersion(path string) string {
	cmd := exec.Command("go", "version", path)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	parts := strings.Fields(string(out))
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// handleCheck checks if a newer version is available and reports it.
// This is check-only mode - no installation is performed.
// Results are cached for 23 hours in the user's cache directory.
// Exit codes: 0 if up-to-date or ahead, 1 if update available or error.
func handleCheck(name, currentVersion, importPath, cachePath string) int {
	latestVersion, ok := readCache(cachePath)
	if !ok {
		var err error
		latestVersion, err = getLatestVersion(importPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to check latest version: %v\n", err)
			return 1
		}
		writeCache(cachePath, latestVersion)
	}

	cmp := compareVersions(currentVersion, latestVersion)

	switch cmp {
	case -1: // current < latest
		fmt.Fprintf(os.Stderr, "%s can be updated from %s to %s\n", name, currentVersion, latestVersion)
		return 1
	case 0: // current == latest
		fmt.Fprintf(os.Stderr, "%s is up-to-date (%s)\n", name, currentVersion)
		return 0
	case 1: // current > latest
		fmt.Fprintf(os.Stderr, "%s %s is newer than latest %s\n", name, currentVersion, latestVersion)
		return 0
	}
	return 0
}

// handleLatest handles --version=latest: installs to default GOBIN and keeps up-to-date.
// If already installed, checks cache (or upstream) to see if a newer version exists.
// If outdated or not installed, runs `go install <url>@latest` (default GOBIN, not tmp).
func handleLatest(name, importPath, cachePath string) int {
	// Check if binary exists on PATH
	path, err := exec.LookPath(name)
	if err != nil {
		// Not installed — install to default GOBIN
		if err := installDefault(importPath); err != nil {
			fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
			return 1
		}
		path, err = exec.LookPath(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s not found on PATH after install\n", name)
			return 1
		}
		// Cache the version we just installed
		if v := getGoModVersion(path); v != "" {
			writeCache(cachePath, v)
		}
		fmt.Println(path)
		return 0
	}

	// Installed — get current version from binary
	installedVersion := getGoModVersion(path)

	// Get latest version from cache or upstream
	latestVersion, ok := readCache(cachePath)
	if !ok {
		latestVersion, err = getLatestVersion(importPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to check latest version: %v\n", err)
			return 1
		}
		writeCache(cachePath, latestVersion)
	}

	// Up-to-date — use existing binary
	if compareVersions(installedVersion, latestVersion) >= 0 {
		fmt.Println(path)
		return 0
	}

	// Outdated — reinstall
	fmt.Fprintf(os.Stderr, "%s: upgrading from %s to %s\n", name, installedVersion, latestVersion)
	if err := installDefault(importPath); err != nil {
		fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
		return 1
	}

	// Update cache with actual installed version
	if v := getGoModVersion(path); v != "" {
		writeCache(cachePath, v)
	}
	fmt.Println(path)
	return 0
}

// installDefault runs `go install <importPath>@latest` using the default GOBIN.
func installDefault(importPath string) error {
	cmd := exec.Command("go", "install", importPath+"@latest")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	return cmd.Run()
}

// notifyIfOutdated prints a stderr notice if a newer version is available.
// Reads from cache first; if no cache exists, queries upstream and populates the cache.
func notifyIfOutdated(name, currentVersion, importPath, cachePath string) {
	latestVersion, ok := readCache(cachePath)
	if !ok {
		var err error
		latestVersion, err = getLatestVersion(importPath)
		if err != nil {
			return // network error during normal resolve is not fatal
		}
		writeCache(cachePath, latestVersion)
	}
	if compareVersions(currentVersion, latestVersion) == -1 {
		fmt.Fprintf(os.Stderr, "%s can be updated from %s to %s\n", name, currentVersion, latestVersion)
	}
}

const cacheTTL = 23 * time.Hour

// readCache returns the cached latest version if the cache file exists and is fresh.
func readCache(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	if time.Since(info.ModTime()) > cacheTTL {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(string(data))
	if v == "" {
		return "", false
	}
	return v, true
}

// writeCache stores the latest version string to disk.
func writeCache(path, version string) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(version+"\n"), 0o644)
}

// getLatestVersion queries the latest version of a module from Go modules.
// It uses `go list -m -json <importPath>@latest` and parses the JSON response.
func getLatestVersion(importPath string) (string, error) {
	cmd := exec.Command("go", "list", "-m", "-json", importPath+"@latest")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list failed: %w", err)
	}

	var result struct {
		Version string `json:"Version"`
	}

	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("failed to parse version response: %w", err)
	}

	if result.Version == "" {
		return "", fmt.Errorf("no version found in response")
	}

	return result.Version, nil
}

// compareVersions compares two semantic versions.
// Returns: -1 if current < latest, 0 if equal, 1 if current > latest.
// Handles v-prefixed versions (e.g., v1.2.3).
func compareVersions(current, latest string) int {
	// Strip 'v' prefix if present
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")

	// Parse versions
	currentParts := parseVersion(current)
	latestParts := parseVersion(latest)

	// Pad to same length
	maxLen := max(len(latestParts), len(currentParts))

	for len(currentParts) < maxLen {
		currentParts = append(currentParts, 0)
	}
	for len(latestParts) < maxLen {
		latestParts = append(latestParts, 0)
	}

	// Compare component by component
	for i := range maxLen {
		if currentParts[i] < latestParts[i] {
			return -1
		}
		if currentParts[i] > latestParts[i] {
			return 1
		}
	}

	return 0
}

// toolName extracts the binary name from a Go import path.
// Skips major version suffixes like /v2, /v3, etc.
// Example: "github.com/haproxytech/check-commit/v5" -> "check-commit"
// Example: "mvdan.cc/gofumpt" -> "gofumpt"
func toolName(importPath string) string {
	base := filepath.Base(importPath)
	// If last component is a major version suffix (v2, v3, ...), use parent
	if len(base) >= 2 && base[0] == 'v' {
		if _, err := strconv.Atoi(base[1:]); err == nil {
			return filepath.Base(filepath.Dir(importPath))
		}
	}
	return base
}

// parseURL splits a "path@version" string into import path and version.
// Example: "mvdan.cc/gofumpt@v0.9.2" -> ("mvdan.cc/gofumpt", "v0.9.2")
func parseURL(url string) (importPath, version string) {
	if i := strings.LastIndex(url, "@"); i >= 0 {
		return url[:i], url[i+1:]
	}
	return url, ""
}

// parseVersion converts a version string to numeric components.
// Example: "1.2.3" -> [1, 2, 3]
// Non-numeric components are treated as 0.
func parseVersion(version string) []int {
	parts := strings.Split(version, ".")
	result := make([]int, 0, len(parts))

	for _, part := range parts {
		num, err := strconv.Atoi(part)
		if err != nil {
			num = 0
		}
		result = append(result, num)
	}

	return result
}
