package domain

import (
	"errors"
	"fmt"
)

// ErrToolNotFound is wrapped by adapters when an executable is absent, so
// EvaluateTool can distinguish "not installed" from "installed but broken".
var ErrToolNotFound = errors.New("tool not found")

// Requirement is a tool the host must have at a minimum version.
type Requirement struct {
	Tool        string
	Min         Version
	InstallHint string
}

// PodmanRequirement and FloxRequirement are the tools doctor checks.
var (
	PodmanRequirement = Requirement{
		Tool:        "podman",
		Min:         Version{Major: 5},
		InstallHint: "install Podman 5 or newer from https://podman.io/docs/installation",
	}
	FloxRequirement = Requirement{
		Tool:        "flox",
		Min:         Version{Major: 1},
		InstallHint: "install Flox from https://flox.dev/docs/install-flox/",
	}
)

// EvaluateTool turns an observation of a tool into a Check. It never
// returns an error: a missing or broken tool is a verdict, not a failure of
// the program.
func EvaluateTool(req Requirement, found Version, err error) Check {
	c := Check{Name: req.Tool}
	switch {
	case errors.Is(err, ErrToolNotFound):
		c.Status, c.Summary, c.Hint = Fail, "not found", req.InstallHint
	case err != nil:
		c.Status, c.Summary = Fail, err.Error()
	case !found.AtLeast(req.Min):
		c.Status = Fail
		c.Summary = fmt.Sprintf("%s is older than the minimum %s", found, req.Min)
		c.Hint = req.InstallHint
	default:
		c.Status = Pass
		c.Summary = fmt.Sprintf("%s (minimum %s)", found, req.Min)
	}
	return c
}

// EvaluateMachines emits one Check per role, in role order: Pass when the
// machine exists (summary says whether it is running), Warn when it does not.
func EvaluateMachines(roles []Role, found []Machine) []Check {
	byName := make(map[string]Machine, len(found))
	for _, m := range found {
		byName[m.Name] = m
	}
	checks := make([]Check, 0, len(roles))
	for _, r := range roles {
		name := r.MachineName()
		c := Check{Name: name}
		if m, ok := byName[name]; ok {
			c.Status = Pass
			if m.Running {
				c.Summary = "running"
			} else {
				c.Summary = "stopped"
			}
		} else {
			c.Status = Warn
			c.Summary = "not created"
			c.Hint = "run `lclaw up` once it is available"
		}
		checks = append(checks, c)
	}
	return checks
}
