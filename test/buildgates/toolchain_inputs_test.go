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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The toolchain image's tag hashes the Makefile's DEVCONTAINER_INPUTS, and
// docker-build-toolchain-image builds nothing when the daemon already holds
// that tag. The tag names the image's content only while every file the
// toolchain stage, and each stage it builds on, copies from the build context
// is one of those inputs (Codex 7563).

// copyInstruction matches a COPY or ADD instruction and captures its
// arguments, flags included.
var copyInstruction = regexp.MustCompile(`(?m)^\s*(?:COPY|ADD)\s+(.*)$`)

// contextCopySources returns, sorted, the build-context paths that the target
// stage and the stages it builds on (its FROM parent and the stages it copies
// --from) COPY or ADD.
func contextCopySources(dockerfile, target string) []string {
	byName := map[string]dockerStage{}
	for _, s := range parseDockerStages(dockerfile) {
		byName[s.name] = s
	}
	visited := map[string]bool{}
	sources := map[string]bool{}
	var visit func(name string)
	visit = func(name string) {
		stage, ok := byName[name]
		if !ok || visited[name] {
			return
		}
		visited[name] = true
		visit(stage.parent)
		body := strings.ReplaceAll(stage.body, "\\\n", " ")
		for _, m := range copyInstruction.FindAllStringSubmatch(body, -1) {
			var from string
			var args []string
			for _, field := range strings.Fields(m[1]) {
				if value, ok := strings.CutPrefix(field, "--from="); ok {
					from = value
				}
				if !strings.HasPrefix(field, "--") {
					args = append(args, field)
				}
			}
			if from != "" {
				visit(from)
				continue
			}
			if len(args) < 2 {
				continue
			}
			for _, src := range args[:len(args)-1] {
				sources[src] = true
			}
		}
	}
	visit(target)
	list := make([]string, 0, len(sources))
	for src := range sources {
		list = append(list, src)
	}
	slices.Sort(list)
	return list
}

// uncoveredCopySources returns the build-context paths the target stage
// copies that are not among inputs.
func uncoveredCopySources(dockerfile, target string, inputs []string) []string {
	var uncovered []string
	for _, src := range contextCopySources(dockerfile, target) {
		if !slices.Contains(inputs, src) {
			uncovered = append(uncovered, src)
		}
	}
	return uncovered
}

// devcontainerInputs returns DEVCONTAINER_INPUTS as make expands it.
func devcontainerInputs(t *testing.T) []string {
	t.Helper()
	const goal = "print-devcontainer-inputs"
	out, err := exec.Command("make", "-s", "--no-print-directory", "-C", repoRoot,
		"--eval", goal+": ; @echo $(DEVCONTAINER_INPUTS)", goal).CombinedOutput()
	if err != nil {
		t.Fatalf("make %s: %v\n%s", goal, err, out)
	}
	return strings.Fields(string(out))
}

func TestToolchainImageHashCoversEveryFileTheImageCopies(t *testing.T) {
	dockerfile := readFile(t, devcontainerDockerfilePath)
	if len(contextCopySources(dockerfile, "toolchain")) == 0 {
		t.Fatalf("%s: the toolchain stage copies nothing from the build context", devcontainerDockerfilePath)
	}
	inputs := devcontainerInputs(t)
	for _, input := range inputs {
		if _, err := os.Stat(filepath.Join(repoRoot, input)); err != nil {
			t.Errorf("DEVCONTAINER_INPUTS lists %s, which does not exist: %v", input, err)
		}
	}
	if uncovered := uncoveredCopySources(dockerfile, "toolchain", inputs); len(uncovered) > 0 {
		t.Errorf("the toolchain image copies %v, which DEVCONTAINER_INPUTS does not hash: "+
			"a change to them would not change TOOLCHAIN_IMAGE", uncovered)
	}
}

func TestUncoveredCopySourcesFindsAnUnlistedCopy(t *testing.T) {
	const dockerfile = "FROM golang:1 AS base\nCOPY go.mod go.sum ./\n" +
		"FROM base AS tools\nCOPY tools.mk /tmp/tools.mk\n" +
		"FROM base AS toolchain\nCOPY --from=tools /opt/bin/ /opt/bin/\n" +
		"COPY --chmod=0644 a/package.json \\\n    a/package-lock.json /tmp/seed/a/\n" +
		"FROM base AS integration\nCOPY . /src\n"
	cases := map[string]struct {
		inputs []string
		want   []string
	}{
		"every copied file listed": {
			inputs: []string{"go.mod", "go.sum", "tools.mk", "a/package.json", "a/package-lock.json"},
		},
		"a manifest missing": {
			inputs: []string{"go.mod", "go.sum", "tools.mk", "a/package.json"},
			want:   []string{"a/package-lock.json"},
		},
		"the parent and the --from stage missing": {
			inputs: []string{"a/package.json", "a/package-lock.json"},
			want:   []string{"go.mod", "go.sum", "tools.mk"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := uncoveredCopySources(dockerfile, "toolchain", tc.inputs)
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
