// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package buildgates

import (
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// toolchainGoVersion is the GO_VERSION that pkg/version/toolchain.mk pins.
func toolchainGoVersion(t *testing.T) string {
	t.Helper()
	const prefix = "GO_VERSION := "
	mk := filepath.Join(repoRoot, "pkg", "version", "toolchain.mk")
	for _, line := range strings.Split(readFile(t, mk), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("%s: no %q line", mk, prefix)
	return ""
}

// goVersionProblems reports each Go version in a go.mod or Dockerfile body
// that is not want. Comment lines do not count; ${GO_VERSION} references
// and a bare `ARG GO_VERSION` are fine.
func goVersionProblems(name, body, want string) []string {
	var problems []string
	isGoMod := strings.HasSuffix(name, "go.mod")
	for i, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		report := func(format string, args ...any) {
			problems = append(problems, fmt.Sprintf("%s:%d: ", name, i+1)+fmt.Sprintf(format, args...))
		}
		fields := strings.Fields(line)
		if isGoMod {
			if len(fields) < 2 {
				continue
			}
			switch fields[0] {
			case "go":
				if fields[1] != want {
					report("go %s, want go %s", fields[1], want)
				}
			case "toolchain":
				if fields[1] != "go"+want {
					report("toolchain %s, want toolchain go%s", fields[1], want)
				}
			}
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "ARG":
			for _, f := range fields[1:] {
				if v, ok := strings.CutPrefix(f, "GO_VERSION="); ok && v != want {
					report("ARG GO_VERSION=%s, want %s", v, want)
				}
			}
		case "FROM":
			for _, f := range fields[1:] {
				if strings.HasPrefix(f, "--") {
					continue
				}
				image := strings.TrimPrefix(f, "docker.io/library/")
				if v, ok := strings.CutPrefix(image, "golang:"); ok && v != "" && v[0] >= '0' && v[0] <= '9' &&
					v != want && !strings.HasPrefix(v, want+"-") && !strings.HasPrefix(v, want+"@") {
					report("FROM %s, want golang:%s", f, want)
				}
				break
			}
		}
	}
	return problems
}

// Every tracked go.mod and Dockerfile pins the toolchain's Go version, so a
// floating `golang:1.27` or a lagging `go 1.24` cannot drift in (#2614).
func TestEveryGoVersionIsTheToolchains(t *testing.T) {
	want := toolchainGoVersion(t)
	out, err := exec.Command("git", "-C", repoRoot, "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	checked := 0
	for _, file := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if !isGoVersionFile(file) {
			continue
		}
		checked++
		for _, p := range goVersionProblems(file, readFile(t, filepath.Join(repoRoot, filepath.FromSlash(file))), want) {
			t.Error(p)
		}
	}
	if checked == 0 {
		t.Fatal("git ls-files listed no go.mod or Dockerfile")
	}
}

func TestGoVersionProblems_Mutations(t *testing.T) {
	const want = "1.27.4"
	cases := []struct {
		name, file, body string
		reported         bool
	}{
		{"floating ARG", "Dockerfile", "ARG GO_VERSION=1.27\n", true},
		{"old FROM", "Dockerfile.cli", "FROM golang:1.26-alpine AS builder\n", true},
		{"docker.io FROM", "Dockerfile", "FROM docker.io/library/golang:1.27 AS b\n", true},
		{"lagging go line", "x/go.mod", "module x\n\ngo 1.24\n", true},
		{"lagging toolchain", "go.mod", "module x\n\ngo 1.27.4\ntoolchain go1.27.1\n", true},
		{"pinned ARG", "Dockerfile", "ARG GO_VERSION=1.27.4\n", false},
		{"bare ARG", "Dockerfile", "ARG GO_VERSION\n", false},
		{"ARG reference", "Dockerfile", "FROM golang:${GO_VERSION}-alpine\n", false},
		{"pinned FROM", "Dockerfile", "FROM golang:1.27.4-alpine AS b\n", false},
		{"pinned FROM digest", "Dockerfile", "FROM --platform=$BUILDPLATFORM golang:1.27.4@sha256:abc\n", false},
		{"comment", "Dockerfile", "# golang:1.27-alpine\n  # FROM golang:1.27-alpine\n", false},
		{"pinned go.mod", "go.mod", "module x\n\ngo 1.27.4\ntoolchain go1.27.4\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := goVersionProblems(tc.file, tc.body, want)
			if (len(got) > 0) != tc.reported {
				t.Errorf("goVersionProblems(%q) = %v, want reported=%v", tc.body, got, tc.reported)
			}
		})
	}
}

