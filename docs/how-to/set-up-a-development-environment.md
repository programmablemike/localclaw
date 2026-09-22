---
title: "Set up a development environment"
description: "Enter the Flox environment, run the check script, build lclaw from source and run doctor against your machine."
diataxis: how-to
status: stable
last_reviewed: 2026-09-22
tags: [development, flox, go, build, testing]
related:
  - ../reference/cli.md
  - ../explanation/cli-architecture.md
  - ../../AGENTS.md
---

# Set up a development environment

When you finish, every check passes on your machine, you have a `lclaw`
binary built from your checkout, and you know how the release package is
built.

## Prerequisites

- [Flox](https://flox.dev/docs/install-flox/) 1.0 or newer. It provides
  Go and every other tool; do not install Go separately.
- A clone of the repository.
- [Podman](https://podman.io/docs/installation) 5 or newer, only if you want
  `lclaw doctor` to report a working host. The checks and build run without
  it.

## Steps

### 1. Enter the environment

```bash
flox activate
```

The first activation downloads the toolchain. Every command below runs
inside this shell. To run a single command without an interactive shell,
prefix it: `flox activate -- go test ./...`.

### 2. Run the checks

```bash
./scripts/check.sh
```

This runs `gofmt`, `go vet`, staticcheck, govulncheck, the tests with the
race detector, `go mod tidy -diff` and a vendor drift check, and ends with
`==> ok`. It is the same script CI runs.

### 3. Build and run a development binary

```bash
go build -o bin/lclaw ./cmd/lclaw
bin/lclaw doctor
```

`bin/` is ignored by git. `doctor` reports Flox, Podman, the scaffold and
the LocalClaw machine checks; see the
[command reference](../reference/cli.md) for what each line means.

### 4. Build the release package

```bash
flox build lclaw
./result-lclaw/bin/lclaw version
```

This is a hermetic Nix build from the vendored dependencies. It runs the
unit tests and writes the binary to `result-lclaw/bin/lclaw`. `version`
reports the commit as `unknown` here because the store path carries no git
metadata.

### 5. Add or update a dependency

```bash
go get <module>@<version>
go mod tidy
go mod vendor
```

Commit `go.mod`, `go.sum` and `vendor/` together. Measure the module graph
first, as the
[dependency policy](../explanation/cli-architecture.md#dependency-policy)
requires.

## Verify

```bash
./scripts/check.sh && flox build lclaw && ./result-lclaw/bin/lclaw version
```

Expected: `==> ok`, a completed build, and a version line.
