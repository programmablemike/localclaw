---
title: "Manage secrets"
description: "Create the keychain, set a provider API key, rotate the LiteLLM master key, and read a value to log into the LiteLLM dashboard."
diataxis: how-to
status: stable
last_reviewed: 2026-09-21
tags: [secrets, keychain, litellm, lclaw]
related:
  - ../reference/secrets.md
  - ../reference/cli.md
  - ../explanation/secrets-management.md
---

# Manage secrets

When you finish, LocalClaw's keychain exists, your provider key is stored
in it, and you know how to rotate a generated secret and read one back.

## Prerequisites

- A `lclaw` binary, built as in
  [Set up a development environment](set-up-a-development-environment.md).
- macOS. The keychain is managed through `/usr/bin/security`.
- A provider API key issued to you, for example an Anthropic key.

## Steps

### 1. Create the scaffold and the keychain

```bash
lclaw init
```

`security` asks for a password for the new keychain and asks again to
confirm. Choose one you can type again: macOS asks for it when the keychain
has locked, which happens on sleep and after fifteen idle minutes.

The command writes the scaffold into `~/.config/lclaw`, then reports the
keychain on a line of its own, with the path expanded:

```text
created  /Users/me/Library/Keychains/lclaw.keychain-db
```

Running `lclaw init` again reports every file and the keychain as
`skipped`; `--force` rewrites scaffold files but never touches the
keychain.

In a script, pipe the password twice:

```bash
printf '%s\n%s\n' "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PASSWORD" | lclaw init
```

Do not run `lclaw init` with standard input redirected from `/dev/null` or
an empty file: `security` then creates the keychain with an empty password
and exits 0.

### 2. Declare the key in `lclaw.toml`

The default file already declares `anthropic-api-key` on the `services`
machine. For another provider, add its name to the same list:

```toml
[machines.services]
secrets = ["anthropic-api-key", "openai-api-key"]
```

Names are lowercase DNS labels. See the
[secrets reference](../reference/secrets.md) for the rules.

### 3. Set the key

At a terminal, `set` prompts without echo:

```bash
lclaw secrets set anthropic-api-key
```

From a file or a pipe, one trailing newline is removed:

```bash
lclaw secrets set anthropic-api-key --from-file ~/Downloads/anthropic.key
```

```bash
pbpaste | lclaw secrets set anthropic-api-key
```

If the machine that uses the key is running, the command also stores the
key in that machine's Podman secret store and prints `services  stored`. A
machine that is stopped or not created prints `services  stopped` and is
picked up by the next `lclaw up`.

### 4. Rotate the LiteLLM master key

`update --generate` makes a new value with the built-in generator and
restores the entry's default source:

```bash
lclaw secrets update litellm-master-key --generate
```

When the `services` machine is running, the command ends with:

```text
pods pick the new value up on the next `lclaw up`
```

`litellm-salt-key` refuses `--generate` and exits 2 with `secret cannot be
generated`, because LiteLLM's stored credentials are encrypted with it; see
[Secrets management](../explanation/secrets-management.md).

To replace a generated secret with one of your own, omit `--generate` and
supply the value as in step 3. The entry's source becomes `user` until you
run `update --generate` again.

### 5. Log into the LiteLLM dashboard

The master key is also the admin password. Print it to paste into the
login form:

```bash
lclaw secrets get litellm-master-key; echo
```

`get` prints the value with no trailing newline, so the `echo` keeps your
prompt on its own line. To copy it without showing it:

```bash
lclaw secrets get litellm-master-key | pbcopy
```

## Verify

```bash
lclaw secrets list
lclaw doctor
```

`list` shows `anthropic-api-key` with source `user` and state `set`, and
every generated entry as either `set` or `unset (generated on next up)`.
`doctor` reports a `PASS` line for the `keychain` check, whose summary is
the keychain's path, and one `PASS` line summarised `set` for each secret
declared in `lclaw.toml`, named after the secret.
