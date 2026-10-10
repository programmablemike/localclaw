---
title: "Provider API"
description: "Why model providers are declared in lclaw.toml, how lclaw renders LiteLLM's model list into a secret the proxy includes, and what the provider commands own."
diataxis: explanation
status: draft
last_reviewed: 2026-09-22
tags: [cli, lclaw, provider, litellm, secrets, config, design-decision]
related:
  - cli-architecture.md
  - lifecycle-commands.md
  - secrets-management.md
  - single-machine.md
  - ../reference/cli.md
  - ../reference/secrets.md
  - ../reference/scaffold.md
---

# Provider API

`lclaw provider` declares which model providers LiteLLM serves, holds their
API keys, and renders the model list the proxy reads. This page records the
design agreed on 2026-09-22 for the command group. It was written before the
code existed and is the specification the implementation follows.

The [CLI architecture](cli-architecture.md) fixes the layering, the
[secrets design](secrets-management.md) fixes where a key lives and how it
reaches the machine, and the [lifecycle commands](lifecycle-commands.md) fix
when it is injected. This page adds one thing none of them own: the path from
"I have an Anthropic key" to "LiteLLM answers for a Claude model".

## The problem

Today that path does not exist.

The default scaffold declares one provider key by name:
`[zones.services]` carries `secrets = ["anthropic-api-key"]`, and because a
declared entry becomes a catalogue entry with source `user`, `lclaw up`
refuses to start until it is set. That refusal is generic machinery, not a
special case: nothing in the Go code mentions Anthropic. But the name is a
literal in a template, and a second provider means hand-editing the topology.

The larger problem is the other end. The LiteLLM Pod file ships
`model_list: []`, and nothing in it references `anthropic-api-key`. So the
key is read from the keychain, injected into the machine's secret store, and
then ignored: no environment variable carries it, no model uses it, and
LiteLLM serves nothing. The Pod file's own comment admits as much, promising
that "provider API keys arrive the same way once the secrets implementation
lands". They did not. This design is the half that is missing, and
de-bespoking `up` falls out of it.

## Goals and constraints

The CLI's three constraints, extensible, reliable and light, and the secrets
design's rules apply unchanged. Four more are specific to providers.

- **A provider is configuration, not state.** `lclaw.toml` plus the keychain
  must reproduce the running system. Nothing about a provider may exist only
  inside a container.
- **A key is never written to the host's disk, and never to a ConfigMap.**
  The keychain and the machine's secret store are the only two places a
  provider key is at rest.
- **Adding a provider edits no manifest.** If the second provider requires a
  hand-edit of `workloads/litellm/pod.yaml`, the command has not earned its
  place.
- **The user keeps their `config.yaml`.** LiteLLM's settings are the user's
  to edit. lclaw contributes the model list and nothing else, and a model the
  user adds by hand survives.

### Facts that shaped the design

Verified on 2026-09-22 against Podman 6.1.2 on macOS 27.0 and the LiteLLM
source at the tag this project pins, `v1.83.14-stable`.

- LiteLLM's `include` directive is resolved in `ProxyConfig._process_includes`
  with `os.path.join(base_dir, include_file)`. Python's `os.path.join`
  discards `base_dir` when the entry is absolute, so **an absolute include
  path is honoured** and no assumption about the proxy's working directory is
  needed.
- Merging an included file is `config[key].extend(value)` when the key is
  already a list in the base config, and assignment otherwise. So an included
  `model_list` is **appended to**, not substituted for, the one in
  `config.yaml`. A user's hand-written models and lclaw's rendered ones
  coexist.
- A named include that does not exist raises `FileNotFoundError` and the
  proxy does not start. The included file must therefore always be present,
  even when no provider is declared.
- `litellm_params.api_key` accepts either a literal value or the indirection
  `os.environ/NAME`, which is `os.getenv("NAME")`.
- A `model_name` of `<provider>/*` with a matching `litellm_params.model`
  routes every model that provider offers, so a model list need not be
  curated by hand.
- With `store_model_in_db` off, which is the default and what the scaffold
  uses, LiteLLM never reads its database for configuration and `config.yaml`
  is fully authoritative. With it on, models written through the admin API
  are stored in `LiteLLM_ProxyModelTable` and served **alongside** the YAML
  ones: a database model with the same name does not replace the file's
  entry, it becomes a second deployment that gets load balanced with it.
