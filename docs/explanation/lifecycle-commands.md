---
title: "Lifecycle commands"
description: "How lclaw up, down and status create the machine and networks, apply the zones in order, mint the agent's key, report every step and tear it down again."
diataxis: explanation
status: stable
last_reviewed: 2026-09-22
tags: [cli, lclaw, up, down, status, podman, kube-play, litellm, design-decision]
related:
  - cli-architecture.md
  - deployment-model.md
  - secrets-management.md
  - single-machine.md
  - ../reference/cli.md
  - ../how-to/apply-a-deployment-by-hand.md
---

# Lifecycle commands

`lclaw up` turns the scaffold into a running system; `lclaw down` turns it
back into a stopped machine that holds nothing lclaw injected; `lclaw
status` says which of the two you have. This page records the design agreed
on 2026-09-21 for the three commands. It was written before the code
existed. The [CLI architecture](cli-architecture.md) fixes the layering and
the output conventions, the [deployment model](deployment-model.md) fixes
what applying one workload means, the [secrets design](secrets-management.md)
fixes the resolve, inject and purge steps, and [Single machine](single-machine.md)
fixes the shape: one machine, one network per zone. This page composes
them: ordering, readiness, the one adapter nobody else owns, reporting, and
failure.

## Goals and constraints

The CLI's three constraints, extensible, reliable and light, and the
secrets design's rules apply unchanged. Four more are specific to
lifecycle.

- **`up` twice is how you apply an edit.** Every step is idempotent, so a
  second `up` is a no-op where nothing changed and a re-apply where
  something did. There is no separate `apply` or `reload`.
- **Fail before the boot, not after.** Anything that can be checked from
  the keychain and the scaffold is checked before a machine is created or
  started, because a boot costs a minute and a missing API key costs
  nothing to detect.
- **Nothing runs in parallel.** Zones are applied in order and workloads
  within a zone in the order listed. The machine boots once, builds are
  layer-cached, and a sequential log a person can read is worth more than
  the seconds parallelism would save.
- **Every step is reported, in the shape `doctor` uses.** A check per
  step, all of them in one report, text or JSON, exit 1 if any failed.

### Facts that shaped the design

Verified on 2026-09-21 against Podman 6.1.2 on macOS 27.0 and the Podman
source at `v6.1.2`, and LiteLLM's documentation.

- `podman machine inspect <name>` on a machine that does not exist exits
  125 with `<name>: VM does not exist`. `podman machine list --format json`
  reports `Running` and `Starting` booleans per machine and sees every
  provider's machines.
- `podman machine start` on a machine that is already running is an
  **error** (`unable to start "<name>": already running`), not a no-op.
  The deployment model assumed otherwise. `podman machine stop` on a
  stopped machine exits 0 (`stopped successfully`).
- `podman machine init` reads the provider from `CONTAINERS_MACHINE_PROVIDER`
  or `--provider`; `--playbook` runs an Ansible playbook after first boot;
  `--volume ""` mounts nothing; `--now` is refused by this design so that
  create and start are separate, reportable steps.
- `podman machine rm --force` removes a running machine without a prompt.
- `podman network exists` exits 0 or 1; `podman network create --internal`
  makes a network with no external route; `podman network rm` refuses
  while a container uses the network.
- `podman kube play --replace` recreates a pod that exists; `--network`
  may be given more than once; `--userns auto` is accepted; `--start`
  defaults to true. `podman kube down` removes only what the file
  describes, and volumes only with `--force`. Containers played from a
  pod are named `<pod>-<container>`.
- `podman build --quiet` prints only the image id; the full build log is
  still on stderr when it fails.
- `podman pod ps --filter label=<k>=<v> --format json` lists pods with
  `Name`, `Status` and their containers' states.
- `podman --connection <name> exec -i <container> <cmd>` works through the
  remote client and passes standard input to the process.
- LiteLLM answers `GET /health/readiness` with 200 and `{"status":
  "healthy","db":"connected"}` once it can serve, 503 while its database
  is unreachable; `GET /health/liveliness` answers as soon as the process
  is up. `POST /key/generate` with the master key as a bearer token
  returns a virtual key; `POST /key/delete` with `{"keys": [...]}` revokes
  one. The generate endpoint does not accept a caller-chosen value.

## Scope

