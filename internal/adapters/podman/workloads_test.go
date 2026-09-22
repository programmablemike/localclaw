package podman

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/domain"
)

func TestNetworkExists(t *testing.T) {
	args := []string{"--connection", "lclaw", "network", "exists", "lclaw-agent"}
	for _, tt := range []struct {
		resp exec.Response
		want bool
		err  bool
	}{
		{exec.Response{}, true, false},
		{exec.Response{Err: &exec.ExitError{Code: 1}}, false, false},
		{exec.Response{Err: &exec.ExitError{Code: 125, Stderr: "cannot connect"}}, false, true},
	} {
		f := exec.NewFake()
		f.Script("podman", args, tt.resp)
		got, err := (&Client{Runner: f}).NetworkExists(context.Background(), "lclaw-agent")
		if (err != nil) != tt.err || got != tt.want {
			t.Errorf("resp %+v: got %v, %v", tt.resp, got, err)
		}
	}
}

func TestCreateNetwork(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--connection", "lclaw", "network", "create", "lclaw-infra"}, exec.Response{Stdout: "lclaw-infra\n"})
	f.Script("podman", []string{"--connection", "lclaw", "network", "create", "--internal", "lclaw-agent"}, exec.Response{Stdout: "lclaw-agent\n"})
	c := &Client{Runner: f}
	if err := c.CreateNetwork(context.Background(), "lclaw-infra", false); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateNetwork(context.Background(), "lclaw-agent", true); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 2 {
		t.Fatalf("calls = %+v", f.Calls)
	}
}

func TestRemoveNetwork(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--connection", "lclaw", "network", "rm", "lclaw-agent"},
		exec.Response{Err: &exec.ExitError{Code: 2, Stderr: `Error: "lclaw-agent" has associated containers with it`}})
	err := (&Client{Runner: f}).RemoveNetwork(context.Background(), "lclaw-agent")
	if err == nil || !strings.HasPrefix(err.Error(), "podman: remove network lclaw-agent: exit status 2") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildIsQuiet(t *testing.T) {
	f := exec.NewFake()
	args := []string{"--connection", "lclaw", "build", "--quiet", "--tag", "localhost/lclaw/openclaw:latest", "/d/workloads/openclaw"}
	f.Script("podman", args, exec.Response{Stdout: "sha256:abc\n"})
	if err := (&Client{Runner: f}).Build(context.Background(), "localhost/lclaw/openclaw:latest", "/d/workloads/openclaw"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.Calls[0].Args, args) {
		t.Fatalf("args = %v", f.Calls[0].Args)
	}
}

func TestBuildFailureKeepsTheLogTail(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--connection", "lclaw", "build", "--quiet", "--tag", "t", "/c"},
		exec.Response{Err: &exec.ExitError{Code: 1, Stderr: "STEP 1/1: FROM x\nError: creating build container: no such image"}})
	err := (&Client{Runner: f}).Build(context.Background(), "t", "/c")
	if err == nil || !strings.Contains(err.Error(), "no such image") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlayArgv(t *testing.T) {
	f := exec.NewFake()
	args := []string{"--connection", "lclaw", "kube", "play", "--replace", "--network", "lclaw-services", "--network", "lclaw-agent", "--userns", "auto", "/d/workloads/agentgateway/pod.yaml"}
	f.Script("podman", args, exec.Response{})
	if err := (&Client{Runner: f}).Play(context.Background(), "/d/workloads/agentgateway/pod.yaml", []string{"lclaw-services", "lclaw-agent"}, "auto"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.Calls[0].Args, args) {
		t.Fatalf("args = %v", f.Calls[0].Args)
	}

	f = exec.NewFake()
	args = []string{"--connection", "lclaw", "kube", "play", "--replace", "--network", "lclaw-infra", "/d/workloads/gateway/pod.yaml"}
	f.Script("podman", args, exec.Response{})
	if err := (&Client{Runner: f}).Play(context.Background(), "/d/workloads/gateway/pod.yaml", []string{"lclaw-infra"}, ""); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.Calls[0].Args, args) {
		t.Fatalf("args = %v, no --userns wanted", f.Calls[0].Args)
	}
}

func TestDownArgv(t *testing.T) {
	f := exec.NewFake()
	args := []string{"--connection", "lclaw", "kube", "down", "/d/workloads/openclaw/pod.yaml"}
	f.Script("podman", args, exec.Response{})
	if err := (&Client{Runner: f}).Down(context.Background(), "/d/workloads/openclaw/pod.yaml"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.Calls[0].Args, args) {
		t.Fatalf("args = %v, --force must never be passed", f.Calls[0].Args)
	}
}

func TestListPods(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--connection", "lclaw", "pod", "ps", "--filter", "label=app.kubernetes.io/part-of=localclaw", "--format", "json"},
		exec.Response{Stdout: fixture(t, "pod-ps.json")})
	got, err := (&Client{Runner: f}).ListPods(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Pod{
		{Name: "litellm", Containers: []domain.Container{{Name: "litellm-litellm", State: "running"}}},
		{Name: "openclaw", Containers: []domain.Container{{Name: "openclaw-openclaw", State: "exited"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListPods() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestListPodsEmpty(t *testing.T) {
	for _, out := range []string{"[]\n", "", "\n"} {
		f := exec.NewFake()
		f.Script("podman", []string{"--connection", "lclaw", "pod", "ps", "--filter", "label=app.kubernetes.io/part-of=localclaw", "--format", "json"}, exec.Response{Stdout: out})
		got, err := (&Client{Runner: f}).ListPods(context.Background())
		if err != nil || len(got) != 0 {
			t.Fatalf("output %q: %v, %v", out, got, err)
		}
	}
}

func TestListPodsBadJSON(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--connection", "lclaw", "pod", "ps", "--filter", "label=app.kubernetes.io/part-of=localclaw", "--format", "json"}, exec.Response{Stdout: "{nope"})
	if _, err := (&Client{Runner: f}).ListPods(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "podman: list pods: decode: ") {
		t.Fatalf("err = %v", err)
	}
}
