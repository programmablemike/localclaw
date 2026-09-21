# Changelog

All notable changes to this project are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
versions follow [Semantic Versioning](https://semver.org).

## [Unreleased]

### Added

- `lclaw doctor` reports whether Flox and Podman are installed at the
  required versions and whether the three LocalClaw machines exist, as text
  or JSON, with every problem reported in one run.
- `lclaw version` prints the version, commit and build details.
- The Go module skeleton with the layering described in
  `docs/explanation/cli-architecture.md`, an import-boundary test,
  `scripts/check.sh`, a Flox Nix-expression build (`flox build lclaw`) and
  CI on Ubuntu and macOS.
- `lclaw init` writes the default scaffold (`lclaw.toml`, three first-boot
  playbooks, and a digest-pinned Containerfile, a Pod file and a
  `.containerignore` for each of the seven workloads) into `~/.config/lclaw`,
  `--dir` or `LCLAW_DIR`, skipping files that exist unless `--force` is
  given, as text or JSON.
- `lclaw doctor` checks that the scaffold directory exists and that its
  `lclaw.toml` decodes and validates, reporting every finding.
- A Linux CI job builds every default Containerfile and plays then tears
  down every default Pod file against the runner's Podman.
- `lclaw secrets` manages LocalClaw's secrets: `list`, `describe`, `set`,
  `update`, `delete` and `get`. Each one writes or reads the LocalClaw
  keychain and, for every machine that uses the secret and is running,
  that machine's Podman secret store, as text or JSON. Values are read
  from `--from-file`, a no-echo prompt or standard input, and only `get`
  ever prints one.
- `lclaw init` creates the keychain named by `lclaw.toml`, locking it on
  sleep and after fifteen idle minutes. An existing keychain is skipped and
  is never overwritten, including under `--force`.
- `lclaw doctor` checks that the keychain file exists and that every secret
  declared in `lclaw.toml` is set, reading each item's attributes and never
  its value.
- Resolve, inject and purge use cases that generate or mint missing
  secrets, store them on a machine as Kubernetes-shaped Podman secrets and
  remove them again by label, ready for the lifecycle commands to consume.
- A Linux CI job proves the secret wire contract: the exact body `lclaw`
  sends is readable inside a pod both as an environment variable and as a
  file, and a secret volume is a named volume that can be removed.

### Changed

- `lclaw doctor` requires Podman 5.8.0 or newer, because
  `podman machine init --playbook` arrived in 5.8.
- `github.com/BurntSushi/toml` v1.6.0 is the second runtime dependency,
  vendored, for reading `lclaw.toml`.
- The exec runner takes a command value with an optional stdin reader and a
  `Sensitive` flag, so a value can travel on standard input and a
  sensitive command's output is never logged. A second method,
  `Interactive`, inherits the terminal for `security create-keychain` and
  `security unlock-keychain`.
- `golang.org/x/term` v0.46.0 is the third runtime dependency, vendored,
  for the no-echo prompt, and brings `golang.org/x/sys` v0.48.0 with it.
