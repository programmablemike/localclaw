# AGENTS.md

Guidance for coding agents (Claude Code, Codex, Copilot, Cursor, and any other
automated contributor) working in this repository. Humans are welcome to follow
it too. More specific instructions in a subdirectory override this file for
that subdirectory.

## Project overview

- **Name:** localclaw
- **Repository:** https://github.com/programmablemike/localclaw
- **Default branch:** `main`
- **Documentation:** `docs/`, organised with [Diátaxis](https://diataxis.fr).
  Agent index at `docs/llms.txt`.
- **Description:** Run OpenClaw agents in isolated Podman machines on macOS,
  with a Go CLI (`lclaw`) to manage them. See [`README.md`](README.md).
- **License:** MIT. See [`LICENSE`](LICENSE).

## Ground rules

These are non-negotiable. Do not rationalize your way around them.

1. **Never commit directly to `main`.** Every change is made on its own branch
   inside a git worktree.
2. **Every change reaches `main` through a pull request.** No local merges into
   `main`, no direct pushes.
3. **Releases follow [Semantic Versioning 2.0.0](https://semver.org).** Version
   tags are the only way a release is created.
4. **Documentation follows [Diátaxis](https://diataxis.fr).** Every new page
   goes in the matching `docs/` section, starts with the frontmatter preamble,
   and is registered in `docs/llms.txt`. See "Documentation" below.
5. **Never commit secrets.** No credentials, tokens, private keys, or `.env`
   files with real values. If you find one already committed, stop and tell the
   user.
6. **Do not merge, force-push to a shared branch, delete a tag, or publish a
   release unless the user has explicitly asked for that specific action in
   the current conversation.**

## Workflow: worktree, then pull request

### 1. Start in an isolated worktree

You already have permission to create a worktree. Do not ask; just do it.

First check whether you are already isolated. If
`git rev-parse --git-dir` and `git rev-parse --git-common-dir` resolve to
different paths, you are already in a linked worktree. Work there and do not
nest another one.

Otherwise:

- **Prefer your platform's native worktree tool** if one exists (for example an
  `EnterWorktree` tool, a `/worktree` command, or a `--worktree` flag). Native
  tools manage placement and cleanup for you.
- **Fall back to plain git** only when no native tool is available:

  ```bash
  git fetch origin
  git worktree add .worktrees/<branch-name> -b <branch-name> origin/main
  cd .worktrees/<branch-name>
  ```

Worktrees created by hand live under `.worktrees/` at the repository root.
That directory is listed in `.gitignore`; confirm with
`git check-ignore -q .worktrees/` (trailing slash included) before creating
anything there.

**Branch naming:** `<type>/<short-kebab-description>`, where `<type>` is one of
the Conventional Commit types below. Examples: `feat/config-loader`,
`fix/null-path-crash`, `docs/agents-md`, `chore/ci-cache`,
`release/v1.2.0`.

One branch, one worktree, one pull request. Do not reuse a branch for a second
unrelated change.

Before editing anything, run the project's setup and test commands (see
"Build, test, lint" below) so you know the baseline is green. If the baseline
is red, report it and ask before continuing.

### 2. Make the change

- Keep the change focused on the task. Do not fold in unrelated refactors,
  formatting sweeps, or dependency bumps.
- Commit in small, coherent steps using Conventional Commits (see below).
- Run tests and linters locally before pushing.
- Preserve unrelated changes already in the worktree; never `git checkout .`,
  `git reset --hard`, or `git clean` over work you did not create.

### 3. Open a pull request

```bash
git push -u origin <branch-name>
gh pr create --base main --title "<type>(<scope>): <summary>" --body-file <notes>
```

The PR title uses the same format as a Conventional Commit. The body must
cover:

- **What** changed.
- **Why** it changed (link the issue if there is one).
- **How it was tested** (commands run and their results, not "tests pass").
- **Breaking changes**, called out explicitly, or "None".

Then:

- Wait for CI. If a check fails, fix it on the same branch and push again.
- Address review comments with new commits. Do not rewrite history on a branch
  that has been reviewed unless the user asks; if asked, use
  `git push --force-with-lease`, never `--force`.
- **Do not merge the PR yourself.** A human merges, or the user explicitly
  tells you to merge in the conversation.

### 4. Clean up after merge

Run these from the main repository root, not from inside the worktree being
removed:

```bash
git worktree remove .worktrees/<branch-name>
git worktree prune
git branch -d <branch-name>
git fetch --prune
```

If a native worktree tool created the worktree, use its exit or cleanup tool
instead and leave the directory alone.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org):

