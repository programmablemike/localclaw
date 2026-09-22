package domain

// MachineName is the one Podman machine LocalClaw runs, and the name of
// the remote connection that reaches it.
const MachineName = "lclaw"

// Role names one of the three zones. A zone is one Podman network on the
// machine and the workloads placed on it. It is the seed of the topology
// model; the single-machine design grows it.
type Role int

const (
	Infra Role = iota
	Services
	Agent
)

// Roles returns every role in the order zones are applied and reported.
// Down runs them in reverse.
func Roles() []Role { return []Role{Infra, Services, Agent} }

// String returns the short role name used in configuration and output.
func (r Role) String() string {
	switch r {
	case Infra:
		return "infra"
	case Services:
		return "services"
	case Agent:
		return "agent"
	default:
		return "unknown"
	}
}

// NetworkName returns the Podman network name for the zone.
func (r Role) NetworkName() string { return "lclaw-" + r.String() }

// ParseRole is the inverse of Role.String.
func ParseRole(s string) (Role, bool) {
	for _, r := range Roles() {
		if r.String() == s {
			return r, true
		}
	}
	return 0, false
}

// SelectRoles turns zone names from a command line into roles in the
// fixed zone order, regardless of the order given. No names means every
// role. An unknown or repeated name is an error naming it.
func SelectRoles(names []string) ([]Role, error) {
	if len(names) == 0 {
		return Roles(), nil
	}
	seen := map[Role]bool{}
	for _, n := range names {
		r, ok := ParseRole(n)
		if !ok {
			return nil, &UnknownZoneError{Name: n}
		}
		if seen[r] {
			return nil, &UnknownZoneError{Name: n, Repeated: true}
		}
		seen[r] = true
	}
	var out []Role
	for _, r := range Roles() {
		if seen[r] {
			out = append(out, r)
		}
	}
	return out, nil
}

// UnknownZoneError reports a zone name that is not a role, or one given
// twice.
type UnknownZoneError struct {
	Name     string
	Repeated bool
}

func (e *UnknownZoneError) Error() string {
	if e.Repeated {
		return "zone " + e.Name + " is named twice"
	}
	return "unknown zone " + e.Name + "; the zones are " + roleNames()
}

// Machine is what the runtime reports about one Podman machine. Only the
// fields the rules need are kept.
type Machine struct {
	Name    string
	Running bool
}
