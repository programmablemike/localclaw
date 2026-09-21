// Package flox wraps the flox command line.
package flox

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/domain"
)

// Client satisfies app.EnvironmentManager.
type Client struct {
	Runner exec.Runner
}

// Version parses `flox --version`, which prints a bare version such as
// "1.13.1-g684cdfb".
func (c *Client) Version(ctx context.Context) (domain.Version, error) {
	res, err := c.Runner.Run(ctx, "flox", "--version")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return domain.Version{}, fmt.Errorf("flox: version: %w", domain.ErrToolNotFound)
		}
		return domain.Version{}, fmt.Errorf("flox: version: %w", err)
	}
	v, err := domain.ParseVersion(strings.TrimSpace(string(res.Stdout)))
	if err != nil {
		return domain.Version{}, fmt.Errorf("flox: version: %w", err)
	}
	return v, nil
}
