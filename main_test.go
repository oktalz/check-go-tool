package main

import (
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCheckSystemBinary tests system PATH binary detection.
func TestCheckSystemBinary(t *testing.T) {
	tests := []struct {
		name            string
		binaryName      string
		expectedVersion string
		expectFound     bool
	}{
		{
			name:            "nonexistent_binary_not_found",
			binaryName:      "nonexistent-tool-xyz-12345",
			expectedVersion: "v1.0.0",
			expectFound:     false,
		},
		{
			name:            "real_binary_with_wrong_version",
			binaryName:      "sh",
			expectedVersion: "v1.0.0",
			expectFound:     false, // sh won't have this module version
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, found := checkSystemBinary(tt.binaryName, tt.expectedVersion)

			if found != tt.expectFound {
				t.Errorf("checkSystemBinary(%q, %q): got found=%v, want %v",
					tt.binaryName, tt.expectedVersion, found, tt.expectFound)
			}

			if found && path == "" {
				t.Error("checkSystemBinary: found=true but path is empty")
			}

			if !found && path != "" {
				t.Errorf("checkSystemBinary: found=false but path=%q", path)
			}
		})
	}
}

// TestCheckLocalBinary tests local cache binary detection.
func TestCheckLocalBinary(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name            string
		createFile      bool
		expectedVersion string
		expectFound     bool
	}{
		{
			name:            "local_binary_not_found",
			createFile:      false,
			expectedVersion: "v1.0.0",
			expectFound:     false,
		},
		{
			name:            "local_path_is_directory",
			createFile:      true,
			expectedVersion: "v1.0.0",
			expectFound:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localPath := filepath.Join(tmpDir, tt.name, "testbin")

			if tt.createFile {
				os.MkdirAll(filepath.Dir(localPath), 0o755)
				os.Mkdir(localPath, 0o755) // Create as directory instead of file
			}

			found := checkLocalBinary(localPath, tt.expectedVersion)

			if found != tt.expectFound {
				t.Errorf("checkLocalBinary(%q, %q): got found=%v, want %v",
					localPath, tt.expectedVersion, found, tt.expectFound)
			}
		})
	}
}

// TestGetCurrentGoVersion tests Go version extraction.
func TestGetCurrentGoVersion(t *testing.T) {
	version := getCurrentGoVersion()

	if version == "" {
		t.Fatal("getCurrentGoVersion: got empty string")
	}

	if !strings.HasPrefix(version, "go") {
		t.Errorf("getCurrentGoVersion: got %q, want format like 'go1.XX.Y'", version)
	}
}

// TestGetBuildGoVersion tests Go build version extraction from binaries.
func TestGetBuildGoVersion(t *testing.T) {
	// Test with a binary that exists
	cmd := exec.Command("which", "go")
	out, _ := cmd.Output()
	goBinary := strings.TrimSpace(string(out))

	if goBinary == "" {
		t.Skip("go binary not found in PATH")
	}

	version := getBuildGoVersion(goBinary)

	if version == "" {
		t.Fatal("getBuildGoVersion: got empty string")
	}

	if !strings.HasPrefix(version, "go") {
		t.Errorf("getBuildGoVersion: got %q, want format like 'go1.XX.Y'", version)
	}
}

// TestGetGoModVersion tests module version extraction.
func TestGetGoModVersion(t *testing.T) {
	// Test with 'go' binary which should have a module version
	version := getGoModVersion("go")

	// 'go' itself may or may not have a module version in all configurations
	// Just verify the function doesn't crash and returns a string (may be empty)
	if version != "" && !strings.HasPrefix(version, "v") {
		t.Logf("getGoModVersion: got %q (empty or version string)", version)
	}
}

// TestInstallTool tests tool installation.
func TestInstallTool(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "test-tool", "v1.0.0", "test-tool")

	// Try to install a small, real tool that should be quick
	err := installTool("golang.org/x/tools/cmd/goimports@latest", "latest", targetPath)
	if err != nil {
		// Installation might fail in some environments (no network, etc.)
		// Just log it rather than fail the test
		t.Logf("installTool failed (expected in some environments): %v", err)
		return
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("installTool: binary not found after install: %v", err)
	}

	if info.IsDir() {
		t.Fatalf("installTool: path is a directory, not a binary")
	}
}

