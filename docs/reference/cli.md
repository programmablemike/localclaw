---
title: "lclaw command reference"
description: "Commands, global flags, environment variables, exit codes and the JSON output shape of the lclaw command-line tool."
diataxis: reference
status: stable
last_reviewed: 2026-09-21
tags: [cli, lclaw, doctor, init, exit-codes, json]
related:
  - ../explanation/cli-architecture.md
  - ../how-to/set-up-a-development-environment.md
  - scaffold.md
---

# lclaw command reference

`lclaw` manages LocalClaw's Podman machines. This page describes the
commands that exist today.

## Synopsis

```text
lclaw [--output text|json] [--verbose] [--dir <path>] <command> [arguments]
```

Global flags may appear before or after the command name.

## Global flags

| Flag        | Values         | Default | Environment    | Effect                                                                 |
| ----------- | -------------- | ------- | -------------- | ---------------------------------------------------------------------- |
| `--output`  | `text`, `json` | `text`  | `LCLAW_OUTPUT` | Output format for every command.                                       |
| `--verbose` |                | off     |                | Log every external command, its duration and stderr to standard error. |
| `--dir`     | path           | `~/.config/lclaw` | `LCLAW_DIR` | Scaffold directory read by `doctor` and written by `init`. |
| `--help`    |                |         |                | Print usage and exit 0.                                                |

When the home directory cannot be determined and neither `--dir` nor
`LCLAW_DIR` is set, `doctor` and `init` exit 2 asking for one.

An unknown flag, an unknown command or an invalid `--output` value prints
the error and `Run 'lclaw --help' for usage.` to standard error and exits 2.

## Commands

### `doctor`

Checks that the host can run LocalClaw. Every check runs regardless of
earlier results, and the order is fixed.

| Check            | Pass                           | Warn                                        | Fail                                          |
| ---------------- | ------------------------------ | ------------------------------------------- | --------------------------------------------- |
| `flox`           | Found, version at least 1.0.0  |                                             | Not found, too old, or `flox --version` failed |
| `podman`         | Found, version at least 5.8.0  |                                             | Not found, too old, or `podman --version` failed |
| `scaffold`       | Directory exists               | Directory not found                         | Not a directory, or unreadable                |
| `topology`       | `lclaw.toml` decodes and validates; summary counts machines and workloads | Skipped because the `scaffold` check failed | Cannot be read or decoded (one check), or one check per validation finding, summary `<where>: <message>` |
| `lclaw-infra`    | Machine exists                 | Machine not created                         |                                               |
| `lclaw-services` | Machine exists                 | Machine not created                         |                                               |
| `lclaw-agent`    | Machine exists                 | Machine not created                         |                                               |
| `machines`       |                                | Skipped because the `podman` check failed   | `podman machine list` failed                  |

Checks run in the order `flox`, `podman`, `scaffold`, `topology`, then the
machines. The three machine checks appear when `podman` passes; the single
`machines` check appears instead when it does not. A Pass summary for a
machine says `running` or `stopped`. The validation rules behind the
`topology` findings are in the [scaffold reference](scaffold.md#validation).

Exit status is 1 when any check is Fail, otherwise 0.

### `init`

Writes the default scaffold into the scaffold directory: `lclaw.toml`, one
playbook per machine, and a `Containerfile`, `pod.yaml` and
`.containerignore` per workload. Files that already exist are skipped and
reported; `--force` overwrites them. Each file is written atomically. The
[scaffold reference](scaffold.md) describes the files.

| Flag      | Effect                                    |
| --------- | ------------------------------------------ |
| `--force` | Overwrite files that already exist.       |

Text output lists `written`, `skipped` and `failed` paths in that order,
then `<dir>: N written, N skipped, N failed`. Exit status is 1 when any
file could not be written, otherwise 0; a directory that already holds
every file is success.

### `version`

Prints the version from the `VERSION` file, the commit, commit time and
whether the tree was modified (as recorded by the Go toolchain; `unknown`
when built outside a git checkout, for example by `flox build`), and the Go
version and platform.

### `completion`

Prints a shell completion script: `lclaw completion bash`, `zsh`, `fish` or
`pwsh`.

## Exit codes

| Code  | Meaning                                                                    |
| ----- | -------------------------------------------------------------------------- |
| `0`   | Success. Warnings do not change the exit code.                             |
| `1`   | At least one `doctor` check failed, an init write failed, or an unexpected error occurred |
| `2`   | Usage error: unknown flag, unknown command, or an invalid `--output` value |
| `130` | Interrupted by SIGINT or SIGTERM                                           |

## JSON output

With `--output json`, `doctor`, `init` and `version` print one indented JSON
document on standard output. Logs never go to standard output.

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
      "summary": "5.8.4 (minimum 5.8.0)"
    },
    {
      "name": "scaffold",
      "status": "pass",
      "summary": "/Users/me/.config/lclaw"
    },
    {
      "name": "topology",
      "status": "pass",
      "summary": "3 machines, 7 workloads"
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

### `init`

`written`, `skipped` and `failed` are always present, as arrays of paths
relative to `dir`. Each `failed` entry carries the error text.

```json
{
  "dir": "/Users/me/.config/lclaw",
  "written": [
    "lclaw.toml",
    "machines/agent/playbook.yaml"
  ],
  "skipped": [
    "workloads/openclaw/pod.yaml"
  ],
  "failed": [
    {
      "path": "workloads/openclaw/Containerfile",
      "error": "osfs: write /Users/me/.config/lclaw/workloads/openclaw/Containerfile: permission denied"
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
