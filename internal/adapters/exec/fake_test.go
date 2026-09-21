package exec

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestFakeReturnsScriptedResponse(t *testing.T) {
	f := NewFake()
	f.Script("podman", []string{"--version"}, Response{Stdout: "podman version 5.8.4\n"})
	res, err := f.Run(context.Background(), Command{Name: "podman", Args: []string{"--version"}})
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
	_, err := f.Run(context.Background(), Command{Name: "flox", Args: []string{"--version"}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFakeFailsOnUnscriptedCall(t *testing.T) {
	f := NewFake()
	_, err := f.Run(context.Background(), Command{Name: "podman", Args: []string{"machine", "list"}})
	if err == nil || err.Error() != `exec.Fake: no response scripted for "podman machine list"` {
		t.Fatalf("err = %v", err)
	}
}

func TestFakeHonoursCancelledContext(t *testing.T) {
	f := NewFake()
	f.Script("podman", nil, Response{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.Run(ctx, Command{Name: "podman"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestFakeRecordsStdinAndSensitive(t *testing.T) {
	f := NewFake()
	f.Script("security", []string{"-i"}, Response{})
	_, err := f.Run(context.Background(), Command{
		Name:      "security",
		Args:      []string{"-i"},
		Stdin:     strings.NewReader("add-generic-password -a x\n"),
		Sensitive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Call{{Name: "security", Args: []string{"-i"}, Stdin: "add-generic-password -a x\n", Sensitive: true}}
	if !reflect.DeepEqual(f.Calls, want) {
		t.Errorf("Calls = %+v, want %+v", f.Calls, want)
	}
}

func TestFakeResponseSequence(t *testing.T) {
	f := NewFake()
	f.Script("security", []string{"find"}, Response{Err: &ExitError{Code: 36}}, Response{Stdout: "ok"})
	if _, err := f.Run(context.Background(), Command{Name: "security", Args: []string{"find"}}); err == nil {
		t.Fatal("first call: want the scripted exit error")
	}
	for i := 0; i < 2; i++ {
		res, err := f.Run(context.Background(), Command{Name: "security", Args: []string{"find"}})
		if err != nil || string(res.Stdout) != "ok" {
			t.Fatalf("call %d: res=%q err=%v, want ok (last response repeats)", i+2, res.Stdout, err)
		}
	}
}

func TestFakeInteractive(t *testing.T) {
	f := NewFake()
	f.Script("security", []string{"create-keychain", "/k"}, Response{Err: &ExitError{Code: 1}})
	err := f.Interactive(context.Background(), Command{Name: "security", Args: []string{"create-keychain", "/k"}})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 1 {
		t.Fatalf("err = %v, want ExitError 1", err)
	}
	want := []Call{{Name: "security", Args: []string{"create-keychain", "/k"}, Interactive: true}}
	if !reflect.DeepEqual(f.Calls, want) {
		t.Errorf("Calls = %+v, want %+v", f.Calls, want)
	}
}
