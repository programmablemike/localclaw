// Package cli is lclaw's presentation layer: the command tree, flags,
// renderers and the exit-code mapping. Nothing here touches os.Stdout,
// os.Stderr or os.Exit. Writers are injected so the whole tree runs in
// tests with buffers.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	ucli "github.com/urfave/cli/v3"

	"github.com/programmablemike/localclaw/internal/domain"
)

// DoctorRunner is the slice of the doctor use case this package needs.
type DoctorRunner interface {
	Run(ctx context.Context, dir string) (domain.Report, error)
}

// BuildInfo is what `lclaw version` prints.
type BuildInfo struct {
	Version  string
	Commit   string
	Date     string
	Modified bool
}

// Deps is everything the command tree needs, supplied by the composition
// root.
type Deps struct {
	Doctor     DoctorRunner
	Init       InitRunner
	Secrets    SecretsRunner
	Build      BuildInfo
	DefaultDir string         // the scaffold directory when --dir and LCLAW_DIR are unset; empty when the home directory is unknown
	Level      *slog.LevelVar // raised to Debug by --verbose; may be nil
	Stdout     io.Writer
	Stderr     io.Writer
	Stdin      io.Reader                           // value input for `secrets set` when stdin is not a terminal
	Prompt     func(prompt string) ([]byte, error) // no-echo prompt, set only when stdin is a terminal, and used by readValue in preference to Stdin whenever it is set
}

// New builds the root command. Run it with (ctx, os.Args); it returns the
// error instead of exiting, and ExitCode maps that error to a status.
func New(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:                  "lclaw",
		Usage:                 "manage LocalClaw's Podman machines",
		HideVersion:           true,
		HideHelpCommand:       true,
		EnableShellCompletion: true,
		Writer:                d.Stdout,
		ErrWriter:             d.Stderr,
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			if cmd.Args().Present() {
				return usage(cmd, fmt.Errorf("unknown command %q", cmd.Args().First()))
			}
			return ucli.ShowAppHelp(cmd)
		},
		Flags: []ucli.Flag{
			&ucli.StringFlag{
				Name:    "output",
				Usage:   "output format: text or json",
				Value:   "text",
				Sources: ucli.EnvVars("LCLAW_OUTPUT"),
			},
			&ucli.BoolFlag{
				Name:  "verbose",
				Usage: "log every external command to stderr",
			},
			&ucli.StringFlag{
				Name:    "dir",
				Usage:   "scaffold directory holding lclaw.toml and the workloads",
				Value:   d.DefaultDir,
				Sources: ucli.EnvVars("LCLAW_DIR"),
			},
		},
		Before: func(ctx context.Context, cmd *ucli.Command) (context.Context, error) {
			if out := cmd.String("output"); out != "text" && out != "json" {
				return ctx, usage(cmd, fmt.Errorf("invalid value %q for --output: want text or json", out))
			}
			if cmd.Bool("verbose") && d.Level != nil {
				d.Level.Set(slog.LevelDebug)
			}
			return ctx, nil
		},
		OnUsageError: onUsageError,
		// A no-op handler stops urfave/cli from calling os.Exit itself, so
		// Run returns the error and the composition root keeps control.
		ExitErrHandler: func(context.Context, *ucli.Command, error) {},
		Commands: []*ucli.Command{
			doctorCommand(d),
			secretsCommand(d),
			initCommand(d),
			versionCommand(d),
		},
	}
}

// onUsageError is set on the root and on every subcommand, because
// urfave/cli does not inherit it: a flag mistake after the command name
// would otherwise take urfave's default path (help on stdout, exit 1).
func onUsageError(ctx context.Context, cmd *ucli.Command, err error, isSubcommand bool) error {
	return usage(cmd, err)
}

// usageError marks a flag or argument mistake so ExitCode maps it to 2.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// usage reports a usage mistake on stderr and returns it as a usageError.
// urfave/cli prints nothing itself once OnUsageError is set, so this is the
// only place the message appears.
func usage(cmd *ucli.Command, err error) error {
	fmt.Fprintf(cmd.Root().ErrWriter, "lclaw: %v\nRun 'lclaw --help' for usage.\n", err)
	return &usageError{err: err}
}

// scaffoldDir returns the --dir value, or a usage error when it is empty,
// which happens only when the home directory is unknown and neither the
// flag nor LCLAW_DIR is set.
func scaffoldDir(cmd *ucli.Command) (string, error) {
	dir := cmd.Root().String("dir")
	if dir == "" {
		return "", usage(cmd, errors.New("no scaffold directory: pass --dir or set LCLAW_DIR"))
	}
	return dir, nil
}

// jsonOutput reports whether --output json is in effect.
func jsonOutput(cmd *ucli.Command) bool {
	return cmd.Root().String("output") == "json"
}
