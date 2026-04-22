# AGENTS.md

## Build, Lint, and Test Commands

### Go Commands

```bash
# Static analysis
go vet ./...

# Run all Go tests
go test ./...

# Run single test (use full name with package)
go test -run TestT0401ModeViewerEmitsTickEvents ./internal/tui/runtime/...

# Build binary (disables version info for reproducibility)
go build -buildvcs=false -o pocket-go ./cmd/pocket

# Format code (required before commit)
gofmt -w ./...
```

### Shell Script Tests

```bash
# Run smoke tests
POCKETCLI_GO_BINARY=./pocket-go sh tests/run_smoke.sh

# Run all shell regression tests
POCKETCLI_TEST_EXCLUDES="test_ish.sh" sh tests/run_all.sh
```

### CI Workflows

- `.github/workflows/ci.yml` - Go build, tests, smoke tests (Linux/macOS/Alpine)
- `.github/workflows/lint.yml` - ShellCheck, bash syntax, shebang consistency

---

## Code Style Guidelines

### Go Conventions

- **Go version:** 1.22
- **Dependencies:** bubbletea (TUI), cobra (CLI) via `third_party/`
- **Formatting:** Use `gofmt` (enforced in CI). Run before commit.
- **No comments** in code unless explicitly requested by user.

### Import Order

1. Standard library (`fmt`, `os`, `context`, etc.)
2. External packages (`github.com/...`)
3. Internal packages (`pocketcli/...`)

Example:
```go
import (
    "context"
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/spf13/cobra"
    "pocketcli/internal/tui/event"
)
```

### Naming Conventions

- **Functions/Variables:** `camelCase`
- **Types:** `PascalCase`
- **Constants:** `SCREAMING_SNAKE_CASE` or `PascalCase` for grouped consts
- **Packages:** short, lowercase, no underscores (`runtime`, `event`, `renderer`)
- **Test functions:** `TestT####Description` where #### is a 4-digit sequence

### Error Handling

- Use sentinel errors:
  ```go
  var ErrTerminalTooSmall = errors.New("runtime: terminal below minimum size (10x3)")
  ```
- Wrap errors with context:
  ```go
  return fmt.Errorf("%w: %v", ErrSetupFailed, err)
  ```

### Concurrency

- Use `sync.Mutex` for shared state access
- Document goroutine lifetimes and ownership

---

## Shell Script Guidelines

### Shebang

- Use `#!/usr/bin/env bash` for bash features (arrays, `[[ ]]`, process substitution)
- Use `#!/bin/sh` for POSIX-compatible scripts

### ShellCheck

- CI runs ShellCheck with SC1091 exclusion (dynamic source paths are intentional)
- Scripts that export variables should include `# shellcheck disable=SC2034` inline

### Variables

- Always quote variables: `"${VAR}"` not `$VAR`
- Use `set -eu` for scripts that should fail fast

---

## Project Architecture

### Directory Structure

```
cmd/pocket/          # CLI entry points
internal/
  tui/               # Terminal UI (runtime, renderer, event)
  memory/            # Memory storage and retrieval
  contextcollector/  # AI context gathering
  audit/             # Command audit logging
  backend/           # Tailscale backend integration
  router/            # Command routing
  core/              # Core domain models
  tools/             # Tool definitions
  ssh/               # SSH operations
  tailscale/         # Tailscale CLI wrapper
profile/             # User customization (shellrc, zshrc, starship, tmux)
scripts/             # Installation and session scripts
tests/               # Shell regression tests
third_party/         # Vendored dependencies (cobra, bubbletea)
```

### Profile/Customization

- User customization belongs in `profile/` directory
- Files outside `profile/` should reference it via constants or a central identifier

---

## Testing Guidelines

### Go Tests

- Tests live alongside implementation: `runtime.go` → `runtime_test.go`
- Use fake implementations for dependencies (e.g., `fakeTerminal`, `fakeRenderer`)

### Shell Tests

- Tests use `set -eu` for strict mode
- Mock external dependencies via `$PATH` manipulation

---

## Regression Prevention

Before opening a PR that touches Go code, run:

```bash
go fmt ./...
go vet ./...
go test ./...
go build -buildvcs=false -o pocket-go ./cmd/pocket
```

For shell changes:

```bash
shellcheck scripts/**/*.sh
bash -n scripts/**/*.sh
sh tests/run_all.sh
```

---

## Existing Project-Specific Rules

- **pocket update:** Preserve user customization even when local files differ from default
- **profile/ folder:** Centralize all user customizations here
- **Release tags:** Follow versioning in git tags (v1.x.y)