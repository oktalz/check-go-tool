# check-go-tool

A standalone utility to resolve, verify, and install Go tools with version and Go build version validation.

## Purpose

`check-go-tool` automates the process of finding or installing Go tools, ensuring both the tool version and the Go compiler version match expectations. It's designed to work with Taskfile or other build orchestration tools.

## Features

- **System PATH Priority**: Checks system `$PATH` for an existing binary with matching version and Go build version
- **Local Fallback**: Falls back to a cached binary at `$TMPDIR/<name>/<version>/<name>`
- **Automatic Installation**: Uses `go install <path>@<version>` to install tools with the current Go compiler when not found
- **Version Validation**: Extracts and validates tool module version via `go version -m <binary>`
- **Go Version Matching**: Ensures tool was built with the same Go version as currently in use
- **Update Detection**: Optional `--check` flag to report whether a newer version is available upstream
- **Latest Mode**: `package@latest` installs to default GOBIN and automatically upgrades when a newer version is available
- **Auto Name Detection**: Binary name is derived from the import path (e.g., `mvdan.cc/gofumpt` -> `gofumpt`, `github.com/mikefarah/yq/v4` -> `yq`)

## Installation

```bash
go install github.com/oktalz/check-go-tool@latest
```

## Usage

```sh
check-go-tool [--check] <package@version>
check-go-tool version | tag
```

### Arguments

- `<package@version>`: Go import path with version (e.g., `mvdan.cc/gofumpt@v0.9.2` or `mvdan.cc/gofumpt@latest`)

### Flags

- `--check`: Check if a newer version is available (no install, informational only)

### Subcommands

- `version`: Print the tool version and exit
- `tag`: Print the tag and exit

### Examples

#### Basic usage — find or install `gofumpt`

```bash
check-go-tool mvdan.cc/gofumpt@v0.9.2
# Output: /tmp/gofumpt/v0.9.2/gofumpt  (or system PATH path)
```

#### Always use latest version

Unlike pinned versions (which install to `$TMPDIR`), `@latest` installs to the
default `GOBIN` (e.g. `~/go/bin`) — the same location as `go install ... @latest`.

On each run it checks whether the installed binary is still the latest version
(using a 23-hour cache). If a newer version exists, it automatically upgrades
the binary in place before returning its path.

```bash
check-go-tool mvdan.cc/gofumpt@latest
# First run:  installs gofumpt to ~/go/bin, caches resolved version
# Later runs: returns ~/go/bin/gofumpt immediately (cache hit, no network)
# After 23h:  re-checks upstream, upgrades if newer version exists
```

#### Check for available updates

```bash
check-go-tool --check mvdan.cc/gofumpt@v0.9.2
# Output: gofumpt can be updated from v0.9.2 to v0.10.0
# Exits: 1 (update available)

check-go-tool --check mvdan.cc/gofumpt@v0.10.0
# Output: gofumpt is up-to-date (v0.10.0)
# Exits: 0 (no update needed)
```

#### Print version info

```bash
check-go-tool version
# Output: v1.0.0.a1b2c3d4

check-go-tool tag
# Output: v1.0.0
```

#### Use as a helper tool

`check-go-tool` is designed to be used as a helper that resolves tool paths for
build systems. It prints the path to the binary on stdout, so you can capture it
in a variable and use it to run the tool. If the tool is not installed or was
built with a different Go version, it installs it automatically.

This makes it easy to pin tool versions across CI and developer machines without
requiring each developer to manually install them.

```bash
# Capture the resolved path and use it
GOFUMPT_BIN=$(check-go-tool mvdan.cc/gofumpt@v0.9.2)
$GOFUMPT_BIN -l -w .
```

#### Use in Taskfile