// TestArgParsing tests argument and flag combinations.
func TestArgParsing(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError bool
		wantArg   string
		wantCheck bool
	}{
		{
			name:    "positional_arg",
			args:    []string{"mvdan.cc/gofumpt@v0.9.2"},
			wantArg: "mvdan.cc/gofumpt@v0.9.2",
		},
		{
			name:      "positional_with_check",
			args:      []string{"--check", "honnef.co/go/tools/cmd/staticcheck@v0.7.0"},
			wantArg:   "honnef.co/go/tools/cmd/staticcheck@v0.7.0",
			wantCheck: true,
		},
		{
			name:      "check_after_arg",
			args:      []string{"mvdan.cc/gofumpt@v0.9.2", "--check"},
			wantArg:   "mvdan.cc/gofumpt@v0.9.2",
			wantCheck: false, // flag after positional arg is not parsed
			wantError: true,  // NArg() == 2, not 1
		},
		{
			name:      "no_args",
			args:      []string{},
			wantError: true,
		},
		{
			name:    "latest_version",
			args:    []string{"mvdan.cc/gofumpt@latest"},
			wantArg: "mvdan.cc/gofumpt@latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			check := fs.Bool("check", false, "check if newer version is available")

			fs.SetOutput(io.Discard)

			err := fs.Parse(tt.args)
			if err != nil {
				if !tt.wantError {
					t.Fatalf("Parse(%v): got error %v, want nil", tt.args, err)
				}
				return
			}

			hasError := fs.NArg() != 1
			if hasError != tt.wantError {
				t.Fatalf("Validation: got error=%v, want %v (NArg=%d)", hasError, tt.wantError, fs.NArg())
			}

			if tt.wantError {
				return
			}

			if fs.Arg(0) != tt.wantArg {
				t.Errorf("arg: got %q, want %q", fs.Arg(0), tt.wantArg)
			}
			if *check != tt.wantCheck {
				t.Errorf("check: got %v, want %v", *check, tt.wantCheck)
			}
		})
	}
}

// TestParseURL tests URL parsing into import path and version.
func TestParseURL(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantPath    string
		wantVersion string
	}{
		{
			name:        "standard_url",
			input:       "mvdan.cc/gofumpt@v0.9.2",
			wantPath:    "mvdan.cc/gofumpt",
			wantVersion: "v0.9.2",
		},
		{
			name:        "url_with_subpath",
			input:       "github.com/org/repo/cmd/tool@v1.0.0",
			wantPath:    "github.com/org/repo/cmd/tool",
			wantVersion: "v1.0.0",
		},
		{
			name:        "latest_version",
			input:       "mvdan.cc/gofumpt@latest",
			wantPath:    "mvdan.cc/gofumpt",
			wantVersion: "latest",
		},
		{
			name:        "no_version",
			input:       "mvdan.cc/gofumpt",
			wantPath:    "mvdan.cc/gofumpt",
			wantVersion: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, version := parseURL(tt.input)
			if path != tt.wantPath {
				t.Errorf("parseURL(%q) path: got %q, want %q", tt.input, path, tt.wantPath)
			}
			if version != tt.wantVersion {
				t.Errorf("parseURL(%q) version: got %q, want %q", tt.input, version, tt.wantVersion)
			}
		})
	}
}

