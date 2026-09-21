package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/programmablemike/localclaw/internal/domain"
)

// renderReportText writes one aligned line per check, hints indented
// beneath the name column, and a final count.
func renderReportText(w io.Writer, r domain.Report) {
	width := 0
	for _, c := range r.Checks {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}
	for _, c := range r.Checks {
		fmt.Fprintf(w, "%-4s  %-*s  %s\n", strings.ToUpper(c.Status.String()), width, c.Name, c.Summary)
		if c.Hint != "" {
			fmt.Fprintf(w, "      hint: %s\n", c.Hint)
		}
	}
	fmt.Fprintf(w, "\n%d passed, %d warnings, %d failed\n",
		r.Count(domain.Pass), r.Count(domain.Warn), r.Count(domain.Fail))
}

// checkDTO and reportDTO are the JSON shape. They live here, not as tags on
// domain types, so serialization decisions never leak inward. The status
// strings are part of the CLI's interface.
type checkDTO struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Hint    string `json:"hint,omitempty"`
}

type reportDTO struct {
	Status string     `json:"status"`
	Checks []checkDTO `json:"checks"`
}

// renderReportJSON writes the report as indented JSON with a top-level
// status equal to the worst check.
func renderReportJSON(w io.Writer, r domain.Report) error {
	dto := reportDTO{Status: r.Worst().String(), Checks: make([]checkDTO, 0, len(r.Checks))}
	for _, c := range r.Checks {
		dto.Checks = append(dto.Checks, checkDTO{
			Name:    c.Name,
			Status:  c.Status.String(),
			Summary: c.Summary,
			Hint:    c.Hint,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(dto)
}
