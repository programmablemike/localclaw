package podman

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
)

const providerLibkrun = "CONTAINERS_MACHINE_PROVIDER=libkrun"

func TestInitMachineArgv(t *testing.T) {
	f := exec.NewFake()
	args := []string{"machine", "init", "lclaw", "--cpus", "4", "--memory", "8192", "--disk-size", "60", "--playbook", "machine/playbook.yaml", "--volume", ""}
	f.Script("podman", args, exec.Response{})
	err := (&Client{Runner: f}).InitMachine(context.Background(), app.MachineInit{
		Name: "lclaw", Provider: "libkrun", CPUs: 4, MemoryMiB: 8192, DiskGiB: 60, Playbook: "machine/playbook.yaml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.Calls[0].Args, args) || !reflect.DeepEqual(f.Calls[0].Env, []string{providerLibkrun}) {
		t.Fatalf("call = %+v", f.Calls[0])
	}
}

func TestInitMachineVolumes(t *testing.T) {
	f := exec.NewFake()
	args := []string{"machine", "init", "lclaw", "--cpus", "1", "--memory", "1", "--disk-size", "1", "--playbook", "p", "--volume", "/a:/b", "--volume", "/c:/d"}
	f.Script("podman", args, exec.Response{})
	err := (&Client{Runner: f}).InitMachine(context.Background(), app.MachineInit{
		Name: "lclaw", Provider: "applehv", CPUs: 1, MemoryMiB: 1, DiskGiB: 1, Playbook: "p", Volumes: []string{"/a:/b", "/c:/d"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.Calls[0].Args, args) {
		t.Fatalf("args = %v", f.Calls[0].Args)
	}
	if !reflect.DeepEqual(f.Calls[0].Env, []string{"CONTAINERS_MACHINE_PROVIDER=applehv"}) {
		t.Fatalf("env = %v", f.Calls[0].Env)
	}
}

func TestInitMachineError(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"machine", "init", "lclaw", "--cpus", "1", "--memory", "1", "--disk-size", "1", "--playbook", "p", "--volume", ""},
		exec.Response{Err: &exec.ExitError{Code: 125, Stderr: "Error: machine lclaw already exists"}})
	err := (&Client{Runner: f}).InitMachine(context.Background(), app.MachineInit{Name: "lclaw", CPUs: 1, MemoryMiB: 1, DiskGiB: 1, Playbook: "p"})
	if err == nil || !strings.HasPrefix(err.Error(), "podman: init machine lclaw: exit status 125") {
		t.Fatalf("err = %v", err)
	}
}

func TestStartStopRemoveMachine(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"machine", "start", "--no-info", "lclaw"}, exec.Response{})
	f.Script("podman", []string{"machine", "stop", "lclaw"}, exec.Response{})
	f.Script("podman", []string{"machine", "rm", "--force", "lclaw"}, exec.Response{})
	c := &Client{Runner: f}
	if err := c.StartMachine(context.Background(), "libkrun", "lclaw"); err != nil {
		t.Fatal(err)
	}
	if err := c.StopMachine(context.Background(), "libkrun", "lclaw"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveMachine(context.Background(), "libkrun", "lclaw"); err != nil {
		t.Fatal(err)
	}
	for i, call := range f.Calls {
		if !reflect.DeepEqual(call.Env, []string{providerLibkrun}) {
			t.Errorf("call %d env = %v, want the provider", i, call.Env)
		}
	}
	if len(f.Calls) != 3 {
		t.Fatalf("calls = %+v", f.Calls)
	}
}

func TestStartMachineAlreadyRunningIsAnError(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"machine", "start", "--no-info", "lclaw"},
		exec.Response{Err: &exec.ExitError{Code: 125, Stderr: `Error: unable to start "lclaw": already running`}})
	err := (&Client{Runner: f}).StartMachine(context.Background(), "libkrun", "lclaw")
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err = %v", err)
	}
}

func TestMachineNotFoundTool(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"machine", "stop", "lclaw"}, exec.Response{Err: exec.ErrNotFound})
	if err := (&Client{Runner: f}).StopMachine(context.Background(), "", "lclaw"); !errors.Is(err, domain.ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
	if len(f.Calls[0].Env) != 0 {
		t.Fatalf("an empty provider must not set the environment: %v", f.Calls[0].Env)
	}
}