This design delivers `lclaw up`, `lclaw down` and `lclaw status`; the
growth of the `MachineRuntime` port and a new `WorkloadRuntime` port with
their Podman adapter; the `KeyMinter` adapter that talks to LiteLLM; the
wiring of the secrets design's resolve, inject and purge use cases; a
provider environment on the process runner; and the reference and how-to
pages listed at the end.

It does not deliver Kuma's configuration or any policy about
who may talk to whom: that is the network design, which will add steps to
`up` the way the secrets design did. It does not deliver a `logs` or
`shell` helper; those are troubleshooting commands with their own page.

## The commands

All three honour the global `--output`, `--verbose` and `--dir` flags.
`up` and `down` accept zero or more zone names; with none, they mean every
zone. Zone order is fixed by the domain, `infra`, `services`, `agent`, and
naming zones on the command line selects, it does not reorder.

### `lclaw up [ZONE...]`

1. **Load and validate** `lclaw.toml`, as `doctor` does. Findings are
   reported and the command stops with exit 1.
2. **Check user secrets.** For every selected zone, every catalogue entry
   with source `user` is looked up in the keychain by attribute, never by
   value. Each missing one is a failed check with the hint
   `lclaw secrets set NAME`. If any is missing, the command stops here,
   before Podman is touched. Generated and minted entries are not
   resolved yet.
3. **Machine.** `podman machine inspect lclaw`; if absent, `podman machine
   init lclaw` with the `[machine]` size, `--volume ""` or one `--volume`
   per entry, and `--playbook machine/playbook.yaml`. Then, if the
   machine is not running, `podman machine start lclaw`. Both calls carry
   `CONTAINERS_MACHINE_PROVIDER` from the file. One check, `machine`,
   summarised `created and started`, `started` or `running`.
4. **Networks.** For every selected zone, `podman network exists
   lclaw-<zone>` and if not `podman network create`, with `--internal`
   when the zone says so. One check per zone, `network/<zone>`.
5. **Zones, in order.** For each selected zone:
   1. **Resolve** its secrets through `ResolveSecrets`: generated values
      are made and stored, minted values are requested from the minter,
      and the user values were confirmed in step 2. A minted value that
      cannot be obtained is a failed check and the zone stops.
   2. **Inject** them through `InjectSecrets` into the machine's store
      with `--replace`. One check, `<zone>/secrets`, summarised with the
      count.
   3. For each workload in order: `podman --connection lclaw build
      --quiet --tag localhost/lclaw/<workload>:latest workloads/<workload>`,
      then `podman --connection lclaw kube play --replace --network
      lclaw-<zone> [--network lclaw-<other>...] [--userns auto]
      workloads/<workload>/pod.yaml`, with one `--network` per bridge the
      topology declares and `--userns auto` for every zone marked
      `internal`. One check per workload, `<zone>/<workload>`, summarised
      `built and playing`.
   4. **Wait for readiness** where the next zone depends on it. After the
      `services` zone, the minter polls LiteLLM's readiness endpoint until
      it answers 200 or two minutes pass; a timeout is a failed check named
      `services/ready`.
6. **Report.** Every check, then the summary line, exit 0 if none failed.

Naming a zone on the command line does not bring up what it depends on.
`lclaw up agent` with the services zone stopped fails at step 5.1 with
`openclaw-litellm-key: not minted` and the hint to run `lclaw up services`
first, exactly the finding `ResolveSecrets` already produces.

### `lclaw down [ZONE...] [--destroy]`

1. **Load and validate** as above. If the machine does not exist or is not
   running, every remaining step is reported as skipped and the command
   exits 0: there is nothing to take down.
2. **Zones, in reverse order.** For each selected zone, for each workload
   in reverse order: `podman --connection lclaw kube down
   workloads/<workload>/pod.yaml`. A pod that does not exist is a skipped
   check, not a failure. Named volumes survive.
3. **Networks.** For each selected zone whose pods are all gone, `podman
   network rm lclaw-<zone>`.
4. **Purge**, only when no LocalClaw pod remains on the machine, because
   the store is shared: `PurgeSecrets` removes every secret carrying the
   LocalClaw label and the volume of the same name. Then `podman machine
   stop lclaw`. If pods from unselected zones remain, both steps are
   reported as skipped with the reason and the machine stays up.
