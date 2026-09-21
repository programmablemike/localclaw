package cli

import (
	ucli "github.com/urfave/cli/v3"
)

func doctorCommand(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:  "doctor",
		Usage: "check that this host can run LocalClaw",
	}
}