- `podman kube play` accepts several files and multi-document YAML, and
  supports the `ConfigMap` and `Secret` kinds, but its `--configmap` flag
  does not work through the remote client, which is why the deployment model
  puts ConfigMaps inside the Pod file.
- `podman secret create --replace <name> -` reads the value from standard
  input. lclaw already sends a Kubernetes `Secret` document as that value,
  with the single key `value`, because that is the shape `kube play`
  consumes.

## Scope

This design delivers `lclaw provider add`, `list`, `describe`, `update` and
`delete`; the `[providers.<name>]` table in `lclaw.toml` and its validation;
a TOML writer in the adapter layer; the rendering of LiteLLM's model list; a
second secret shape; the static edits to the LiteLLM Pod file that receive
it; and the reference and how-to pages listed at the end.

It does not deliver per-provider budgets, rate limits or fallbacks. Those are
LiteLLM router policy, they belong to whoever owns the agent's virtual key,
and each is one more field on a table that already knows how to grow. It does
not deliver provider health checks; `lclaw status` reports pods, and whether
Anthropic is reachable from the services zone is the network design's
question.

## Providers in `lclaw.toml`

A provider is a table. The default scaffold ships one, commented out.

```toml
[providers.anthropic]
kind = "anthropic"
models = ["*"]

[providers.openrouter]
kind = "openai"
api-base = "https://openrouter.ai/api/v1"
models = ["moonshotai/kimi-k2", "qwen/qwen3-coder"]
```

| Key        | Required | Meaning                                                                 |
| ---------- | -------- | ----------------------------------------------------------------------- |
| `kind`     | yes      | LiteLLM's provider prefix: `anthropic`, `openai`, `gemini`, `groq`, …  |
| `models`   | no       | Model ids to serve. Defaults to `["*"]`, the provider's wildcard route. |
| `api-base` | no       | Endpoint override, for an OpenAI-compatible provider.                   |
| `secret`   | no       | Keychain item holding the key. Defaults to `<name>-api-key`.            |

The table name is the provider's local name and is validated as a lowercase
DNS label by the same `domain.ValidateName` the secrets design uses, because
it is also the stem of a keychain item and a Podman secret name. That caps it
at 55 characters, eight short of the 63-character limit, so the derived
`-api-key` suffix still fits.

`kind` and each entry in `models` are validated against a conservative
character set rather than escaped, because the values are interpolated into
YAML and a validator is easier to be sure of than a quoting rule.

### Providers are secrets, derived

A provider does not need its key listed under `[zones.services].secrets`.
Declaring the table is the declaration: the domain turns every provider into
a `SecretSpec` with source `user`, zone `services` and `Rotatable: true`, and
merges it into the catalogue beside the built-in entries and the hand-declared
ones.

Everything downstream then works with no new code. `doctor` reports the key as
set or unset. `lclaw up` refuses before it boots the machine when it is
missing, with the hint it already prints. `lclaw secrets list` shows it.
`PurgeSecrets` removes it on the way down. The provider commands are a
friendlier way to write a catalogue entry, not a second mechanism beside it.

A provider whose derived secret name collides with a built-in entry or with a
name declared under `[zones.services].secrets` is a validation finding, in the
shape `NewCatalogue` already produces, reported by `doctor`, by `up` and by
every `provider` command.

The scaffold's `secrets = ["anthropic-api-key"]` line goes away, replaced by
the commented-out `[providers.anthropic]` table above. `lclaw secrets` keeps
working for every key that is not a provider's.

## How a provider reaches LiteLLM

This is the part with a decision in it.

LiteLLM needs two things: a model list, and a key per provider. The model list
is configuration and belongs in a file. The key is a secret and belongs in the
secret store. The obvious arrangement gives each its natural home, renders the
model list into a ConfigMap and writes `api_key: os.environ/ANTHROPIC_API_KEY`
against an environment variable fed by a `secretKeyRef`.

It fails the third constraint. An environment variable per provider is an
`env:` entry per provider in `workloads/litellm/pod.yaml`, which means adding
a provider means editing a manifest, which is the thing the command exists to
stop.

So the model list and the keys travel together, as one secret.

`lclaw up` renders the whole model list, keys included, into a YAML document
and injects it into the machine's secret store under the name
`litellm-providers`, with the key `models.yaml`. The LiteLLM Pod file mounts
that secret as a volume at `/etc/litellm-providers`, which gives the container
the file `/etc/litellm-providers/models.yaml`. The `config.yaml` ConfigMap
gains one static line:

