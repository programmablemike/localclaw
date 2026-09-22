---
title: "Single machine"
description: "Why LocalClaw runs on one Podman machine with one network per zone, not three machines, what Podman on macOS allows, and what changes in lclaw.toml."
diataxis: explanation
status: stable
last_reviewed: 2026-09-22
tags: [podman, machine, network, topology, isolation, kuma, design-decision]
related:
  - ../../README.md
  - cli-architecture.md
  - deployment-model.md
  - secrets-management.md
  - lifecycle-commands.md
  - ../reference/scaffold.md
---

# Single machine

LocalClaw was designed as three Podman machines, one per role, so that the
agent would share neither a kernel nor a network with the services it talks
to. This page records the decision taken on 2026-09-21 to run everything on
**one** Podman machine, with **one Podman network per role** and Kuma as the
service mesh between them, and it says what that changes. It supersedes the
"Machines" section of the [deployment model](deployment-model.md); the
rest of that page, the workload files and the apply sequence, stands.

The short version: Podman on macOS refuses to run two machines at once, by
design and with no override. Rather than replace `podman machine` with a
second VM manager, LocalClaw keeps one machine and moves the boundaries
between roles from the hypervisor to the machine's network stack, where the
[network design](deployment-model.md#scope) was always going to police them
anyway.

## What Podman on macOS allows

Verified on 2026-09-21 against Podman 6.1.2 on macOS 27.0 (arm64), the
Podman source at tags `v5.8.0` and `v6.1.2`, and the upstream issue tracker.

- **Only one Podman machine can be running at a time on macOS.** Both the
  `applehv` and `libkrun` providers report `RequireExclusiveActive()`, and
  `podman machine start` refuses when any other machine, on any provider,
  is running or starting: `unable to start "<name>": <other> already
  starting or running: only one VM can be active at a time`. The rule is
  in `pkg/machine/shim/host.go` (`checkExclusiveActiveVM`) at both tags.
- **It is intended.** The `podman-machine-start` man page has said "Only
  one Podman managed VM can be active at a time" since 2021. Running an
  `applehv` and a `libkrun` machine together was treated as a bug and
  closed in Podman 5.4.0 (containers/podman#25112, PR #25139: "a forbidden
  use case"). The maintainers' stated reasons are the single default
  connection and Docker socket that `podman-mac-helper` links to one
  machine, and supporting tools (gvproxy, vfkit, krunkit, the helper) that
  were not built for several. A request to lift it
  (containers/podman#26281, June 2025) is open with no movement.
- **The deployment model's fact was wrong.** It read Podman 6.0's note
  that "all `podman machine` commands can now operate on VMs from all
  providers" as permission to run them together. That note is about
  listing and inspecting machines across providers, not running them.
- **`podman machine list` and `inspect` see every provider's machines**,
  but `podman machine init` creates on the provider named by
  `CONTAINERS_MACHINE_PROVIDER` or `--provider`. Podman 6 added
  `--provider`.
- **A Podman network is an isolation boundary inside the machine.** Each
  `podman network create` makes a bridge with its own subnet and its own
  DNS (aardvark); containers on different networks cannot reach each other
  by address or by name. `--internal` removes the default route, so a
  container on such a network cannot reach the internet or the host.
  `podman kube play --network` accepts several networks, so one pod can
  sit on two.
- **`podman kube play --userns auto`** gives a pod its own user-namespace
  range, so a process that escapes the container is an unprivileged user
  the machine has never heard of, not `core`.

## Alternatives considered

**Three VMs under a different manager.** Lima (`limactl`) runs any number
of Virtualization.framework VMs, forwards each guest's Podman socket to a
host socket, and its `user-v2` network lets guests reach each other as
`lima-<name>.internal`. Because lclaw only ever speaks to a machine through
`podman --connection`, only the create/start/stop/destroy adapter would
have changed. This was the runner-up. It was rejected for now because it
adds a second VM manager with its own release cadence and image pipeline,
gives up Podman's own machine image and its integration tests of the VM
path, and makes first boot a `dnf install` over the network. It remains
the documented path if the shared kernel below turns out to matter more
than expected; nothing in the layering closes it off.

**One machine, one network, mesh policy only.** Kuma's traffic permissions
and mTLS can decide who may talk to whom without any Podman networks. But
they decide it in the sidecar, and a process that owns its container can
route around its own sidecar unless a transparent proxy set up by a
privileged init container forbids it. Per-role networks are enforced by the
kernel, cost nothing, and need no sidecar to be present or configured
correctly. The mesh runs on top of them, not instead of them.

**Two machines, agent alone.** Would keep a hypervisor between the agent
and every secret that matters, at the price of every cost of the Lima
option. Recorded as the shape to reach for if the threat model changes.

## What the shape is now

One Podman machine named `lclaw`, three Podman networks named after the
roles, and the same seven workloads.

| Zone       | Network          | Workloads                              | Reachable from                      |
| ---------- | ---------------- | -------------------------------------- | ----------------------------------- |
| `infra`    | `lclaw-infra`    | `wireguard`, `kuma-cp`, `gateway`      | the host, through published ports   |
| `services` | `lclaw-services` | `litellm-db`, `litellm`, `agentgateway` | `infra`, through `kuma-cp`          |
| `agent`    | `lclaw-agent`    | `openclaw`                             | `services`, through `agentgateway`; the zone is `--internal` |

A **zone** is the new name for what a machine table used to describe: a
role, its workloads and its secrets. The word changes because the thing
changed; `Role` and the three fixed names do not.

- **Every zone is its own network**, created by `lclaw up` before any pod
  in it is played, and removed by `lclaw down` after the last one is torn
  down. The agent's network is created with `--internal`: the agent has no
  route to the internet, the host, or any other zone.
- **A pod that must bridge zones is placed on both networks.** `kuma-cp`
  is the control plane and every sidecar must reach it, so it joins all
  three. `agentgateway` is the only door out of the agent zone, so it joins
  `agent` and `services`. Nothing else is multi-homed. Which pods bridge
  is declared in the topology file, not inferred.
- **Kuma decides what flows across the bridges.** Which service may call
  which, with which identity, is the network design's business and is
  configured through `kuma-cp`; this page only guarantees that traffic
  which the mesh does not carry has no path at all.
- **Resource limits move from the machine to the pod.** The machine has one
  size. Each workload's Pod file sets `resources.limits` for CPU and
  memory, which `kube play` honours through cgroups, so a runaway agent
  cannot starve LiteLLM.
- **The agent pod is hardened by its Pod file**, since it no longer has a
  VM to itself: `--userns auto` at play time, and in `pod.yaml` a
  `securityContext` that drops all capabilities and forbids privilege
  escalation. No host directory is mounted unless `volumes` says so, as
  before. These are the deployment model's conventions; the lifecycle
  design applies them.
- **One secret store.** Every secret lclaw injects goes into the machine's
  store once. A pod still receives only the secrets its own file names, and
  no pod can read the store, because no pod is given the Podman socket.
  The catalogue's "machines" column becomes "zones" and keeps doing what it
  did: saying which pods consume a value, so `up` can mint the agent's
  LiteLLM key after the services zone is up and not before.

### The topology file

`lclaw.toml` gains a `[machine]` table and renames `[machines.<role>]` to
`[zones.<role>]`. `schema` becomes `2`; a file with `schema = 1` is a
validation finding whose hint is to re-run `lclaw init --force`. There are
no released users to migrate.

```toml
# LocalClaw topology. One Podman machine, one network per zone. The three
# zones are fixed; edit the machine size and the workload lists. Workloads
# are applied in the order listed and torn down in reverse.
schema = 2
provider = "libkrun"

[keychain]
path = "~/Library/Keychains/lclaw.keychain-db"

[machine]
cpus = 4
memory-mib = 8192
disk-gib = 60
# Host directories to mount into the machine, as "host:guest" pairs. Empty by
# default: nothing on the host is visible to any container unless listed.
# volumes = ["/Users/me/workspace:/mnt/workspace"]

[zones.infra]
workloads = ["wireguard", "kuma-cp", "gateway"]
# Pods in this zone that also join other zones' networks.
bridges = { kuma-cp = ["services", "agent"] }

[zones.services]
workloads = ["litellm-db", "litellm", "agentgateway"]
bridges = { agentgateway = ["agent"] }
secrets = ["anthropic-api-key"]

[zones.agent]
internal = true
workloads = ["openclaw"]
```

- `[machine]` carries what `[machines.<role>]` carried, once: `cpus`,
  `memory-mib`, `disk-gib`, `volumes`. The default is the sum of the old
  three, rounded to what a 16 GiB laptop can spare.
- `[zones.<role>]` keeps `workloads` and `secrets` with their existing
  rules, and adds `internal` (boolean, default `false`; `true` only makes
  sense for `agent` and is the default there) and `bridges`, a table from
  a workload in this zone to the other zones whose networks it joins.
  Validation adds: a bridge names a workload of this zone and zones other
  than this one; the agent zone bridges nothing out; and no zone may be
  both `internal` and the target of no bridge, because then nothing could
  ever reach it.
- `machines/<role>/playbook.yaml` becomes `machine/playbook.yaml`, one
  file, unchanged in content.

The machine is named `lclaw` and its connection is `lclaw`;
`Role.MachineName()` becomes `Role.NetworkName()` and returns
`lclaw-<role>`. The `doctor` machine checks collapse to one, plus one
check per zone network when the machine is running.

## Consequences

- The README's headline changes from "three Podman machines" to "one
  Podman machine, three isolated networks, a service mesh across them".
  The isolation claim it makes, that the agent cannot reach the host and
  can reach the services only through the gateway, is unchanged; the
  claim that the agent has its own kernel is withdrawn, and the README
  says why and points here.
- The [deployment model](deployment-model.md) keeps its Containerfile and
  Pod file conventions, `init`, and the per-workload apply sequence. Its
  "Machines" section and its statement that Podman 5.8 allows two
  providers to run together are superseded by this page, and it says so.
- The [secrets design](secrets-management.md) loses nothing but a word:
  per-machine stores become one store, `SecretTarget` takes no role, and
  the catalogue's machines become zones. The purge step runs once, at the
  end of a full `down`.
- The [scaffold reference](../reference/scaffold.md) documents the
  `schema = 2` file, `[machine]`, `[zones.<role>]`, `internal`, `bridges`,
  and the single playbook.
- One machine boots instead of three, images build once, and the
  cross-machine routing problem that blocked the minted key is gone.
- What is given up is the hypervisor between the agent and the secrets in
  the services zone. A kernel escape from the agent container is now an
  escape into the machine that also runs LiteLLM. `--userns auto`, dropped
  capabilities and the absence of the Podman socket raise the cost of that
  escape and limit what it yields; they do not make it impossible. That is
  the trade this page records, and the Lima alternative above is the way
  back if it stops being acceptable.

## Refinements made during implementation

The code landed on 2026-09-22 and matches this page with these changes,
recorded so the page stays accurate. Where a change contradicts something
above, the [scaffold reference](../reference/scaffold.md) is the fact.

- **`internal` defaults to `false` for every zone, including `agent`.**
  The default file sets `internal = true` on the agent zone explicitly,
  because a default that differs per zone is a rule nobody can see in the
  file. Validation refuses an internal zone that no bridge reaches.
- **A schema 1 file still decodes.** The loader keeps the `[machines.*]`
  tables as ignored fields so that an old file reaches validation and gets
  the `schema` finding with the `init --force` hint, rather than failing
  on "unknown keys" with no explanation.
- **Every default Pod file carries `resources.limits`**, sized from the
  old per-machine budgets, and the `openclaw` file drops all capabilities
  and forbids privilege escalation. `--userns auto` is passed by `up` for
  every pod of an internal zone rather than written in the file, because
  `kube play` takes it as a flag.
- **`init --force` does not delete the old `machines/` directory.** The
  writer only writes; the scaffold reference says to remove it by hand.

## What landed with the code

- [`docs/reference/scaffold.md`](../reference/scaffold.md): the
  `schema = 2` topology, its rules, the zones and the default limits.
- The README architecture section, the `lclaw.toml` loader, `Validate`,
  `init`'s embedded defaults, and `doctor`'s `machine` and `network/<zone>`
  checks.
- The [lifecycle commands](lifecycle-commands.md).
- This page is `stable`: the implementation matches it.
