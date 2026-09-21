package domain

import "testing"

func TestStatusString(t *testing.T) {
	tests := map[Status]string{Pass: "pass", Warn: "warn", Fail: "fail", Status(9): "unknown"}
	for s, want := range tests {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", int(s), got, want)
		}
	}
}

func TestReportWorstAndCount(t *testing.T) {
	r := Report{Checks: []Check{
		{Name: "a", Status: Pass},
		{Name: "b", Status: Warn},
		{Name: "c", Status: Pass},
		{Name: "d", Status: Fail},
		{Name: "e", Status: Warn},
	}}
	if got := r.Worst(); got != Fail {
		t.Fatalf("Worst() = %v, want Fail", got)
	}
	if got := r.Count(Pass); got != 2 {
		t.Fatalf("Count(Pass) = %d, want 2", got)
	}
	if got := r.Count(Warn); got != 2 {
		t.Fatalf("Count(Warn) = %d, want 2", got)
	}
	if got := r.Count(Fail); got != 1 {
		t.Fatalf("Count(Fail) = %d, want 1", got)
	}
}

func TestReportWorstOfEmptyIsPass(t *testing.T) {
	if got := (Report{}).Worst(); got != Pass {
		t.Fatalf("Worst() of empty report = %v, want Pass", got)
	}
}

func TestReportWorstWarnOnly(t *testing.T) {
	r := Report{Checks: []Check{{Status: Pass}, {Status: Warn}}}
	if got := r.Worst(); got != Warn {
		t.Fatalf("Worst() = %v, want Warn", got)
	}
}
