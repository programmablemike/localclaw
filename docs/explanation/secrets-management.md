---
title: "Secrets management"
description: "Why secrets live in a dedicated macOS keychain, how lclaw injects them into each Podman machine as Kubernetes-shaped secrets, and what the secrets commands own."
diataxis: explanation
status: draft
last_reviewed: 2026-09-20
tags: [secrets, keychain, podman, kube-play, security, cli, design-decision]
related:
  - ../../README.md
  - cli-architecture.md
  - deployment-model.md
---

# Secrets management

LocalClaw's workloads need credentials: provider API keys for LiteLLM, the
LiteLLM master key, a database password, the OpenClaw gateway token, and
later the WireGuard and Kuma material the network design will add. This page
records the design agreed on 2026-09-20 for where those values live on the
host, how they reach the containers, and which commands manage them. It was
written before the code existed and is the specification the implementation
follows. Once the code lands, the facts in it (the catalogue, flags, output
shapes, exit codes) move to reference pages and the reasoning stays here.

The short version: every secret is an item in a keychain file that belongs to
LocalClaw. `lclaw up` reads the items it needs, generates any that are
missing, and pushes each one into the right Podman machine as a
Kubernetes-shaped Podman secret through the remote client. Pod files
reference secrets by name and never contain a value. `lclaw down` removes
them again. A `lclaw secrets` command group manages the items.

## Goals and constraints

The README's two goals, isolate the agent and keep operation easy, and the
[CLI architecture](cli-architecture.md)'s three constraints, extensible,
reliable and light, become five rules for secrets.

- **A value is never written where it can be read by accident.** Not in the
  scaffold directory, not in shell history, not in a process argument list,
  not in logs, not in a stopped machine's disk image.
- **The user types as little as possible.** Everything LocalClaw can invent
  for itself is generated. Only credentials issued by a third party are
  typed, and any generated value can be overridden by hand.
- **Everything goes through the remote client.** The
  [deployment model](deployment-model.md) already forbids copying files into
  a VM by hand, and secrets are no exception.
- **A stopped machine holds nothing secret.** Values are present in a VM
  only while it is up.
- **No cgo and standard library first.** The toolchain builds with
  `CGO_ENABLED=0`, so nothing may link Apple's Security framework.

### Facts that shaped the design

Verified on 2026-09-20 against macOS 27.0, Podman 5.8.4, the Podman 5.8.4
source, and the `security` command line tool.

The keychain side:

- `security` is the macOS command line front end to the keychain and calls
  the same framework that Keychain Access uses. Every Go keychain library
  binds that framework through cgo, so `security` as a child process is the
  only route compatible with the toolchain.
- An item created by `security` is readable by `security` without a prompt.
  The keychain access control list trusts the binary that created the item,
  and for lclaw that binary is always `/usr/bin/security`.
- The keychain file is a trailing positional argument. The documented way to
  be prompted for a value, placing `-w` last, therefore only works on the
  default keychain: with a named keychain, `-w` swallows the path as the
  value.
- Interactive mode, `security -i`, reads commands from stdin. Inside it, `-X`
  takes the value as a hexadecimal string, which stores any byte sequence
  with no quoting, and `-U` updates an existing item in place. The value never
  appears in a process argument list. A command line in this mode is limited
  to about 4 KiB.
- Reading with `-w` prints the value verbatim only when every byte is
  printable ASCII, and prints a hexadecimal string otherwise, so a value that
  is itself hexadecimal is ambiguous. Reading with `-g` is unambiguous: it
  prints `password: "..."` for printable values and `password: 0x...` for
  anything else, on stderr.
- `security` exits with the low byte of the Security framework status. Item
  not found is 44, duplicate item is 45, wrong password is 51, interaction
  not allowed is 36, no such keychain is 50.
- `create-keychain` does not add the new file to the search list, and reads
  the password from stdin when stdin is not a terminal. `set-keychain-settings`
  can lock a keychain on sleep and after an idle timeout.

The Podman side:

- `podman secret create` works through the remote client, reads stdin on the
  client, and `--replace` makes it idempotent.
