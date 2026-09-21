package domain

// Status is the outcome of one check. The order matters: a higher value is
// worse, which is what Report.Worst relies on.
type Status int

const (
	Pass Status = iota
	Warn
	Fail
)

// String returns the lowercase name used in JSON output. These strings are
// part of the CLI's interface.
func (s Status) String() string {
	switch s {
	case Pass:
		return "pass"
	case Warn:
		return "warn"
	case Fail:
		return "fail"
	default:
		return "unknown"
	}
}

// Check is one observation turned into a verdict. Hint, when present, tells
// the user what to do about it.
type Check struct {
	Name    string
	Status  Status
	Summary string
	Hint    string
}

// Report is an ordered list of checks. Order is fixed by the producer so
// people and scripts see the same shape every run.
type Report struct {
	Checks []Check
}

// Worst returns the highest Status in the report, or Pass when empty.
func (r Report) Worst() Status {
	worst := Pass
	for _, c := range r.Checks {
		if c.Status > worst {
			worst = c.Status
		}
	}
	return worst
}

// Count returns how many checks have the given status.
func (r Report) Count(s Status) int {
	n := 0
	for _, c := range r.Checks {
		if c.Status == s {
			n++
		}
	}
	return n
}
