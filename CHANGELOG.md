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

### Changed

- `lclaw doctor` requires Podman 5.8.0 or newer, because
  `podman machine init --playbook` arrived in 5.8.
- `github.com/BurntSushi/toml` v1.6.0 is the second runtime dependency,
  vendored, for reading `lclaw.toml`.
