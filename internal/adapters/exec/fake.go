package exec

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Call records one invocation of Fake.Run or Fake.Interactive. Stdin holds
// everything the fake read from Command.Stdin, so a test can assert the
// exact bytes sent to a child.
type Call struct {
	Name        string
	Args        []string
	Stdin       string
	Sensitive   bool
	Interactive bool
}

// Response is what Fake returns for a scripted command. Err is returned as
// given, so tests can script ErrNotFound or an *ExitError directly.
type Response struct {
	Stdout, Stderr string
	Err            error
}

// Fake is a Runner scripted by exact argv. It records every call. A call
// with no script fails loudly so a test cannot pass by accident. When
// several responses are scripted for one argv they are returned in order and
// the last one repeats, which lets a test script "locked, then unlocked".
type Fake struct {
	mu        sync.Mutex
	responses map[string][]Response
	Calls     []Call
}

// NewFake returns an empty Fake.
func NewFake() *Fake {
	return &Fake{responses: map[string][]Response{}}
}

// Script sets the responses for name with exactly args.
func (f *Fake) Script(name string, args []string, responses ...Response) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[key(name, args)] = append([]Response(nil), responses...)
}

// Run implements Runner.
func (f *Fake) Run(ctx context.Context, c Command) (Result, error) {
	var stdin string
	if c.Stdin != nil {
		b, err := io.ReadAll(c.Stdin)
		if err != nil {
			return Result{}, fmt.Errorf("exec.Fake: read stdin: %w", err)
		}
		stdin = string(b)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Name: c.Name, Args: append([]string(nil), c.Args...), Stdin: stdin, Sensitive: c.Sensitive})
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	r, err := f.next(c)
	if err != nil {
		return Result{}, err
	}
	return Result{Stdout: []byte(r.Stdout), Stderr: []byte(r.Stderr)}, r.Err
}

// Interactive implements Runner. Only the scripted error is returned.
func (f *Fake) Interactive(ctx context.Context, c Command) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Name: c.Name, Args: append([]string(nil), c.Args...), Interactive: true})
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := f.next(c)
	if err != nil {
		return err
	}
	return r.Err
}

// next pops the next scripted response for c, keeping the last one. The
// caller holds f.mu.
func (f *Fake) next(c Command) (Response, error) {
	k := key(c.Name, c.Args)
	rs, ok := f.responses[k]
	if !ok || len(rs) == 0 {
		return Response{}, fmt.Errorf("exec.Fake: no response scripted for %q", k)
	}
	r := rs[0]
	if len(rs) > 1 {
		f.responses[k] = rs[1:]
	}
	return r, nil
}

func key(name string, args []string) string {
	return strings.Join(append([]string{name}, args...), " ")
}
