---
title: "Scaffold reference"
description: "Layout of the lclaw scaffold directory, the lclaw.toml schema and validation rules, the Pod file conventions, and what lclaw init does to each file."
diataxis: reference
status: stable
last_reviewed: 2026-09-22
tags: [scaffold, lclaw-toml, topology, zones, pod, containerfile, playbook, init]
related:
  - cli.md
  - secrets.md
  - ../how-to/apply-a-deployment-by-hand.md
  - ../explanation/deployment-model.md
  - ../explanation/single-machine.md
---

# Scaffold reference

The scaffold is the directory of files that describe a LocalClaw deployment:
the one Podman machine and its size, the zones and what runs on each, and
how each workload's image is built and its pod is run. `lclaw init` writes
the defaults; the user owns and edits the result.

## Location

| Source      | Value                                                         |
| ----------- | ------------------------------------------------------------- |
| Default     | `~/.config/lclaw`                                             |
| Environment | `LCLAW_DIR`                                                   |
| Flag        | `--dir`, a global flag (see the [command reference](cli.md)) |

## Layout

```text
lclaw.toml                         topology: the machine, the zones, workload lists
machine/playbook.yaml              first-boot Ansible playbook for the machine
workloads/<name>/Containerfile     image build: FROM the upstream image, pinned
workloads/<name>/pod.yaml          Kubernetes Pod file applied with podman kube play
workloads/<name>/.containerignore  build-context exclusions
```

The machine is named `lclaw`, and so is the Podman connection that
reaches it. Each workload directory is the build context of its image,
which is tagged `localhost/lclaw/<name>:latest` on the machine.

## Zones

A zone is one Podman network on the machine and the workloads placed on
it. The three zones are `infra`, `services` and `agent`; their networks are
`lclaw-infra`, `lclaw-services` and `lclaw-agent`. Containers on different
networks cannot reach each other by address or by name. A pod joins its
own zone's network and, when its zone's `bridges` table names it, the
networks of the zones listed there. An `internal` zone's network has no
route to the host, the internet or anything else; only pods bridged into
it can reach it.

| Zone       | Network          | Default workloads                       | Default bridges                          | Internal |
| ---------- | ---------------- | --------------------------------------- | ---------------------------------------- | -------- |
| `infra`    | `lclaw-infra`    | `kuma-cp`, `gateway`                    | `kuma-cp` joins `services` and `agent`   | no       |
| `services` | `lclaw-services` | `litellm-db`, `litellm`, `agentgateway` | `agentgateway` joins `agent`             | no       |
| `agent`    | `lclaw-agent`    | `openclaw`                              |                                          | yes      |

`lclaw up` plays every pod of an internal zone with `--userns auto`. Why
the zones are shaped this way is in [Single machine](../explanation/single-machine.md).

## `lclaw.toml`

### Top-level keys

| Key        | Required | Type    | Allowed values                        | Purpose                                                                      |
| ---------- | -------- | ------- | -------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `schema`   | yes      | integer | `2`                                   | Format version. `1` described three machines and is refused with a hint to run `lclaw init --force`. |
| `provider` | yes      | string  | `libkrun`, `applehv`                  | Podman machine provider, passed as `CONTAINERS_MACHINE_PROVIDER` to every `podman machine` command. |
| `keychain` | no       | table   | one key, `path`                       | Where the secrets lclaw injects are stored. Absent means `~/Library/Keychains/lclaw.keychain-db`. |
| `machine`  | yes      | table   | see below                             | The one machine's size and host mounts.                                      |
| `zones`    | yes      | table   | sub-tables `infra`, `services`, `agent`, each exactly once | One table per zone. The zones are fixed; a fourth is a finding. |

### `[machine]`

| Key          | Required | Type             | Allowed values                                                              | Maps to                                    |
| ------------ | -------- | ---------------- | --------------------------------------------------------------------------- | ------------------------------------------ |
| `cpus`       | yes      | integer          | greater than 0                                                              | `podman machine init --cpus`               |
| `memory-mib` | yes      | integer          | greater than 0, in MiB                                                      | `--memory`                                 |
| `disk-gib`   | yes      | integer          | greater than 0, in GiB                                                      | `--disk-size`                              |
| `volumes`    | no       | array of strings | `"<host>:<guest>"`, both absolute paths; anything after a second colon is passed to Podman unchanged as mount options | One `--volume` flag each; empty means `--volume ""`, and no host directory is mounted |

### `[zones.<role>]`

