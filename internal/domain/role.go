package domain

// Role names one of the three Podman machines. It is the seed of the
// topology model; the deployment design grows it.
type Role int

const (
	Infra Role = iota
	Services
	Agent
)

// Roles returns every role in the order machines are created and reported.
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

// MachineName returns the Podman machine name for the role.
func (r Role) MachineName() string { return "lclaw-" + r.String() }

// Machine is what the runtime reports about one Podman machine. Only the
// fields the rules need are kept.
type Machine struct {
	Name    string
	Running bool
}