```yaml
    include:
      - /etc/litellm-providers/models.yaml
```

and the rendered document is, for the two providers above:

```yaml
# Generated by lclaw. Every edit is replaced on the next `lclaw up`.
model_list:
  - model_name: anthropic/*
    litellm_params:
      model: anthropic/*
      api_key: sk-ant-…
  - model_name: moonshotai/kimi-k2
    litellm_params:
      model: openai/moonshotai/kimi-k2
      api_base: https://openrouter.ai/api/v1
      api_key: sk-or-…
  - model_name: qwen/qwen3-coder
    litellm_params:
      model: openai/qwen/qwen3-coder
      api_base: https://openrouter.ai/api/v1
      api_key: sk-or-…
```

What this buys, in order of how much it matters:

- **No manifest edit, ever.** The volume, the mount and the `include` line are
  written once into the scaffold and never change. The tenth provider costs
  the same as the first.
- **No environment variable and no `os.environ/` indirection.** One less place
  a key can be misspelled, and one less place it shows up: an environment
  variable is visible to anything that can read `/proc` in that container.
- **The keys never touch the host's disk or a ConfigMap.** The document is
  rendered in memory during `up` and travels to `podman secret create` on
  standard input, on the runner's sensitive path, exactly as every other
  secret does. Podman stores ConfigMaps in the clear; a Secret is the right
  primitive for a file that contains keys, and lclaw already knows how to
  write one.
- **Teardown is already written.** `PurgeSecrets` removes every secret
  carrying the LocalClaw label and the volume of the same name, which is
  precisely this.
- **The user's `config.yaml` stays theirs.** Because LiteLLM extends a list
  rather than replacing it, a model written by hand into `config.yaml`
  survives, and `litellm_settings` is untouched.

The costs, stated plainly:

- **A second secret shape.** Every secret lclaw injects today is a Kubernetes
  Secret with the single key `value`. This one has the key `models.yaml`. The
  helper that builds that document takes the key name as a parameter instead
  of hardcoding it; the [scaffold reference](../reference/scaffold.md) stops
  being able to say "the single key `value`".
- **A secret that is not in the keychain.** `litellm-providers` is rendered
  from the topology and the other secrets, so it exists only in the machine's
  store. It is deliberately **not** a catalogue entry: `lclaw secrets list`
  would show something you cannot set, and `lclaw secrets get` would print
  every provider key at once. It is injected by its own step, immediately
  after `InjectSecrets` for the services zone.
- **A restart applies a change.** The secret is read at proxy start, so
  `lclaw provider add` ends by telling you to run `lclaw up`, which replays
  the pod. That is the same answer the lifecycle design already gives for
  every edit, and `up` twice is how this project applies one.
- **The file must always exist.** A missing include stops the proxy, so `up`
  injects the secret unconditionally, with `model_list: []` when no provider
  is declared.

## The commands

All five honour the global `--output`, `--verbose` and `--dir` flags, and
report through the renderers `secrets` already uses: the catalogue-findings
report for a bad topology, and the store outcome for a change.

### `lclaw provider add NAME --kind KIND [flags]`

1. Load and validate `lclaw.toml`. Findings stop the command with exit 1.
2. Refuse if `[providers.NAME]` exists, with exit 2 and the hint to use
   `lclaw provider update`.
3. Read the key: `--from-file PATH`, otherwise the prompt when standard input
   is a terminal, otherwise standard input. This is `readValue`, unchanged.
4. Write `[providers.NAME]` into `lclaw.toml`.
5. Store the key through the existing secrets use case, so it lands in the
   keychain and, when the machine is up, in its store.
6. Report, and say that `lclaw up` applies it.

`--kind` is required. `--api-base URL`, `--model ID` (repeatable) and
`--secret NAME` set the optional keys; with no `--model`, `models` is omitted
and the wildcard applies.

Steps 4 and 5 are two writes that can half-succeed. The order is deliberate:
the file first, because a table with no key is a state `doctor` already
reports and `lclaw secrets set` already fixes, whereas a key with no table is
an orphan nothing mentions.

### `lclaw provider list`

Every provider with its kind, its models, its secret's name and whether that
secret is set. One row per provider, the column layout `secrets list` uses.

### `lclaw provider describe NAME`

The table's fields, the derived secret name with its source, timestamps and
store state, and the `model_list` entries the provider renders, with the key
shown as `<set>` or `<unset>` and never as a value.