5. **`--destroy`**, only with no zone named and after the machine is
   stopped: `podman machine rm --force lclaw`, which removes the disk and
   every image and volume on it. It also revokes the agent's minted key
   at LiteLLM if the services zone is still up at the time, and removes
   the minted item from the keychain, so the next `up` mints a fresh one.
   Nothing prompts; the flag is the confirmation, and the report lists
   what was removed.

`down` does not stop at the first failure. A pod that will not stop is
reported and the next one is tried, because a half-torn-down system is
worse to reason about than a report with two failures in it.

### `lclaw status`

Reads and never writes. One check for the machine (`running`, `stopped`
as a warning, `not created` as a warning), one per zone network when the
machine is running (`present`, or `missing` as a warning), and one per
workload declared in the topology: `running` when its pod is up with every
container running, `degraded` as a failure when the pod exists but a
container is not running, `absent` as a warning. Exit 1 only for a
failure, so `lclaw status` in a script distinguishes "down" from "broken".

```text
PASS  machine             running
PASS  network/infra       present
PASS  network/services    present
PASS  network/agent       present (internal)
PASS  infra/kuma-cp       running
PASS  infra/gateway       running
PASS  services/litellm-db running
FAIL  services/litellm    degraded: container litellm exited (1)
      hint: podman --connection lclaw logs litellm-litellm
PASS  services/agentgateway running
WARN  agent/openclaw      absent
      hint: run `lclaw up agent`

7 passed, 1 warning, 1 failed
```

JSON carries the same `status` and `checks` array `doctor` emits, so one
renderer serves all four commands.

## Progress and output

A `doctor` run is quick, so it prints nothing until it is done. `up` is
not: a first run downloads a machine image, boots, and builds six
images. So `up` and `down` announce each step on **standard error** as it
begins, one line, `==> machine: starting lclaw`, and print the report on
standard output when they finish. Standard output stays clean for
`--output json`, and a person watching a terminal is never left staring
at nothing. With `--verbose`, the runner's log of every command and its
duration interleaves as it already does.

Build output is captured, not streamed. A successful build's log is
noise; a failed build's log is the error, and the check's summary carries
the last lines of it.

## Minting the agent's key

The agent's LiteLLM virtual key is the one secret nothing on the host can
invent: LiteLLM has to issue it, and LiteLLM lives in the services zone
with no port published to the host. The `KeyMinter` adapter therefore runs
**inside the LiteLLM container**:

```text
podman --connection lclaw exec -i litellm-litellm python3 -
```

with a short Python script on standard input. The script reads the master
key from the container's own environment (`LITELLM_MASTER_KEY`, which the
pod file already sets from the secret), posts to
`http://127.0.0.1:4000/key/generate` with `{"key_alias": "openclaw",
"metadata": {"created_by": "lclaw"}}`, and prints the returned key and
nothing else. `Revoke` does the same with `/key/delete`, reading the key
to revoke from standard input, never from an argument. Readiness polling
is the same mechanism against `/health/readiness`.

Why this and not the alternatives: the master key never leaves the
machine, not even to the host process that orchestrates it; no port is
published for the purpose; no image is pulled for the purpose, because the
LiteLLM image is a Python application; and it needs nothing from the
network design. The command is marked sensitive on the runner, so neither
the script nor the output is ever logged, and the adapter redacts the
value from any error it wraps, as the Podman adapter already does for
`secret create`.

The key is minted with no model restriction, because the model list is
LiteLLM configuration the user edits, and with no budget, because a budget
is a policy the network design or a later `[agent]` table should own. Both
are one field each in the request body when they arrive.

## Where it sits in the layering

- **Domain** gains the zone model described in [Single machine](single-machine.md),
  `Role.NetworkName`, the order of zones and of workloads within them, a
  `Pod` observation type (name, running, container states), and
  `EvaluateStatus`, a pure rule from machine, network and pod observations
  to checks, like the existing evaluation functions.
- **Application** grows `MachineRuntime` with `InspectMachine`,
  `InitMachine`, `StartMachine`, `StopMachine` and `RemoveMachine`, and
  adds `WorkloadRuntime` with `NetworkExists`, `CreateNetwork`,
  `RemoveNetwork`, `Build`, `Play`, `Down` and `ListPods`. `SecretTarget`
  loses its role parameter. `KeyMinter` gains `Ready`. Use cases are `Up`,
  `Down` and `Status`; each takes a reporter it calls as steps begin, so
  the progress lines are the presentation layer's business.
