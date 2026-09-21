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
