package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
)

// writeTable pads every column but the last to its widest cell, two
// spaces apart, the way doctor's output is aligned.
func writeTable(w io.Writer, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			if len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	for _, r := range rows {
		for i, c := range r {
			if i == len(r)-1 {
				fmt.Fprintln(w, c)
				break
			}
			fmt.Fprintf(w, "%-*s  ", widths[i], c)
		}
	}
}

func machinesText(roles []domain.Role) string {
	names := make([]string, 0, len(roles))
	for _, r := range roles {
		names = append(names, r.String())
	}
	return strings.Join(names, ",")
}

// stateText is the STATE column: set, or unset with what the next up does.
func stateText(s app.SecretStatus) string {
	if s.Set {
		return "set"
	}
	switch s.Spec.Source {
	case domain.Generated:
		return "unset (generated on next up)"
	case domain.Minted:
		return "unset (minted on next up)"
	default:
		return "unset (run `lclaw secrets set " + s.Spec.Name + "`)"
	}
}

func stateWord(set bool) string {
	if set {
		return "set"
	}
	return "unset"
}

func storeText(s app.StoreOutcome) string {
	if s.Err != nil {
		return string(s.State) + ": " + s.Err.Error()
	}
	return string(s.State)
}

func renderListText(w io.Writer, rows []app.SecretStatus) {
	table := [][]string{{"NAME", "SOURCE", "MACHINES", "STATE"}}
	for _, r := range rows {
		table = append(table, []string{r.Spec.Name, r.Source.String(), machinesText(r.Spec.Machines), stateText(r)})
	}
	writeTable(w, table)
}

func renderDetailText(w io.Writer, d app.SecretDetail) {
	fmt.Fprintf(w, "name:      %s\n", d.Spec.Name)
	fmt.Fprintf(w, "source:    %s\n", d.Source)
	fmt.Fprintf(w, "machines:  %s\n", machinesText(d.Spec.Machines))
	fmt.Fprintf(w, "state:     %s\n", stateWord(d.Set))
	if d.Set {
		fmt.Fprintf(w, "created:   %s\n", d.Created.UTC().Format(time.RFC3339))
		fmt.Fprintf(w, "modified:  %s\n", d.Modified.UTC().Format(time.RFC3339))
	}
	fmt.Fprintln(w, "stores:")
	var rows [][]string
	for _, s := range d.Stores {
		rows = append(rows, []string{"  " + s.Role.String(), storeText(s)})
	}
	writeTable(w, rows)
}

func renderOutcomeText(w io.Writer, verb string, o app.Outcome) {
	fmt.Fprintf(w, "%s %s\n", verb, o.Name)
	var rows [][]string
	stored := false
	for _, s := range o.Stores {
		rows = append(rows, []string{"  " + s.Role.String(), storeText(s)})
		stored = stored || s.State == app.StoreStored
	}
	writeTable(w, rows)
	if verb == "updated" && stored {
		fmt.Fprintln(w, "pods pick the new value up on the next `lclaw up`")
	}
}

// The JSON shapes. State and source strings are part of the interface.
type secretDTO struct {
	Name     string   `json:"name"`
	Source   string   `json:"source"`
	Machines []string `json:"machines"`
	State    string   `json:"state"`
}

type listDTO struct {
	Secrets []secretDTO `json:"secrets"`
}

type storeDTO struct {
	Machine string `json:"machine"`
	State   string `json:"state"`
	Error   string `json:"error,omitempty"`
}

type detailDTO struct {
	secretDTO
	Created  string     `json:"created,omitempty"`
	Modified string     `json:"modified,omitempty"`
	Stores   []storeDTO `json:"stores"`
}

type outcomeDTO struct {
	Name    string     `json:"name"`
	Action  string     `json:"action"`
	Stores  []storeDTO `json:"stores"`
	Warning string     `json:"warning,omitempty"`
}

func toSecretDTO(s app.SecretStatus) secretDTO {
	machines := make([]string, 0, len(s.Spec.Machines))
	for _, r := range s.Spec.Machines {
		machines = append(machines, r.String())
	}
	return secretDTO{Name: s.Spec.Name, Source: s.Source.String(), Machines: machines, State: stateWord(s.Set)}
}

func toStoreDTOs(stores []app.StoreOutcome) []storeDTO {
	out := make([]storeDTO, 0, len(stores))
	for _, s := range stores {
		dto := storeDTO{Machine: s.Role.String(), State: string(s.State)}
		if s.Err != nil {
			dto.Error = s.Err.Error()
		}
		out = append(out, dto)
	}
	return out
}

func encode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func renderListJSON(w io.Writer, rows []app.SecretStatus) error {
	dto := listDTO{Secrets: make([]secretDTO, 0, len(rows))}
	for _, r := range rows {
		dto.Secrets = append(dto.Secrets, toSecretDTO(r))
	}
	return encode(w, dto)
}

func renderDetailJSON(w io.Writer, d app.SecretDetail) error {
	dto := detailDTO{secretDTO: toSecretDTO(d.SecretStatus), Stores: toStoreDTOs(d.Stores)}
	if d.Set {
		dto.Created = d.Created.UTC().Format(time.RFC3339)
		dto.Modified = d.Modified.UTC().Format(time.RFC3339)
	}
	return encode(w, dto)
}

func renderOutcomeJSON(w io.Writer, verb string, o app.Outcome) error {
	return encode(w, outcomeDTO{Name: o.Name, Action: verb, Stores: toStoreDTOs(o.Stores), Warning: o.Warning})
}