| Key         | Required | Type             | Allowed values                                                              | Purpose                                    |
| ----------- | -------- | ---------------- | --------------------------------------------------------------------------- | ------------------------------------------ |
| `internal`  | no       | boolean          | default `false`                                                             | Create the zone's network with `--internal`, and play its pods with `--userns auto` |
| `workloads` | no       | array of strings | lowercase letters, digits and hyphens, not starting with a hyphen; each names a directory under `workloads/`; no name under two zones; may be empty or absent | Applied in order, torn down in reverse |
| `secrets`   | no       | array of strings | lowercase DNS labels of at most 63 characters; must not name a built-in secret; no duplicates within a zone | Keychain items this zone's pods consume; injected on `up` |
| `bridges`   | no       | inline table     | `<workload> = ["<zone>", ...]`: a workload of this zone, and zones other than this one, each once | Extra `--network` flags for that pod |

Unknown keys anywhere in the file are rejected when the file is read.

### `[keychain]`

| Key    | Required | Type   | Allowed values                        | Purpose                                              |
| ------ | -------- | ------ | -------------------------------------- | ----------------------------------------------------- |
| `path` | no       | string | absolute, or starting with `~/`        | The keychain file. `~/` is the user's home directory. |

An absent table, or an absent `path`, means
`~/Library/Keychains/lclaw.keychain-db`. A relative path is an error naming
the value. The catalogue, the item layout and the commands are in the
[secrets reference](secrets.md).

### Validation

`lclaw doctor` reads the file and reports one failed `topology` check per
problem, as `<where>: <message>` where `<where>` is the TOML path; `up`,
`down` and `status` report the same findings and stop. The rules, in the
order they are checked:

| Where                              | Rule                                                                   |
| ---------------------------------- | ------------------------------------------------------------------------ |
| `schema`                           | is `2`; `1` gets the migration hint                                    |
| `provider`                         | is `libkrun` or `applehv`                                              |
| `machine.cpus`, `.memory-mib`, `.disk-gib` | positive                                                       |
| `machine.volumes[<i>]`             | two absolute paths joined by a colon                                   |
| `zones.<name>`                     | `<name>` is a role                                                     |
| `zones.<role>.workloads`           | each name is valid, listed once in the whole file, and its directory holds `Containerfile`, `pod.yaml` and `.containerignore` |
| `zones.<role>.bridges.<workload>`  | the workload belongs to this zone; the zone is not internal; each target is another role, listed once |
| `zones`                            | each role appears exactly once                                         |
| `zones.<role>.internal`            | an internal zone is the target of at least one bridge                  |
| `zones.<role>.secrets`             | each name is a lowercase DNS label of at most 63 characters, does not collide with a built-in secret, and is listed once under that zone |

### Default

```toml
schema = 2
provider = "libkrun"

[keychain]
path = "~/Library/Keychains/lclaw.keychain-db"

[machine]
cpus = 4
memory-mib = 8192
disk-gib = 60

[zones.infra]
workloads = ["kuma-cp", "gateway"]
bridges = { kuma-cp = ["services", "agent"] }

[zones.services]
workloads = ["litellm-db", "litellm", "agentgateway"]
bridges = { agentgateway = ["agent"] }
secrets = ["anthropic-api-key"]

[zones.agent]
internal = true
workloads = ["openclaw"]
```

## `machine/playbook.yaml`

An Ansible playbook that `podman machine init --playbook` copies into the
machine and runs once after the first boot, as the `core` user. Podman
copies only this file, so it cannot include or copy anything else from the
host. The default playbook has one task: enable `podman-restart.service`
at user scope, which restarts every container whose restart policy is
`always` after the machine reboots.

## `workloads/<name>/Containerfile`

One comment block and one instruction: `FROM <image>:<tag>@sha256:<digest>`.
Customizations go after the `FROM` line. The default images:

| Zone       | Workload       | Image                                                                     |
| ---------- | -------------- | -------------------------------------------------------------------------------------------------- |
| `infra`    | `kuma-cp`      | `docker.io/kumahq/kuma-cp:2.14.5` (provisional)                           |
| `infra`    | `gateway`      | `docker.io/kumahq/kuma-dp:2.14.5` (provisional)                           |
| `services` | `litellm-db`   | `docker.io/library/postgres:18.6`                                         |
| `services` | `litellm`      | `ghcr.io/berriai/litellm:v1.83.14-stable`                                 |
| `services` | `agentgateway` | `cr.agentgateway.dev/agentgateway:v1.5.0`                                 |
| `agent`    | `openclaw`     | `ghcr.io/openclaw/openclaw:2026.9.5`                                      |

The digests are in the files. Provisional rows are placeholders until the
network design settles how the gateway runs.