- **Adapters:** the Podman client satisfies the two runtime ports beside
  `SecretTarget`. A new `adapters/litellm` package satisfies `KeyMinter`
  using the exec runner to invoke `podman exec`; it knows the endpoints
  and the script, and nothing else knows them. The process runner's
  `Command` gains `Env`, a list of `KEY=VALUE` pairs added to the child's
  environment, for `CONTAINERS_MACHINE_PROVIDER`.
- **Presentation** gains the three commands, the zone argument parsing,
  the `--destroy` flag, and the progress writer. The report renderer is
  the one `doctor` uses.
- **Composition root** wires the LiteLLM adapter and the progress writer
  to standard error.

The dependency rule holds without new edges: the domain imports nothing,
the application imports the domain, adapters import the application's
ports, and only the composition root imports everything.

## Error handling

The CLI design's rules apply unchanged. Four rules are specific to
lifecycle.

- **A missing tool is a failed `machine` check**, with the same
  `ErrToolNotFound` mapping `doctor` uses, and `up` stops there.
- **`up` stops the zone, then skips the rest.** A failed step fails its
  check and no later step in that zone runs; later zones are reported as
  `skipped because <zone>/<step> failed`. Already-applied pods are left
  running, because they are correct, and the next `up` resumes from
  wherever the failure was. There is no rollback.
- **`down` continues.** Every selected pod is attempted; failures
  accumulate in the report.
- **Interruption leaves a consistent machine.** The runner kills the
  child on cancellation and the use case checks the context between
  steps. Every step is idempotent, so `up` after an interrupted `up` picks
  up where it stopped, and `down` after an interrupted `down` finishes
  the job.

Exit codes follow the [CLI reference](../reference/cli.md): 0 when no
check failed, 1 when one did or on an unexpected error, 2 for a usage
error such as an unknown zone name or `--destroy` with a zone named, 130
on interrupt.

## Testing

- **Domain:** tables for zone ordering and selection, for the network
  arguments a workload's bridges produce, and for `EvaluateStatus` over
  every combination of machine state, network presence and pod state.
- **Application:** `Up`, `Down` and `Status` against in-memory fakes of
  the runtime ports, the keychain, the target and the minter. Cases: a
  clean first run creating everything; a second run changing nothing; a
  missing user secret stopping before any runtime call; a missing minted
  key when the services zone is not up; a build failure failing the zone
  and skipping the next; `down` with a pod that refuses to stop still
  tearing down the rest; `down` of one zone leaving the store and the
  machine alone; `--destroy` revoking and removing the minted item;
  cancellation between steps; and the reporter receiving one call per
  step in order.
- **Podman adapter:** argv assertions for every new call against the exec
  fake, including the provider in the environment, `--internal`, several
  `--network` flags, `--userns auto`, `--replace` and `--quiet`, and
  fixtures captured from this machine for `machine inspect`, the
  not-found exit, and `pod ps --format json`.
- **LiteLLM adapter:** the exact `exec` argv, the script arriving on
  standard input, the sensitive flag set, the key parsed from the output,
  the readiness loop giving up, and redaction of the key from a wrapped
  error.
- **Presentation:** golden text and JSON for `up`, `down` and `status`
  reports with mixed outcomes; the progress lines on standard error and
  none on standard output; exit 2 for a bad zone name and for `--destroy`
  with a zone.
- **Integration on Linux CI:** the existing scaffold check gains the
  network steps: create the three networks with the agent's `--internal`,
  play the pods with the `--network` flags the topology produces, assert
  from inside a probe pod on the agent network that LiteLLM's address is
  unreachable while the gateway's is reachable, then tear down and remove
  the networks. The minting script is run against a real LiteLLM container
  on the runner, with a throwaway master key, to pin the endpoint
  contract.
- **On a Mac, by hand:** no CI runner can run `podman machine`. The
  implementation pull request records a full `up`, `status`, `down` and
  `down --destroy` on this machine, with output.

### Assumptions to verify during implementation

- `podman kube play --userns auto` works with a pod file that declares a
  `persistentVolumeClaim`, and the volume's ownership is usable by the
  container's user across restarts.
- `podman network rm` after `kube down` succeeds without `--force`; if
  the pod's infra container lingers, the adapter waits or forces.