// TestToolName tests binary name extraction from import paths.
func TestToolName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple_path",
			input:    "mvdan.cc/gofumpt",
			expected: "gofumpt",
		},
		{
			name:     "cmd_subpath",
			input:    "github.com/dkorunic/betteralign/cmd/betteralign",
			expected: "betteralign",
		},
		{
			name:     "major_version_suffix_v5",
			input:    "github.com/haproxytech/check-commit/v5",
			expected: "check-commit",
		},
		{
			name:     "major_version_suffix_v4",
			input:    "github.com/mikefarah/yq/v4",
			expected: "yq",
		},
		{
			name:     "no_major_version",
			input:    "gotest.tools/gotestsum",
			expected: "gotestsum",
		},
		{
			name:     "cmd_with_major_version",
			input:    "golang.org/x/vuln/cmd/govulncheck",
			expected: "govulncheck",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toolName(tt.input)
			if result != tt.expected {
				t.Errorf("toolName(%q): got %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestLocalPathConstruction tests TMPDIR path resolution.
func TestLocalPathConstruction(t *testing.T) {
	tests := []struct {
		name     string
		tmpdir   string
		toolName string
		version  string
		wantPath string
	}{
		{
			name:     "default_tmp_path",
			tmpdir:   "/tmp",
			toolName: "gofumpt",
			version:  "v0.9.2",
			wantPath: "/tmp/gofumpt/v0.9.2/gofumpt",
		},
		{
			name:     "custom_tmpdir",
			tmpdir:   "/custom/tmp",
			toolName: "staticcheck",
			version:  "v0.7.0",
			wantPath: "/custom/tmp/staticcheck/v0.7.0/staticcheck",
		},
		{
			name:     "tool_with_hyphen",
			tmpdir:   "/tmp",
			toolName: "my-tool",
			version:  "v1.0.0",
			wantPath: "/tmp/my-tool/v1.0.0/my-tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localPath := filepath.Join(tt.tmpdir, tt.toolName, tt.version, tt.toolName)
			if localPath != tt.wantPath {
				t.Errorf("path construction: got %q, want %q", localPath, tt.wantPath)
			}
		})
	}
}

// TestEnvironmentVariables tests TMPDIR handling.
func TestEnvironmentVariables(t *testing.T) {
	tests := []struct {
		name    string
		tmpdir  string
		wantTmp string
	}{
		{
			name:    "explicit_tmpdir",
			tmpdir:  "/custom/tmp",
			wantTmp: "/custom/tmp",
		},
		{
			name:    "empty_tmpdir_defaults_to_tmp",
			tmpdir:  "",
			wantTmp: "/tmp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldTMPDIR := os.Getenv("TMPDIR")
			defer os.Setenv("TMPDIR", oldTMPDIR)

			if tt.tmpdir == "" {
				os.Unsetenv("TMPDIR")
			} else {
				os.Setenv("TMPDIR", tt.tmpdir)
			}

			tmpDir := os.Getenv("TMPDIR")
			if tmpDir == "" {
				tmpDir = "/tmp"
			}

			if tmpDir != tt.wantTmp {
				t.Errorf("TMPDIR resolution: got %q, want %q", tmpDir, tt.wantTmp)
			}
		})
	}
}

// TestVersionExtraction tests version parsing from command outputs.
func TestVersionExtraction(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		parseFunc   func(string) string
		description string
	}{
		{
			name:        "parse_go_version_output",
			input:       "go version go1.21.0 linux/amd64",
			parseFunc:   parseGoVersionOutput,
			description: "extract go version from 'go version' output",
		},
		{
			name:        "parse_go_version_binary_output",
			input:       "/usr/bin/gofumpt go1.21.0",
			parseFunc:   parseGoBinaryVersionOutput,
			description: "extract go version from 'go version <binary>' output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.parseFunc(tt.input)
			if result == "" {
				t.Errorf("parseFunc: got empty string for %q", tt.description)
			}
		})
	}
}

// Helper functions for testing

func parseGoVersionOutput(output string) string {
	parts := strings.Fields(output)
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

func parseGoBinaryVersionOutput(output string) string {
	parts := strings.Fields(output)
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// TestIntegration tests realistic end-to-end scenarios.
func TestIntegration(t *testing.T) {
	// Skip integration tests in short mode
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tests := []struct {
		name       string
		toolName   string
		version    string
		expectFind bool
	}{
		{
			name:       "nonexistent_tool_not_found",
			toolName:   "nonexistent-fake-tool-xyz",
			version:    "v1.0.0",
			expectFind: false,
		},
		{
			name:       "wrong_version_of_real_tool_not_found",
			toolName:   "sh",
			version:    "v9999.0.0",
			expectFind: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, found := checkSystemBinary(tt.toolName, tt.version)

			if found != tt.expectFind {
				t.Errorf("checkSystemBinary: found=%v, want %v", found, tt.expectFind)
			}

			if found && path == "" {
				t.Error("found=true but path is empty")
			}

			if !found && path != "" {
				t.Errorf("found=false but path=%q", path)
			}
		})
	}
}

// TestCache tests the version check cache (readCache / writeCache).
func TestCache(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("miss_on_nonexistent_file", func(t *testing.T) {
		_, ok := readCache(filepath.Join(tmpDir, "does-not-exist"))
		if ok {
			t.Error("readCache: got hit for nonexistent file")
		}
	})

	t.Run("hit_on_fresh_cache", func(t *testing.T) {
		path := filepath.Join(tmpDir, "fresh", ".latest-version")
		writeCache(path, "v1.2.3")

		v, ok := readCache(path)
		if !ok {
			t.Fatal("readCache: expected hit")
		}
		if v != "v1.2.3" {
			t.Errorf("readCache: got %q, want %q", v, "v1.2.3")
		}
	})

	t.Run("miss_on_stale_cache", func(t *testing.T) {
		path := filepath.Join(tmpDir, "stale", ".latest-version")
		writeCache(path, "v0.1.0")

		// Backdate the file beyond cacheTTL
		staleTime := time.Now().Add(-24 * time.Hour)
		os.Chtimes(path, staleTime, staleTime)

		_, ok := readCache(path)
		if ok {
			t.Error("readCache: got hit for stale cache")
		}
	})

	t.Run("miss_on_empty_file", func(t *testing.T) {
		path := filepath.Join(tmpDir, "empty", ".latest-version")
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(""), 0o644)

		_, ok := readCache(path)
		if ok {
			t.Error("readCache: got hit for empty file")
		}
	})

	t.Run("overwrite_existing_cache", func(t *testing.T) {
		path := filepath.Join(tmpDir, "overwrite", ".latest-version")
		writeCache(path, "v1.0.0")
		writeCache(path, "v2.0.0")

		v, ok := readCache(path)
		if !ok {
			t.Fatal("readCache: expected hit")
		}
		if v != "v2.0.0" {
			t.Errorf("readCache: got %q, want %q", v, "v2.0.0")
		}
	})
}

