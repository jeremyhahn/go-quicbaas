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
	"strings"
	"testing"
)

// makeRecipe returns the recipe lines of target in makefile: the lines after
// `target:` up to the first line that is not indented with a tab.
func makeRecipe(makefile, target string) string {
	start := strings.Index(makefile, "\n"+target+":")
	if start < 0 {
		return ""
	}
	lines := strings.Split(makefile[start+1:], "\n")[1:]
	var recipe []string
	for _, line := range lines {
		if !strings.HasPrefix(line, "\t") {
			break
		}
		recipe = append(recipe, line)
	}
	return strings.Join(recipe, "\n")
}

// sumdbProblem names why a deps-check recipe would run the go command with
// the image's read-only GOPATH, where it keeps the checksum database's state
// ($GOPATH/pkg/sumdb), or with checksum verification turned off. It returns
// "" when the recipe pins GOMODCACHE and then points GOPATH at a temporary
// directory before its first go command that resolves modules.
func sumdbProblem(recipe string) string {
	for _, off := range []string{"GOSUMDB=off", "GONOSUMCHECK", "-insecure"} {
		if strings.Contains(recipe, off) {
			return "it turns checksum verification off with " + off
		}
	}
	firstList := strings.Index(recipe, "$(GOCMD) list")
	if firstList < 0 {
		return "it runs no go list"
	}
	before := recipe[:firstList]
	modcache := strings.Index(before, `export GOMODCACHE="$$($(GOCMD) env GOMODCACHE)"`)
	tmp := strings.Index(before, "mktemp -d")
	gopath := strings.Index(before, "export GOPATH=")
	switch {
	case tmp < 0 || gopath < 0:
		return "it never points GOPATH at a temporary directory before go list"
	case modcache < 0 || modcache > gopath:
		return "it moves GOPATH without pinning GOMODCACHE first"
	}
	return ""
}

func TestDepsCheckGivesTheGoCommandAWritableSumdb(t *testing.T) {
	recipe := makeRecipe(readFile(t, makefilePath), "deps-check")
	if recipe == "" {
		t.Fatal("Makefile has no deps-check recipe")
	}
	if problem := sumdbProblem(recipe); problem != "" {
		t.Errorf("deps-check would fail to record the checksum database's state: %s", problem)
	}
}

func TestSumdbProblemFindsAReadOnlySumdb(t *testing.T) {
	const list = "\t$(GOCMD) list -m -u all; \\\n"
	const pin = "\texport GOMODCACHE=\"$$($(GOCMD) env GOMODCACHE)\"; \\\n"
	const tmp = "\td=\"$$(mktemp -d)\"; \\\n\texport GOPATH=\"$$d\"; \\\n"
	cases := map[string]struct {
		recipe  string
		problem bool
	}{
		"image GOPATH":         {recipe: list, problem: true},
		"GOPATH after go list": {recipe: pin + list + tmp, problem: true},
		"GOPATH without pin":   {recipe: tmp + list, problem: true},
		"pin after GOPATH":     {recipe: tmp + pin + list, problem: true},
		"verification off":     {recipe: "\texport GOSUMDB=off; \\\n" + pin + tmp + list, problem: true},
		"temporary GOPATH":     {recipe: pin + tmp + list},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := sumdbProblem(tc.recipe); (got != "") != tc.problem {
				t.Errorf("sumdbProblem = %q, want a problem: %v", got, tc.problem)
			}
		})
	}
}
