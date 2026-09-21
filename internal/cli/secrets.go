package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	ucli "github.com/urfave/cli/v3"

	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
)

// SecretsRunner is the slice of the secrets use case this package needs.
type SecretsRunner interface {
	List(ctx context.Context, dir string) ([]app.SecretStatus, error)
	Describe(ctx context.Context, dir, name string) (app.SecretDetail, error)
	Set(ctx context.Context, dir, name string, value []byte) (app.Outcome, error)
	Update(ctx context.Context, dir, name string, value []byte, generate bool) (app.Outcome, error)
	Delete(ctx context.Context, dir, name string) (app.Outcome, error)
	Get(ctx context.Context, dir, name string) ([]byte, error)
}

func secretsCommand(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "secrets",
		Usage:        "manage the secrets in the LocalClaw keychain",
		OnUsageError: onUsageError,
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			if cmd.Args().Present() {
				return usage(cmd, fmt.Errorf("unknown command %q", cmd.Args().First()))
			}
			return ucli.ShowSubcommandHelp(cmd)
		},
		Commands: []*ucli.Command{
			secretsList(d),
			secretsDescribe(d),
			secretsSet(d),
			secretsUpdate(d),
			secretsDelete(d),
			secretsGet(d),
		},
	}
}

func fromFileFlag() ucli.Flag {
	return &ucli.StringFlag{Name: "from-file", Usage: "read the value from `PATH` instead of stdin or a prompt"}
}

func secretsList(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "list",
		Usage:        "list every secret with its source, machines and state",
		OnUsageError: onUsageError,
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			if cmd.Args().Present() {
				return usage(cmd, fmt.Errorf("unexpected argument %q", cmd.Args().First()))
			}
			dir, err := scaffoldDir(cmd)
			if err != nil {
				return err
			}
			rows, err := d.Secrets.List(ctx, dir)
			if err != nil {
				return report(cmd, err)
			}
			w := cmd.Root().Writer
			if jsonOutput(cmd) {
				return renderListJSON(w, rows)
			}
			renderListText(w, rows)
			return nil
		},
	}
}

func secretsDescribe(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "describe",
		Usage:        "show a secret's source, machines, timestamps and store state, never its value",
		ArgsUsage:    "NAME",
		OnUsageError: onUsageError,
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			name, err := nameArg(cmd)
			if err != nil {
				return err
			}
			dir, err := scaffoldDir(cmd)
			if err != nil {
				return err
			}
			detail, err := d.Secrets.Describe(ctx, dir, name)
			if err != nil {
				return report(cmd, err)
			}
			w := cmd.Root().Writer
			if jsonOutput(cmd) {
				return renderDetailJSON(w, detail)
			}
			renderDetailText(w, detail)
			return nil
		},
	}
}

func secretsSet(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "set",
		Usage:        "create a secret from a file, stdin or a prompt",
		ArgsUsage:    "NAME",
		OnUsageError: onUsageError,
		Flags:        []ucli.Flag{fromFileFlag()},
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			name, err := nameArg(cmd)
			if err != nil {
				return err
			}
			dir, err := scaffoldDir(cmd)
			if err != nil {
				return err
			}
			value, err := readValue(cmd, d, name)
			if err != nil {
				return err
			}
			o, err := d.Secrets.Set(ctx, dir, name, value)
			if err != nil {
				return report(cmd, err)
			}
			return renderOutcome(cmd, "set", o)
		},
	}
}

func secretsUpdate(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "update",
		Usage:        "replace an existing secret, or regenerate it with --generate",
		ArgsUsage:    "NAME",
		OnUsageError: onUsageError,
		Flags: []ucli.Flag{
			fromFileFlag(),
			&ucli.BoolFlag{Name: "generate", Usage: "re-run the generator or the minter and restore the default source"},
		},
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			name, err := nameArg(cmd)
			if err != nil {
				return err
			}
			dir, err := scaffoldDir(cmd)
			if err != nil {
				return err
			}
			var value []byte
			generate := cmd.Bool("generate")
			if generate && cmd.String("from-file") != "" {
				return usage(cmd, errors.New("--generate and --from-file cannot be combined"))
			}
			if !generate {
				if value, err = readValue(cmd, d, name); err != nil {
					return err
				}
			}
			o, err := d.Secrets.Update(ctx, dir, name, value, generate)
			if err != nil {
				return report(cmd, err)
			}
			return renderOutcome(cmd, "updated", o)
		},
	}
}

