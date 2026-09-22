package domain

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
)

// Source says how a secret's value comes to exist.
type Source int

const (
	Generated Source = iota // lclaw makes it with crypto/rand the first time up needs it
	Minted                  // lclaw obtains it from a running service
	User                    // supplied with `lclaw secrets set`
)

// String returns the lowercase name stored in the keychain comment and
// printed in output. These strings are part of the CLI's interface.
func (s Source) String() string {
	switch s {
	case Generated:
		return "generated"
	case Minted:
		return "minted"
	case User:
		return "user"
	default:
		return "unknown"
	}
}

// ParseSource is the inverse of String.
func ParseSource(s string) (Source, bool) {
	for _, src := range []Source{Generated, Minted, User} {
		if src.String() == s {
			return src, true
		}
	}
	return 0, false
}

// Encoding is how a generator renders random bytes.
type Encoding int

const (
	Base64URL Encoding = iota // RFC 4648 URL alphabet without padding
	Hex
)

// Generator makes a random value: Prefix, then Bytes random bytes in
// Encoding. The zero Generator means the entry has none.
type Generator struct {
	Prefix   string
	Bytes    int
	Encoding Encoding
}

// Generate reads Bytes bytes from random and encodes them. It is a pure
// function of the reader, so tests use a deterministic one.
func (g Generator) Generate(random io.Reader) ([]byte, error) {
	if g.Bytes <= 0 {
		return nil, errors.New("generate: entry has no generator")
	}
	raw := make([]byte, g.Bytes)
	if _, err := io.ReadFull(random, raw); err != nil {
		return nil, fmt.Errorf("generate: read randomness: %w", err)
	}
	var enc string
	switch g.Encoding {
	case Hex:
		enc = hex.EncodeToString(raw)
	default:
		enc = base64.RawURLEncoding.EncodeToString(raw)
	}
	return []byte(g.Prefix + enc), nil
}

// SecretSpec is one catalogue entry: what the secret is called, how it comes
// to exist by default, which zones consume it, and whether it may be
// rotated. The salt key may not, because LiteLLM's stored credentials are
// encrypted with it.
type SecretSpec struct {
	Name      string
	Source    Source
	Zones     []Role
	Generator Generator
	Rotatable bool
}

// UsedBy reports whether the zone with role r consumes the secret.
func (s SecretSpec) UsedBy(r Role) bool {
	for _, z := range s.Zones {
		if z == r {
			return true
		}
	}
	return false
}

// SecretItem is what the keychain reports about one stored secret. It never
// carries the value.
type SecretItem struct {
	Name     string
	Source   Source
	Created  time.Time
	Modified time.Time
}

// MaxSecretLen bounds a value. It sits well under the security interactive
// mode line limit after hexadecimal encoding and is many times larger than
// any key in the catalogue.
const MaxSecretLen = 1024

const maxNameLen = 63

// namePattern is a lowercase DNS label, because the same string is the
// keychain account, the Podman secret name and the Kubernetes Secret name.
var namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

var (
	// ErrInvalidName is wrapped when a secret name is malformed.
	ErrInvalidName = errors.New("invalid secret name")
	// ErrInvalidValue is wrapped when a value is empty or too large.
	ErrInvalidValue = errors.New("invalid secret value")
	// ErrSecretNotFound is wrapped by the keychain adapter when an item is
	// absent, so the evaluation rule can distinguish "unset" from "broken".
	ErrSecretNotFound = errors.New("secret not found")
)

// ValidateName checks that name is a lowercase DNS label of at most 63
// characters.
func ValidateName(name string) error {
	if len(name) > maxNameLen || !namePattern.MatchString(name) {
		return fmt.Errorf("%w: %q must be a lowercase DNS label of at most %d characters", ErrInvalidName, name, maxNameLen)
	}
	return nil
}

// ValidateValue checks that v has between 1 and MaxSecretLen bytes.
func ValidateValue(v []byte) error {
	switch {
	case len(v) == 0:
		return fmt.Errorf("%w: empty", ErrInvalidValue)
	case len(v) > MaxSecretLen:
		return fmt.Errorf("%w: %d bytes is over the %d-byte limit", ErrInvalidValue, len(v), MaxSecretLen)
	}
	return nil
}

// BuiltinSecrets returns the entries for the workloads the deployment model
// defines. It returns fresh slices so callers may append to them.
func BuiltinSecrets() []SecretSpec {
	return []SecretSpec{
		{Name: "litellm-master-key", Source: Generated, Zones: []Role{Services}, Generator: Generator{Prefix: "sk-", Bytes: 32}, Rotatable: true},
		{Name: "litellm-salt-key", Source: Generated, Zones: []Role{Services}, Generator: Generator{Bytes: 32, Encoding: Hex}, Rotatable: false},
		{Name: "litellm-db-password", Source: Generated, Zones: []Role{Services}, Generator: Generator{Bytes: 32}, Rotatable: true},
		{Name: "openclaw-gateway-token", Source: Generated, Zones: []Role{Agent}, Generator: Generator{Bytes: 32}, Rotatable: true},
		{Name: "openclaw-litellm-key", Source: Minted, Zones: []Role{Agent}, Rotatable: true},
	}
}
