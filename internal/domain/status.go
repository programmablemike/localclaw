package domain

import "fmt"

// Pod is what the runtime reports about one pod on the machine. Only the
// fields the rules need are kept: the pod's name, each container's state
// as Podman words it ("running", "exited", "created", ...), and the ports
// the pod publishes on the host.
type Pod struct {
	Name       string
	Containers []Container
	Ports      []PortBinding
}

// Container is one container's name and state inside a Pod.
type Container struct {
	Name  string
	State string
}

// Running reports whether every container in the pod is running. A pod
// with no containers is not running.
func (p Pod) Running() bool {
	if len(p.Containers) == 0 {
		return false
	}
	for _, c := range p.Containers {
		if c.State != "running" {
			return false
		}
	}
	return true
}

// EvaluateMachine turns the machine list into the single machine check:
// Pass when the machine exists (summary says whether it is running), Warn
// when it does not. It also reports whether the machine is running, so
// callers can decide whether to ask about networks and pods.
func EvaluateMachine(found []Machine) (Check, bool) {
	c := Check{Name: "machine"}
	for _, m := range found {
		if m.Name != MachineName {
			continue
		}
		c.Status = Pass
		if m.Running {
			c.Summary = "running"
			return c, true
		}
		c.Summary, c.Hint = "stopped", "run `lclaw up`"
		return c, false
	}
	c.Status, c.Summary, c.Hint = Warn, "not created", "run `lclaw up`"
	return c, false
}

// EvaluateNetworks emits one check per selected zone: Pass when its
// network exists, Warn when it does not. An internal zone says so.
func EvaluateNetworks(t Topology, roles []Role, present map[string]bool) []Check {
	checks := make([]Check, 0, len(roles))
	for _, r := range roles {
		c := Check{Name: "network/" + r.String()}
		z, _ := t.Zone(r)
		if present[r.NetworkName()] {
			c.Status, c.Summary = Pass, "present"
			if z.Internal {
				c.Summary += " (internal)"
			}
		} else {
			c.Status, c.Summary, c.Hint = Warn, "missing", "run `lclaw up "+r.String()+"`"
		}
		checks = append(checks, c)
	}
	return checks
}

// EvaluateWorkloads emits one check per workload of the selected zones:
// Pass when its pod is up with every container running, Fail when the pod
// exists but a container is not running, Warn when there is no such pod.
func EvaluateWorkloads(t Topology, roles []Role, pods []Pod) []Check {
	byName := make(map[string]Pod, len(pods))
	for _, p := range pods {
		byName[p.Name] = p
	}
	var checks []Check
	for _, r := range roles {
		z, ok := t.Zone(r)
		if !ok {
			continue
		}
		for _, w := range z.Workloads {
			c := Check{Name: r.String() + "/" + string(w)}
			p, ok := byName[string(w)]
			switch {
			case !ok:
				c.Status, c.Summary, c.Hint = Warn, "absent", "run `lclaw up "+r.String()+"`"
			case p.Running():
				c.Status, c.Summary = Pass, "running"
			default:
				c.Status = Fail
				c.Summary = "degraded: " + degraded(p)
				c.Hint = fmt.Sprintf("podman --connection %s pod logs %s", MachineName, p.Name)
			}
			checks = append(checks, c)
		}
	}
	return checks
}

// degraded names the containers that are not running.
func degraded(p Pod) string {
	if len(p.Containers) == 0 {
		return "no containers"
	}
	var out string
	for _, c := range p.Containers {
		if c.State == "running" {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += "container " + c.Name + " " + c.State
	}
	return out
}