func secretsDelete(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "delete",
		Usage:        "remove a secret from the keychain and from every running machine",
		ArgsUsage:    "NAME",
		OnUsageError: onUsageError,
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			name, err := nameArg(cmd)
			if err != nil {
				return err
			}
			dir, err := scaffoldDir(cmd)
			if err != nil {
				return err
			}
			o, err := d.Secrets.Delete(ctx, dir, name)
			if err != nil {
				return report(cmd, err)
			}
			return renderOutcome(cmd, "deleted", o)
		},
	}
}

func secretsGet(d Deps) *ucli.Command {
	return &ucli.Command{
		Name:         "get",
		Usage:        "print a secret's value and nothing else",
		ArgsUsage:    "NAME",
		OnUsageError: onUsageError,
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			name, err := nameArg(cmd)
			if err != nil {
				return err
			}
			dir, err := scaffoldDir(cmd)
			if err != nil {
				return err
			}
			value, err := d.Secrets.Get(ctx, dir, name)
			if err != nil {
				return report(cmd, err)
			}
			_, err = cmd.Root().Writer.Write(value)
			return err
		},
	}
}

// nameArg returns the single positional NAME or a usage error.
func nameArg(cmd *ucli.Command) (string, error) {
	args := cmd.Args()
	switch {
	case !args.Present():
		return "", usage(cmd, errors.New("missing secret name"))
	case args.Len() > 1:
		return "", usage(cmd, fmt.Errorf("unexpected argument %q", args.Get(1)))
	}
	return args.First(), nil
}

// readValue gets the value for set and update: from --from-file, else from
// stdin when it is not a terminal, else from a prompt that does not echo.
// A file or a pipe has exactly one trailing newline removed, because no
// real key ends with one and a stray one fails silently.
func readValue(cmd *ucli.Command, d Deps, name string) ([]byte, error) {
	if path := cmd.String("from-file"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read --from-file: %w", err)
		}
		return trimNewline(b), nil
	}
	if d.Prompt != nil {
		return d.Prompt("value for " + name + ": ")
	}
	if d.Stdin == nil {
		return nil, errors.New("no value: pass --from-file or pipe the value on stdin")
	}
	b, err := io.ReadAll(d.Stdin)
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	return trimNewline(b), nil
}

func trimNewline(b []byte) []byte {
	b = bytes.TrimSuffix(b, []byte("\n"))
	return bytes.TrimSuffix(b, []byte("\r"))
}

// report renders findings carried by err as a doctor-style report and
// turns them into ErrChecksFailed. Any other error passes through for the
// composition root to print.
func report(cmd *ucli.Command, err error) error {
	var fe *app.FindingsError
	if !errors.As(err, &fe) {
		return err
	}
	rep := domain.Report{Checks: findingsToChecks(fe.Findings)}
	w := cmd.Root().Writer
	if jsonOutput(cmd) {
		if err := renderReportJSON(w, rep); err != nil {
			return err
		}
	} else {
		renderReportText(w, rep)
	}
	return ErrChecksFailed
}

// findingsToChecks turns catalogue findings into the checks the report
// renderers understand. A domain.Finding carries only Where and Message;
// NewCatalogue writes Message as "problem; hint" when there is advice to
// give, so the first "; " splits it into the check's summary and hint, the
// same shape doctor's own checks use. Where becomes the check's name, and
// every finding is a failure: NewCatalogue only ever reports problems.
func findingsToChecks(findings []domain.Finding) []domain.Check {
	checks := make([]domain.Check, 0, len(findings))
	for _, f := range findings {
		c := domain.Check{Name: f.Where, Status: domain.Fail, Summary: f.Message}
		if summary, hint, ok := strings.Cut(f.Message, "; "); ok {
			c.Summary, c.Hint = summary, hint
		}
		checks = append(checks, c)
	}
	return checks
}

// renderOutcome prints what set, update and delete did, warns on stderr,
// and fails the command when any machine's store failed.
func renderOutcome(cmd *ucli.Command, verb string, o app.Outcome) error {
	if o.Warning != "" {
		fmt.Fprintf(cmd.Root().ErrWriter, "lclaw: warning: %s\n", o.Warning)
	}
	w := cmd.Root().Writer
	if jsonOutput(cmd) {
		if err := renderOutcomeJSON(w, verb, o); err != nil {
			return err
		}
	} else {
		renderOutcomeText(w, verb, o)
	}
	if o.Failed() {
		return ErrChecksFailed
	}
	return nil
}
