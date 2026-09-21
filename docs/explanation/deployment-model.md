---
title: "Deployment model"
description: "Why each workload is a Containerfile over its upstream image plus a Pod file, how lclaw init scaffolds them, and the podman sequence that applies them."
diataxis: explanation
status: stable
last_reviewed: 2026-09-21
tags: [deployment, podman, containers, kube-play, scaffold, secrets, iac, design-decision]
related:
  - ../../README.md
  - cli-architecture.md
  - secrets-management.md
  - ../reference/scaffold.md
  - ../reference/cli.md
  - ../how-to/apply-a-deployment-by-hand.md
---

# Deployment model

LocalClaw runs seven containers across three Podman machines. This page
records the design agreed on 2026-09-20 for how those containers and machines
are described, where the description lives, and how it is applied. It was
written before the code existed and is the specification the implementation
follows. Once the code lands, the facts in it (file layout, the `lclaw.toml`
schema, command names) move to reference pages and the reasoning stays here.

The short version: every workload is a Containerfile that starts from its
upstream image, plus a Kubernetes Pod file that Podman applies directly.
`lclaw init` writes these into a directory the user owns. A small TOML file
says which machine runs what. Nothing about *how* a workload runs is in a
LocalClaw-specific format.

## Goals and constraints

The README states two goals: isolate the agent from the host, and make setup
and operation easy and reliable. The [CLI architecture](cli-architecture.md)
adds three constraints: extensible, reliable, light. For deployment they
translate into the following.

- **Customizable without forking.** A user who wants a different LiteLLM
  version, an extra tool in the agent image, or more memory for a machine
  should edit a file, not rebuild LocalClaw.
- **Readable by anyone who knows Podman.** The files that describe a workload
  should be Podman's own formats, so Podman's documentation is the reference
  and a person can apply them by hand when `lclaw` is not enough.
- **Applied from the host.** macOS talks to its Podman machines as a remote
  client over a socket. Every step must work through that client. Nothing
  may depend on copying files into a VM by hand.
- **Thin driver.** `lclaw` should wrap a few documented `podman` commands with
  JSON output, in the adapter shape the CLI design already commits to.

### Facts that shaped the design

These were verified against Podman 5.8.4 and Flox 1.13.1 on 2026-09-20.

- Every workload publishes an official image: OpenClaw, LiteLLM, Agent
  Gateway, Kuma and Postgres. Building images from scratch is a choice, not a
  necessity.
- `flox containerize` can build an image from a Flox environment, but on
  macOS it needs a running Podman machine to host its proxy build container.
  Its `[containerize.config]` manifest table is marked experimental.
- Podman 5.6 added `podman quadlet install`, `list`, `print` and `rm`, but
  none of them work through the remote client. Quadlet files would have to
  reach a VM over `podman machine ssh`.
- `podman kube play` works through the remote client, accepts several files
  and multi-document YAML, supports `Pod`, `PersistentVolumeClaim`,
  `ConfigMap` and `Secret` kinds, and its `--replace` flag makes re-applying
  idempotent. Its `--build` and `--configmap` flags are not available
  remotely, so images are built separately and ConfigMaps go inside the file.
