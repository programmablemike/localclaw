---
title: "lclaw command reference"
description: "Commands, global flags, environment variables, exit codes and the JSON output shape of the lclaw command-line tool."
diataxis: reference
status: stable
last_reviewed: 2026-09-22
tags: [cli, lclaw, up, down, status, doctor, init, secrets, exit-codes, json]
related:
  - ../explanation/cli-architecture.md
  - ../explanation/lifecycle-commands.md
  - ../how-to/set-up-a-development-environment.md
  - ../how-to/bring-the-system-up-and-down.md
  - scaffold.md
  - secrets.md
---

# lclaw command reference

`lclaw` manages LocalClaw's Podman machine, its zones and their workloads.
This page describes the commands that exist today.

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
| `--dir`     | path           | `~/.config/lclaw` | `LCLAW_DIR` | Scaffold directory read by every command except `version`, and written by `init`. |
| `--help`    |                |         |                | Print usage and exit 0.                                                |

When neither `--dir` nor `LCLAW_DIR` supplies a directory, because the home
directory cannot be determined or `LCLAW_DIR` is set but empty, every
command that needs one exits 2 asking for it.

An unknown flag, an unknown command or an invalid `--output` value prints
the error and `Run 'lclaw --help' for usage.` to standard error and exits 2.

## Zones

