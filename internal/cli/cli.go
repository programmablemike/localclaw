// Package cli is lclaw's presentation layer: the command tree, flags,
// renderers and the exit-code mapping. Nothing here touches os.Stdout,
// os.Stderr or os.Exit. Writers are injected so the whole tree runs in
// tests with buffers.
package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	ucli "github.com/urfave/cli/v3"

	"github.com/programmablemike/localclaw/internal/domain"
)

// DoctorRunner is the slice of the doctor use case this package needs.
type DoctorRunner interface {
	Run(ctx context.Context) (domain.Report, error)
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
	Doctor DoctorRunner
	Build  BuildInfo
	Level  *slog.LevelVar // raised to Debug by --verbose; may be nil
	Stdout io.Writer
	Stderr io.Writer
}

// New builds the root command. Run it with (ctx, os.Args); it returns the
// error instead of exiting, and ExitCode maps that error to a status.
func New(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:                  "lclaw",
		Usage:                 "manage LocalClaw's Podman machines",
		HideVersion:           true,
		EnableShellCompletion: true,
		Writer:                d.Stdout,
		ErrWriter:             d.Stderr,
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
		OnUsageError: func(ctx context.Context, cmd *ucli.Command, err error, isSubcommand bool) error {
			return usage(cmd, err)
		},
		// A no-op handler stops urfave/cli from calling os.Exit itself, so
		// Run returns the error and the composition root keeps control.
		ExitErrHandler: func(context.Context, *ucli.Command, error) {},
		Commands: []*ucli.Command{
			doctorCommand(d),
			versionCommand(d),
		},
	}
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