- `podman machine init` accepts `-v ""` to create a machine with no host
  directories mounted (accepted upstream in containers/podman#23254). The
  default otherwise mounts `/Users`, `/private` and `/var/folders` into the
  VM.
- Podman 5.8 added `podman machine init --playbook`, which runs an Ansible
  playbook inside the VM after first boot. The playbook cannot include other
  files from the host.
- The machine image enables lingering for the `core` user but does not enable
  `podman-restart.service`, so containers do not come back after a VM reboot
  unless something arranges it.
- Podman 5.7 made `libkrun` the default machine provider on macOS, and 5.8
  allows `libkrun` and `applehv` machines to run at the same time.

## Scope

This page covers the deployment model: the scaffolded files, the `lclaw init`
command that writes them, the contract each file must satisfy, the fixed
`podman` sequence that applies them, and two new `lclaw doctor` checks.

Three neighbouring designs get their own pages and are only referenced here.

- **Network overlay and mesh policy.** WireGuard peering between machines,
  the Kuma control plane, access control lists and egress rules. That design
  owns the contents of the `infra` machine's workloads and any sidecars it
  adds to the others.
- **Secrets.** API keys for LiteLLM and OpenClaw, WireGuard keys, the
  Postgres password. Pod files reference secrets by name; creating them is
  that design's job.
- **Lifecycle commands.** `lclaw up`, `down` and `status` compose the apply
  sequence defined here. The CLI design already reserves a page for them.

## Images derive from upstream

Each workload has a Containerfile whose first instruction is `FROM` the
upstream image, pinned by tag and digest:

```dockerfile
# LocalClaw: OpenClaw agent. Derived from the upstream image so that
# customizing is an edit here, not a new build system. Add RUN, COPY or ENV
# instructions below the FROM line. Rebuild by re-running `lclaw up` or the
# manual procedure in the how-to guide.
FROM ghcr.io/openclaw/openclaw:2026.9.3@sha256:0000000000000000000000000000000000000000000000000000000000000000
```

The default files contain no instructions after `FROM`. They exist so that
the place to customize is already there, named and commented. A user who
wants Chromium in the agent image, or a `pip install` in the LiteLLM image,
appends a line. The diff against upstream is the whole customization surface,
which is easy to review and easy to revert.

Pinning by digest keeps builds reproducible and keeps the trust boundary at
the upstream registry. Each LocalClaw release bumps the digests in its
embedded defaults; a user who has customized a Containerfile updates the
`FROM` line by hand, which is the one merge conflict this design accepts.

Images are built on the machine that runs them, because Podman machines do
not share an image store, and are tagged `localhost/lclaw/<workload>:latest`.
Pod files reference that name with `imagePullPolicy: Never`, so a missing
build fails immediately instead of trying to pull something that does not
exist.

| Machine    | Workload       | Upstream image                                  |
| ---------- | -------------- | ----------------------------------------------- |
| `infra`    | `wireguard`    | `docker.io/linuxserver/wireguard` (provisional) |
| `infra`    | `kuma-cp`      | `docker.io/kumahq/kuma-cp` (provisional)        |
| `infra`    | `gateway`      | `docker.io/kumahq/kuma-dp` (provisional)        |
| `services` | `litellm-db`   | `docker.io/library/postgres`                    |
| `services` | `litellm`      | `ghcr.io/berriai/litellm`                       |
| `services` | `agentgateway` | `cr.agentgateway.dev/agentgateway`              |
| `agent`    | `openclaw`     | `ghcr.io/openclaw/openclaw`                     |

Rows marked provisional are placeholders until the network design chooses
how WireGuard and the gateway run. LiteLLM's stateful mode needs a database,
so the `services` machine carries Postgres, which the README's table does not
yet show.

## Workloads are Pod files

Next to each Containerfile is `pod.yaml`, a multi-document Kubernetes file
that `podman kube play` applies as one unit. The conventions every default
file follows, and that customized files must keep:

- **One `Pod` per workload**, named after the workload directory, with the
  labels `app.kubernetes.io/name: <workload>` and
  `app.kubernetes.io/part-of: localclaw`. `lclaw status` will find pods by
  the second label.
- **`restartPolicy: Always`**, stated explicitly even though it is Podman's
  default, because it is what the boot-time restart service keys on.
- **One container by default.** The pod is the unit so that the network
  design can add a mesh sidecar without changing the file's shape.
- **Ports.** `containerPort` documents what the workload listens on. Only
  workloads that must be reachable from the macOS host add `hostPort`, and
  in the default files that is only the `infra` machine, because Podman's
  gvproxy forwards every published port to the host's loopback and two
  machines cannot publish the same one.
- **State** lives in named Podman volumes declared as
  `persistentVolumeClaim` documents named `<workload>-<purpose>`. Nothing is
  bind-mounted from the host. The volume lives on the machine's disk and is
  removed with `podman machine rm`.
- **Configuration files** are `ConfigMap` documents in the same file, mounted
  as a directory. LiteLLM's `config.yaml`, for example, is mounted at
  `/etc/litellm` and passed with `--config`.
- **Secrets** are referenced by name through `secretKeyRef` or secret
  volumes and are never present in the scaffold. The shape of the stored
  secret is settled by the secrets design.

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: openclaw-state
---
apiVersion: v1
kind: Pod
metadata:
  name: openclaw
  labels:
    app.kubernetes.io/name: openclaw
    app.kubernetes.io/part-of: localclaw
spec:
  restartPolicy: Always
  containers:
    - name: openclaw
      image: localhost/lclaw/openclaw:latest
      imagePullPolicy: Never
      ports:
        - containerPort: 18789
      env:
        - name: OPENCLAW_GATEWAY_TOKEN
          valueFrom:
            secretKeyRef:
              name: openclaw-gateway-token
              key: value
      volumeMounts:
        - name: state
          mountPath: /home/node/.openclaw
  volumes:
    - name: state
      persistentVolumeClaim:
        claimName: openclaw-state
```

## Machines

### The topology file

`lclaw.toml` at the root of the scaffold is the only LocalClaw-specific
format in the design. It answers one question: what machines exist, how big
are they, and what runs on each.

```toml
# LocalClaw topology. The three roles are fixed; edit sizes and workload
# lists. Workloads are applied in the order listed and torn down in reverse.
schema = 1
provider = "libkrun"

# The keychain holding the secrets lclaw injects into the machines.
[keychain]
path = "~/Library/Keychains/lclaw.keychain-db"

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
secrets = ["anthropic-api-key"]

[machines.agent]
cpus = 2
memory-mib = 4096
disk-gib = 30
workloads = ["openclaw"]
# volumes = ["/Users/me/workspace:/mnt/workspace"]   # opt in; see below
```

The three role keys are fixed by the domain model: the file can resize a
machine or move a workload, but cannot add a fourth machine or drop one.
Field names map one to one onto `podman machine init` flags, with units in
the name so there is nothing to parse. `provider` selects the machine
provider and is passed as the `CONTAINERS_MACHINE_PROVIDER` environment
variable. The default sizes total five CPUs and nine GiB, which fits a
16 GiB laptop alongside a browser; they are starting points, not
recommendations.

Each machine table also accepts an optional `volumes` list of
`host:guest` strings, empty by default. The top-level `[keychain]` table
and each machine's `secrets` list belong to
[Secrets management](secrets-management.md), which names the keychain that
holds the values and the items each machine receives on `up`. Validation
lives in the domain and returns findings rather than errors, like
`doctor`'s checks:

- Every workload named in the file has a directory containing both
  `Containerfile` and `pod.yaml`.
- No workload appears under two machines.
- Every role appears exactly once, and `cpus`, `memory-mib` and `disk-gib`
  are positive.
- `provider` is `libkrun` or `applehv`.
- Every `volumes` entry has an absolute host path and an absolute guest path
  separated by a colon.

### How a machine is created

For role `<role>` the machine is named `lclaw-<role>`, as the CLI design's
`Role.MachineName` already specifies, and is created with:

```text
podman machine init lclaw-<role>
  --cpus <cpus> --memory <memory-mib> --disk-size <disk-gib>
  --volume ""
  --playbook machines/<role>/playbook.yaml
```

Three choices in that command carry the isolation goal.

- **`--volume ""`** creates the machine with no host directories mounted.
  The default would expose the whole of `/Users` to every container in the
  VM through a single bind mount. Nothing in this design needs a host mount:
  image build contexts travel through the remote client, configuration
  travels inside the Pod file, and state lives in named volumes. A user who
  wants a workspace mounted into the agent adds it to the machine's
  `volumes` list in `lclaw.toml`, and each entry becomes a `--volume` flag in
  place of the empty one. The reference page for that field will say what it
  costs in isolation.
- **Rootless** is the default and is kept. Nothing here needs a rootful
  machine.
- **No `--now`.** Creating and starting are separate steps so the lifecycle
  design can decide when each happens and report each separately.

### First-boot playbook

`machines/<role>/playbook.yaml` runs once, inside the VM, as the `core`
user. The default playbook has a single task: enable
`podman-restart.service` at user scope. Podman's machine image turns on
lingering for `core` but does not enable that service, and without it no
container survives a VM reboot. With it, every container whose restart
policy is `always` is started at boot, which is exactly the policy the Pod
files set.

The playbook is the network design's hook for anything else the VM needs,
such as kernel parameters. Because Podman copies only the playbook file, it
must stay self-contained.

## The scaffold and `lclaw init`

### Layout

```text
~/.config/lclaw/
  lclaw.toml
  machines/
    infra/playbook.yaml
    services/playbook.yaml
    agent/playbook.yaml
  workloads/
    wireguard/     Containerfile  pod.yaml  .containerignore
    kuma-cp/       Containerfile  pod.yaml  .containerignore
    gateway/       Containerfile  pod.yaml  .containerignore
    litellm-db/    Containerfile  pod.yaml  .containerignore
    litellm/       Containerfile  pod.yaml  .containerignore
    agentgateway/  Containerfile  pod.yaml  .containerignore
    openclaw/      Containerfile  pod.yaml  .containerignore
```

The default location is `~/.config/lclaw`, overridable with `--dir` and the
`LCLAW_DIR` environment variable, following the CLI's existing flag and
environment convention. The directory holds configuration the user edits, not
runtime state; state is whatever Podman records about machines, images,
pods and volumes. The directory is suitable for a user's own git repository.

Each workload directory is also that image's build context. The remote
client sends the whole directory to the machine as a tarball, so a
`.containerignore` in each directory excludes `pod.yaml`, and users who add
large files next to a Containerfile pay for them on every build.

### Behaviour of `lclaw init`

- Writes every default file whose path does not yet exist, creating
  directories as needed.
- Skips every file that already exists and reports each one, so a user can
  see what a newer release would have changed. `--force` overwrites.
- Writes each file to a temporary name in the same directory and renames it,
  so an interrupted run leaves no half-written file.
- Reports the full list of written and skipped paths in text or, with
  `--output json`, as an object holding `dir` and the `written`, `skipped`
  and `failed` arrays.
- Exits 0 when nothing needed writing. A directory that already matches the
  defaults is success, not an error. Exits 1 if any write failed.

Merging a user's edits with newer defaults is out of scope. The skipped-file
report and the pinned `FROM` lines are the tools a user has for that today.

### Where it sits in the layering

The CLI design's dependency rule holds without new edges.

- **Domain** gains `Topology`, `MachineSpec` and `Workload` types and the
  validation rules above. Pure Go, tested with literals.
- **Application** gains an `Init` use case with two ports: an `fs.FS` of
  default files, and a `FileWriter` that writes a file and refuses to
  overwrite unless told to. It gains a
  `TopologyLoader` port that reads and decodes `lclaw.toml`. `Doctor` gains
  two checks: the scaffold directory exists, and its topology validates.
- **Adapters** gain an `os`-backed `FileWriter` and a TOML-backed
  `TopologyLoader`.
- **Presentation** gains the `init` subcommand and the two renderers for its
  report.
- **Composition root** embeds the default files with `embed` and passes them
  to `Init` as an `fs.FS`. Keeping `embed` out of `app` lets tests use
  `fstest.MapFS` and keeps the default files a data asset rather than code.

The TOML decoder is the design's one new dependency. The CLI design
pre-approved TOML for configuration and named two self-contained candidate
modules; the implementation measures both, graph and linked, before choosing,
as the dependency policy requires. The decoder is needed now because
`doctor` reads the file, so `init` does not write a file no code reads.

The playbook requirement raises the minimum Podman version. The domain's
`PodmanRequirement` moves from `5.0.0` to `5.8.0`, and `doctor` reports the
new minimum.

## Applying the deployment

This is the sequence for one machine. The lifecycle design decides ordering
across machines, parallelism and reporting; this page fixes what each step
is. Every command is a documented `podman` subcommand run through the remote
client from the host, and the how-to guide that lands with the code walks a
person through the same sequence by hand.

**Up**, for role `<role>` with connection name `lclaw-<role>`, which
`podman machine init` creates:

1. `podman machine inspect lclaw-<role>`. If it fails with not found, run the
   `init` command above.
2. `podman machine start lclaw-<role>`. Already running is success.
3. **Resolve**, from [Secrets management](secrets-management.md): read
   every catalogue entry naming this machine out of the keychain,
   generating or minting what is missing. A missing user secret is a
   finding; if there are any, all of them are reported and the machine
   stops here without Podman being touched.
4. **Inject**, from the same design:
   `podman --connection lclaw-<role> secret create --replace` for each
   resolved value, wrapped as a Kubernetes `Secret` with the single key
   `value`.
5. For each workload in the order listed in `lclaw.toml`:
   1. `podman --connection lclaw-<role> build --tag localhost/lclaw/<workload>:latest workloads/<workload>`
   2. `podman --connection lclaw-<role> kube play --replace workloads/<workload>/pod.yaml`

**Down** runs `kube down` for each workload in reverse order, then
**purges** the secrets that the secrets design injected — every secret in
the machine's store carrying the `app.kubernetes.io/part-of=localclaw`
label, and the named volume of the same name where `kube play` made one —
and then `podman machine stop`. Named volumes other than those survive.
**Destroy** is `podman machine rm --force`, which removes the disk and
everything on it.

Every step is idempotent: `inspect` guards `init`, `start` tolerates
running, `build` is cached by layer, and `--replace` recreates a pod that
already exists. Running "up" twice is the way to apply an edit.

Traffic between machines is not addressed by these steps. Each Podman machine
has its own user-mode network and cannot reach another directly; the network
design routes inter-machine traffic through ports the `infra` machine
publishes to the host, which is why only `infra` workloads carry `hostPort`.

## Error handling

The CLI design's rules apply unchanged: errors wrap with `%w` and a prefix
naming the layer and operation, adapters return rather than log, and the
domain's validation returns findings rather than errors. Two rules are
specific to this design.

- **Validation reports everything.** A topology with a missing workload
  directory, a duplicated workload and a zero CPU count reports all three,
  in file order, the way `doctor` reports every failed check.
- **A skipped file is not an error.** `init` distinguishes "already exists"
  from "could not write", reports the first as information and the second as
  a failure with the path and the underlying error, and continues past both
  so the report is complete.

## Testing

- **Domain:** table-driven tests for every validation rule and for the
  machine-name mapping.
- **Application:** `Init` against `fstest.MapFS` defaults and an in-memory
  `FileWriter` fake. Cases: empty directory, every file present, a mix,
  `--force`, a write failure in the middle. `Doctor` gains cases for a
  missing scaffold and an invalid topology.
- **Embedded defaults:** a test that loads the real embedded files and
  checks that every workload named in the default `lclaw.toml` has all
  three files, every `FROM` line carries a digest, and every `pod.yaml` sets
  `imagePullPolicy: Never` and `restartPolicy: Always`. These are line
  checks over the embedded text, so no YAML parser enters the module.
- **Adapters:** the TOML loader against fixtures in `testdata/`, including a
  malformed file and unknown keys. The `FileWriter` against a temporary
  directory.
- **Presentation:** golden files for the text and JSON `init` reports.
- **Integration on Linux CI:** the Ubuntu job runs `podman build` on every
  default Containerfile and `podman kube play --replace` then `kube down`
  on every default `pod.yaml` against the runner's own Podman, with no
  machine involved. This proves the files are valid Podman input on every
  change. Workloads that need capabilities the runner lacks, such as
  WireGuard's `NET_ADMIN`, are listed in one place and skipped with a
  reason.

## Alternatives considered

**Building images with `flox containerize`.** The README says agents are
built with Flox, and Flox can emit an OCI image from a manifest. Two problems
outweighed the appeal. On macOS it needs a running Podman machine, which
makes image building depend on the thing being deployed, and it produces an
image assembled from Nix packages rather than the upstream project's own
release, so a user who reads OpenClaw's or LiteLLM's documentation finds a
different filesystem than the one described. Deriving from upstream keeps
those projects' documentation valid inside LocalClaw. A Flox-built layer
remains a documented customization for the agent image, not the default.

**Quadlet over `podman machine ssh`.** Quadlet is Podman's idiomatic
infrastructure as code, and systemd would give dependencies, journal logs
and boot start for free. But installing Quadlets is not supported through
the remote client, and neither is listing or printing them, so `lclaw` would
own a second transport it cannot introspect. When remote support lands, the
Pod files here become the payload of `.kube` units without any user file
changing.

**A LocalClaw manifest translated into `podman run`.** One format, full
control, no Kubernetes YAML. It would mean inventing a format, owning the
reconciliation of declared versus running state, and producing files that
nothing but `lclaw` can read.

**Embedded defaults with an override directory, or repository files only.**
An override directory hides the effective configuration until someone asks
for it. Repository files tie users to a source checkout rather than a
released binary. A scaffolded directory the user owns is how `flox init` and
Compose behave, and it is what people expect to put in their own git
repository.

**Host bind mounts for state.** Visible and easy to back up, but they punch
through the isolation the project exists to provide, are slower through
virtiofs, and bring UID mapping problems. Named volumes with an explicit
opt-in for a workspace mount keep the default safe.

**Compose.** `podman compose` delegates to an external tool and adds a
dependency on Python or a compose binary inside the VM. `kube play` is built
in and works remotely.

## Consequences

- The README's architecture section should say that workloads run from
  upstream images customized through Containerfiles, and that Flox provides
  the toolchain and packages `lclaw`. Its table gains a Postgres row.
- The network design inherits three provisional `infra` workloads, the
  playbook as its provisioning hook, and the rule that only `infra`
  publishes host ports.
- The secrets design inherits the contract that Pod files reference secrets
  by name.
- The lifecycle design inherits the apply sequence and the topology type.

## Refinements made during implementation

The code landed on 2026-09-20 and matches this page with these changes,
recorded so the page stays accurate.

- **`--dir` is a global flag**, not an `init` flag, because `doctor` reads
  the same directory. `lclaw init --dir X` still works, since global flags
  may follow the command name.
- **Doctor statuses.** A missing scaffold directory is a warning, the
  expected state before the first `init`, like a machine that is not
  created; the topology check is then a warning saying it was skipped. A
  topology that cannot be read or decoded is one failed check; each
  validation finding is its own failed check named `topology`, so every
  problem shows and JSON consumers get an array. Checks run in the order
  `flox`, `podman`, `scaffold`, `topology`, machines.
- **Two more validation rules.** `schema` must be `1`, and a workload name
  is lowercase letters, digits and hyphens, because it becomes a directory
  and an image tag.
- **`Validate` takes an `exists` function** rather than an `fs.FS`, so the
  domain performs no I/O; callers pass a closure over a file system or a
  map.
- **TOML decoder.** `github.com/BurntSushi/toml` v1.6.0 and
  `github.com/pelletier/go-toml/v2` v2.4.3 were both measured on
  2026-09-20: one module in the graph and one linked each, no extras.
  BurntSushi was chosen because `Decode` returns metadata whose
  `Undecoded()` lists every unknown key, and because the adapter reads the
  file itself so a missing file keeps reporting not-found. The loader
  takes an `fs.FS` rooted at the scaffold, opened by a `DirOpener` port, so
  only the `osfs` adapter touches the operating system.
- **The init text report is grouped**: written, then skipped, then failed
  lines, then one summary line naming the directory.
- **Postgres 18** keeps its cluster under `/var/lib/postgresql`, so the
  `litellm-db` volume mounts there rather than at `/var/lib/postgresql/data`.
- **Placeholder secrets in CI.** `podman kube play` refuses a pod whose
  `secretKeyRef` names a missing secret and reads secrets saved from a
  Kubernetes `Secret` document, so the integration script generates one
  such document per referenced name from the Pod files and plays it first.
- **Image tags** were chosen on 2026-09-20 as the newest stable tag of each
  upstream; the [scaffold reference](../reference/scaffold.md) lists them.
- **Secret keys follow the secrets design.** Every injected secret has the
  single key `value`; the LiteLLM pod composes `DATABASE_URL` from
  `DATABASE_*` variables so the database password is one secret, and it
  also consumes `litellm-salt-key`.
- **`.containerignore` is a required file.** Validation and the lint test
  require all three files in a workload directory, not only `Containerfile`
  and `pod.yaml`, because the build context must exclude `pod.yaml`.
- **Findings come out in role order.** The loader orders machines `infra`,
  `services`, `agent`, then any other name alphabetically, so "file order"
  for machine findings means role order.

## What landed with the code

- [`docs/reference/scaffold.md`](../reference/scaffold.md): the layout, the
  `lclaw.toml` schema and validation rules, the Pod file conventions, the
  default images, ports and secrets, and what `init` does to each file.
- [`docs/how-to/apply-a-deployment-by-hand.md`](../how-to/apply-a-deployment-by-hand.md):
  the `podman` sequence above as a procedure.
- The README changes listed under consequences.
- This page is `stable`: the implementation matches it.
