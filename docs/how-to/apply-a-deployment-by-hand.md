---
title: "Apply a deployment by hand"
description: "Create, start and load the LocalClaw machine and one zone from the scaffold with plain podman commands, for troubleshooting or when lclaw is not enough."
diataxis: how-to
status: stable
last_reviewed: 2026-09-22
tags: [podman, machine, network, kube-play, scaffold, troubleshooting]
related:
  - bring-the-system-up-and-down.md
  - ../reference/scaffold.md
  - ../reference/cli.md
  - ../explanation/deployment-model.md
  - ../explanation/single-machine.md
---

# Apply a deployment by hand

When you finish, the LocalClaw machine exists and is running, one zone's
network exists, and every workload listed for that zone in `lclaw.toml` is
built and playing on it, all done with `podman` commands you can rerun one
at a time. This is what `lclaw up` does; use it to see each step's own
output when `up` reports a failure.

## Prerequisites

- Podman 5.8 or newer on macOS.
- A scaffold directory written by `lclaw init`; this guide assumes
  `~/.config/lclaw`. The [scaffold reference](../reference/scaffold.md)
  describes every file.
- The zone you are applying, one of `infra`, `services` or `agent`. The
  steps use `agent`; substitute the zone and its lists from `lclaw.toml`.

## Steps

### 1. Select the provider

```bash
export CONTAINERS_MACHINE_PROVIDER=libkrun
cd ~/.config/lclaw
```

Use the `provider` value from `lclaw.toml`.

### 2. Create the machine if it does not exist

```bash
podman machine inspect lclaw >/dev/null 2>&1 || podman machine init lclaw \
  --cpus 4 --memory 8192 --disk-size 60 \
  --volume "" \
  --playbook machine/playbook.yaml
```

The numbers are `cpus`, `memory-mib` and `disk-gib` from the `[machine]`
table. `--volume ""` creates the machine with no host directory mounted;
if the table has a `volumes` list, pass one `--volume host:guest` per
entry instead of the empty one.

### 3. Start the machine

```bash
podman machine list --format '{{.Name}} {{.Running}}' | grep -q '^lclaw true$' || podman machine start lclaw
```

Podman refuses to start a machine that is already running, which is why
the state is checked first.

### 4. Create the zone's network

```bash
podman --connection lclaw network exists lclaw-agent || podman --connection lclaw network create --internal lclaw-agent
```

Pass `--internal` only for a zone whose table says `internal = true`. A
pod that bridges into this zone also needs the zone's own network to
exist, so create a bridged zone's network before playing the bridging pod.

### 5. Create the secrets the workloads reference

Each Pod file names its secrets; the [scaffold reference](../reference/scaffold.md#default-secrets)
lists them. Create each one from a Kubernetes `Secret` document fed on
standard input so no value lands in a file, with the label `lclaw down`
purges by:

```bash
printf '{"apiVersion":"v1","kind":"Secret","metadata":{"name":"openclaw-gateway-token"},"type":"Opaque","data":{"value":"%s"}}' \
  "$(openssl rand -hex 32 | tr -d '\n' | base64)" \
  | podman --connection lclaw secret create --replace --label app.kubernetes.io/part-of=localclaw openclaw-gateway-token -
```

Repeat for every secret the zone's workloads reference. To use the value
lclaw holds instead of a fresh one, pipe `lclaw secrets get NAME | base64`
into the `data.value` field.

### 6. Build and play each workload, in the order listed

For each name in the zone's `workloads` list:

```bash
podman --connection lclaw build --tag localhost/lclaw/openclaw:latest workloads/openclaw
podman --connection lclaw kube play --replace --network lclaw-agent --userns auto workloads/openclaw/pod.yaml
```

Give one `--network` for the zone's own network and one more for each
zone the workload's `bridges` entry names, in that order; for example
`agentgateway` in the default file takes `--network lclaw-services
--network lclaw-agent`. Pass `--userns auto` only for an internal zone.
`build` is cached by layer and `--replace` recreates a pod that already
exists, so rerunning both is how you apply an edit.

## Verify

```bash
podman --connection lclaw pod ps --filter label=app.kubernetes.io/part-of=localclaw
```

Expected: one row per workload with status `Running`. `lclaw status`
reports the same from the topology's point of view.

## Tear down

Stop the workloads in reverse order, remove the network, and, once no
LocalClaw pod remains on the machine, purge the secrets and stop the
machine. Named volumes survive:

```bash
podman --connection lclaw kube down workloads/openclaw/pod.yaml
podman --connection lclaw network rm lclaw-agent
podman --connection lclaw secret ls --quiet | xargs -n1 podman --connection lclaw secret inspect --format '{{.Spec.Name}} {{index .Spec.Labels "app.kubernetes.io/part-of"}}' | awk '$2 == "localclaw" { print $1 }' | xargs -n1 podman --connection lclaw secret rm
podman machine stop lclaw
```

To remove the machine and everything on its disk:

```bash
podman machine rm --force lclaw
```
