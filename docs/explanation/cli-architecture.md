---
title: "CLI architecture"
description: "Why lclaw is layered as presentation, domain and data, which dependencies it accepts, and how the doctor command proves the design."
diataxis: explanation
status: stable
last_reviewed: 2026-09-21
tags: [cli, architecture, hexagonal, go, dependencies, design-decision]
related:
  - ../../README.md
  - ../../AGENTS.md
  - deployment-model.md
  - secrets-management.md
---

# CLI architecture

`lclaw` is the Go command-line tool that manages LocalClaw's Podman machines.
This page records the design agreed on 2026-09-20 for its first code: the
layering, the dependency policy, the toolchain, and the one command,
`lclaw doctor`, that proves the layering works end to end. It was written
before the code existed and is the specification the implementation follows.
Once the code lands, the facts in it (commands, flags, exit codes, output
shapes) move to reference pages and the reasoning stays here.

## Goals and constraints

The README states two goals: isolate the agent from the host, and make setup
and operation easy and reliable. For the CLI they become three constraints.

- **Extensible.** Machine lifecycle, the WireGuard overlay, Kuma policy and
  troubleshooting helpers all land later. Adding them must not mean reworking
  what exists.
- **Reliable.** Every behaviour must be testable on a machine without Podman
  or Flox installed, and problems must be reported completely rather than one
  at a time.
- **Light.** External dependencies are the supply chain. Every module in
  `go.sum` is code somebody must trust, and every module linked into the
  binary is attack surface. Builds must need no network and be reproducible.

The first implementation is deliberately narrow: the skeleton of every layer,
the toolchain, and a working `lclaw doctor`. The machine lifecycle (`up`,
`down`, `status`) pulls the whole topology model into scope and gets its own
design page.

## The layering

The design is hexagonal, also called ports and adapters. The application core
knows nothing about Podman, Flox, terminals or files. It declares the
interfaces (ports) it needs, and thin adapters at the edges satisfy them. The
same shape is often described as presentation, domain and data; the table maps
the two vocabularies onto the package tree.

| Layer        | Packages                          | Contains                                                                 |
| ------------ | --------------------------------- | ------------------------------------------------------------------------ |
| Presentation | `internal/cli`                    | Command tree, flags, text and JSON renderers, exit-code mapping          |
| Domain       | `internal/domain`, `internal/app` | Entities and pure rules; use cases and the ports they consume            |
| Data         | `internal/adapters/...`           | Implementations of the ports: process execution, Podman, Flox            |
| Wiring       | `cmd/lclaw`                       | The composition root that constructs adapters and hands them to the core |

```text
cmd/lclaw/main.go            composition root
internal/cli/                presentation
internal/domain/             entities and pure rules
internal/app/                use cases and port interfaces
internal/adapters/exec/      os/exec runner and its scripted fake
internal/adapters/podman/    wraps the podman command line
internal/adapters/flox/      wraps the flox command line
```

### The dependency rule

Imports flow in one direction only.

- `domain` imports nothing from this module.
- `app` imports `domain`.
- `cli` imports `app` and `domain`, never an adapter.
- The Podman and Flox adapters import `app` and `domain` to satisfy a port,
  plus `adapters/exec` for the process seam. `adapters/exec` imports nothing
  from this module. Adapters never import `cli`.
- `cmd/lclaw` is the only package that imports adapters.

A unit test walks the import graph of every package under `internal/` and
`cmd/` with `go/parser` and fails when an edge breaks these rules. The
boundary is therefore enforced by CI, not by review.

### Why ports live beside their consumers

A separate `ports/` package is common in hexagonal codebases written in other
languages. Go convention is the opposite: an interface is declared in the
package that uses it, and implementations satisfy it implicitly. So
`MachineRuntime` lives in `app` next to the `Doctor` use case that calls it,
and `cli` declares its own tiny `DoctorRunner` interface rather than importing
the concrete use case. Each package states exactly the behaviour it needs and
nothing more, which keeps tests small and lets implementations change without
touching consumers.

## Components

