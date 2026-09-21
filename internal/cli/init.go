package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	ucli "github.com/urfave/cli/v3"

	"github.com/programmablemike/localclaw/internal/app"
)

// InitRunner is the slice of the init use case this package needs.
type InitRunner interface {
	Run(ctx context.Context, dir string, force bool) (app.InitReport, error)
}

func initCommand(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "init",
		Usage:        "write the default scaffold into the scaffold directory",
		OnUsageError: onUsageError,
		Flags: []ucli.Flag{
			&ucli.BoolFlag{
				Name:  "force",
				Usage: "overwrite files that already exist",
			},
		},
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			dir, err := scaffoldDir(cmd)
			if err != nil {
				return err
			}
			report, err := d.Init.Run(ctx, dir, cmd.Bool("force"))
			if err != nil {
				return err
			}
			w := cmd.Root().Writer
			if jsonOutput(cmd) {
				if err := renderInitJSON(w, report); err != nil {
					return err
				}
			} else {
				renderInitText(w, report)
			}
			if !report.Ok() {
				return ErrInitFailed
			}
			return nil
		},
	}
}

// renderInitText writes the written, skipped and failed paths in that
// order, one per line, then a summary naming the directory.
func renderInitText(w io.Writer, r app.InitReport) {
	for _, p := range r.Written {
		fmt.Fprintf(w, "written  %s\n", p)
	}
	for _, p := range r.Skipped {
		fmt.Fprintf(w, "skipped  %s\n", p)
	}
	for _, f := range r.Failed {
		fmt.Fprintf(w, "failed   %s: %v\n", f.Path, f.Err)
	}
	switch r.Keychain.State {
	case app.KeychainCreated:
		fmt.Fprintf(w, "created  %s\n", r.Keychain.Path)
	case app.KeychainSkipped:
		fmt.Fprintf(w, "skipped  %s\n", r.Keychain.Path)
	case app.KeychainFailed:
		name := r.Keychain.Path
		if name == "" {
			name = "keychain"
		}
		fmt.Fprintf(w, "failed   %s: %v\n", name, r.Keychain.Err)
	}
	fmt.Fprintf(w, "\n%s: %d written, %d skipped, %d failed\n", r.Dir, len(r.Written), len(r.Skipped), len(r.Failed))
}

// initDTO and initFailureDTO are the JSON shape of the init report. Arrays
// are always present, never null.
type initDTO struct {
	Dir      string           `json:"dir"`
	Written  []string         `json:"written"`
	Skipped  []string         `json:"skipped"`
	Failed   []initFailureDTO `json:"failed"`
	Keychain keychainDTO      `json:"keychain"`
}

type initFailureDTO struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

type keychainDTO struct {
	Path  string `json:"path,omitempty"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

func renderInitJSON(w io.Writer, r app.InitReport) error {
	dto := initDTO{
		Dir:     r.Dir,
		Written: nonNil(r.Written),
		Skipped: nonNil(r.Skipped),
		Failed:  make([]initFailureDTO, 0, len(r.Failed)),
	}
	for _, f := range r.Failed {
		dto.Failed = append(dto.Failed, initFailureDTO{Path: f.Path, Error: f.Err.Error()})
	}
	dto.Keychain = keychainDTO{Path: r.Keychain.Path, State: string(r.Keychain.State)}
	if r.Keychain.Err != nil {
		dto.Keychain.Error = r.Keychain.Err.Error()
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(dto)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
