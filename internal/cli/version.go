package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	ucli "github.com/urfave/cli/v3"
)

type versionDTO struct {
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	Date     string `json:"date"`
	Modified bool   `json:"modified"`
	Go       string `json:"go"`
	Platform string `json:"platform"`
}

func versionCommand(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "version",
		Usage:        "print version and build information",
		OnUsageError: onUsageError,
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			w := cmd.Root().Writer
			info := versionDTO{
				Version:  d.Build.Version,
				Commit:   d.Build.Commit,
				Date:     d.Build.Date,
				Modified: d.Build.Modified,
				Go:       runtime.Version(),
				Platform: runtime.GOOS + "/" + runtime.GOARCH,
			}
			if cmd.Root().String("output") == "json" {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			}
			commit := info.Commit
			if info.Modified {
				commit += " (modified)"
			}
			_, err := fmt.Fprintf(w, "lclaw %s\ncommit:   %s\nbuilt:    %s\ngo:       %s %s\n",
				info.Version, commit, info.Date, info.Go, info.Platform)
			return err
		},
	}
}
