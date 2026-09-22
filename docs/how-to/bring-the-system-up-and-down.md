---
title: "Bring the system up and down"
description: "Run lclaw up for the first time, apply an edit, check status, take one zone or everything down, and destroy the machine."
diataxis: how-to
status: stable
last_reviewed: 2026-09-22
tags: [lclaw, up, down, status, lifecycle]
related:
  - ../reference/cli.md
  - ../reference/scaffold.md
  - manage-secrets.md
  - apply-a-deployment-by-hand.md
  - ../explanation/lifecycle-commands.md
---

# Bring the system up and down

When you finish, every LocalClaw pod is running on the machine, you have
applied an edit to one workload, and you have taken the system down again,
leaving a stopped machine that holds nothing lclaw injected.

## Prerequisites

- A `lclaw` binary, built as in
  [Set up a development environment](set-up-a-development-environment.md).
- `lclaw doctor` passing its `flox`, `podman`, `scaffold`, `topology` and
  `keychain` checks.
- Your provider API key set, as in [Manage secrets](manage-secrets.md).
  `up` stops before touching Podman if it is not.

## Steps

### 1. Bring everything up

```bash
lclaw up
```

The first run downloads the machine image, creates and boots the machine,
creates the three zone networks, generates the secrets that do not exist
yet, builds seven images and plays seven pods. Each step is announced on
standard error as it begins, and the report follows on standard output:

```text
==> secrets: checking the keychain
==> machine: inspecting lclaw
==> machine: creating lclaw
==> machine: starting lclaw
==> network/infra: creating lclaw-infra
...
==> services/ready: waiting for LiteLLM
==> agent/secrets: resolving secrets
==> agent/openclaw: building localhost/lclaw/openclaw:latest
==> agent/openclaw: playing workloads/openclaw/pod.yaml
PASS  machine                created and started
PASS  network/infra          created
PASS  network/services       created
PASS  network/agent          created (internal)
PASS  infra/secrets          0 injected
PASS  infra/wireguard        built and playing
PASS  infra/kuma-cp          built and playing
PASS  infra/gateway          built and playing
PASS  services/secrets       4 injected
PASS  services/litellm-db    built and playing
PASS  services/litellm       built and playing
PASS  services/agentgateway  built and playing
PASS  services/ready         LiteLLM is ready
PASS  agent/secrets          2 injected
PASS  agent/openclaw         built and playing

15 passed, 0 warnings, 0 failed
```

If a check fails, the report says which step and why, the zones after it
are marked skipped, and what was already applied keeps running. Fix the
cause and run `lclaw up` again; it resumes from the failure.

### 2. Check status

```bash
lclaw status
```

One line for the machine, one per zone network, and one per workload:
`running`, `absent` (a warning) or `degraded` (a failure naming the
container and its state, with the `podman` command that shows its logs).

### 3. Apply an edit

Edit a workload's `Containerfile` or `pod.yaml` under `~/.config/lclaw`,
then run `up` for that zone:

```bash
lclaw up services
```

`build` is cached by layer and `kube play --replace` recreates the pod, so
this is the whole apply step. Zones that need nothing report `running` and
`present` and are otherwise untouched.

### 4. Take one zone down

```bash
lclaw down agent
```

The zone's pods are stopped in reverse order and its network removed. The
machine stays up and the secret store is kept, because the other zones are
still using them; the report says so as warnings.

### 5. Take everything down

```bash
lclaw down
```

Every pod is stopped, every network removed, every secret lclaw injected is
purged from the machine's store, and the machine is stopped. The keychain
is untouched, and named volumes such as the LiteLLM database survive, so
the next `up` picks up where this left off.

### 6. Destroy the machine

Only when you want the disk gone too:

```bash
lclaw down --destroy
```

This removes the machine with every image and volume on it and forgets
the minted LiteLLM key, so the next `up` starts from nothing but the
keychain. It takes no zone and asks no question.

## Verify

After step 1 or 3:

```bash
lclaw status && echo up
```

Expected: every line `PASS`, and `up` printed. After step 5:

```bash
lclaw status
```

Expected: `PASS  machine  stopped` and nothing else, exit 0.
