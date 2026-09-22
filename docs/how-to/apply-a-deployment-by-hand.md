---
title: "Apply a deployment by hand"
description: "Create, start and load one LocalClaw machine from the scaffold with plain podman commands, for troubleshooting or when lclaw is not enough."
diataxis: how-to
status: stable
last_reviewed: 2026-09-22
tags: [podman, machine, kube-play, scaffold, troubleshooting]
related:
  - ../reference/scaffold.md
  - ../reference/cli.md
  - ../explanation/deployment-model.md
---

# Apply a deployment by hand

When you finish, one LocalClaw machine exists, is running, and every
workload listed for it in `lclaw.toml` is built and playing, all done with
`podman` commands you can rerun one at a time.

> **Changing.** These steps follow the current `schema = 1` scaffold, one
> machine per role. [Single machine](../explanation/single-machine.md)
> replaces that with a single machine named `lclaw` and one network per
> zone; the guide is rewritten for that shape, with the network and bridge
> steps, when the [lifecycle commands](../explanation/lifecycle-commands.md)
> land. Until then the steps here still work for one machine at a time.

## Prerequisites

- Podman 5.8 or newer on macOS.
- A scaffold directory written by `lclaw init`; this guide assumes
  `~/.config/lclaw`. The [scaffold reference](../reference/scaffold.md)
  describes every file.
- The role you are applying, one of `infra`, `services` or `agent`. The
  steps use `agent`; substitute the role and its sizes from `lclaw.toml`.

## Steps

### 1. Select the provider

```bash
export CONTAINERS_MACHINE_PROVIDER=libkrun
cd ~/.config/lclaw
```

Use the `provider` value from `lclaw.toml`.

### 2. Create the machine if it does not exist

```bash
podman machine inspect lclaw-agent >/dev/null 2>&1 || podman machine init lclaw-agent \
  --cpus 2 --memory 4096 --disk-size 30 \
  --volume "" \
  --playbook machines/agent/playbook.yaml
```

The numbers are `cpus`, `memory-mib` and `disk-gib` from the machine's
table. `--volume ""` creates the machine with no host directory mounted; if
the table has a `volumes` list, pass one `--volume host:guest` per entry
instead of the empty one.

### 3. Start the machine

```bash
podman machine start lclaw-agent
```

A machine that is already running is fine; the command says so and exits 0.

### 4. Create the secrets the workloads reference

Each Pod file names its secrets; the [scaffold reference](../reference/scaffold.md#default-secrets)
lists them. Create each one from a Kubernetes `Secret` document fed on
standard input so no value lands in a file:

```bash
printf 'apiVersion: v1\nkind: Secret\nmetadata:\n  name: openclaw-gateway-token\nstringData:\n  value: %s\n' "$(openssl rand -hex 32)" \
  | podman --connection lclaw-agent kube play -
```

Repeat for every secret the machine's workloads reference.

### 5. Build and play each workload, in the order listed

For each name in the machine's `workloads` list:

```bash
podman --connection lclaw-agent build --tag localhost/lclaw/openclaw:latest workloads/openclaw
podman --connection lclaw-agent kube play --replace workloads/openclaw/pod.yaml
```

`build` is cached by layer and `--replace` recreates a pod that already
exists, so rerunning both is how you apply an edit.

## Verify

```bash
podman --connection lclaw-agent pod ps --filter label=app.kubernetes.io/part-of=localclaw
```

Expected: one row per workload with status `Running`.

## Tear down

Stop the workloads in reverse order, then the machine. Named volumes
survive:

```bash
podman --connection lclaw-agent kube down workloads/openclaw/pod.yaml
podman machine stop lclaw-agent
```

To remove the machine and everything on its disk:

```bash
podman machine rm --force lclaw-agent
```
