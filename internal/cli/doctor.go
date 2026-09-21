package cli

import (
	"context"

	ucli "github.com/urfave/cli/v3"

	"github.com/programmablemike/localclaw/internal/domain"
)

func doctorCommand(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "doctor",
		Usage:        "check that this host can run LocalClaw",
		OnUsageError: onUsageError,
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			dir, err := scaffoldDir(cmd)
			if err != nil {
				return err
			}
			report, err := d.Doctor.Run(ctx, dir)
			if err != nil {
				return err
			}
			w := cmd.Root().Writer
			if cmd.Root().String("output") == "json" {
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
		},
	}
}
