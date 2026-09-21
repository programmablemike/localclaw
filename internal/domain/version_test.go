package domain

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in      string
		want    Version
		wantErr bool
	}{
		{in: "5.8.4", want: Version{Major: 5, Minor: 8, Patch: 4, Raw: "5.8.4"}},
		{in: "1.13.1-g684cdfb", want: Version{Major: 1, Minor: 13, Patch: 1, Raw: "1.13.1-g684cdfb"}},
		{in: "2.0.0+build.7", want: Version{Major: 2, Minor: 0, Patch: 0, Raw: "2.0.0+build.7"}},
		{in: "  5.8.4\n", want: Version{Major: 5, Minor: 8, Patch: 4, Raw: "5.8.4"}},
		{in: "5", want: Version{Major: 5, Raw: "5"}},
		{in: "5.1", want: Version{Major: 5, Minor: 1, Raw: "5.1"}},
		{in: "", wantErr: true},
		{in: "abc", wantErr: true},
		{in: "5.x.1", wantErr: true},
		{in: "1.2.3.4", wantErr: true},
		{in: "-1.0.0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseVersion(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseVersion(%q) = %+v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseVersion(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseVersion(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		v, min string
		want   bool
	}{
		{"5.8.4", "5.0.0", true},
		{"5.0.0", "5.0.0", true},
		{"4.9.9", "5.0.0", false},
		{"5.8.4", "5.9.0", false},
		{"5.8.4", "5.8.5", false},
		{"6.0.0", "5.8.4", true},
		{"1.13.1-g684cdfb", "1.0.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.v+">="+tt.min, func(t *testing.T) {
			v, err := ParseVersion(tt.v)
			if err != nil {
				t.Fatal(err)
			}
			min, err := ParseVersion(tt.min)
			if err != nil {
				t.Fatal(err)
			}
			if got := v.AtLeast(min); got != tt.want {
				t.Fatalf("%s.AtLeast(%s) = %v, want %v", tt.v, tt.min, got, tt.want)
			}
		})
	}
}

func TestVersionString(t *testing.T) {
	v, err := ParseVersion("1.13.1-g684cdfb")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "1.13.1"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if got, want := (Version{Major: 5}).String(), "5.0.0"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
