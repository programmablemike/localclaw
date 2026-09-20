package localclaw

import (
	"strings"
	"testing"
)

func TestVersionIsEmbedded(t *testing.T) {
	v := strings.TrimSpace(Version)
	if v == "" {
		t.Fatal("Version is empty; is VERSION embedded?")
	}
	if strings.ContainsAny(v, " \t") {
		t.Fatalf("Version %q contains whitespace", v)
	}
}