Each unit below answers three questions: what it does, how it is used, and
what it depends on.

### Domain (`internal/domain`)

Pure Go over the standard library. No I/O, no logging, no imports from this
module.

```go
type Version struct{ Major, Minor, Patch int; Raw string }
func ParseVersion(s string) (Version, error)   // accepts "5.8.4" and "1.13.1-g684cdfb"
func (v Version) AtLeast(min Version) bool
func (v Version) String() string

type Status int                                 // Pass, Warn, Fail
type Check struct{ Name string; Status Status; Summary, Hint string }
type Report struct{ Checks []Check }
func (r Report) Worst() Status
func (r Report) Count(s Status) int

type Role int                                   // Infra, Services, Agent
func Roles() []Role
func (r Role) MachineName() string              // "lclaw-infra", "lclaw-services", "lclaw-agent"
type Machine struct{ Name string; Running bool }

type Requirement struct{ Tool string; Min Version; InstallHint string }
var PodmanRequirement = Requirement{Tool: "podman", Min: Version{Major: 5}, InstallHint: "install Podman 5 or newer from https://podman.io/docs/installation"}
var FloxRequirement   = Requirement{Tool: "flox",   Min: Version{Major: 1}, InstallHint: "install Flox from https://flox.dev/docs/install-flox/"}

func EvaluateTool(req Requirement, found Version, err error) Check
func EvaluateMachines(roles []Role, found []Machine) []Check
```

`ParseVersion` ignores anything after a dash or plus sign, because Flox
reports `1.13.1-g684cdfb` and pre-release suffixes do not change the
comparison that matters here.

The two `Evaluate` functions hold every pass, warn or fail decision. They take
observations and return checks. They never return errors. That is the
property that lets `doctor` finish on a broken host: a missing tool is an
observation, not a failure of the program.

`Role` and `Machine` are the seed of the topology model. The lifecycle design
will grow them, and nothing else in this slice depends on their shape.

### Application (`internal/app`)

The use cases, and the ports they consume.

```go
type MachineRuntime interface {
    Version(ctx context.Context) (domain.Version, error)
    ListMachines(ctx context.Context) ([]domain.Machine, error)
}

type EnvironmentManager interface {
    Version(ctx context.Context) (domain.Version, error)
}

var ErrToolNotFound = errors.New("tool not found")

type Doctor struct {
    Runtime MachineRuntime
    Envs    EnvironmentManager
}

func (d *Doctor) Run(ctx context.Context) (domain.Report, error)
```

Ports are named for the role they play, not the vendor that fills it. Adapters
wrap `ErrToolNotFound` when the executable is absent so the domain rule can
distinguish "not installed" from "installed but broken".

`Doctor.Run` returns an error only for context cancellation or a programming
mistake. Everything a user can fix is a `Check` in the report.

### Adapters (`internal/adapters`)

**`exec`** is the single seam between the program and the operating system.

```go
type Result struct{ Stdout, Stderr []byte }
type Runner interface {
    Run(ctx context.Context, name string, args ...string) (Result, error)
}
var ErrNotFound error                      // wraps os/exec.ErrNotFound
type ExitError struct{ Code int; Stderr string }

type System struct{ Log *slog.Logger }     // real implementation
type Fake struct{ /* scripted by argv; records calls */ }
```

`System` uses `exec.CommandContext`, sets `WaitDelay` so a child that ignores
cancellation is killed after a grace period, logs every command with its
duration and stderr at debug level, and reports a non-zero exit as an
`ExitError` carrying the tail of stderr. `Fake` lives in the same package so
the Podman and Flox adapters share one test seam.

**`podman.Client`** satisfies `MachineRuntime`. It parses the last field of
`podman --version` and decodes `podman machine list --format json`, reading
only the `Name` and `Running` fields. Every other field is ignored on purpose
so Podman upgrades do not break the adapter.

**`flox.Client`** satisfies `EnvironmentManager` from `flox --version`.

Neither adapter links Podman's Go bindings. Those pull the whole containers
stack into the module graph (see the measurements below) and require cgo.

