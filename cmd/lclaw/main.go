// Command lclaw manages LocalClaw's Podman machines. This file is the
// composition root: the only place adapters are constructed and the only
// place os.Exit is called.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/programmablemike/localclaw"
	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/adapters/flox"
	"github.com/programmablemike/localclaw/internal/adapters/keychain"
	"github.com/programmablemike/localclaw/internal/adapters/osfs"
	"github.com/programmablemike/localclaw/internal/adapters/podman"
	"github.com/programmablemike/localclaw/internal/adapters/toml"
	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/cli"
)

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

// run wires the dependencies, runs the command tree and returns the exit
// status. Debug logs go to stderr so stdout stays clean for --output json.
func run(args []string, stdout, stderr io.Writer) int {
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	runner := &exec.System{Log: logger}
	files := osfs.System{}
	doctor := &app.Doctor{
		Runtime:  &podman.Client{Runner: runner},
		Envs:     &flox.Client{Runner: runner},
		Scaffold: files,
		Topology: toml.Loader{},
	}
	root := cli.New(cli.Deps{
		Doctor: doctor,
		Init: &app.Init{
			Defaults: scaffoldFS(),
			Writer:   files,
			Scaffold: files,
			Topology: toml.Loader{},
			Keychain: &keychain.Client{Runner: runner},
		},
		Build:      buildInfo(),
		DefaultDir: defaultDir(),
		Level:      level,
		Stdout:     stdout,
		Stderr:     stderr,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := root.Run(ctx, args)
	code := cli.ExitCode(err)
	switch {
	case code == 130:
		fmt.Fprintln(stderr, "lclaw: interrupted")
	case code == 1 && !errors.Is(err, cli.ErrChecksFailed) && !errors.Is(err, cli.ErrInitFailed):
		// Failed checks and failed writes were already reported; usage
		// errors were already printed by the cli package. Everything else
		// is unexpected.
		fmt.Fprintf(stderr, "lclaw: %v\n", err)
	}
	return code
}

// buildInfo combines the embedded VERSION file with what the Go toolchain
// recorded about the checkout. Inside the Nix store there is no .git, so
// commit and date read "unknown" there.
func buildInfo() cli.BuildInfo {
	bi := cli.BuildInfo{
		Version: strings.TrimSpace(localclaw.Version),
		Commit:  "unknown",
		Date:    "unknown",
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return bi
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			bi.Commit = s.Value
		case "vcs.time":
			bi.Date = s.Value
		case "vcs.modified":
			bi.Modified = s.Value == "true"
		}
	}
	return bi
}

// defaultDir is ~/.config/lclaw, the scaffold location when neither --dir
// nor LCLAW_DIR is set. It is empty when the home directory is unknown, and
// the cli then asks for --dir.
func defaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "lclaw")
}