- The LiteLLM image's `python3` has `urllib` available with no extra
  packages, and the container's environment carries
  `LITELLM_MASTER_KEY` as the pod file sets it.
- `--internal` on the agent network does not break aardvark DNS for names
  on the bridged networks that `agentgateway` joins.
- Two minutes is enough for LiteLLM's first-boot schema migration on this
  machine.

## Alternatives considered

**Apply zones in parallel.** The machine is one, builds share a cache, and
the only real dependency is agent-after-services. Parallelism would save
seconds on a run dominated by one boot and one image download, at the
cost of an interleaved log and a harder failure story.

**Roll back on failure.** Tearing down what a failed `up` applied would
leave a user with less than they had. Idempotent steps and a clear report
of where it stopped make "run it again" the recovery, which is also how
`kube play --replace` and `build` want to be used.

**A separate `lclaw destroy` command.** A verb is harder to hit by
accident than a flag. But destroy is the last step of the same sequence
with the same zone rules, and two commands would carry the same logic
twice. `--destroy` is refused with a zone named and is loud about what it
removed.

**Mint from the host over a published port.** Publishing LiteLLM to the
host loopback for the duration of `up` would work and would need no
`exec`. It also puts the master key in a host process's memory and an
endpoint on the host's loopback that any local process could reach while
it is open. `exec` keeps both inside the machine.

**Mint lazily, from the agent pod.** OpenClaw could ask LiteLLM for a key
itself on first start. That puts the master key inside the agent's zone,
which is the one place it must never be.

**`status` from `podman machine list` alone.** Cheap, but it cannot tell a
stopped agent from a crashed one, which is the question a person asking
for status has.

## Refinements made during implementation

The code landed on 2026-09-22 and matches this page with these changes,
recorded so the page stays accurate. Where a change contradicts something
above, the [command reference](../reference/cli.md) is the fact.

- **Podman refuses to start a running machine**, so `up` reads the machine
  list first and calls `machine start` only when the machine is stopped;
  `start` carries `--no-info` to keep Podman's rootless advice out of the
  log. Stopping a stopped machine is a no-op to Podman, so `down` does not
  need the same guard.
- **`--destroy` does not revoke the key at LiteLLM.** By the time the
  machine is removed, LiteLLM's database has gone with it, so there is
  nothing to revoke; the keychain item is deleted and the next `up` mints
  a fresh one. `KeyMinter.Revoke` exists and is tested, for `secrets
  delete` to use later.
- **The readiness wait runs only when a minter is wired.** With
  `UnavailableMinter`, as in tests without LiteLLM, `up` skips the
  `services/ready` check rather than failing on it.
- **`ListPods` drops the pod's infra container**, which Podman names
  `<id>-infra`, so `status` judges only the workload's own containers.
- **`down` of a subset reports the kept store and machine as warnings**,
  named `secrets` and `machine`, so the report says why they stayed rather
  than omitting them.
- **The minter's scripts use only `urllib`**, so the LiteLLM image needs
  nothing beyond CPython. A missing container maps to
  `ErrMinterUnavailable`, and the readiness error keeps only the last
  stderr line, which is the exception text.
- **No tutorial yet.** A tutorial must be guaranteed to work on a clean
  checkout, and a working agent still needs the network design to route
  it to LiteLLM.

### The assumptions this page listed

Not yet exercised on a real machine; see the pull request for the state
of the by-hand run. `--userns auto` with a `persistentVolumeClaim`,
`network rm` straight after `kube down`, `python3` with `urllib` in the
LiteLLM image, aardvark DNS across an internal network's bridges, and the
two-minute readiness budget are each pinned by the Linux CI scaffold check
where the runner can (`--internal`, `--network` and `kube down` followed
by `network rm`), and remain listed here until the macOS run confirms the
rest.

## What landed with the code

- [`docs/reference/cli.md`](../reference/cli.md): `up`, `down`, `status`,
  their arguments and flags, exit codes, and the JSON shape.
- [`docs/how-to/bring-the-system-up-and-down.md`](../how-to/bring-the-system-up-and-down.md)
  and [`docs/how-to/apply-a-deployment-by-hand.md`](../how-to/apply-a-deployment-by-hand.md).
- `CHANGELOG.md`: the three commands under Unreleased.
- The `doctor` hint "run `lclaw up` once it is available" lost its last
  four words.
- This page is `stable`: the implementation matches it.