### Presentation (`internal/cli`)

```go
type DoctorRunner interface {
    Run(ctx context.Context) (domain.Report, error)
}

type BuildInfo struct{ Version, Commit, Date string; Modified bool }

type Deps struct {
    Doctor DoctorRunner
    Build  BuildInfo
    Level  *slog.LevelVar
    Stdout io.Writer
    Stderr io.Writer
}

func New(d Deps) *cli.Command
var ErrChecksFailed error
func ExitCode(err error) int
```

`New` returns the root [urfave/cli v3](https://github.com/urfave/cli) command
named `lclaw`. Global flags are `--output text|json`, with `LCLAW_OUTPUT` as
an environment source, and `--verbose`. Shell completion is enabled. The
subcommands are `doctor` and `version`.

The root's `ExitErrHandler` is a no-op. By default urfave/cli calls `os.Exit`
itself when an action returns an error that carries an exit code; the no-op
makes `Run` return the error instead, so the composition root keeps control
and the whole command tree can be exercised in tests with buffers as writers.
Nothing in this package touches `os.Stdout`, `os.Stderr` or `os.Exit`.
`OnUsageError` hooks on the root and on every subcommand wrap flag-parsing
failures in a usage error type so `ExitCode` can map them to 2, and a root
action turns an unknown command into the same error.

Two renderers turn a `domain.Report` into aligned text or JSON. JSON goes
through a small data-transfer struct declared in this package rather than
tags on domain types, so serialization decisions never leak inward.

### Composition root (`cmd/lclaw`)

`main` builds a `slog` logger on a `LevelVar` writing to stderr, constructs
`exec.System`, the two clients, the `Doctor` use case and the CLI, wraps the
context with `signal.NotifyContext`, runs, and calls `os.Exit` with
`cli.ExitCode(err)`. It prints unexpected errors to stderr prefixed with
`lclaw:`, prints nothing extra for `ErrChecksFailed` because the report has
already said what failed, and nothing extra for usage errors because the cli
package has already printed the message and a pointer to `--help`.

## How `lclaw doctor` flows through the layers

1. `main` wires the dependencies and calls `Run` on the root command with
   `os.Args`.
2. urfave/cli parses the global flags. The root `Before` hook rejects an
   unknown `--output` value as a usage error and raises the log level when
   `--verbose` is set.
3. The `doctor` action calls `DoctorRunner.Run`, receives a `Report`, picks the
   renderer, writes to the injected stdout, and returns `ErrChecksFailed` if
   the report's worst status is `Fail`. Otherwise it returns nil.
4. `Doctor.Run` executes the checks in a fixed order: Flox version, Podman
   version, then the machine list. Between steps it checks `ctx.Err()` and
   returns it if the user interrupted.
5. Each adapter call goes through `Runner.Run`. A not-found error is wrapped as
   `ErrToolNotFound` before it leaves the adapter; any other failure is wrapped
   with a prefix naming the adapter and operation.
6. `EvaluateTool` turns each observation into a `Check`: not found gives
   `Fail` with the install hint; a version below the minimum gives `Fail`
   naming both numbers; any other error gives `Fail` with the error text;
   success gives `Pass` with the version.
7. If the Podman check is `Fail`, the machine list cannot be obtained. The use
   case appends one `Warn` check named `machines` whose summary says it was
   skipped and why. Otherwise `EvaluateMachines` emits one check per role:
   `Pass` when the machine exists, `Warn` with a hint when it does not.

Every problem is reported. A host with Flox missing, Podman too old and no
machines created shows all of it in one run, because a failed check is a
value appended to the report, never an error that ends the run. The order of
checks is fixed regardless of outcome, so people and scripts see the same
shape every time.

## Error handling

- Errors are wrapped with `%w` and a short prefix naming the layer and
  operation, so a failure reads as a path:
  `podman: list machines: exit status 125: ...`.
- Adapters never log and swallow. They return. The runner's debug log is the
  only place execution details are recorded, and `--verbose` shows them.
- The domain never returns errors from evaluation. See above.
- Cancellation is honoured at three points: `exec.CommandContext` kills the
  child, `WaitDelay` bounds how long that takes, and the use case checks the
  context between steps.
- Debug logs go to stderr so stdout stays clean for `--output json`.

## Output

Text output is one line per check with the name column padded to the longest
name, hints indented beneath, and a final count.

```text
PASS  flox            1.13.1 (minimum 1.0.0)
PASS  podman          5.8.4 (minimum 5.0.0)
WARN  lclaw-infra     not created
      hint: run `lclaw up` once it is available
WARN  lclaw-services  not created
      hint: run `lclaw up` once it is available
WARN  lclaw-agent     not created
      hint: run `lclaw up` once it is available

2 passed, 3 warnings, 0 failed
```

JSON output has a top-level `status` equal to the worst check, and `hint` is
omitted when empty. Status values are lowercase strings and are part of the
interface.

```json
{
  "status": "warn",
  "checks": [
    {"name": "flox", "status": "pass", "summary": "1.13.1 (minimum 1.0.0)"},
    {"name": "podman", "status": "pass", "summary": "5.8.4 (minimum 5.0.0)"},
    {"name": "lclaw-infra", "status": "warn", "summary": "not created", "hint": "run `lclaw up` once it is available"}
  ]
}
```

`lclaw version` prints the version from the `VERSION` file, the commit, commit
time and dirty flag that `runtime/debug.ReadBuildInfo` supplies when built
from a git checkout, and the Go version and platform. Inside the Nix store
there is no `.git`, so the commit reads as unknown there. It honours
`--output json`.

### Exit codes

`cli.ExitCode` owns this mapping and `main` only calls it.

| Code | Meaning                                                      |
| ---- | ------------------------------------------------------------ |
| 0    | Every check passed or warned                                 |
| 1    | At least one check failed, or an unexpected error            |
| 2    | Usage error such as an unknown flag or a bad `--output` value |
| 130  | Interrupted by a signal                                      |

## Dependency policy

Standard library first. A third-party module is added only when the stdlib
answer is genuinely worse, and only after measuring its footprint the same
way as below. Two numbers matter. "In graph" is what `go list -m all` reports
and what `go.sum` must vouch for: the supply chain. "Linked" is what
`go list -deps` says actually compiles into the binary: the attack surface.
Since Go 1.17 pruned the module graph, a library's test-only dependencies
appear in the first number but not the second.

Measured on 2026-09-20 with Go 1.26.7, one throwaway module per library:

| Candidate                 | In graph | Linked | Notes on the extras                                    |
| ------------------------- | -------- | ------ | ------------------------------------------------------ |
| `urfave/cli/v3`           | 3        | 1      | testify and yaml.v3, both test-only                    |
| `spf13/cobra`             | 7        | 2      | go-md2man, blackfriday, mousetrap, yaml.v3, check.v1   |
| `alecthomas/kong`         | 4        | 1      | assert, repr, gotextdiff, all test-only                |
| `peterbourgon/ff/v4`      | 3        | 1      | go-toml and yaml.v2, runtime if its parsers are used   |
| `google/go-cmp`           | 1        | 1      | none                                                   |
| `BurntSushi/toml`         | 1        | 1      | none                                                   |
| `golang.org/x/term`       | 2        | 2      | x/sys                                                  |
| `golang.org/x/crypto`     | 5        | 1      | x/sys, x/term, x/net, x/text                           |
| `spf13/viper`             | 26       | 13     | fsnotify, afero, mapstructure, cast and more           |
| `stretchr/testify`        | 3        | 2      | yaml.v3                                                |
| Podman Go bindings        | 382      | 97     | the entire containers stack                            |

`golang.org/x/term` was re-measured on 2026-09-21, with the versions now
vendored: two modules in the graph, `golang.org/x/term` v0.46.0 and
`golang.org/x/sys` v0.48.0, both linked, no cgo.

The code has three runtime dependencies and no test-only dependencies:
`github.com/urfave/cli/v3` for the command tree,
`github.com/BurntSushi/toml` for `lclaw.toml`, and `golang.org/x/term` for
the no-echo secret prompt, which pulls in `golang.org/x/sys`. Everything
else the design needs has a standard-library answer.

| Need                         | Standard library answer                                   |
| ---------------------------- | --------------------------------------------------------- |
| Structured logging           | `log/slog` with a `LevelVar`                              |
| Error wrapping and joining   | `errors`, `fmt.Errorf` with `%w`                          |
| Running Podman and Flox      | `os/exec` behind the `Runner` port                        |
| Aligned text and JSON        | `fmt` padding verbs, `encoding/json`                      |
| TTY detection                | `term.IsTerminal`, alongside the no-echo prompt it ships with |
| Parallel machine operations  | `sync.WaitGroup.Go` and `errors.Join`, when needed later  |
| WireGuard keys, if ever host-side | `crypto/ecdh.X25519()` produces the 32-byte keys      |
| Build and version metadata   | `runtime/debug.ReadBuildInfo`                             |
| Signals and cancellation     | `os/signal.NotifyContext`, `context`                      |
| Import-boundary test         | `go/parser` in `ImportsOnly` mode                         |

The configuration file arrived with the
[deployment model](deployment-model.md): `lclaw.toml` is decoded by
`github.com/BurntSushi/toml`, chosen over `github.com/pelletier/go-toml/v2`,
which measured the same one module in the graph and one linked, for its
wider use and for `MetaData.Undecoded`, which is how the loader rejects
unknown keys instead of letting a typo become a default.

## Testing

Standard `testing` only, with `reflect.DeepEqual` and `%+v` for comparisons.
`go-cmp` is reconsidered if diffs become painful.

- **Domain:** table-driven tests for version parsing and comparison, and for
  both evaluation rules. No fakes.
- **Application:** `Doctor` against hand-written fakes of the two ports. Cases
  cover everything passing, each tool missing, Podman below the minimum, the
  machine list erroring, and cancellation between steps. One case asserts
  that several simultaneous problems all appear in the report.
- **Adapters:** the Podman and Flox clients against `exec.Fake`, with fixtures
  captured from a real macOS host in `testdata/`. Separate tests cover the
  not-found and non-zero-exit mappings. `exec.System` gets a small real test
  using `sh` for exit code, stderr capture and cancellation, skipped when `sh`
  is absent.
- **Presentation:** the root command runs with a fake `DoctorRunner` and
  buffer writers. Golden files check the text and JSON renderings. Further
  cases assert that a bad `--output` maps to exit 2 and failed checks to
  exit 1, and that `version` renders in both formats.
- **Architecture:** the import-graph test described under the dependency
  rule.

Tests run with the race detector. It is cgo-free on macOS but still needs cgo
on Linux with Go 1.26, so the check script enables cgo for that one step on
Linux and the manifest installs gcc for the Linux systems only. Builds stay
static.

## Toolchain and build

Everything runs through Flox. There is no Makefile and no Nix flake.

- **Dev environment**, `.flox/env/manifest.toml`. Installs Go, staticcheck and
  govulncheck. Sets `GOTOOLCHAIN=local` so Go can never auto-download a
  different toolchain, and `CGO_ENABLED=0` for static binaries.
- **Package build**, `.flox/pkgs/lclaw.nix`. A Nix expression build using
  `buildGoModule` with `vendorHash = null`, symbols stripped, and the version
  injected with `-ldflags -X`. `flox build lclaw` is the hermetic gate and the
  release artifact. It runs the unit tests in its check phase. Flox evaluates
  the expression against the same nixpkgs pin that provides the dev
  environment's Go, so there is one toolchain pin.
- **Vendoring.** Dependencies are vendored and committed. With
  `vendorHash = null`, `buildGoModule` compiles from `vendor/` and fetches
  nothing, so the derivation is hermetic by construction, and on Linux the Nix
  sandbox blocks the network so an unvendored dependency fails loudly. A
  dependency change also shows up as a reviewable diff in the pull request.
- **Version source of truth** is a `VERSION` file at the repository root. The
  Nix expression reads it, dev builds pass the same value, and the release
  procedure in `AGENTS.md` bumps it.
- **Checks** are one POSIX shell script, `scripts/check.sh`, run as
  `flox activate -- ./scripts/check.sh` locally, in CI and in `AGENTS.md`. It
  runs `gofmt -l`, `go vet`, staticcheck, govulncheck, `go test -race`, and
  `go mod tidy -diff` plus a vendor drift check. One entry point, no task
  runner.
- **CI** installs Flox with its official GitHub action pinned by commit SHA,
  then runs the check script and `flox build lclaw` on macOS and Ubuntu.

## Alternatives considered

**Command framework.** Cobra is the ecosystem default and what Podman itself
uses, but it adds five modules to the graph that never link on macOS. Kong has
the best dependency-injection story but needs a third-party add-on for shell
completion. A hand-written dispatcher on `flag`, modelled on the Go
toolchain's own `base.Command`, would be zero modules but means owning help
formatting and deferring completion. urfave/cli v3 was chosen because it is
one linked module with no runtime transitive dependencies and includes
completion for four shells. Its v3 API differs from the v2 that most training
material shows; the compiler catches the mismatches.

**Layout.** A `ports/` package is unidiomatic in Go, as explained above.
Vertical feature slices are fast to extend but keep the layer boundary by
convention only. Literal `presentation`, `domain` and `data` packages name
packages for their layer rather than what they provide, and `data` becomes a
bucket where Podman, Flox and exec collide.

**Configuration and logging libraries.** Viper brings 26 modules into the
graph for a config file this tool does not yet have. `slog` made zap, zerolog
and logrus unnecessary.

**Podman Go bindings.** 382 modules in the graph, 97 linked, and cgo. The
command line with `--format json` is a stable, documented interface.

**Make or a Nix flake.** Make was habit, not need. A flake would carry a
second nixpkgs pin next to Flox's and the two drift, and flakes remain behind
an experimental-features flag upstream. Flox's Nix expression build gives
hermetic packaging and `flox publish` for distribution, which matches the
README's statement that Flox builds and manages the agents. A thin flake can
wrap the same expression later if `nix run` consumption ever matters.

## Refinements made during implementation

The code landed on 2026-09-20 and matches this page with six small changes,
recorded so the page stays accurate.

- **Version via `go:embed`.** A root package `localclaw` embeds `VERSION`,
  so `go run`, `go test`, development builds and the Nix build all report
  the same version without `-ldflags -X`. The Nix expression strips symbols
  and nothing more, and `cmd/lclaw` imports the root package for the value.
- **`ErrToolNotFound` is declared in `domain`.** `EvaluateTool` must
  recognise "not installed", so the sentinel lives next to it;
  `app.ErrToolNotFound` is the same value.
- **Text output is padded by hand.** A hint line has fewer cells than a
  check line, which splits `tabwriter`'s column blocks; a computed
  name width gives the alignment shown above.
- **staticcheck comes from the nixpkgs package `go-tools`**, and
  golangci-lint left the manifest.
- **`subPackages` is not set in the Nix expression.** Limiting it to
  `cmd/lclaw` would also limit the check phase to that package; building
  every package installs only the one `main` and tests all of them.
- **The race detector needs cgo on Linux.** The first CI run showed that
  `go test -race` aborts under `CGO_ENABLED=0` on Linux with Go 1.26, while
  macOS is cgo-free. The check script enables cgo for that step on Linux only
  and the manifest installs gcc for the Linux systems.

## What lands with the code

- `docs/reference/cli.md`: commands, global flags, exit codes and the JSON
  output shape.
- `docs/how-to/set-up-a-development-environment.md`: activating Flox, running
  the check script, building and running `lclaw doctor`.
- The "Build, test, lint" placeholder in `AGENTS.md` was replaced with the
  real commands.
- `CHANGELOG.md` landed with an Unreleased section, as the release procedure
  expects.
- This page is `stable`: the implementation matches it.
