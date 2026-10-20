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
	"regexp"
	"strings"
	"testing"
)

var (
	// srcAnyUID makes /src an empty directory any uid can write.
	srcAnyUID = regexp.MustCompile(`rm -rf /src;\s*\\\s*\n\s*install -d -m 1777 /src`)
	// srcSafeDirectory lets git use /src whoever owns it.
	srcSafeDirectory = regexp.MustCompile(`git config --system --add safe\.directory /src\b`)
)

// toolchainSrcProblem names why the toolchain stage would hand the copied
// checkout a /src the run uid cannot use, or returns "". The stages copy the
// checkout into /src as a uid the image does not know, and the copy leaves
// /src itself as the image has it (scripts/in-toolchain-image.sh).
func toolchainSrcProblem(stage, workdir string) string {
	if run := runRemovingItsWorkdir(stage, workdir); run != "" {
		return "a RUN removes /src while /src is its working directory, so the commands after it fail: " + run
	}
	user := strings.Index(stage, "\nUSER ")
	if user < 0 {
		return "the stage never drops to a non-root USER"
	}
	asRoot := stage[:user]
	if !srcAnyUID.MatchString(asRoot) {
		return "/src is not made an empty directory any uid can write before USER"
	}
	if !srcSafeDirectory.MatchString(asRoot) {
		return "git is not told to trust /src before USER"
	}
	return ""
}

var (
	// dockerInstruction is the start of an instruction: a keyword at column 0.
	dockerInstruction = regexp.MustCompile(`(?m)^([A-Z]+)\s+`)
	removesSrc        = regexp.MustCompile(`\brm\s+-[a-z]*\s+/src\b`)
	leavesForRoot     = regexp.MustCompile(`\bcd\s+/(?:\s|;|&|$)`)
)

// runRemovingItsWorkdir returns the rm line of the first RUN in stage that
// removes /src while /src is its working directory, starting from workdir and
// following each WORKDIR, unless the RUN first runs `cd /`. It returns "" when
// no RUN does.
func runRemovingItsWorkdir(stage, workdir string) string {
	starts := dockerInstruction.FindAllStringSubmatchIndex(stage, -1)
	for i, m := range starts {
		end := len(stage)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		keyword, args := stage[m[2]:m[3]], strings.TrimSpace(stage[m[1]:end])
		switch keyword {
		case "WORKDIR":
			workdir = strings.TrimSuffix(args, "/")
		case "RUN":
			rm := removesSrc.FindStringIndex(args)
			if workdir == "/src" && rm != nil && !leavesForRoot.MatchString(args[:rm[0]]) {
				lineStart := strings.LastIndex(args[:rm[0]], "\n") + 1
				lineEnd := len(args)
				if nl := strings.Index(args[rm[0]:], "\n"); nl >= 0 {
					lineEnd = rm[0] + nl
				}
				return strings.TrimSpace(args[lineStart:lineEnd])
			}
		}
	}
	return ""
}

// stageWorkdir is the working directory the named stage starts with: the
// last WORKDIR of the stages it builds FROM, nearest first, or "/".
func stageWorkdir(stages []dockerStage, name string) string {
	byName := map[string]dockerStage{}
	for _, s := range stages {
		byName[s.name] = s
	}
	for p := byName[name].parent; p != ""; p = byName[p].parent {
		var last string
		for _, m := range dockerInstruction.FindAllStringSubmatchIndex(byName[p].body, -1) {
			if byName[p].body[m[2]:m[3]] == "WORKDIR" {
				rest := byName[p].body[m[1]:]
				last = strings.TrimSuffix(strings.TrimSpace(strings.SplitN(rest, "\n", 2)[0]), "/")
			}
		}
		if last != "" {
			return last
		}
	}
	return "/"
}

func TestToolchainImageGivesTheCopiedCheckoutAWritableSrc(t *testing.T) {
	stages := parseDockerStages(readFile(t, devcontainerDockerfilePath))
	for _, s := range stages {
		if s.name != "toolchain" {
			continue
		}
		if problem := toolchainSrcProblem(s.body, stageWorkdir(stages, s.name)); problem != "" {
			t.Errorf("%s toolchain stage: %s", devcontainerDockerfilePath, problem)
		}
		return
	}
	t.Fatalf("%s has no toolchain stage", devcontainerDockerfilePath)
}

func TestToolchainSrcProblemFindsAnUnusableSrc(t *testing.T) {
	const fix = "RUN set -eu; \\\n    rm -rf /src; \\\n    install -d -m 1777 /src; \\\n" +
		"    git config --system --add safe.directory /src\n"
	const fixFromRoot = "RUN set -eu; \\\n    cd /; \\\n    rm -rf /src; \\\n    install -d -m 1777 /src; \\\n" +
		"    git config --system --add safe.directory /src\n"
	const user = "USER dev\nWORKDIR /src\n"
	cases := map[string]struct {
		stage   string
		workdir string
		problem bool
	}{
		"root-owned /src from base": {stage: "\n" + user, workdir: "/", problem: true},
		"no safe.directory": {
			stage:   "\nRUN rm -rf /src; \\\n    install -d -m 1777 /src\n" + user,
			workdir: "/",
			problem: true,
		},
		"fix after USER":       {stage: "\n" + user + fix, workdir: "/", problem: true},
		"never drops root":     {stage: "\n" + fix, workdir: "/", problem: true},
		"writable and trusted": {stage: "\n" + fix + user, workdir: "/"},
		// CI runs 57039 and 57128: base's WORKDIR /src is inherited, so the
		// RUN deletes its own working directory and git config exits 128.
		"removes its inherited working directory": {stage: "\n" + fix + user, workdir: "/src", problem: true},
		"leaves /src before replacing it":         {stage: "\n" + fixFromRoot + user, workdir: "/src"},
		"WORKDIR / before the RUN":                {stage: "\nWORKDIR /\n" + fix + user, workdir: "/src"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := toolchainSrcProblem(tc.stage, tc.workdir); (got != "") != tc.problem {
				t.Errorf("toolchainSrcProblem = %q, want a problem: %v", got, tc.problem)
			}
		})
	}
}
