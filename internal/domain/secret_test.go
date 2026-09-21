package domain

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// sequence returns a reader yielding the bytes 0, 1, 2 ... n-1, so expected
// encodings are literal strings.
func sequence(n int) *bytes.Reader {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return bytes.NewReader(b)
}

func TestSourceStrings(t *testing.T) {
	for s, want := range map[Source]string{Generated: "generated", Minted: "minted", User: "user", Source(9): "unknown"} {
		if got := s.String(); got != want {
			t.Errorf("Source(%d).String() = %q, want %q", int(s), got, want)
		}
	}
	for _, s := range []Source{Generated, Minted, User} {
		got, ok := ParseSource(s.String())
		if !ok || got != s {
			t.Errorf("ParseSource(%q) = %v, %v", s.String(), got, ok)
		}
	}
	if _, ok := ParseSource("banana"); ok {
		t.Error("ParseSource(banana) should fail")
	}
}

func TestGenerators(t *testing.T) {
	tests := []struct {
		name string
		gen  Generator
		want string
	}{
		{"base64url with prefix", Generator{Prefix: "sk-", Bytes: 32}, "sk-AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"},
		{"base64url", Generator{Bytes: 32}, "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"},
		{"hex", Generator{Bytes: 32, Encoding: Hex}, "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.gen.Generate(sequence(32))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("Generate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGeneratorShortRandomness(t *testing.T) {
	if _, err := (Generator{Bytes: 32}).Generate(sequence(4)); err == nil {
		t.Fatal("want error for short reader")
	}
}

func TestZeroGeneratorFails(t *testing.T) {
	if _, err := (Generator{}).Generate(sequence(32)); err == nil {
		t.Fatal("want error for the zero generator")
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"a", "anthropic-api-key", "k8s", "a-b-c", strings.Repeat("x", 63)}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", n, err)
		}
	}
	invalid := []string{"", "-a", "a-", "A", "a_b", "a b", "a.b", "é", strings.Repeat("x", 64)}
	for _, n := range invalid {
		if err := ValidateName(n); !errors.Is(err, ErrInvalidName) {
			t.Errorf("ValidateName(%q) = %v, want ErrInvalidName", n, err)
		}
	}
}

func TestValidateValue(t *testing.T) {
	if err := ValidateValue([]byte("x")); err != nil {
		t.Errorf("1 byte: %v", err)
	}
	if err := ValidateValue(bytes.Repeat([]byte{0}, MaxSecretLen)); err != nil {
		t.Errorf("%d bytes: %v", MaxSecretLen, err)
	}
	if err := ValidateValue(nil); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("empty: %v", err)
	}
	if err := ValidateValue(bytes.Repeat([]byte{0}, MaxSecretLen+1)); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("%d bytes: %v", MaxSecretLen+1, err)
	}
}

func TestBuiltinSecrets(t *testing.T) {
	want := map[string]struct {
		source    Source
		machines  []Role
		prefix    string
		encoding  Encoding
		rotatable bool
	}{
		"litellm-master-key":     {Generated, []Role{Services}, "sk-", Base64URL, true},
		"litellm-salt-key":       {Generated, []Role{Services}, "", Hex, false},
		"litellm-db-password":    {Generated, []Role{Services}, "", Base64URL, true},
		"openclaw-gateway-token": {Generated, []Role{Agent}, "", Base64URL, true},
		"openclaw-litellm-key":   {Minted, []Role{Agent}, "", Base64URL, true},
	}
	got := BuiltinSecrets()
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for _, s := range got {
		w, ok := want[s.Name]
		if !ok {
			t.Errorf("unexpected entry %q", s.Name)
			continue
		}
		if err := ValidateName(s.Name); err != nil {
			t.Errorf("%s: %v", s.Name, err)
		}
		if s.Source != w.source || s.Generator.Prefix != w.prefix || s.Generator.Encoding != w.encoding || s.Rotatable != w.rotatable {
			t.Errorf("%s = %+v", s.Name, s)
		}
		if len(s.Machines) != 1 || s.Machines[0] != w.machines[0] {
			t.Errorf("%s machines = %v, want %v", s.Name, s.Machines, w.machines)
		}
		if s.Source == Generated && s.Generator.Bytes != 32 {
			t.Errorf("%s generator bytes = %d, want 32", s.Name, s.Generator.Bytes)
		}
		if s.Source == Minted && s.Generator.Bytes != 0 {
			t.Errorf("%s minted entry must have no generator", s.Name)
		}
	}
}

func TestSecretSpecUsedBy(t *testing.T) {
	s := SecretSpec{Machines: []Role{Services, Agent}}
	if !s.UsedBy(Services) || !s.UsedBy(Agent) || s.UsedBy(Infra) {
		t.Fatalf("UsedBy wrong for %v", s.Machines)
	}
}
