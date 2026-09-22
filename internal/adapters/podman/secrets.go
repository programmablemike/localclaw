package podman

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/domain"
)

// partOfLabel marks every secret lclaw stores, so purge can find them by
// label rather than by catalogue.
const (
	partOfKey   = "app.kubernetes.io/part-of"
	partOfValue = "localclaw"
)

// connection prefixes args with the remote connection for role.
func connection(role domain.Role, args ...string) []string {
	return append([]string{"--connection", role.MachineName()}, args...)
}

// MachineRunning implements app.SecretTarget from the machine list. A
// machine that does not exist is not running.
func (c *Client) MachineRunning(ctx context.Context, role domain.Role) (bool, error) {
	machines, err := c.ListMachines(ctx)
	if err != nil {
		return false, err
	}
	for _, m := range machines {
		if m.Name == role.MachineName() {
			return m.Running, nil
		}
	}
	return false, nil
}

// StoreSecret wraps value as a Kubernetes Secret, the only shape kube play
// accepts, and pipes it to secret create --replace. The value travels on
// stdin and the command is sensitive.
func (c *Client) StoreSecret(ctx context.Context, role domain.Role, name string, value []byte) error {
	body, err := kubeSecret(name, value)
	if err != nil {
		return fmt.Errorf("podman: store secret %s: %w", name, err)
	}
	_, err = c.Runner.Run(ctx, exec.Command{
		Name:      "podman",
		Args:      connection(role, "secret", "create", "--replace", "--label", partOfKey+"="+partOfValue, name, "-"),
		Stdin:     bytes.NewReader(body),
		Sensitive: true,
	})
	if err != nil {
		// Sensitive only suppresses the exec adapter's debug log; on a
		// stdin parse failure podman may echo part of the Kubernetes
		// Secret body back on stderr, and that body carries the
		// base64-encoded value.
		return wrap("store secret "+name, exec.Redact(err, base64.StdEncoding.EncodeToString(value)))
	}
	return nil
}

// kubeSecret renders the Kubernetes Secret object with the single key
// "value". Nothing outside this package knows this shape.
func kubeSecret(name string, value []byte) ([]byte, error) {
	doc := struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Type string            `json:"type"`
		Data map[string]string `json:"data"`
	}{
		APIVersion: "v1",
		Kind:       "Secret",
		Type:       "Opaque",
		Data:       map[string]string{"value": base64.StdEncoding.EncodeToString(value)},
	}
	doc.Metadata.Name = name
	return json.Marshal(doc)
}

// SecretExists implements app.SecretTarget with secret exists, which exits
// 0 when present and 1 when absent.
func (c *Client) SecretExists(ctx context.Context, role domain.Role, name string) (bool, error) {
	_, err := c.run(ctx, connection(role, "secret", "exists", name)...)
	return existsResult("secret exists "+name, err)
}

// RemoveSecret implements app.SecretTarget.
func (c *Client) RemoveSecret(ctx context.Context, role domain.Role, name string) error {
	if _, err := c.run(ctx, connection(role, "secret", "rm", name)...); err != nil {
		return wrap("remove secret "+name, err)
	}
	return nil
}

// ListSecrets implements app.SecretTarget. secret ls filters only by name
// and id, so the adapter lists every id, inspects them, and keeps the ones
// carrying lclaw's label. Only the name and labels are decoded.
func (c *Client) ListSecrets(ctx context.Context, role domain.Role) ([]string, error) {
	res, err := c.run(ctx, connection(role, "secret", "ls", "--quiet")...)
	if err != nil {
		return nil, wrap("list secrets", err)
	}
	ids := strings.Fields(string(res.Stdout))
	if len(ids) == 0 {
		return []string{}, nil
	}
	res, err = c.run(ctx, connection(role, append([]string{"secret", "inspect"}, ids...)...)...)
	if err != nil {
		return nil, wrap("inspect secrets", err)
	}
	var rows []struct {
		Spec struct {
			Name   string            `json:"Name"`
			Labels map[string]string `json:"Labels"`
		} `json:"Spec"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(res.Stdout), &rows); err != nil {
		return nil, fmt.Errorf("podman: inspect secrets: decode: %w", err)
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Spec.Labels[partOfKey] == partOfValue {
			names = append(names, r.Spec.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// RemoveVolume implements app.SecretTarget. A secret volume created by kube
// play is a named volume with the secret's name; removing it after kube
// down leaves nothing of the value on the machine's disk.
func (c *Client) RemoveVolume(ctx context.Context, role domain.Role, name string) error {
	_, err := c.run(ctx, connection(role, "volume", "exists", name)...)
	exists, err := existsResult("volume exists "+name, err)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if _, err := c.run(ctx, connection(role, "volume", "rm", name)...); err != nil {
		return wrap("remove volume "+name, err)
	}
	return nil
}

// existsResult maps a podman `exists` subcommand's exit status: 0 is true,
// 1 is false, anything else is an error.
func existsResult(op string, err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.Code == 1 {
		return false, nil
	}
	return false, wrap(op, err)
}
