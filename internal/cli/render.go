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
	renderEndpointsText(w, r.Endpoints)
}

// renderEndpointsText writes the endpoint list under its own heading, with
// the URL where there is one and the reason where there is not. A report
// without endpoints prints nothing extra, so doctor and secrets are
// unchanged.
func renderEndpointsText(w io.Writer, endpoints []domain.Endpoint) {
	if len(endpoints) == 0 {
		return
	}
	width := 0
	for _, e := range endpoints {
		if len(e.Label) > width {
			width = len(e.Label)
		}
	}
	fmt.Fprintf(w, "\nEndpoints\n")
	for _, e := range endpoints {
		detail := e.URL
		if !e.Reachable() {
			detail = "unavailable: " + e.Reason
		}
		fmt.Fprintf(w, "  %-*s  %s\n", width, e.Label, detail)
	}
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

// endpointDTO is the JSON shape of one endpoint. url and reason are
// mutually exclusive, so a consumer can branch on which one is present
// rather than on a separate boolean.
type endpointDTO struct {
	Workload string `json:"workload"`
	Label    string `json:"label"`
	URL      string `json:"url,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type reportDTO struct {
	Status    string        `json:"status"`
	Checks    []checkDTO    `json:"checks"`
	Endpoints []endpointDTO `json:"endpoints,omitempty"`
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
	for _, e := range r.Endpoints {
		dto.Endpoints = append(dto.Endpoints, endpointDTO{
			Workload: string(e.Workload),
			Label:    e.Label,
			URL:      e.URL,
			Reason:   e.Reason,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(dto)
}