`up` and `down` take zero or more zone names: `infra`, `services`,
`agent`. No name means every zone. Names select; they never reorder: `up`
always applies zones in the order `infra`, `services`, `agent`, and `down`
takes them down in reverse. An unknown name, or a name given twice, is a
usage error (exit 2). The zones and their networks are described in the
[scaffold reference](scaffold.md#zones).

## Commands

### `up`

```text
lclaw up [ZONE...]
```

Brings the selected zones up, in this order, and reports one check per
step in the shape `doctor` uses:

| Step | Check name          | Pass summary                                        | Stops when                                                                 |
| ---- | ------------------- | --------------------------------------------------- | -------------------------------------------------------------------------- |
| 1    | `<secret name>`     | (only failures appear)                              | A declared `user` secret the selected zones consume is unset: one `FAIL` per secret, hint `lclaw secrets set NAME`, and nothing else runs |
| 2    | `machine`           | `created and started`, `started` or `running`       | `podman machine init` or `start` fails                                     |
| 3    | `network/<zone>`    | `created`, `created (internal)` or `present`        | `podman network create` fails                                              |
| 4    | `<zone>/secrets`    | `N injected`                                        | A minted secret cannot be obtained (a `FAIL` named after the secret), or `podman secret create` fails |
| 5    | `<zone>/<workload>` | `built and playing`                                 | `podman build` or `kube play` fails; the rest of the zone is not attempted |
| 6    | `services/ready`    | `LiteLLM is ready`                                  | LiteLLM does not answer its readiness endpoint within two minutes         |

Step 1 reads item attributes from the keychain, never values, and runs
before anything else so a missing API key is reported without a boot. Step
2 creates the machine `lclaw` if it does not exist, with the `[machine]`
size, `--volume ""` (or one `--volume` per `volumes` entry) and the
playbook, and starts it if it is not running; a running machine is left
alone. Step 3 creates `lclaw-<zone>` for each selected zone that has none,
with `--internal` where the zone says so. Steps 4 and 5 run once per
selected zone: generated secrets are made and stored in the keychain on
first use, the minted key is requested from LiteLLM, every value the zone
consumes is stored in the machine's secret store with `--replace`, then
each workload in order is built with `podman build --quiet --tag
localhost/lclaw/<workload>:latest` and played with `podman kube play
--replace`, one `--network` per network the topology gives it and
`--userns auto` when its zone is internal. Step 6 follows the `services`
zone and waits for LiteLLM before the `agent` zone's key is minted.

A failed step fails its check, no later step in that zone runs, and every
later zone is reported as a `WARN` named after the zone, summarised
`skipped because <check> failed`. Pods already playing are left running.
Running `up` again resumes from the failure, because every step is
idempotent: a second `up` on an unchanged system creates nothing and
reports `running` and `present`.

`lclaw up agent` with the `services` zone not running fails at step 4
with `openclaw-litellm-key  not minted: ...` and the hint `run `lclaw up
services` first`.

Each step is announced on standard error as it begins, as `==> <check
name>: <detail>`; the report goes to standard output when the command
finishes. Exit status is 1 when any check is `FAIL`, otherwise 0.

### `down`

```text
lclaw down [ZONE...] [--destroy]
```

Takes the selected zones down in reverse order and reports one check per
step:

| Step | Check name          | Pass summary                  | Notes                                                                                       |
| ---- | ------------------- | ----------------------------- | ------------------------------------------------------------------------------------------- |
| 1    | `machine`           |                               | `WARN not created` or `WARN stopped` when there is nothing to take down; the command ends there |
| 2    | `<zone>/<workload>` | `stopped`                     | `podman kube down` in reverse workload order; `WARN absent` for a pod that is not playing   |
| 3    | `network/<zone>`    | `removed`                     | `podman network rm` after the zone's last pod; `WARN kept ...` when a pod could not be stopped |
| 4    | `secrets`           | `N purged`                    | Only when no LocalClaw pod remains on the machine; otherwise `WARN kept: N pod(s) still running` |
| 5    | `machine`           | `stopped`                     | Only when no pod remains; otherwise `WARN running`                                          |
| 6    | `destroy`           | `lclaw removed with its images and volumes` | With `--destroy` only                                                          |
| 7    | `secrets/<name>`    | `forgotten; minted again on the next up` | With `--destroy` only, for each minted secret in the keychain                    |

`down` does not stop at the first failure: a pod that will not stop is a
`FAIL`, the next one is tried, and the report says what remains. Named
volumes survive `down`; `kube down` is never given `--force`. The secret
store is purged and the machine stopped only when nothing lclaw played is
left, because the store is shared by every zone; `lclaw down agent` leaves
the machine up.

| Flag        | Effect                                                                                                                   |
| ----------- | ------------------------------------------------------------------------------------------------------------------------ |
| `--destroy` | After the machine is stopped, `podman machine rm --force lclaw`, which removes the disk and every image and volume on it, and remove the minted key from the keychain so the next `up` mints a fresh one. Refused with exit 2 when a zone is named. Nothing prompts. |

Exit status is 1 when any check is `FAIL`, otherwise 0.

### `status`

```text
lclaw status
```

Reads and never writes. One check for the machine, then, when it is
running, one per zone network and one per workload declared in
`lclaw.toml`:

| Check name          | Pass                   | Warn                                          | Fail                                                        |
| ------------------- | ---------------------- | --------------------------------------------- | ----------------------------------------------------------- |
| `machine`           | `running`              | `not created`, or `stopped` (still a Pass, with the hint `run `lclaw up``) | `podman machine list` failed          |
| `network/<zone>`    | `present`, or `present (internal)` | `missing`, hint `run `lclaw up <zone>``  | `podman network exists` failed (one check named `networks`) |
| `<zone>/<workload>` | `running`: every container running | `absent`: no such pod, hint `run `lclaw up <zone>`` | `degraded: container <name> <state>`, hint `podman --connection lclaw pod logs <pod>`; or `podman pod ps` failed (one check named `pods`) |

Exit status is 1 only for a `FAIL`, so a script can tell "down" (exit 0
with warnings) from "broken".

### `doctor`

Checks that the host can run LocalClaw. Every check runs regardless of
earlier results, and the order is fixed.

| Check            | Pass                           | Warn                                        | Fail                                          |
| ---------------- | ------------------------------ | ------------------------------------------- | --------------------------------------------- |
| `flox`           | Found, version at least 1.0.0  |                                             | Not found, too old, or `flox --version` failed |
| `podman`         | Found, version at least 5.8.0  |                                             | Not found, too old, or `podman --version` failed |
| `scaffold`       | Directory exists               | Directory not found                         | Not a directory, or unreadable                |
| `topology`       | `lclaw.toml` decodes and validates; summary `1 machine, N zones, N workloads` | Skipped because the `scaffold` check failed | Cannot be read or decoded (one check), or one check per validation finding, summary `<where>: <message>` |
| `keychain`       | The keychain file exists       | Skipped because the `topology` check failed | Not found at the configured path              |
| `<secret name>`  | The declared secret is set     |                                             | The declared secret is unset                  |
| `secrets`        |                                | Skipped because the `keychain` check failed |                                               |
| `machine`        | Machine exists: `running` or `stopped` | Machine not created; or skipped because the `podman` check failed | `podman machine list` failed |
| `network/<zone>` | `present`, or `present (internal)` | `missing`                               |                                               |
| `networks`       |                                |                                             | `podman network exists` failed                |

Checks run in the order `flox`, `podman`, `scaffold`, `topology`,
`keychain`, one per secret declared in `lclaw.toml`, `machine`, then the
networks. The secret checks read each item's attributes, never its value,
and appear only when the `keychain` check passes and at least one secret is
declared; the single `secrets` check appears instead when a secret is
declared and the `keychain` check failed. The network checks appear only
when the machine is running and the topology loaded. The validation rules
behind the `topology` findings are in the
[scaffold reference](scaffold.md#validation); the keychain and the
catalogue are in the [secrets reference](secrets.md).

Exit status is 1 when any check is Fail, otherwise 0.

### `init`

Writes the default scaffold into the scaffold directory: `lclaw.toml`, the
machine's playbook, and a `Containerfile`, `pod.yaml` and
`.containerignore` per workload. Files that already exist are skipped and
reported; `--force` overwrites them. Each file is written atomically. The
[scaffold reference](scaffold.md) describes the files.

| Flag      | Effect                                    |
| --------- | ------------------------------------------ |
| `--force` | Overwrite files that already exist.       |

After writing the files, `init` reads `lclaw.toml` and creates the keychain
it names, unless the file already exists. `security` prompts for the new
keychain's password on the terminal, so lclaw never sees it; when standard
input is not a terminal it reads the password and its confirmation as two
lines. An existing keychain is reported as skipped and is never
overwritten, including under `--force`, because overwriting one destroys
every secret in it. When a keychain is created and standard input is not a
terminal, `init` prints a warning to stderr, because an empty stdin makes
`security` create the keychain with an empty password.

Text output lists `written`, `skipped` and `failed` paths in that order,
then the keychain's own `created`, `skipped` or `failed` line, then
`<dir>: N written, N skipped, N failed`, whose counts cover files only.
Exit status is 1 when any file could not be written or the keychain step
failed, otherwise 0; a directory that already holds every file and a
keychain that already exists are both success.

A scaffold written by an earlier `lclaw` with `schema = 1` is reported by
`doctor`, `up`, `down` and `status` as a `topology` finding with the hint
to run `lclaw init --force`.

### `secrets`

`lclaw secrets list`, `describe`, `set`, `update`, `delete` and `get`
manage the items in the LocalClaw keychain and, when the machine is
running, its Podman secret store. The catalogue, the flags, the output
shapes and the exit codes are in the [secrets reference](secrets.md).

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
| `1`   | At least one `up`, `down`, `status` or `doctor` check failed, an init write or the keychain step failed, a secrets command could not reach the keychain, a secret already exists or is not set, the store failed, `lclaw.toml` has findings, or an unexpected error occurred |
| `2`   | Usage error: unknown flag, unknown command, an invalid `--output` value, an unknown or repeated zone name, `--destroy` with a zone, `status` with an argument, an unknown secret name, an invalid secret value, or `--generate` refused |
| `130` | Interrupted by SIGINT or SIGTERM. Every lifecycle step is idempotent, so running the interrupted command again finishes the job |

## JSON output

With `--output json`, `up`, `down`, `status`, `doctor`, `init`, `version`
and every `secrets` subcommand except `get` print one indented JSON
document on standard output. `secrets get` writes the raw value and no
JSON, whatever `--output` says. Logs and `up`/`down` progress lines go to
standard error, never standard output. The `secrets` shapes are in the
[secrets reference](secrets.md#json-output).

### `up`, `down`, `status`, `doctor`

All four share one shape. `status` is the worst check status. `hint` is
present only when non-empty. Status values are `pass`, `warn` and `fail`.

```json
{
  "status": "warn",
  "checks": [
    {
      "name": "machine",
      "status": "pass",
      "summary": "running"
    },
    {
      "name": "network/agent",
      "status": "pass",
      "summary": "present (internal)"
    },
    {
      "name": "agent/openclaw",
      "status": "warn",
      "summary": "absent",
      "hint": "run `lclaw up agent`"
    }
  ]
}
```

### `init`

`written`, `skipped` and `failed` are always present, as arrays of paths
relative to `dir`. Each `failed` entry carries the error text. `keychain`
is always present; its `state` is `created`, `skipped` or `failed`, `path`
is omitted when the topology could not be read, and `error` appears only on
`failed`.

```json
{
  "dir": "/Users/me/.config/lclaw",
  "written": [
    "lclaw.toml",
    "machine/playbook.yaml"
  ],
  "skipped": [
    "workloads/openclaw/pod.yaml"
  ],
  "failed": [
    {
      "path": "workloads/openclaw/Containerfile",
      "error": "osfs: write /Users/me/.config/lclaw/workloads/openclaw/Containerfile: permission denied"
    }
  ],
  "keychain": {
    "path": "/Users/me/Library/Keychains/lclaw.keychain-db",
    "state": "created"
  }
}
```
