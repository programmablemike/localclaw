package exec

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestFakeReturnsScriptedResponse(t *testing.T) {
	f := NewFake()
	f.Script("podman", []string{"--version"}, Response{Stdout: "podman version 5.8.4\n"})
	res, err := f.Run(context.Background(), "podman", "--version")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(res.Stdout); got != "podman version 5.8.4\n" {
		t.Errorf("stdout = %q", got)
	}
	want := []Call{{Name: "podman", Args: []string{"--version"}}}
	if !reflect.DeepEqual(f.Calls, want) {
		t.Errorf("Calls = %+v, want %+v", f.Calls, want)
	}
}

func TestFakeReturnsScriptedError(t *testing.T) {
	f := NewFake()
	f.Script("flox", []string{"--version"}, Response{Err: ErrNotFound})
	_, err := f.Run(context.Background(), "flox", "--version")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFakeFailsOnUnscriptedCall(t *testing.T) {
	f := NewFake()
	_, err := f.Run(context.Background(), "podman", "machine", "list")
	if err == nil || err.Error() != `exec.Fake: no response scripted for "podman machine list"` {
		t.Fatalf("err = %v", err)
	}
}

func TestFakeHonoursCancelledContext(t *testing.T) {
	f := NewFake()
	f.Script("podman", nil, Response{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.Run(ctx, "podman")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
