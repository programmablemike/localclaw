---
title: "Secrets reference"
description: "The secret catalogue, the keychain layout, the lclaw.toml keys, the secrets command flags, output shapes, store states and exit codes."
diataxis: reference
status: stable
last_reviewed: 2026-09-22
tags: [secrets, keychain, podman, cli, lclaw, json, exit-codes]
related:
  - cli.md
  - scaffold.md
  - ../how-to/manage-secrets.md
  - ../explanation/secrets-management.md
---

# Secrets reference

`lclaw secrets` manages the items in LocalClaw's keychain and, when the
machine is running, its Podman secret store. This page describes the
catalogue, the keychain, the configuration keys, the commands and their
output. The reasoning is in
[Secrets management](../explanation/secrets-management.md).

## Catalogue

Every secret lclaw knows about is either built in or declared in
`lclaw.toml`. The catalogue is the union, sorted by name.

### Built-in entries

| Name                     | Default source | Zones      | Generated value                       | Rotatable |
| ------------------------ | -------------- | ---------- | ------------------------------------- | --------- |
| `litellm-master-key`     | `generated`    | `services` | `sk-` plus 32 random bytes, base64url | yes       |
| `litellm-salt-key`       | `generated`    | `services` | 32 random bytes, hexadecimal          | no        |
| `litellm-db-password`    | `generated`    | `services` | 32 random bytes, base64url            | yes       |
| `openclaw-gateway-token` | `generated`    | `agent`    | 32 random bytes, base64url            | yes       |
| `openclaw-litellm-key`   | `minted`       | `agent`    | whatever LiteLLM returns              | yes       |

