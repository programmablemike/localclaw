---
title: "Scaffold reference"
description: "Layout of the lclaw scaffold directory, the lclaw.toml schema and validation rules, the Pod file conventions, and what lclaw init does to each file."
diataxis: reference
status: stable
last_reviewed: 2026-09-21
tags: [scaffold, lclaw-toml, topology, pod, containerfile, playbook, init]
related:
  - cli.md
  - ../how-to/apply-a-deployment-by-hand.md
  - ../explanation/deployment-model.md
---

# Scaffold reference

The scaffold is the directory of files that describe a LocalClaw deployment:
which Podman machines exist, how big they are, what runs on each, and how
each workload's image is built and its pod is run. `lclaw init` writes the
defaults; the user owns and edits the result.

## Location

| Source      | Value                                                         |
| ----------- | ------------------------------------------------------------- |
| Default     | `~/.config/lclaw`                                             |
| Environment | `LCLAW_DIR`                                                   |
| Flag        | `--dir`, a global flag (see the [command reference](cli.md)) |

## Layout

```text
lclaw.toml                         topology: machines, sizes, workload lists
machines/<role>/playbook.yaml      first-boot Ansible playbook for lclaw-<role>
workloads/<name>/Containerfile     image build: FROM the upstream image, pinned
workloads/<name>/pod.yaml          Kubernetes Pod file applied with podman kube play
workloads/<name>/.containerignore  build-context exclusions
```

Roles are `infra`, `services` and `agent`; the machines are named
`lclaw-<role>`. Each workload directory is the build context of its image,
which is tagged `localhost/lclaw/<name>:latest` on the machine that runs it.

## `lclaw.toml`

### Top-level keys

| Key        | Required | Type    | Allowed values                        | Purpose                                                                      |
| ---------- | -------- | ------- | -------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `schema`   | yes      | integer | `1`                                   | Format version.                                                              |
| `provider` | yes      | string  | `libkrun`, `applehv`                  | Podman machine provider, passed as `CONTAINERS_MACHINE_PROVIDER`.            |
| `machines` | yes      | table   | sub-tables `infra`, `services`, `agent`, each exactly once | One table per role. The roles are fixed; a fourth machine is a finding. |

### `[machines.<role>]`

| Key          | Required | Type             | Allowed values                                                              | Maps to                                    |
| ------------ | -------- | ---------------- | --------------------------------------------------------------------------- | ------------------------------------------ |
| `cpus`       | yes      | integer          | greater than 0                                                              | `podman machine init --cpus`               |
| `memory-mib` | yes      | integer          | greater than 0, in MiB                                                      | `--memory`                                 |
| `disk-gib`   | yes      | integer          | greater than 0, in GiB                                                      | `--disk-size`                              |
| `workloads`  | yes      | array of strings | lowercase letters, digits and hyphens, not starting with a hyphen; each names a directory under `workloads/`; no name under two machines | Applied in order, torn down in reverse |
| `volumes`    | no       | array of strings | `"<host>:<guest>"`, both absolute paths                                     | One `--volume` flag each; empty means no host directory is mounted |

Unknown keys anywhere in the file are rejected when the file is read.

### Validation

`lclaw doctor` reads the file and reports one failed `topology` check per
problem, as `<where>: <message>` where `<where>` is the TOML path. The rules,
in the order they are checked:

| Where                            | Rule                                                                   |
| --------------------------------- | ------------------------------------------------------------------------ |
| `schema`                         | is `1`                                                                 |
| `provider`                       | is `libkrun` or `applehv`                                              |
| `machines.<name>`                | `<name>` is a role                                                     |
| `machines.<role>.cpus`, `.memory-mib`, `.disk-gib` | positive                                             |
| `machines.<role>.workloads`      | each name is valid, listed once in the whole file, and its directory holds `Containerfile`, `pod.yaml` and `.containerignore` |
| `machines.<role>.volumes[<i>]`   | two absolute paths joined by a colon                                   |
| `machines`                       | each role appears exactly once                                         |

### Default

```toml
schema = 1
provider = "libkrun"

[machines.infra]
cpus = 1
memory-mib = 1024
disk-gib = 10
workloads = ["wireguard", "kuma-cp", "gateway"]

[machines.services]
cpus = 2
memory-mib = 4096
disk-gib = 30
workloads = ["litellm-db", "litellm", "agentgateway"]

[machines.agent]
cpus = 2
memory-mib = 4096
disk-gib = 30
workloads = ["openclaw"]
```

## `machines/<role>/playbook.yaml`

