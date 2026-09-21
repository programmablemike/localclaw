---
title: "lclaw command reference"
description: "Commands, global flags, environment variables, exit codes and the JSON output shape of the lclaw command-line tool."
diataxis: reference
status: stable
last_reviewed: 2026-09-20
tags: [cli, lclaw, doctor, exit-codes, json]
related:
  - ../explanation/cli-architecture.md
  - ../how-to/set-up-a-development-environment.md
---

# lclaw command reference

`lclaw` manages LocalClaw's Podman machines. This page describes the
commands that exist today.

## Synopsis

```text
lclaw [--output text|json] [--verbose] <command> [arguments]
```

Global flags come before the command name.

## Global flags

| Flag        | Values         | Default | Environment    | Effect                                                                 |
| ----------- | -------------- | ------- | -------------- | ---------------------------------------------------------------------- |
| `--output`  | `text`, `json` | `text`  | `LCLAW_OUTPUT` | Output format for every command.                                       |
| `--verbose` |                | off     |                | Log every external command, its duration and stderr to standard error. |
| `--help`    |                |         |                | Print usage and exit 0.                                                |

An unknown flag or an invalid `--output` value prints the error and
`Run 'lclaw --help' for usage.` to standard error and exits 2.

## Commands

### `doctor`

Checks that the host can run LocalClaw. Every check runs regardless of
earlier results, and the order is fixed.

| Check            | Pass                           | Warn                                        | Fail                                          |
| ---------------- | ------------------------------ | ------------------------------------------- | --------------------------------------------- |
| `flox`           | Found, version at least 1.0.0  |                                             | Not found, too old, or `flox --version` failed |
| `podman`         | Found, version at least 5.0.0  |                                             | Not found, too old, or `podman --version` failed |
| `lclaw-infra`    | Machine exists                 | Machine not created                         |                                               |
| `lclaw-services` | Machine exists                 | Machine not created                         |                                               |
| `lclaw-agent`    | Machine exists                 | Machine not created                         |                                               |
| `machines`       |                                | Skipped because the `podman` check failed   | `podman machine list` failed                  |

The three machine checks appear when `podman` passes; the single
`machines` check appears instead when it does not. A Pass summary for a
machine says `running` or `stopped`.

Exit status is 1 when any check is Fail, otherwise 0.

### `version`

Prints the version from the `VERSION` file, the commit, commit time and
whether the tree was modified (as recorded by the Go toolchain; `unknown`
when built outside a git checkout, for example by `flox build`), and the Go
version and platform.

### `completion`

Prints a shell completion script: `lclaw completion bash`, `zsh`, `fish` or
`pwsh`.

## Exit codes

| Code  | Meaning                                                            |
| ----- | ------------------------------------------------------------------ |
| `0`   | Success. Warnings do not change the exit code.                     |
| `1`   | At least one `doctor` check failed, or an unexpected error occurred |
| `2`   | Usage error: unknown flag, or an invalid `--output` value          |
| `130` | Interrupted by SIGINT or SIGTERM                                   |

## JSON output

With `--output json` every command prints one indented JSON document on
standard output. Logs never go to standard output.

### `doctor`

`status` is the worst check status. `hint` is present only when non-empty.
Status values are `pass`, `warn` and `fail`.

```json
{
  "status": "warn",
  "checks": [
    {
      "name": "flox",
      "status": "pass",
      "summary": "1.13.1 (minimum 1.0.0)"
    },
    {
      "name": "podman",
      "status": "pass",
      "summary": "5.8.4 (minimum 5.0.0)"
    },
    {
      "name": "lclaw-infra",
      "status": "warn",
      "summary": "not created",
      "hint": "run `lclaw up` once it is available"
    }
  ]
}
```

### `version`

```json
{
  "version": "0.1.0-dev",
  "commit": "9c2afc1e0f3b4a5d6c7e8f9a0b1c2d3e4f5a6b7c",
  "date": "2026-09-20T19:04:11Z",
  "modified": false,
  "go": "go1.26.7",
  "platform": "darwin/arm64"
}
```