// isGoVersionFile reports whether a tracked path names Go's version: a
// go.mod, a Dockerfile, a Dockerfile.<variant> or a <name>.Dockerfile.
func isGoVersionFile(file string) bool {
	base := path.Base(file)
	return base == "go.mod" || base == "Dockerfile" ||
		strings.HasPrefix(base, "Dockerfile.") || strings.HasSuffix(base, ".Dockerfile")
}

// workflowGoVersionProblems reports each literal Go version a workflow pins,
// as a GO_VERSION: env value or a setup-go go-version: input, that is not
// want. An expression such as ${{ env.GO_VERSION }} and comment lines do not
// count.
func workflowGoVersionProblems(name, body, want string) []string {
	var problems []string
	for i, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimPrefix(key, "- ")
		if key != "GO_VERSION" && key != "go-version" {
			continue
		}
		if before, _, found := strings.Cut(value, " #"); found {
			value = before
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		if value == "" || strings.Contains(value, "${{") {
			continue
		}
		if value != want {
			problems = append(problems, fmt.Sprintf("%s:%d: %s %s, want %s", name, i+1, key, value, want))
		}
	}
	return problems
}

// TestWorkflowGoVersionIsTheToolchains checks that every workflow pins the
// toolchain's Go version, so CI cannot build with a Go the images and go.mod
// do not name.
func TestWorkflowGoVersionIsTheToolchains(t *testing.T) {
	want := toolchainGoVersion(t)
	pinned := false
	for _, f := range workflowFiles(t) {
		body := readFile(t, f)
		for _, p := range workflowGoVersionProblems(filepath.Base(f), body, want) {
			t.Error(p)
		}
		if strings.Contains(body, "GO_VERSION: '"+want+"'") {
			pinned = true
		}
	}
	if !pinned {
		t.Fatalf("no workflow sets GO_VERSION: '%s'; the env key may have been renamed", want)
	}
}

func TestWorkflowGoVersionProblems_Mutations(t *testing.T) {
	const want = "1.27.4"
	cases := []struct {
		name, body string
		reported   bool
	}{
		{"floating minor", "env:\n  GO_VERSION: '1.27'\n", true},
		{"old env", "env:\n  GO_VERSION: \"1.26.7\"\n", true},
		{"old setup-go", "      - uses: actions/setup-go@v5\n        with:\n          go-version: 1.27.1\n", true},
		{"old setup-go with comment", "          go-version: '1.27.1'  # old\n", true},
		{"pinned env", "env:\n  GO_VERSION: '1.27.4'\n", false},
		{"env reference", "          go-version: ${{ env.GO_VERSION }}\n", false},
		{"comment", "# GO_VERSION: '1.20'\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := workflowGoVersionProblems("ci.yml", tc.body, want)
			if (len(got) > 0) != tc.reported {
				t.Errorf("workflowGoVersionProblems(%q) = %v, want reported=%v", tc.body, got, tc.reported)
			}
		})
	}
}

func TestIsGoVersionFile(t *testing.T) {
	cases := []struct {
		file string
		want bool
	}{
		{"go.mod", true},
		{"sdk/go/go.mod", true},
		{"Dockerfile", true},
		{"test/integration/Dockerfile.node", true},
		{"deployments/agents/software-factory-worker/variants/go-xkms.Dockerfile", true},
		{"go.sum", false},
		{"docs/Dockerfile-notes.md", false},
		{"scripts/build.sh", false},
		{"deployments/agents/software-factory-worker/testdata/serena/go/go.mod.fixture", false},
	}
	for _, tc := range cases {
		if got := isGoVersionFile(tc.file); got != tc.want {
			t.Errorf("isGoVersionFile(%q) = %v, want %v", tc.file, got, tc.want)
		}
	}
}