An Ansible playbook that `podman machine init --playbook` copies into the
machine and runs once after the first boot, as the `core` user. Podman
copies only this file, so it cannot include or copy anything else from the
host. The default playbook has one task: enable `podman-restart.service`
at user scope, which restarts every container whose restart policy is
`always` after the machine reboots.

## `workloads/<name>/Containerfile`

One comment block and one instruction: `FROM <image>:<tag>@sha256:<digest>`.
Customizations go after the `FROM` line. The default images:

| Machine    | Workload       | Image                                                                     |
| ---------- | -------------- | -------------------------------------------------------------------------------------------------- |
| `infra`    | `wireguard`    | `docker.io/linuxserver/wireguard:1.0.20260223-r0-ls122` (provisional)     |
| `infra`    | `kuma-cp`      | `docker.io/kumahq/kuma-cp:2.14.5` (provisional)                           |
| `infra`    | `gateway`      | `docker.io/kumahq/kuma-dp:2.14.5` (provisional)                           |
| `services` | `litellm-db`   | `docker.io/library/postgres:18.6`                                         |
| `services` | `litellm`      | `ghcr.io/berriai/litellm:v1.83.14-stable`                                 |
| `services` | `agentgateway` | `cr.agentgateway.dev/agentgateway:v1.5.0`                                 |
| `agent`    | `openclaw`     | `ghcr.io/openclaw/openclaw:2026.9.5`                                      |

The digests are in the files. Provisional rows are placeholders until the
network design settles how WireGuard and the gateway run.

## `workloads/<name>/pod.yaml`

A multi-document Kubernetes file that `podman kube play` applies as one
unit. Every default file keeps these conventions and customized files must
too:

| Item            | Convention                                                                                                  |
| --------------- | ----------------------------------------------------------------------------------------------------------- |
| Pod             | Exactly one `Pod`, named `<name>`, labelled `app.kubernetes.io/name: <name>` and `app.kubernetes.io/part-of: localclaw` |
| Restart         | `restartPolicy: Always`                                                                                     |
| Image           | `localhost/lclaw/<name>:latest` with `imagePullPolicy: Never`                                               |
| Ports           | `containerPort` documents what the workload listens on; `hostPort` appears only in `infra` workloads        |
| State           | `PersistentVolumeClaim` documents named `<name>-<purpose>`, never `hostPath`                                |
| Configuration   | `ConfigMap` documents in the same file, mounted as a directory                                              |
| Secrets         | Referenced by name with `secretKeyRef`; no `Secret` document is ever present                                |

### Default ports

| Workload       | Container ports | Host port |
| -------------- | ---------------- | --------- |
| `wireguard`    | 51820/udp       | 51820     |
| `kuma-cp`      | 5681, 5678      | 5681      |
| `gateway`      | 8080            | 8080      |
| `litellm-db`   | 5432            |           |
| `litellm`      | 4000            |           |
| `agentgateway` | 3000, 15000     |           |
| `openclaw`     | 18789           |           |

### Default secrets

The secrets design creates these; the Pod files only name them. Every
injected secret is a Kubernetes Secret with the single key `value`, as the
secrets design specifies.

| Secret                   | Key     | Used by                    | Environment variable                |
| ------------------------ | ------- | --------------------------- | ------------------------------------ |
| `litellm-master-key`     | `value` | `litellm`                  | `LITELLM_MASTER_KEY`                |
| `litellm-salt-key`       | `value` | `litellm`                  | `LITELLM_SALT_KEY`                  |
| `litellm-db-password`    | `value` | `litellm-db`, `litellm`    | `POSTGRES_PASSWORD`, `DATABASE_PASSWORD` |
| `openclaw-gateway-token` | `value` | `openclaw`                 | `OPENCLAW_GATEWAY_TOKEN`            |

## `workloads/<name>/.containerignore`

Excludes `pod.yaml` from the build context, which the remote client sends
to the machine as a tarball on every build.

## What `lclaw init` does to each file

| Condition                              | Outcome   | Reported as     |
| --------------------------------------- | --------- | ----------------- |
| Path does not exist                    | written   | `written  <path>`|
| Path exists, no `--force`              | untouched | `skipped  <path>`|
| Path exists, `--force`                 | replaced  | `written <path>`|
| Write fails (permissions, disk)        | untouched | `failed   <path>: <error>` |

Each file is written to a temporary name in the same directory and renamed
into place, so an interrupted run leaves no half-written file. The run
continues past failures and exits 1 only if any occurred. Flags and the
JSON shape are in the [command reference](cli.md#init).