- `kube play` only accepts secrets shaped like a Kubernetes `Secret` object.
  A `secretKeyRef` looks the Podman secret up, unmarshals it as JSON and then
  YAML, and reads the requested key from its data map. A raw blob fails with
  "secret X is not valid JSON/YAML". A secret volume is stricter and fails
  with "only secrets created via the kube yaml file are supported".
- `kube play` copies the value out of the store when it applies a pod. A
  `secretKeyRef` becomes a plain environment variable in the container's
  configuration, visible to `podman inspect`. A secret volume becomes a
  named volume, named after the secret, holding one plaintext file per key
  with the requested mode.
- `kube down` removes only the secrets it created from `kind: Secret`
  documents in the file it is given, and removes volumes only with
  `--force`. Secrets created out of band are left alone.
- The default `file` secret driver stores data read-protected but
  unencrypted under the machine's container storage. The machine's disk is a
  raw image file in the user's home directory.

The LiteLLM side:

- The master key must start with `sk-` and is also the admin UI password.
- The salt key encrypts credentials stored in the database and must not be
  changed once a model has been added.
- When `DATABASE_URL` is unset, LiteLLM composes it from `DATABASE_HOST`,
  `DATABASE_USERNAME`, `DATABASE_PASSWORD` and `DATABASE_NAME`, so the
  password can stay a single secret.
- The key generation endpoint does not accept a caller-chosen key, so the
  agent's virtual key has to be minted by a running LiteLLM.

## Scope

This page covers the secret catalogue, the keychain layout, the `lclaw
secrets` command group, the resolve and inject steps that `up` gains, the
purge step that `down` gains, the keychain step that `init` gains, and the
changes to the process runner that make all of it testable.

Two things it builds on are designed but not yet implemented: the `init`
command and topology loader from the deployment model, and the `up` and
`down` commands from the lifecycle design. This design delivers the
`secrets` command group, the keychain step in `init`, the `doctor` checks,
and the resolve, inject and purge use cases with their tests. The lifecycle
design wires the use cases into `up` and `down` when those commands land.

Three neighbouring designs get their own pages and are only referenced here.

- **Lifecycle commands.** The order in which machines come up, which decides
  when the agent's minted key can be obtained, and the adapter that calls
  LiteLLM to mint it.
- **Network overlay and mesh policy.** The WireGuard and Kuma secrets, added
  to the catalogue as entries with their own generator.
- **Deployment model.** How a pod file consumes a secret, as an environment
  variable or a file, and which of the two each default pod file uses.

## The catalogue

A domain type lists every secret with its name, the machines that need it,
and how it comes to exist. There are three sources.

- **generated**: lclaw makes the value with `crypto/rand` the first time
  `up` needs it and stores it in the keychain like any other item.
- **minted**: lclaw obtains the value from a running service. Only the
  agent's LiteLLM virtual key is minted. Resolution goes through a small
  port; the adapter and the ordering belong to the lifecycle design.
- **user**: supplied through `lclaw secrets set`. Any built-in secret becomes
  `user` the moment someone sets or updates it by hand, and
  `lclaw secrets update --generate` returns it to its default source. That is
  the override rule.

The built-in entries for the workloads the deployment model defines today:

| Name                     | Default source           | Machines   | Value                                     |
| ------------------------ | ------------------------ | ---------- | ----------------------------------------- |
| `litellm-master-key`     | generated                | `services` | `sk-` plus 32 random bytes, base64url     |
| `litellm-salt-key`       | generated, never rotated | `services` | 32 random bytes, hexadecimal              |
| `litellm-db-password`    | generated                | `services` | 32 random bytes, base64url                |
| `openclaw-gateway-token` | generated                | `agent`    | 32 random bytes, base64url                |
| `openclaw-litellm-key`   | minted                   | `agent`    | whatever LiteLLM returns                  |

User-declared entries live in `lclaw.toml`, one list per machine, mirroring
the existing `workloads` list. The default scaffold declares one provider
key, and the default LiteLLM pod file maps it to the environment variable
that image expects. A second provider is one more name here and one more
`secretKeyRef` in the pod file.