```
<type>(<optional scope>): <imperative summary, lowercase, no trailing period>

<optional body: what and why, wrapped at 72 columns>

<optional footers, e.g. "BREAKING CHANGE: ..." or "Closes #12">
```

The commit type determines the semantic version bump at release time:

| Type                                                        | SemVer bump               |
| ----------------------------------------------------------- | ------------------------- |
| Any commit with a `BREAKING CHANGE:` footer or `!` after type | **MAJOR** (MINOR while `0.y.z`) |
| `feat`                                                      | **MINOR** (PATCH while `0.y.z`) |
| `fix`, `perf`                                               | **PATCH**                 |
| `docs`, `style`, `refactor`, `test`, `build`, `ci`, `chore` | none                      |

## Documentation

All documentation lives in `docs/` and follows the
[Diátaxis](https://diataxis.fr) framework. Read [`docs/README.md`](docs/README.md)
before writing a page and [`docs/llms.txt`](docs/llms.txt) to find existing
ones.

### Choose the section before you write

Answer two questions about the content:

1. Does it tell the reader **what to do** (action) or **what to know**
   (cognition)?
2. Does it serve someone **learning** the subject or someone **working** with
   it?

|               | Learning             | Working            |
| ------------- | -------------------- | ------------------ |
| **Action**    | `docs/tutorials/`    | `docs/how-to/`     |
| **Cognition** | `docs/explanation/`  | `docs/reference/`  |

A page that answers both ways is two pages. Split it. Never mix a tutorial
with reference tables, or a how-to guide with paragraphs of rationale.

### Every page must

- Start from the matching skeleton in `docs/_templates/`.
- Begin with the YAML frontmatter preamble defined in
  [`docs/reference/frontmatter-schema.md`](docs/reference/frontmatter-schema.md).
  Required fields: `title`, `description`, `diataxis`, `status`,
  `last_reviewed`.
- Carry a `diataxis` value that matches its directory.
- Be registered in its section's `README.md` and in `docs/llms.txt`, reusing
  the frontmatter `title` and `description` verbatim.
- Use a lowercase kebab-case filename ending in `.md`, and relative links.

The step-by-step procedure, including verification commands, is
[`docs/how-to/add-a-document.md`](docs/how-to/add-a-document.md).

### Where things go

- **This file** states rules for agents and stays short. A procedure longer
  than a few lines belongs in `docs/how-to/` and is linked from here.
- **`README.md`** at the repository root is the human landing page and links
  to `docs/`.
- **Code comments and docstrings** document code in place. They do not replace
  `docs/reference/`.
- **`assets/`** holds brand assets such as the project icon, as SVG source.

Documentation changes follow the same worktree-and-PR workflow with the `docs`
commit type. Update `last_reviewed` whenever you verify or change a page.

## Releases

Releases use **Semantic Versioning 2.0.0**: `MAJOR.MINOR.PATCH`, with optional
pre-release identifiers such as `1.2.0-rc.1`. Git tags carry a `v` prefix:
`v1.2.0`, `v2.0.0-beta.2`.

### Choosing the next version

Given the most recent tag, inspect what has landed since:

```bash
git fetch --tags
git log "$(git describe --tags --abbrev=0)"..origin/main --oneline
```

Then apply the rules:

- **MAJOR** when any change is backwards-incompatible.
- **MINOR** when functionality is added in a backwards-compatible way.
- **PATCH** when only backwards-compatible bug fixes shipped.
- **While the version is `0.y.z`** the public API is not yet stable: breaking
  changes bump MINOR and everything else bumps PATCH. Move to `1.0.0` only
  when the user decides the API is stable.
- **Pre-releases** (`-alpha.N`, `-beta.N`, `-rc.N`) are allowed for testing a
  release candidate. They sort before the final version and are never
  promoted by moving the tag; cut the final tag as a new release.
- Do not use build metadata (`+build.123`) in tags.

### Cutting a release

Only do this when the user asks for a release. Releases are cut from `main`
and follow the same worktree-and-PR rule as everything else.

1. **Release PR.** From a worktree on `release/vX.Y.Z`:
   - Update the project's version field (whichever the project adopts:
     `package.json`, `Cargo.toml`, `pyproject.toml`, a `VERSION` file, etc.).
   - Update `CHANGELOG.md` following [Keep a Changelog](https://keepachangelog.com):
     move items out of `Unreleased` into a dated `## [X.Y.Z] - YYYY-MM-DD`
     section grouped under Added / Changed / Deprecated / Removed / Fixed /
     Security.
   - Commit as `chore(release): vX.Y.Z` and open a PR titled the same.
2. **Tag after merge.** Once the release PR is merged, tag the merge commit on
   `main` with an annotated tag:

   ```bash
   git checkout main
   git pull --ff-only origin main
   git tag -a vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

3. **Publish the GitHub release** from the tag, using the changelog section as
   the notes:

   ```bash
   gh release create vX.Y.Z --title "vX.Y.Z" --notes-file <changelog-section>
   ```

   Pass `--prerelease` for any version with a pre-release identifier.

### Release rules

- Tags are created only on commits that are on `main`.
- A published tag is immutable. Never move, re-point, or delete one. If a
  release is broken, ship a new PATCH (or a new pre-release) instead.
- The version in the project metadata, the `CHANGELOG.md` heading, and the git
  tag must all agree.
- Do not skip versions or reuse numbers.

## Build, test, lint

### Environment

The toolchain is declared in `.flox/env/manifest.toml` and installed by
[Flox](https://flox.dev). Enter the environment before running anything else:

```bash
flox activate                  # interactive shell
flox activate -- go test ./... # a single command
```

Add tools with `flox install <package>` so the manifest and lockfile stay the
source of truth. Do not rely on a host-installed Go. `.flox/.gitignore` already
excludes the generated `run/`, `cache/` and `log/` directories; commit the
rest of `.flox/`.

### Commands

All from the repository root, inside `flox activate`.

| Task                                                        | Command                                                                          |
| ----------------------------------------------------------- | -------------------------------------------------------------------------------- |
| Run every check (format, vet, staticcheck, govulncheck, tests with the race detector, tidy and vendor drift) | `./scripts/check.sh`                     |
| Run the tests alone                                         | `go test ./...` (the check script adds the race detector, with cgo on Linux)     |
| Build and run a development binary                          | `go build -o bin/lclaw ./cmd/lclaw && bin/lclaw doctor`                          |
| Build the release package hermetically                      | `flox build lclaw` (binary at `result-lclaw/bin/lclaw`)                          |
| Validate the default scaffold as Podman input (Linux only; CI runs it) | `./scripts/scaffold-check.sh bin/lclaw` after `go build -o bin/lclaw ./cmd/lclaw` |
| Add or update a dependency                                  | `go get <module>@<version> && go mod tidy && go mod vendor`; commit `go.mod`, `go.sum` and `vendor/` |
| Regenerate CLI golden files after an intended output change | `go test ./internal/cli/ -update`, then review the diff in `internal/cli/testdata/` |

CI runs `./scripts/check.sh` and `flox build lclaw` on Ubuntu and macOS, and `./scripts/scaffold-check.sh` on Ubuntu.
Dependencies follow the policy in
[`docs/explanation/cli-architecture.md`](docs/explanation/cli-architecture.md):
standard library first, and measure the full module graph before adding
anything.

## Bootstrapping note

This repository currently has no commits. A worktree cannot be created until
`main` has at least one commit, so the **initial commit** (this file,
`.gitignore`, the `docs/` tree, and any other scaffolding) is the single
permitted exception to
the "never commit directly to `main`" rule. Every commit after that one goes
through a worktree and a pull request. Once `main` exists on GitHub, enable
branch protection on it (require a PR, require status checks) so the rule is
enforced by the platform and not just by this document.

## Things agents must never do

- Commit or push directly to `main` (after the bootstrap commit).
- Merge a pull request without explicit instruction from the user.
- `git push --force` to any branch; use `--force-with-lease` only when asked.
- Move, delete, or re-point a published tag.
- Create a release from a branch other than `main`.
- Commit credentials, tokens, private keys, or real `.env` values.
- Discard or overwrite worktree changes you did not make.
- Add a page outside the `docs/` Diátaxis sections, without frontmatter, or
  without registering it in its section index and `docs/llms.txt`.
- Claim tests pass without having run them and read the output.
