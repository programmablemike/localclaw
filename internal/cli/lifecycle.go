package cli

import (
	"context"
	"fmt"
	"io"

	ucli "github.com/urfave/cli/v3"

	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
)

// LifecycleRunner is the slice of the lifecycle use case this package
// needs.
type LifecycleRunner interface {
	Up(ctx context.Context, dir string, roles []domain.Role) (domain.Report, error)
	Down(ctx context.Context, dir string, roles []domain.Role, destroy bool) (domain.Report, error)
	Status(ctx context.Context, dir string) (domain.Report, error)
}

// progressWriter is the app.Progress that announces each step on standard
// error as it begins, so stdout stays clean for --output json and a person
// watching a boot or a build is not left staring at nothing.
type progressWriter struct{ w io.Writer }

// Step implements app.Progress.
func (p progressWriter) Step(name, detail string) {
	fmt.Fprintf(p.w, "==> %s: %s\n", name, detail)
}

// Progress returns the progress writer for the given stderr.
func Progress(stderr io.Writer) app.Progress { return progressWriter{w: stderr} }

// zoneArgs parses the zone names on the command line.
func zoneArgs(cmd *ucli.Command) ([]domain.Role, error) {
	roles, err := domain.SelectRoles(cmd.Args().Slice())
	if err != nil {
		return nil, usage(cmd, err)
	}
	return roles, nil
}

// renderLifecycle prints the report and maps a failed check to
// ErrChecksFailed, exactly as doctor does.
func renderLifecycle(cmd *ucli.Command, report domain.Report) error {
	w := cmd.Root().Writer
	if jsonOutput(cmd) {
		if err := renderReportJSON(w, report); err != nil {
			return err
		}
	} else {
		renderReportText(w, report)
	}
	if report.Worst() == domain.Fail {
		return ErrChecksFailed
	}
	return nil
}

func upCommand(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "up",
		Usage:        "create and start the machine and apply the zones",
		ArgsUsage:    "[ZONE...]",
		OnUsageError: onUsageError,
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			roles, err := zoneArgs(cmd)
			if err != nil {
				return err
			}
			dir, err := scaffoldDir(cmd)
			if err != nil {
				return err
			}
			report, err := d.Lifecycle.Up(ctx, dir, roles)
			if err != nil {
				return reportFindings(cmd, err)
			}
			return renderLifecycle(cmd, report)
		},
	}
}

func downCommand(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "down",
		Usage:        "stop the zones' pods, purge the secrets and stop the machine",
		ArgsUsage:    "[ZONE...]",
		OnUsageError: onUsageError,
		Flags: []ucli.Flag{
			&ucli.BoolFlag{
				Name:  "destroy",
				Usage: "also remove the machine with its images and volumes; takes no zone",
			},
		},
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			roles, err := zoneArgs(cmd)
			if err != nil {
				return err
			}
			destroy := cmd.Bool("destroy")
			if destroy && cmd.Args().Present() {
				return usage(cmd, fmt.Errorf("--destroy applies to the whole machine; name no zone"))
			}
			dir, err := scaffoldDir(cmd)
			if err != nil {
				return err
			}
			report, err := d.Lifecycle.Down(ctx, dir, roles, destroy)
			if err != nil {
				return reportFindings(cmd, err)
			}
			return renderLifecycle(cmd, report)
		},
	}
}

func statusCommand(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "status",
		Usage:        "report the machine, the zone networks and every workload's pod",
		OnUsageError: onUsageError,
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			if cmd.Args().Present() {
				return usage(cmd, fmt.Errorf("unexpected argument %q", cmd.Args().First()))
			}
			dir, err := scaffoldDir(cmd)
			if err != nil {
				return err
			}
			report, err := d.Lifecycle.Status(ctx, dir)
			if err != nil {
				return reportFindings(cmd, err)
			}
			return renderLifecycle(cmd, report)
		},
	}
}
