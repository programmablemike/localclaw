package domain

import "errors"

// EvaluateKeychain turns an observation of the keychain file into a Check.
func EvaluateKeychain(path string, exists bool, err error) Check {
	c := Check{Name: "keychain"}
	switch {
	case err != nil:
		c.Status, c.Summary = Fail, err.Error()
	case !exists:
		c.Status, c.Summary, c.Hint = Fail, "not found at "+path, "run `lclaw init` to create it"
	default:
		c.Status, c.Summary = Pass, path
	}
	return c
}

// EvaluateSecret turns the result of describing a declared secret, read
// without touching its value, into a Check.
func EvaluateSecret(spec SecretSpec, err error) Check {
	c := Check{Name: spec.Name}
	switch {
	case errors.Is(err, ErrSecretNotFound):
		c.Status, c.Summary, c.Hint = Fail, "unset", "run `lclaw secrets set "+spec.Name+"`"
	case err != nil:
		c.Status, c.Summary = Fail, err.Error()
	default:
		c.Status, c.Summary = Pass, "set"
	}
	return c
}