When used with [Taskfile](https://taskfile.dev/), define tool URLs as vars and
use `sh:` to resolve binary paths. You can use `go run .` during development
of `check-go-tool` itself, or the installed binary in other projects:

```yaml
vars:
  GOFUMPT: mvdan.cc/gofumpt@v0.9.2
  STATICCHECK: honnef.co/go/tools/cmd/staticcheck@v0.7.0

tasks:
  format:
    vars:
      GOFUMPT_BIN:
        sh: check-go-tool {{.GOFUMPT}}
    cmds:
      - "{{.GOFUMPT_BIN}} -l -w ."

  lint:
    vars:
      STATICCHECK_BIN:
        sh: check-go-tool {{.STATICCHECK}}
    cmds:
      - "{{.STATICCHECK_BIN}} ./..."

  check-tool-updates:
    desc: "check if tools have updates available"
    cmds:
      - check-go-tool --check {{.GOFUMPT}} || true
      - check-go-tool --check {{.STATICCHECK}} || true
```

## Resolution Logic

### Pinned version (e.g. `package@v0.9.2`)

1. **System PATH**: If a binary matching the tool name exists on `$PATH` with:
   - Module version matching the requested version
   - Go build version matching current `go version` output
   -> Use system binary

2. **Local Cache**: If binary exists at `$TMPDIR/<name>/<version>/<name>` with:
   - Module version matching the requested version
   - Go build version matching current `go version` output
   -> Use cached binary

3. **Install**: Run `go install <path>@<version>` with `GOBIN=$TMPDIR/<name>/<version>`
   -> Use newly installed binary

After resolving, the tool checks `$TMPDIR/<name>/.latest-version` for a cached latest version.
If no cache exists, it queries the upstream Go module proxy and populates the cache.
If a newer version is available, a notice is printed to stderr:

```sh
gofumpt can be updated from v0.9.2 to v0.10.0
```

### Latest version (`package@latest`)

1. **Check PATH**: Look for the tool on `$PATH`
2. **If not found**: Run `go install <path>@latest` (default GOBIN, not tmp), cache the resolved version
3. **If found**: Compare installed version against cached latest (or query upstream if no cache)
   - Up-to-date -> use existing binary
   - Outdated -> run `go install <path>@latest`, update cache

The binary lives in the default GOBIN (e.g. `~/go/bin`), not in the tmp folder.

### Check mode (`--check`)

- Queries latest version via `go list -m -json <path>@latest` (requires network)
- Caches the result in `$TMPDIR/<name>/.latest-version` for 23 hours
- Compares the requested version against latest using semantic versioning
- Reports result to stderr and exits (no install, no path output)

### Version cache

All modes share the same cache file at `$TMPDIR/<name>/.latest-version` with a 23-hour TTL.
The cache is populated on first use (any mode) and refreshed after expiry.
Normal pinned-version runs show a passive update notice from cache; `--check` forces
an explicit check and reports the result as its exit code.

## Environment Variables

- `TMPDIR`: Used for local tool cache and version cache (defaults to `/tmp` on Unix if not set)

## Exit Codes

- `0`: Success (tool found or installed; or `--check` mode and version is up-to-date)
- `1`: Failure (missing required arguments, install failed, or `--check` mode with update available)

## Check Mode (--check)

The `--check` flag queries the upstream Go module proxy for the latest version, caches the result, and compares it against the requested version.

**Behavior:**

- Queries the latest available version from upstream Go modules
- Caches the result for 23 hours in `$TMPDIR/<name>/.latest-version`
- Compares the requested version with latest version
- Outputs human-readable status message to stderr
- Does NOT install or modify any files
- Exits with appropriate code (0 = up-to-date or ahead, 1 = update available or error)

**Output Examples:**

```sh
gofumpt can be updated from v0.9.2 to v0.10.0  (exit 1)
gofumpt is up-to-date (v0.10.0)                (exit 0)
gofumpt v0.11.0 is newer than latest v0.10.0   (exit 0)
error: failed to check latest version: ...     (exit 1)
```

**Update Notifications (normal mode):**

During normal tool resolution (without `--check`), the tool reads the version cache.
If no cache exists, it queries upstream and populates the cache (network errors are silently ignored).
If a newer version is known, a notice is printed to stderr. Subsequent runs use the cached value (no network) until the 23-hour TTL expires.

**Requirements for --check:**

- Network access (queries Go module proxy)
- Version included in the argument (used for comparison, not installation)
- Fails strictly on network errors — no silent fallback

**Use Cases:**

- Run `--check` in CI to populate cache and detect outdated tool versions
- Normal runs passively show update notices from cache (no network overhead)
- Scheduled `--check` keeps the cache fresh for all developers

## Version Detection

### Module Version

Extracted from `go version -m <binary>` output (third field after `mod` keyword).

Example:

```sh
go version -m /usr/bin/gofumpt
/usr/bin/gofumpt: go1.21.0
  mod mvdan.cc/gofumpt v0.9.2 h5:...
```

Extracted version: `v0.9.2`

### Go Build Version

Extracted from `go version <binary>` output (last field).

Example:

```sh
go version /usr/bin/gofumpt
/usr/bin/gofumpt go1.21.0
```

Extracted version: `go1.21.0`

## Use Cases

### Build-Time Tool Installation

Ensures all build tools are consistently versioned across CI/CD environments and developer machines:

```bash
STATICCHECK=$(check-go-tool honnef.co/go/tools/cmd/staticcheck@v0.7.0)
$STATICCHECK ./...
```

### CI Update Monitoring

Check that tools are up-to-date in CI:

```bash
check-go-tool --check github.com/mgechev/revive@v1.15.0
if [ $? -ne 0 ]; then
  echo "revive has an update available"
fi
```

### Cross-Platform Builds

Ensures tools are built with the local Go version, preventing "cannot run binary built with go1.20 on go1.21" errors.

## Testing

```bash
go test -v ./...
```

Run benchmarks for version detection:

```bash
go test -bench=. -benchmem ./...
```