## `workloads/<name>/pod.yaml`

A multi-document Kubernetes file that `podman kube play` applies as one
unit. Every default file keeps these conventions and customized files must
too:

| Item            | Convention                                                                                                  |
| --------------- | ----------------------------------------------------------------------------------------------------------- |
| Pod             | Exactly one `Pod`, named `<name>`, labelled `app.kubernetes.io/name: <name>` and `app.kubernetes.io/part-of: localclaw` |
| Restart         | `restartPolicy: Always`                                                                                     |
| Image           | `localhost/lclaw/<name>:latest` with `imagePullPolicy: Never`                                               |
| Limits          | `resources.limits` with `cpu` and `memory`, because every zone shares one machine                           |
| Ports           | `containerPort` documents what the workload listens on; `hostPort` appears only in `infra` workloads        |
| State           | `PersistentVolumeClaim` documents named `<name>-<purpose>`, never `hostPath`                                |
| Configuration   | `ConfigMap` documents in the same file, mounted as a directory                                              |
| Secrets         | Referenced by name with `secretKeyRef`; no `Secret` document is ever present                                |
| Networks        | None in the file: `lclaw up` passes `--network` from the topology                                           |

The `openclaw` Pod file additionally sets `securityContext` with
`allowPrivilegeEscalation: false` and `capabilities.drop: ["ALL"]`,
because the agent is the untrusted workload and no longer has a machine
to itself.

### Default limits

| Workload       | CPU | Memory |
| -------------- | --- | ------ |
| `kuma-cp`      | 1   | 512Mi  |
| `gateway`      | 1   | 256Mi  |
| `litellm-db`   | 1   | 1Gi    |
| `litellm`      | 2   | 2Gi    |
| `agentgateway` | 1   | 512Mi  |
| `openclaw`     | 2   | 4Gi    |

### Default ports

| Workload       | Container ports | Host port |
| -------------- | ---------------- | --------- |
| `kuma-cp`      | 5681, 5678      | 5681      |
| `gateway`      | 8080            | 8080      |
| `litellm-db`   | 5432            |           |
| `litellm`      | 4000            |           |
| `agentgateway` | 3000, 15000     |           |
| `openclaw`     | 18789           |           |

### Default secrets

`lclaw up` injects these into the machine's store; the Pod files only name
them. Every injected secret is a Kubernetes Secret with the single key
`value`. The catalogue's five built-in entries are always present, and are
listed in full in the [secrets reference](secrets.md#built-in-entries);
`anthropic-api-key` is a user-supplied secret, declared in the default
topology's `[zones.services]` table.

| Secret                   | Key     | Used by                 | Environment variable                     |
| ------------------------ | ------- | ----------------------- | ---------------------------------------- |
| `litellm-master-key`     | `value` | `litellm`               | `LITELLM_MASTER_KEY`                     |
| `litellm-salt-key`       | `value` | `litellm`               | `LITELLM_SALT_KEY`                       |
| `litellm-db-password`    | `value` | `litellm-db`, `litellm` | `POSTGRES_PASSWORD`, `DATABASE_PASSWORD` |
| `openclaw-gateway-token` | `value` | `openclaw`              | `OPENCLAW_GATEWAY_TOKEN`                 |
| `openclaw-litellm-key`   | `value` | no default Pod file     |                                          |

`openclaw-litellm-key` is minted by the running LiteLLM during `lclaw up`
and stored for the `agent` zone; no default Pod file consumes it until the
network design routes the agent to LiteLLM, so the `openclaw` Pod file
names only `openclaw-gateway-token` today.

## `workloads/<name>/.containerignore`

Excludes `pod.yaml` from the build context, which the remote client sends
to the machine as a tarball on every build.

## What `lclaw init` does to each file

| Condition                              | Outcome   | Reported as     |
| --------------------------------------- | --------- | ----------------- |
| Path does not exist                    | written   | `written  <path>`|
| Path exists, no `--force`              | untouched | `skipped  <path>`|
| Path exists, `--force`                 | replaced  | `written  <path>`|
| Path is a directory, with or without `--force` | untouched | `failed   <path>: <error>` |
| Write fails (permissions, disk)        | untouched | `failed   <path>: <error>` |

Each file is written to a temporary name in the same directory and renamed
into place, so an interrupted run leaves no half-written file. The run
continues past failures and exits 1 only if any occurred. Flags and the
JSON shape are in the [command reference](cli.md#init).

A scaffold written before schema 2 keeps its `machines/<role>/playbook.yaml`
files; `init --force` writes `machine/playbook.yaml` beside them and never
deletes anything. Remove the old directory by hand.
