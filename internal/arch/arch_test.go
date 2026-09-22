// Package arch holds the import-boundary test and no production code. The
// test walks every package under the module with go/parser in ImportsOnly
// mode and checks each module-internal import against the rule table, so
// the layering is enforced by CI rather than by review.
package arch

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/programmablemike/localclaw"

// allowed maps a package (path relative to the module root; "" is the root
// package) to the module packages it may import. "*" means anything.
var allowed = map[string][]string{
	"":                           {},
	"internal/domain":            {},
	"internal/app":               {"internal/domain"},
	"internal/cli":               {"internal/app", "internal/domain"},
	"internal/adapters/exec":     {},
	"internal/adapters/flox":     {"internal/app", "internal/domain", "internal/adapters/exec"},
	"internal/adapters/keychain": {"internal/app", "internal/domain", "internal/adapters/exec"},
	"internal/adapters/litellm":  {"internal/app", "internal/domain", "internal/adapters/exec"},
	"internal/adapters/osfs":     {},
	"internal/adapters/podman":   {"internal/app", "internal/domain", "internal/adapters/exec"},
	"internal/adapters/toml":     {"internal/domain"},
	"internal/adapters/tty":      {},
	"cmd/lclaw":                  {"*"},
}

func TestImportBoundaries(t *testing.T) {
	root := moduleRoot(t)
	imports := map[string]map[string]bool{} // package -> module-internal imports

	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)
		if pkg == "." {
			pkg = ""
		}
		if imports[pkg] == nil {
			imports[pkg] = map[string]bool{}
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			switch {
			case p == module:
				imports[pkg][""] = true
			case strings.HasPrefix(p, module+"/"):
				imports[pkg][strings.TrimPrefix(p, module+"/")] = true
			}
		}
		return nil
	}
	if err := filepath.WalkDir(root, walk); err != nil {
		t.Fatal(err)
	}

	var problems []string
	for pkg, imps := range imports {
		rules, known := allowed[pkg]
		if !known {
			problems = append(problems, fmt.Sprintf("%s: no rule in the allowed table; add one", display(pkg)))
			continue
		}
		for imp := range imps {
			if !permitted(rules, imp) {
				problems = append(problems, fmt.Sprintf("%s imports %s", display(pkg), display(imp)))
			}
		}
	}
	for pkg := range allowed {
		if _, ok := imports[pkg]; !ok {
			problems = append(problems, fmt.Sprintf("rule for %s but no such package with non-test files", display(pkg)))
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}

func permitted(rules []string, imp string) bool {
	for _, r := range rules {
		if r == "*" || r == imp {
			return true
		}
	}
	return false
}

func display(pkg string) string {
	if pkg == "" {
		return "(root package)"
	}
	return pkg
}

// moduleRoot walks up from the test's working directory to the go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
