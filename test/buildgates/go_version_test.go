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
		base := path.Base(file)
		if base != "go.mod" && base != "Dockerfile" && !strings.HasPrefix(base, "Dockerfile.") {
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