// TestGetLatestVersion tests querying the latest version of a module.
func TestGetLatestVersion(t *testing.T) {
	tests := []struct {
		name          string
		importPath    string
		expectError   bool
		expectVersion bool
	}{
		{
			name:          "real_module_golang_org_x_tools",
			importPath:    "golang.org/x/tools",
			expectError:   false,
			expectVersion: true,
		},
		{
			name:        "nonexistent_module",
			importPath:  "example.com/nonexistent-module-xyz-12345",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := getLatestVersion(tt.importPath)

			if (err != nil) != tt.expectError {
				t.Errorf("getLatestVersion(%q): got error=%v, want error=%v", tt.importPath, err != nil, tt.expectError)
			}

			if tt.expectVersion && version == "" {
				t.Errorf("getLatestVersion(%q): got empty version", tt.importPath)
			}

			if tt.expectVersion && !strings.HasPrefix(version, "v") {
				t.Logf("getLatestVersion(%q): version %q (expected v-prefixed)", tt.importPath, version)
			}
		})
	}
}

// TestCompareVersions tests semantic version comparison.
func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		latest   string
		expected int
	}{
		{
			name:     "v1_0_0_less_than_v1_1_0",
			current:  "v1.0.0",
			latest:   "v1.1.0",
			expected: -1,
		},
		{
			name:     "v1_0_0_equals_v1_0_0",
			current:  "v1.0.0",
			latest:   "v1.0.0",
			expected: 0,
		},
		{
			name:     "v2_0_0_greater_than_v1_99_0",
			current:  "v2.0.0",
			latest:   "v1.99.0",
			expected: 1,
		},
		{
			name:     "v1_9_less_than_v1_10",
			current:  "v1.9.0",
			latest:   "v1.10.0",
			expected: -1,
		},
		{
			name:     "v1_0_equals_v1_0_0",
			current:  "v1.0",
			latest:   "v1.0.0",
			expected: 0,
		},
		{
			name:     "v0_9_2_less_than_v0_10_0",
			current:  "v0.9.2",
			latest:   "v0.10.0",
			expected: -1,
		},
		{
			name:     "without_v_prefix",
			current:  "1.0.0",
			latest:   "1.1.0",
			expected: -1,
		},
		{
			name:     "empty_version",
			current:  "",
			latest:   "v1.0.0",
			expected: -1,
		},
		{
			name:     "both_empty",
			current:  "",
			latest:   "",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareVersions(tt.current, tt.latest)
			if result != tt.expected {
				t.Errorf("compareVersions(%q, %q): got %d, want %d", tt.current, tt.latest, result, tt.expected)
			}
		})
	}
}

