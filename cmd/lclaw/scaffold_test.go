package main

import (
	"bufio"
	"bytes"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw/internal/adapters/toml"
	"github.com/programmablemike/localclaw/internal/domain"
)

// fromLine matches the one instruction a default Containerfile may hold: a
// FROM pinned by tag and digest.
var fromLine = regexp.MustCompile(`^FROM [a-z0-9./-]+:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$`)

func loadScaffold(t *testing.T) (fs.FS, domain.Topology) {
	t.Helper()
	fsys := scaffoldFS()
	top, err := (toml.Loader{}).Load(fsys)
	if err != nil {
		t.Fatalf("embedded lclaw.toml: %v", err)
	}
	return fsys, top
}

func read(t *testing.T, fsys fs.FS, path string) []byte {
	t.Helper()
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return data
}

// hasLine reports whether data contains line exactly, ignoring a trailing CR.
func hasLine(data []byte, line string) bool {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if strings.TrimRight(sc.Text(), "\r") == line {
			return true
		}
	}
	return false
}

// documents splits a multi-document YAML file on its "---" separator lines.
func documents(data []byte) [][]byte {
	return bytes.Split(data, []byte("\n---\n"))
}

// metadataName returns the text after the first line in doc that begins
// with exactly two spaces then "name: ", or "" if there is none.
func metadataName(doc []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(doc))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.HasPrefix(line, "  name: ") {
			return strings.TrimPrefix(line, "  name: ")
		}
	}
	return ""
}

func TestScaffoldTopologyIsValid(t *testing.T) {
	fsys, top := loadScaffold(t)
	exists := func(p string) bool {
		_, err := fs.Stat(fsys, p)
		return err == nil
	}
	if findings := domain.Validate(top, exists); len(findings) != 0 {
		t.Fatalf("embedded topology has findings: %v", findings)
	}
	if got := len(top.Workloads()); got != 6 {
		t.Fatalf("%d workloads, want 6", got)
	}
	if got := len(top.Zones); got != 3 {
		t.Fatalf("%d zones, want 3", got)
	}
	agent, _ := top.Zone(domain.Agent)
	if !agent.Internal {
		t.Fatal("the agent zone must be internal")
	}
}

func TestScaffoldContainerfilesAreOnePinnedFrom(t *testing.T) {
	fsys, top := loadScaffold(t)
	for _, w := range top.Workloads() {
		path := w.Dir() + "/Containerfile"
		froms := 0
		sc := bufio.NewScanner(bytes.NewReader(read(t, fsys, path)))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			switch {
			case line == "" || strings.HasPrefix(line, "#"):
			case fromLine.MatchString(line):
				froms++
			default:
				t.Errorf("%s: unexpected line %q; a default Containerfile holds comments and one FROM pinned by tag and digest", path, line)
			}
		}
		if froms != 1 {
			t.Errorf("%s: %d FROM lines, want exactly 1", path, froms)
		}
	}
}

func TestScaffoldPodFilesKeepTheConventions(t *testing.T) {
	fsys, top := loadScaffold(t)
	infra := map[domain.Workload]bool{}
	if z, ok := top.Zone(domain.Infra); ok {
		for _, w := range z.Workloads {
			infra[w] = true
		}
	}
	for _, w := range top.Workloads() {
		path := w.Dir() + "/pod.yaml"
		data := read(t, fsys, path)
		name := string(w)
		for _, want := range []string{
			"kind: Pod",
			"  name: " + name,
			"    app.kubernetes.io/name: " + name,
			"    app.kubernetes.io/part-of: localclaw",
			"  restartPolicy: Always",
			"      image: localhost/lclaw/" + name + ":latest",
			"      imagePullPolicy: Never",
		} {
			if !hasLine(data, want) {
				t.Errorf("%s: missing line %q", path, want)
			}
		}
		if bytes.Contains(data, []byte("kind: Secret")) {
			t.Errorf("%s: contains a Secret document; secrets are referenced by name only", path)
		}
		if bytes.Contains(data, []byte("hostPort:")) && !infra[w] {
			t.Errorf("%s: publishes a hostPort but only infra workloads may", path)
		}
		if bytes.Contains(data, []byte("hostPath:")) {
			t.Errorf("%s: bind-mounts a host path; state lives in named volumes", path)
		}
		pods := 0
		for _, doc := range documents(data) {
			if hasLine(doc, "kind: Pod") {
				pods++
			}
			if hasLine(doc, "kind: PersistentVolumeClaim") {
				if claim := metadataName(doc); !strings.HasPrefix(claim, name+"-") {
					t.Errorf("%s: PersistentVolumeClaim named %q, want prefix %q", path, claim, name+"-")
				}
			}
		}
		if pods != 1 {
			t.Errorf("%s: %d Pod documents, want exactly 1", path, pods)
		}
	}
}

func TestScaffoldContainerignoreExcludesThePodFile(t *testing.T) {
	fsys, top := loadScaffold(t)
	for _, w := range top.Workloads() {
		path := w.Dir() + "/.containerignore"
		if !hasLine(read(t, fsys, path), "pod.yaml") {
			t.Errorf("%s: must list pod.yaml", path)
		}
	}
}

func TestScaffoldPlaybookEnablesTheRestartService(t *testing.T) {
	fsys, _ := loadScaffold(t)
	path := domain.PlaybookFile
	data := read(t, fsys, path)
	for _, want := range []string{"        name: podman-restart.service", "        scope: user", "        enabled: true", "  hosts: localhost"} {
		if !hasLine(data, want) {
			t.Errorf("%s: missing line %q", path, want)
		}
	}
}