```toml
[machines.services]
workloads = ["litellm-db", "litellm", "agentgateway"]
secrets = ["anthropic-api-key"]
```

A name is a lowercase DNS label of at most 63 characters, matching
`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, because the same string is the keychain
account, the Podman secret name and the Kubernetes Secret name. Validation
returns findings the way topology validation does, and reports all of them:
a malformed name, an unknown machine, a name listed twice, or a name that
collides with a built-in entry.

A value is any byte sequence of at least one and at most 1024 bytes. The
limit sits well under the interactive-mode line limit after hexadecimal
encoding and is many times larger than any key in the catalogue.

Generators are pure functions over an `io.Reader` of randomness. The first
implementation has one, a random token with a prefix, a byte count and an
encoding. The network design adds an X25519 private key generator for
WireGuard as one more case, using `crypto/ecdh` as the CLI design already
anticipated. Each entry also says whether it may be rotated; the salt key
may not.

## The keychain

Items live in a keychain file that belongs to LocalClaw, by default
`~/Library/Keychains/lclaw.keychain-db`. The path is configurable in
`lclaw.toml`, and pointing it at `login.keychain-db` is how someone opts into
the login keychain instead.

```toml
[keychain]
path = "~/Library/Keychains/lclaw.keychain-db"
```

**`lclaw init` creates it.** After writing the scaffold, `init` loads the
topology, which may be the file it just wrote or one the user already had,
and creates the keychain at the configured path if it does not exist. The
`security create-keychain` call runs with the terminal attached, so
`security` asks for the new keychain's password itself and lclaw never sees
it; in a script the password can be piped on stdin. lclaw then sets the
keychain to lock when the system sleeps and after fifteen idle minutes.
Changing either is a `security set-keychain-settings` call that the how-to
guide will show; lclaw has no setting for it. An existing keychain is
reported as skipped, like an existing scaffold file, and
`--force` never touches it, because overwriting a keychain destroys secrets.
Every other command that needs the keychain fails with exit 1 and the hint
to run `lclaw init` when the file is missing, and `doctor` reports it as a
failed check.

The file is never added to the keychain search list. lclaw names it on every
call, so nothing else on the machine finds its items by accident, and the
user's login keychain configuration is never modified.

**Each item** is a generic password with service `lclaw`, account set to the
secret name, kind `LocalClaw secret`, and the comment attribute holding the
source. The keychain records creation and modification times itself, which
`describe` reads back.

**When the keychain is locked**, macOS shows its unlock dialog in a session
with a display. In a session without one, such as SSH, `security` fails with
"user interaction is not allowed", and lclaw runs `security unlock-keychain`
with the terminal attached and retries the operation once.

**How lclaw talks to `security`.** Writes go through interactive mode: lclaw
runs `security -i` and sends one `add-generic-password` command on stdin,
with the value hexadecimal-encoded after `-X`, the keychain path last, and
`-U` when updating. Reads use `find-generic-password -g` and parse the
`password:` line from stderr, decoding the hexadecimal form when present.
Attribute-only reads, for `list` and `describe`, use the same command without
`-g`. Every one of these calls is marked sensitive on the runner so that no
output is ever logged, and the adapter strips any `password:` line from
stderr before it wraps an error.

Why a dedicated keychain rather than the login keychain: the access control
list trusts `/usr/bin/security`, so while a keychain is unlocked any local
process can read the items by running the same command. The login keychain
is unlocked for the whole session. A dedicated keychain with a lock timeout
narrows that window to a few minutes after each use, which is the same
reasoning behind aws-vault's own keychain file.

## The commands

Every subcommand of `lclaw secrets` honours the global `--output text|json`
flag, and none prints a value except `get`. The commands write the keychain
and, where a machine that uses the secret is running, that machine's Podman
secret store. They never apply pods; `up` remains the only command that does.

- **`list`** prints one row per catalogue entry with name, source, machines
  and state. State is `set` or `unset`. An unset generated secret shows
  "generated on next up", an unset minted secret shows "minted on next up",
  and an unset user secret shows "run lclaw secrets set".
- **`set NAME`** creates a secret. The value comes from `--from-file PATH`,
  else from stdin when stdin is not a terminal, else from a prompt that does
  not echo. Input from a file or a pipe has exactly one trailing newline
  removed, because no real key ends with one and a stray one fails silently.
  Exit 1 if the secret already exists, with a hint to use `update`. Exit 2
  if the name is not in the catalogue, with a hint to declare it in
  `lclaw.toml`, or if the value is empty or over the size limit. The source
  becomes `user`.
- **`update NAME [--generate]`** replaces an existing secret using the same
  input rules. With `--generate` it re-runs the generator or the minter and
  restores the default source. The flag is refused for user-declared names,
  which have nothing to generate, and for entries that may not be rotated.
  After writing the keychain, it replaces the secret in the store of every
  running machine that uses it and says that pods pick the value up on the
  next `up`.
- **`delete NAME`** removes the item from the keychain and from the store of
  every running machine that uses it. Exit 1 if it is not set. A deleted
  generated secret comes back fresh on the next `up`; a deleted user secret
  makes the next `up` stop with a finding. Deleting an entry that may not be
  rotated prints a warning, because the next `up` generates a new value and,
  for the salt key, LiteLLM's stored credentials become unreadable.
- **`describe NAME`** prints the name, source, machines, creation and
  modification times, and one line per machine whose state is `stored`,
  `absent` or `stopped`. Never the value.
- **`get NAME`** prints the value and nothing else, for logging into the
  LiteLLM dashboard with the master key or piping a value into another tool.

Text output uses the same tabwriter conventions as `doctor`.

```text
NAME                  SOURCE     MACHINES  STATE
anthropic-api-key     user       services  set
litellm-master-key    generated  services  set
litellm-salt-key      generated  services  unset (generated on next up)
openclaw-litellm-key  minted     agent     unset (minted on next up)
```

JSON output for `list` carries a `secrets` array whose entries have `name`,
`source`, `machines` and `state`. `describe` adds `created`, `modified` and a
`stores` array with one object per machine holding `machine` and `state`.
Values are lowercase strings and are part of the interface.

## Injection and removal

**Up, per machine.** Two steps slot into the deployment model's sequence
between `podman machine start` and the workload loop.

1. **Resolve.** For every catalogue entry naming this machine, read the
   keychain item. A missing generated secret is generated and stored with
   source `generated`. A missing minted secret is requested from the minter
   port. A missing user secret is a finding. Findings are collected rather
   than thrown, and if there are any, lclaw reports all of them, each with
   the hint `lclaw secrets set NAME`, and stops before touching Podman on this
   machine. Exit 1.
2. **Inject.** For each resolved value, the Podman adapter wraps it as a
   Kubernetes Secret with a single key, `value`, and pipes the JSON to
   `podman --connection lclaw-<role> secret create --replace` with the label
   `app.kubernetes.io/part-of=localclaw`. Then the build and `kube play`
   steps run unchanged.

```json
{"apiVersion":"v1","kind":"Secret","metadata":{"name":"litellm-master-key"},"type":"Opaque","data":{"value":"c2stLi4u"}}
```

A pod file consumes a secret either as an environment variable through
`secretKeyRef` with key `value`, or as a file through a secret volume. The
deployment model's example changes its key from `token` to `value`. Its
reference page will recommend a file wherever the upstream image can read
one, because an environment variable also appears in `podman inspect`.

**Down, per machine.** After `kube down` for every workload and before
`podman machine stop`, lclaw lists the store's secrets by the LocalClaw
label, removes each one, and removes the named volume of the same name if
one exists. Secret names and volume claim names share one namespace on a
machine, so the default files keep them distinct and the reference page
states the rule. Listing by label rather than by catalogue means entries dropped
from the catalogue since the last `up` are cleaned up too. Once the machine
stops, its disk holds nothing that lclaw injected. A machine that reboots
while it is up still restarts its pods through `podman-restart.service`,
because the copies that `kube play` made exist until `down` runs.

## Where it sits in the layering

The CLI design's dependency rule holds without new edges.

- **Domain** gains `SecretSpec`, the three-valued `Source`, the `Catalogue`
  that merges built-in and declared entries, name validation, the generators,
  and an `EvaluateSecrets` rule that turns keychain observations into checks,
  like the existing evaluation functions. Pure Go, tested with literals and a
  deterministic byte reader.
- **Application** gains three ports, named for their role. `Keychain` has
  exists, create, get, describe, put and delete. `SecretTarget` has
  machine-running, store, exists, remove, list-by-label and remove-volume,
  and the Podman client satisfies it alongside `MachineRuntime`. `KeyMinter`
  has mint and revoke and stays unimplemented until the lifecycle design
  lands. Use cases are `Secrets` for the command group, `ResolveSecrets` and
  `InjectSecrets` for `up`, and `PurgeSecrets` for `down`. `Init` gains the
  keychain step. `Doctor` gains two checks: the keychain exists, and every
  declared user secret is set, the second read without touching values.
- **Adapters** gain `adapters/keychain`, which wraps `security`. The Podman
  adapter grows the store methods and the JSON wrapping. Nothing outside the
  Podman adapter knows the Kubernetes shape, and nothing outside the keychain
  adapter knows about `-i`, `-X` or `-g`.
- **Presentation** gains the `secrets` command group, its two renderers, and
  the value input path.
- **Composition root** wires the keychain adapter and passes the Podman
  client as both ports.

Two changes to the process runner from the CLI design, made before any code
exists:

- `Run` takes a command value with an optional stdin reader and a
  `Sensitive` flag. A sensitive command logs its argv and exit status but
  never its output. The fake records stdin so tests can assert the exact JSON
  sent to Podman and the exact command sent to `security`.
- A second method, `Interactive`, inherits the terminal and captures nothing.
  It exists for `create-keychain` and `unlock-keychain`.

One dependency is added. The prompt that does not echo has no standard
library answer. `golang.org/x/term` was measured in the CLI design at two
modules in the graph and two linked, both from the Go project, with no cgo.
The alternative, running `stty` through the runner around a plain read, is
zero modules but leaves the terminal without echo if the process dies
between the two calls.

## Error handling

The CLI design's rules apply unchanged: errors wrap with `%w` and a prefix
naming the layer and operation, adapters return rather than log, and the
domain returns findings rather than errors. Three rules are specific to
secrets.

- **`security` exit codes map to sentinel errors.** The keychain adapter
  maps 44, 45, 36, 50 and 51 to not found, already exists, keychain locked,
  no such keychain and wrong password. Anything else wraps with the operation
  name. A missing `security` binary wraps `ErrToolNotFound` like the other
  tools.
- **Missing secrets are findings, not errors.** `up` reports every missing
  user secret at once in the report shape `doctor` uses. A missing minted
  secret reports that it needs the services machine up.
- **Partial failure across machines does not roll back.** If `update` writes
  the keychain and then one machine's store fails, both outcomes are reported
  and the exit code is 1. The keychain is the source of truth and the next
  `up` reconciles, because `--replace` is idempotent.

A value never appears in a log line, an error message, JSON output other
than `get`, or a process argument list. Cancellation is honoured between
steps, and a cancelled interactive call kills the child.

Exit codes follow the CLI design's table: 0 for success, 1 for a failed
check or an unexpected error, 2 for a usage error, 130 for an interrupt.

## Testing

- **Domain:** tables for name validation, catalogue merging and collisions,
  and each generator against a deterministic byte reader so the expected
  prefix, length and encoding are literal strings.
- **Application:** use cases against in-memory fakes of the keychain and the
  target. Cases cover everything present, a generated secret missing, a user
  secret missing with no store call made, several missing at once all
  reported, `--replace` on every inject, purge removing both secret and
  volume, `update` skipping a stopped machine, one store failing, and
  cancellation.
- **Keychain adapter:** against the exec fake with fixtures captured while
  this design was verified, including both `-g` output forms and the five
  exit codes, asserting that `-i` is the only argument and that the
  hexadecimal command arrives on stdin. Plus one real test against a
  throwaway keychain in a temporary directory, skipped where `security` is
  absent, that does create, put, get for ASCII, binary and 1024-byte values,
  describe, update and delete.
- **Podman adapter:** a golden file for the JSON body, argv assertions for
  the label and `--replace`, and a fixture for `podman secret ls --format
  json` decoding only the name.
- **Presentation:** golden text and JSON for `list` and `describe`, `set`
  from a pipe removing one newline, the prompt path through an injected
  reader rather than a real terminal, and the exit codes for exists, unknown
  and too large.
- **Integration on Linux CI:** create a Kubernetes-shaped secret with
  `podman secret create`, play a pod that consumes it both as an environment
  variable and as a file, assert both inside the container along with the
  file mode, then confirm that a volume named after the secret exists and is
  removed. This pins the wire contract and the volume-naming assumption on
  every change.
- **Architecture:** the import-graph test covers the new packages.

### Assumptions to verify during implementation

These could not be checked without a display or a real terminal and are
carried into the implementation plan as explicit tasks.

- macOS shows its unlock dialog when `security` touches a locked keychain in
  a session with a display.
- `security create-keychain` prompts without echo on a real terminal.
- A secret volume created by `kube play` is a named volume with the secret's
  name.
- The interactive-mode line limit is near 4 KiB, so 1024-byte values fit.

## Alternatives considered

**The login keychain by default.** Zero extra prompts, but any local
process can read the items for the whole session. Kept as a configuration
choice rather than the default.

**Secrets inside the `kube play` input.** lclaw could concatenate `kind:
Secret` documents with each pod file and pipe the result to `kube play -`,
letting Podman own the lifecycle. A secret shared by two pods on one machine
would be created twice, `update` would have nothing to update without
replaying pods, lclaw would start assembling YAML text, and a person applying
by hand would have to author Secret documents. Rejected.

**The VM pulls from the host on demand.** Podman's shell secret driver could
run a lookup script inside each VM that fetches from a small server on the
host. It needs a daemon reachable from every machine including the untrusted
one, with its own authentication, and gains nothing at rest because `kube
play` copies the value into the pod regardless. Rejected.

**Environment variables at run time.** Passing `-e` through the remote
client puts the value in the container configuration and in `podman
inspect`, with no way to remove it short of recreating the container. The
Podman secret store with `--replace` is strictly better.

**Injection at first boot.** Ignition or the `--playbook` hook could write
secrets when a machine is created. Values would sit in a file on the host
and in the VM image forever, and rotation would mean recreating the machine.
Rejected.

**A Go keychain library.** Every one links the Security framework through
cgo, which the toolchain disables. `security` also fits the existing runner
port and gets a scripted fake for free.

**Wrapping values in base64 before storage.** It would make every item
printable ASCII and let the simpler `-w` reader be exact, at the cost of
showing base64 in Keychain Access and to anyone using `security` directly.
The `-g` reader costs one line of parsing and keeps items readable.

## Consequences

- The deployment model page gains the per-machine `secrets` list, the
  `[keychain]` table, the two `up` steps, the `down` step, the keychain step
  in `init`, and `value` as the key in its example.
- The network design inherits the catalogue and the generator switch.
- The lifecycle design inherits the `KeyMinter` port and the rule that
  `services` comes up before `agent`.
- `golang.org/x/term` joins the dependency list under the dependency policy,
  with its measured numbers.
- The README's architecture section should say that secrets live in a
  keychain and are injected on `up`.

## What lands with the code

- `docs/reference/secrets.md`: the catalogue, the keychain layout, the
  command flags, the JSON shapes and the exit codes.
- `docs/how-to/manage-secrets.md`: set a provider key, rotate the master
  key, log into the LiteLLM dashboard.
- A `podman secret create` step with the JSON shape added to
  `docs/how-to/apply-a-deployment-by-hand.md`.
- This page moves from `draft` to `stable` once the implementation matches
  it.