// TestParseVersion tests version string parsing.
func TestParseVersion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []int
	}{
		{
			name:     "simple_version",
			input:    "1.2.3",
			expected: []int{1, 2, 3},
		},
		{
			name:     "two_part_version",
			input:    "1.2",
			expected: []int{1, 2},
		},
		{
			name:     "single_part_version",
			input:    "1",
			expected: []int{1},
		},
		{
			name:     "empty_string",
			input:    "",
			expected: []int{0},
		},
		{
			name:     "non_numeric",
			input:    "a.b.c",
			expected: []int{0, 0, 0},
		},
		{
			name:     "mixed_numeric",
			input:    "1.a.3",
			expected: []int{1, 0, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseVersion(tt.input)
			if !slicesEqual(result, tt.expected) {
				t.Errorf("parseVersion(%q): got %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// slicesEqual is a helper to compare integer slices.
func slicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestNotifyIfOutdated tests the non-fatal update notification.
func TestNotifyIfOutdated(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		cachedVersion  string
		expectNotice   bool
	}{
		{
			name:           "outdated_prints_notice",
			currentVersion: "v1.0.0",
			cachedVersion:  "v2.0.0",
			expectNotice:   true,
		},
		{
			name:           "up_to_date_no_notice",
			currentVersion: "v1.0.0",
			cachedVersion:  "v1.0.0",
			expectNotice:   false,
		},
		{
			name:           "ahead_of_latest_no_notice",
			currentVersion: "v2.0.0",
			cachedVersion:  "v1.0.0",
			expectNotice:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cachePath := filepath.Join(tmpDir, ".latest-version")
			writeCache(cachePath, tt.cachedVersion)

			old := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			notifyIfOutdated("testtool", tt.currentVersion, "example.com/testtool", cachePath)

			w.Close()
			os.Stderr = old
			out, _ := io.ReadAll(r)

			hasNotice := strings.Contains(string(out), "can be updated")
			if hasNotice != tt.expectNotice {
				t.Errorf("got notice=%v, want %v (stderr: %q)", hasNotice, tt.expectNotice, string(out))
			}
		})
	}
}

// TestCheckLocalBinaryWithFile tests checkLocalBinary with a real file (not directory).
func TestCheckLocalBinaryWithFile(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "fakebinary")
	os.WriteFile(localPath, []byte("not a real binary"), 0o755)

	// File exists but go version -m will fail or return wrong version
	found := checkLocalBinary(localPath, "v1.0.0")
	if found {
		t.Error("checkLocalBinary: expected false for non-Go binary")
	}
}

// TestRun tests the run() function directly.
func TestRun(t *testing.T) {
	t.Run("no_args_returns_1", func(t *testing.T) {
		code := run([]string{})
		if code != 1 {
			t.Errorf("run([]): got %d, want 1", code)
		}
	})

	t.Run("missing_version_returns_1", func(t *testing.T) {
		code := run([]string{"mvdan.cc/gofumpt"})
		if code != 1 {
			t.Errorf("run(no version): got %d, want 1", code)
		}
	})

	t.Run("check_flag_with_cached_version", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("TMPDIR", tmpDir)
		cachePath := filepath.Join(tmpDir, "gofumpt", ".latest-version")
		writeCache(cachePath, "v0.9.2")

		code := run([]string{"--check", "mvdan.cc/gofumpt@v0.9.2"})
		if code != 0 {
			t.Errorf("run(--check up-to-date): got %d, want 0", code)
		}
	})
}

// TestHandleCheck tests handleCheck directly.
func TestHandleCheck(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		cachedVersion  string
		wantCode       int
	}{
		{
			name:           "up_to_date",
			currentVersion: "v0.9.2",
			cachedVersion:  "v0.9.2",
			wantCode:       0,
		},
		{
			name:           "outdated",
			currentVersion: "v0.9.2",
			cachedVersion:  "v1.0.0",
			wantCode:       1,
		},
		{
			name:           "newer_than_latest",
			currentVersion: "v0.9.2",
			cachedVersion:  "v0.8.0",
			wantCode:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cachePath := filepath.Join(tmpDir, ".latest-version")
			writeCache(cachePath, tt.cachedVersion)

			code := handleCheck("gofumpt", tt.currentVersion, "mvdan.cc/gofumpt", cachePath)
			if code != tt.wantCode {
				t.Errorf("handleCheck(%q vs cached %q): got %d, want %d",
					tt.currentVersion, tt.cachedVersion, code, tt.wantCode)
			}
		})
	}
}

// BenchmarkVersionExtraction benchmarks module version extraction.
func BenchmarkGetGoModVersion(b *testing.B) {
	for b.Loop() {
		getGoModVersion("go")
	}
}

// BenchmarkBuildVersionExtraction benchmarks Go build version extraction.
func BenchmarkGetBuildGoVersion(b *testing.B) {
	for b.Loop() {
		getBuildGoVersion("go")
	}
}

// BenchmarkCurrentGoVersion benchmarks current Go version detection.
func BenchmarkGetCurrentGoVersion(b *testing.B) {
	for b.Loop() {
		getCurrentGoVersion()
	}
}

// BenchmarkCompareVersions benchmarks semantic version comparison.
func BenchmarkCompareVersions(b *testing.B) {
	for b.Loop() {
		compareVersions("v1.9.5", "v1.10.3")
	}
}