base64url is the RFC 4648 URL alphabet without padding, 43 characters for
32 bytes. Minting needs LiteLLM running: `lclaw up` mints the key after the
`services` zone is ready, and `update --generate` on a minted entry asks
the running LiteLLM for a new one. When LiteLLM is not running, `update
--generate` exits 2 with `minting needs the services zone up; run `lclaw
up services` first`, and an `up` of the `agent` zone alone reports a
failed check named after the secret, summarised `not minted: <that text>`,
with the same hint. The minting mechanism is described in
[Lifecycle commands](../explanation/lifecycle-commands.md#minting-the-agents-key).

### Sources

| Source      | Meaning                                                                                    |
| ----------- | ------------------------------------------------------------------------------------------ |
| `generated` | lclaw makes the value with `crypto/rand` the first time `up` needs it                      |
| `minted`    | lclaw obtains the value from a running service                                              |
| `user`      | Supplied with `lclaw secrets set`, or a built-in entry replaced with `lclaw secrets update` |

Any built-in entry becomes `user` the moment it is set or updated by hand.
`lclaw secrets update NAME --generate` returns it to its default source.
The source is stored on the keychain item; an item whose comment holds none
of the three strings reads back as `user`.

### Declared entries

Declared entries have source `user`. They are listed per zone in
`lclaw.toml` (see below). A name listed under two zones is one entry
consumed by both.

### Names and values

| Rule  | Value                                                                                   |
| ----- | --------------------------------------------------------------------------------------- |
| Name  | Matches `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, at most 63 characters (a lowercase DNS label) |
| Value | 1 to 1024 bytes, any content                                                             |

The same name is the keychain account, the Podman secret name and the
Kubernetes Secret name.

### Validation findings

Building the catalogue reports every problem at once. A `secrets` command
renders them in the shape `doctor` uses, one `FAIL` line per finding, and
exits 1:

| Finding                        | Check name                |
| ------------------------------ | ------------------------- |
| Malformed name                 | `zones.<role>.secrets` |
| Name collides with a built-in  | `zones.<role>.secrets` |
| Name listed twice under a role | `zones.<role>.secrets` |

```text
FAIL  zones.services.secrets  "Bad" is not a valid secret name; use a lowercase DNS label of at most 63 characters

0 passed, 0 warnings, 1 failed
```

The whole message is the summary; there is no hint line. A zone name
that is not a role is not one of these findings: `doctor` reports it once
as a `topology` finding named `zones.<name>`, and the `secrets` commands
ignore that table's `secrets` list.

## Keychain

| Property      | Value                                                                                                  |
| ------------- | ------------------------------------------------------------------------------------------------------ |
| Default path  | `~/Library/Keychains/lclaw.keychain-db`                                                                |
| Created by    | `lclaw init`, through `security create-keychain` with the terminal attached                            |
| Lock settings | Locks when the system sleeps and after 900 idle seconds (`security set-keychain-settings -l -u -t 900`) |
| Search list   | Never added; lclaw names the file on every call                                                        |
| Item class    | Generic password                                                                                       |
| Item service  | `lclaw`                                                                                                |
| Item account  | The secret name                                                                                        |
| Item kind     | `LocalClaw secret`                                                                                     |
| Item comment  | The source: `generated`, `minted` or `user`                                                            |
| Timestamps    | Creation and modification times recorded by the keychain, shown by `describe`                          |

A path starting with `~/` is expanded against the home directory when
`lclaw.toml` is loaded; any other path must be absolute. Every message that
names the keychain names the expanded path.

When stdin is not a terminal, `security create-keychain` reads the new
password and its confirmation as two lines from stdin. An empty stdin
creates a keychain with an empty password.

Every command that needs the keychain fails with exit 1 and the hint to
run `lclaw init` when the file is missing. `doctor` reports it as a failed
`keychain` check, unless the topology could not be loaded at all, in which
case that check is a warning summarised `skipped because the topology
check failed`.

When the keychain is locked in a session without a display, lclaw runs
`security unlock-keychain` with the terminal attached and retries the
operation once.

### `security` exit codes

| Exit | Meaning                 | lclaw error                                   |
| ---- | ----------------------- | --------------------------------------------- |
| 44   | Item not found          | `secret not found`                            |
| 45   | Duplicate item          | `secret already exists`                       |
| 36   | Interaction not allowed | `keychain is locked` (after one unlock retry) |
| 50   | No such keychain        | `keychain not found`                          |
| 51   | Wrong password          | `wrong keychain password`                     |

## Configuration

The keys that declare secrets, `[keychain] path` and each zone's
`secrets` list, are part of the topology file and are documented in the
[scaffold reference](scaffold.md#lclawtoml). A declared name must satisfy
the name rule above, must not collide with a built-in entry, and must not
appear twice under one zone; `lclaw doctor` reports each of these as a
`topology` check whose summary is `zones.<role>.secrets: <message>`.

## Commands

Every subcommand honours the global `--dir` and `--output` flags described
in the [command reference](cli.md). None prints a value except `get`.
Commands that change a secret write the keychain and then, when the
machine is running, its Podman secret store; they never apply pods.

| Command                                               | Effect                                                                                                   |
| ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `secrets list`                                        | One row per catalogue entry: name, source, zones, state. The store is not inspected                      |
| `secrets describe NAME`                               | Name, source, zones, state, timestamps, and the store's state                                            |
| `secrets set NAME [--from-file PATH]`                 | Create the secret with source `user`                                                                     |
| `secrets update NAME [--from-file PATH] [--generate]` | Replace an existing secret; `--generate` re-runs the generator or minter and restores the default source |
| `secrets delete NAME`                                 | Remove the item from the keychain and from the machine's store when it is running                        |
| `secrets get NAME`                                    | Write the value to standard output, raw, with no trailing newline, regardless of `--output`              |

`lclaw secrets` with no subcommand prints the subcommand help.

### Value input

For `set` and `update` without `--generate`, the value comes from the first
of:

1. `--from-file PATH`: the file's content.
2. A prompt on standard error that does not echo, when standard input is a
   terminal.
3. Standard input.

Input from a file or standard input has exactly one trailing newline (`\n`
or `\r\n`) removed. A value read from the prompt is taken as typed. When
there is no `--from-file`, standard input is not a terminal and carries
nothing, the command exits 2: as a usage error where the process has no
standard input at all, otherwise as `invalid secret value: empty`.

### Refusals

| Situation                                               | Exit | Message                                                                       |
| ------------------------------------------------------- | ---- | ----------------------------------------------------------------------------- |
| `set` on a secret that already exists                   | 1    | `secret already exists`, hint to use `update`                                 |
| `update`, `delete` or `get` on a secret that is not set | 1    | `secret not found`                                                            |
| Name not in the catalogue                               | 2    | `unknown secret`, hint to declare it in `lclaw.toml`                          |
| Empty value or over 1024 bytes                          | 2    | `invalid secret value`                                                        |
| `--generate` on a declared entry                        | 2    | `secret cannot be generated: "NAME" is user-supplied and has nothing to generate` |
| `--generate` on an entry that may not be rotated        | 2    | `secret cannot be generated: "NAME" may not be rotated`                       |
| `--generate` on a minted entry while LiteLLM is not running | 2 | `minting needs the services zone up; run `lclaw up services` first`          |
| `--generate` combined with `--from-file`                | 2    | usage error                                                                   |
| Keychain file missing                                   | 1    | `keychain not found`, hint to run `lclaw init`                                |
| The machine's store failed                              | 1    | reported in the outcome; the keychain write stands                            |
| `lclaw.toml` has catalogue findings                     | 1    | one `FAIL` line per finding                                                   |

Checks happen in a fixed order. Flag combinations are rejected first, then
the value is collected from the file, the prompt or standard input, then
the catalogue is built, the name is looked up, `--generate` is accepted or
refused, the value is validated, the keychain file is checked, and only
then is the item read or written. So `update --generate` on a minted entry
that is not set reports `secret not found` before it reaches the minter.

`delete` on an entry that may not be rotated prints a warning on standard
error and proceeds.

## Text output

### `list`

Columns are padded to their widest cell, two spaces apart. State is `set`,
or `unset` followed by what the next `up` does.

```text
NAME                    SOURCE     ZONES     STATE
anthropic-api-key       user       services  set
litellm-db-password     generated  services  unset (generated on next up)
litellm-master-key      generated  services  set
litellm-salt-key        generated  services  unset (generated on next up)
openclaw-gateway-token  generated  agent     unset (generated on next up)
openclaw-litellm-key    minted     agent     unset (minted on next up)
```

An unset declared entry shows ``unset (run `lclaw secrets set NAME`)``. A
secret consumed by more than one zone shows them comma-separated, without
a space: `services,agent`.

### `describe`

```text
name:      litellm-master-key
source:    generated
zones:     services
state:     set
created:   2026-09-21T07:03:45Z
modified:  2026-09-21T07:03:45Z
store:     stored
```

`created` and `modified` appear only when the secret is set.

### `set`, `update`, `delete`

```text
updated litellm-master-key
  store  stored
pods pick the new value up on the next `lclaw up`
```

The verb is `set`, `updated` or `deleted`, followed by one `store` line.
The closing line appears only after `updated`, and only when the store was
written. A `failed` store is followed by its error on the same line, after
a colon:

```text
set anthropic-api-key
  store  failed: podman: store secret anthropic-api-key: exit status 125
```

### Store states

| State     | Meaning                                                                                                            |
| --------- | ------------------------------------------------------------------------------------------------------------------ |
| `stored`  | The machine is running and its store holds the secret (`describe`), or the value was just stored (`set`, `update`) |
| `absent`  | The machine is running and its store does not hold the secret                                                      |
| `removed` | The secret was just removed from the store (`delete`)                                                              |
| `stopped` | The machine is not running or not created; nothing was done                                                        |
| `failed`  | The store operation failed; the error follows                                                                      |

## JSON output

One document per command, indented two spaces. Source, state and
store-state strings are lowercase and part of the interface. `state` is
`set` or `unset`, without the parenthetical the text output adds. The
`secrets` array and the `store` object are always present, never null.

### `list`

```json
{
  "secrets": [
    {
      "name": "anthropic-api-key",
      "source": "user",
      "zones": [
        "services"
      ],
      "state": "set"
    },
    {
      "name": "litellm-salt-key",
      "source": "generated",
      "zones": [
        "services"
      ],
      "state": "unset"
    }
  ]
}
```

### `describe`

`created` and `modified` are RFC 3339 in UTC and present only when set.

```json
{
  "name": "litellm-master-key",
  "source": "generated",
  "zones": [
    "services"
  ],
  "state": "set",
  "created": "2026-09-21T07:03:45Z",
  "modified": "2026-09-21T07:03:45Z",
  "store": {
    "state": "stored"
  }
}
```

### `set`, `update`, `delete`

`action` is `set`, `updated` or `deleted`. `error` appears on a `failed`
store and `warning` when `delete` removed an entry that may not be rotated.

```json
{
  "name": "litellm-master-key",
  "action": "updated",
  "store": {
    "state": "stored"
  }
}
```

## Podman secret shape

Each value is stored on the machine with

```text
podman --connection lclaw secret create --replace --label app.kubernetes.io/part-of=localclaw NAME -
```

and the following document on standard input, `data.value` being the value
in standard base64:

```json
{"apiVersion":"v1","kind":"Secret","metadata":{"name":"litellm-master-key"},"type":"Opaque","data":{"value":"c2stLi4u"}}
```

A pod file consumes the secret as an environment variable through
`secretKeyRef` with `key: value`, or as a file through a secret volume.
`kube play` turns a secret volume into a named volume with the secret's
name, so secret names and volume claim names share one namespace on the
machine. A full `lclaw down` lists secrets carrying the label above,
removes each one, and removes the named volume of the same name if it
exists.

## Exit codes

| Code  | Meaning                                                                                               |
| ----- | ------------------------------------------------------------------------------------------------------ |
| `0`   | Success                                                                                               |
| `1`   | Keychain missing, secret exists or not set, a store failed, catalogue findings, or an unexpected error |
| `2`   | Usage error, unknown secret, invalid value, `--generate` refused                                      |
| `130` | Interrupted                                                                                           |
