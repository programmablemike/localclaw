package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestEvaluateKeychain(t *testing.T) {
	path := "/Users/me/Library/Keychains/lclaw.keychain-db"
	tests := []struct {
		name   string
		exists bool
		err    error
		want   Check
	}{
		{"exists", true, nil, Check{Name: "keychain", Status: Pass, Summary: path}},
		{"missing", false, nil, Check{Name: "keychain", Status: Fail, Summary: "not found at " + path, Hint: "run `lclaw init` to create it"}},
		{"error", false, errors.New("keychain: stat: permission denied"), Check{Name: "keychain", Status: Fail, Summary: "keychain: stat: permission denied"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EvaluateKeychain(path, tt.exists, tt.err); got != tt.want {
				t.Fatalf("EvaluateKeychain() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestEvaluateSecret(t *testing.T) {
	spec := SecretSpec{Name: "anthropic-api-key", Source: User}
	tests := []struct {
		name string
		err  error
		want Check
	}{
		{"set", nil, Check{Name: "anthropic-api-key", Status: Pass, Summary: "set"}},
		{"unset", fmt.Errorf("keychain: describe: %w", ErrSecretNotFound), Check{Name: "anthropic-api-key", Status: Fail, Summary: "unset", Hint: "run `lclaw secrets set anthropic-api-key`"}},
		{"error", errors.New("keychain: describe: exit status 36"), Check{Name: "anthropic-api-key", Status: Fail, Summary: "keychain: describe: exit status 36"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EvaluateSecret(spec, tt.err); got != tt.want {
				t.Fatalf("EvaluateSecret() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