### `lclaw provider update NAME [flags]`

Replaces only what the flags name. `--kind`, `--api-base`, `--model` (which
replaces the list rather than appending, because appending has no matching
removal), `--secret`. `--rotate-key` reads a new value the way `add` does.
With no flag at all it is a usage error, exit 2: an update that changes
nothing is a mistake, not a no-op.

### `lclaw provider delete NAME`

Removes the table, then deletes the secret through the existing secrets use
case, which removes it from the keychain and from the machine. `--keep-key`
leaves the keychain item alone, for a provider you expect to add back.

## Where it sits in the layering

- **Domain** gains `provider.go`: the `Provider` entity, its validation, its
  derived secret name and the conversion of a provider set into
  `SecretSpec`s, which `NewCatalogue` merges. And `litellm.go`: `RenderModels`,
  a pure function from providers and their resolved key values to the bytes of
  the included document. Pure functions over the standard library, so the
  rendering is a table test with golden output.
- **Application** gains a `Providers` use case with `Add`, `List`, `Describe`,
  `Update` and `Delete`, composing the existing `Secrets` use case rather than
  reaching past it to the keychain. It declares one new port,
  `TopologyWriter`, with a single method that writes a provider table back to
  `lclaw.toml` and one that removes it. `Up` gains a step that renders and
  injects `litellm-providers` after the services zone's secrets.
- **Adapters:** `adapters/toml` gains the writer. `adapters/podman`'s secret
  helper takes the Kubernetes key name as a parameter.
- **Presentation** gains `provider.go`, reusing `readValue`, `renderOutcome`
  and `reportFindings` unchanged.
- **Composition root** wires the TOML writer into the use case.

No new module enters the graph. The rendered YAML is a fixed shape of
validated strings, so `text/template` in the standard library writes it and
`gopkg.in/yaml.v3` stays out of `go.sum`. That is a deliberate trade: the
safety a YAML marshaller would give is bought instead with input validation,
which is the constraint the CLI architecture set.

The dependency rule holds without new edges.

## Error handling

The CLI design's rules apply unchanged. Four are specific to providers.

- **A malformed `[providers.*]` table is a finding, not an error.** All of
  them are reported together, through the path `NewCatalogue`'s findings
  already take, so a topology with three mistakes takes one run to fix.
- **A write to `lclaw.toml` is atomic.** The writer renders the whole file to
  a temporary file in the same directory and renames it, so an interrupted
  `provider add` leaves the original.
- **The machine being down is a warning, not a failure.** `provider add` with
  no machine stores the key in the keychain, reports the machine's store as
  skipped, and exits 0. This is the `StoreOutcome` behaviour `secrets set`
  already has.
- **A provider whose key is unset is a failed check, not a refusal to write.**
  You may declare a provider before you have its key; `doctor` and `up` say
  so until you set it.

Exit codes follow the [CLI reference](../reference/cli.md): 0 when no check
failed, 1 when one did, 2 for a usage error such as `add` on an existing name,
`update` with no flags, or an unknown provider name.

## Testing

- **Domain:** tables for name and kind validation; for the derived secret name
  and its length limit; for catalogue merging, including a provider colliding
  with a built-in entry and with a declared secret; and golden output from
  `RenderModels` for no providers, a wildcard provider, an explicit model
  list, an `api-base`, and several providers together.
- **Application:** the five operations against the existing in-memory fakes.
  Cases: `add` writing table, key and nothing else; `add` on an existing name
  refusing before any write; `add` with the machine down warning and exiting
  0; `update` with no flags refusing; `update --model` replacing rather than
  appending; `delete` removing table and key; `delete --keep-key`; a failed
  TOML write leaving the keychain untouched; and `Up` injecting
  `litellm-providers` with `model_list: []` when no provider is declared.
- **TOML adapter:** round-trip tests proving that writing a provider table
  leaves every other table, and every comment, byte-identical.
- **Podman adapter:** argv and stdin assertions for a secret created with a
  key other than `value`.
- **Presentation:** golden text and JSON for all five commands, and proof that
  no rendering path prints a key.
- **Integration on Linux CI:** the scaffold check gains a provider. Render a
  config for a provider with an `api-base` pointing at a stub endpoint and a
  throwaway key, play the LiteLLM pod with the secret mounted, and assert that
  `GET /v1/models` with the master key lists the declared model. That pins the
  include path, the mount, the secret shape and the rendering in one test,
  without a real provider credential.
