package exec

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Call records one invocation of Fake.Run.
type Call struct {
	Name string
	Args []string
}

// Response is what Fake.Run returns for a scripted command. Err is returned
// as given, so tests can script ErrNotFound or an *ExitError directly.
type Response struct {
	Stdout, Stderr string
	Err            error
}

// Fake is a Runner scripted by exact argv. It records every call. A call
// with no script fails loudly so a test cannot pass by accident.
type Fake struct {
	mu        sync.Mutex
	responses map[string]Response
	Calls     []Call
}

// NewFake returns an empty Fake.
func NewFake() *Fake {
	return &Fake{responses: map[string]Response{}}
}

// Script sets the response for name with exactly args.
func (f *Fake) Script(name string, args []string, r Response) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[key(name, args)] = r
}

// Run implements Runner.
func (f *Fake) Run(ctx context.Context, name string, args ...string) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Name: name, Args: append([]string(nil), args...)})
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	r, ok := f.responses[key(name, args)]
	if !ok {
		return Result{}, fmt.Errorf("exec.Fake: no response scripted for %q", key(name, args))
	}
	return Result{Stdout: []byte(r.Stdout), Stderr: []byte(r.Stderr)}, r.Err
}

func key(name string, args []string) string {
	return strings.Join(append([]string{name}, args...), " ")
}