- **On a Mac, by hand:** the implementation pull request records
  `lclaw provider add`, `lclaw up`, a completion through the proxy, and
  `lclaw provider delete`, with output.

### Assumptions to verify during implementation

- `podman kube play` mounts a Podman secret through a pod volume with a
  `secret` source, not only through `secretKeyRef`. Every secret in the
  scaffold today is consumed as an environment variable; this design is the
  first to mount one as a file.
- A Kubernetes `Secret` document with a key other than `value` survives the
  round trip through `podman secret create` and back out through `kube play`,
  and the file appears at `<mountPath>/<key>`.
- Replaying the pod with `kube play --replace` picks up the new content of a
  secret that changed, rather than a cached copy.
- The absolute include path and the list-extending merge behave at runtime as
  the pinned source reads. Both were read from `v1.83.14-stable`, not run.
- A wildcard route reports something sensible from `GET /v1/models`, which
  cannot enumerate a wildcard.
- The rendered document stays well under any size limit Podman puts on a
  secret. A hundred providers is a few kilobytes, but the limit is unmeasured.

## Alternatives considered

**LiteLLM's admin API as the source of truth.** `POST /model/new` through the
same `podman exec` seam the key minter already uses, with
`store_model_in_db` on. It is live, needs no restart, and reuses an adapter
that exists. It was rejected on the first constraint: the providers would then
live in Postgres inside the machine, so `lclaw down --destroy` loses them,
nothing is reviewable as code, and restoring a laptop means re-entering every
key by hand. It also makes `litellm-salt-key` load-bearing in a way the
secrets design already flags as uncomfortable, since that key encrypts the
stored credentials and is the one entry that may not be rotated. And the two
sources do not compose: a database model with the same name as a YAML one is
not an override but an additional deployment that gets load balanced with it,
so a user who reached for the admin UI once would silently double their
provider.

**Generate the whole `litellm-config` ConfigMap.** Simplest possible
rendering, and no `include` to reason about. It takes `litellm_settings` away
from the user, or forces a second representation of it in `lclaw.toml`. The
`include` line costs one static line in a file the user already owns.

**An aggregate Podman secret whose keys are environment variable names,
consumed by a single static `envFrom: secretRef:`.** This was the first shape
considered, and it does satisfy "no manifest edit". But it keeps the model
list in a ConfigMap and the keys in the environment, which is two mechanisms
where the chosen design has one, and it still needs a secret shape lclaw does
not create today. Worth revisiting only if mounting a secret as a file turns
out not to work through `kube play`.

**Write the rendered document to the scaffold directory and play it as a
second file.** `kube play` takes several files, so a generated
`workloads/litellm/providers.yaml` could carry a ConfigMap alongside the Pod
file. It was rejected the moment the keys moved into the document: that file
would be plaintext provider keys sitting in a directory users put under
version control. Keeping the rendering in memory removes the temptation
entirely.

**A `[providers]` table that is just a list of names, with the keys still
declared as secrets.** Less to learn, and it de-bespokes `up`. It also does
not render a model list, which is the half that is actually missing.

**Per-provider commands under `lclaw secrets`.** A provider is a secret plus
three fields, so `lclaw secrets set anthropic-api-key --kind anthropic` is
conceivable. It overloads a command whose whole job is that it knows nothing
about what a secret means.

## What lands with the code

- [`docs/reference/cli.md`](../reference/cli.md): the five commands, their
  arguments, flags, exit codes and JSON shape.
- [`docs/reference/scaffold.md`](../reference/scaffold.md): the
  `[providers.<name>]` table, and the correction to "every injected secret has
  the single key `value`".
- [`docs/reference/secrets.md`](../reference/secrets.md): derived provider
  entries in the catalogue, and `litellm-providers` as a store-only secret.
- `docs/how-to/add-a-model-provider.md`: adding a provider, rotating its key,
  and pointing the agent at a model.
- [`docs/how-to/manage-secrets.md`](../how-to/manage-secrets.md): its
  Anthropic example becomes a `provider` example.
- `CHANGELOG.md`: the command group under Unreleased, and the scaffold change
  as a breaking one for anyone holding a `schema = 2` file with
  `secrets = ["anthropic-api-key"]` in it.
- This page moves from `draft` to `stable` when the implementation matches it,
  with a "Refinements made during implementation" section recording where it
  did not.
