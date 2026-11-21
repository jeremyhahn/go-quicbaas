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
	"strings"
	"testing"
)

// fakeNPM stands in for npm in the check-sdk-ts-dist recipe: `npm ci` does
// nothing and `npm run build` "compiles" each src/*.ts to dist/*.js by copying
// it, so the gate's diff logic runs without node or tsc.
const fakeNPM = `#!/bin/sh
case "$1" in
ci) exit 0 ;;
run)
	mkdir -p dist
	for f in src/*.ts; do cp "$f" "dist/$(basename "$f" .ts).js"; done
	exit 0 ;;
esac
echo "fake npm: unexpected $*" >&2
exit 2
`

// sdkDistTree builds a git repository holding sdk/typescript/src and the
// committed dist files, and returns its root.
func sdkDistTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	for path, body := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "seed"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return root
}

// recipeCommands splits `make -n` output into make's commands: one per line,
// with a backslash-newline continuing the command onto the next line.
func recipeCommands(dryRun string) []string {
	var cmds []string
	var cur strings.Builder
	for _, line := range strings.Split(dryRun, "\n") {
		cur.WriteString(line)
		if strings.HasSuffix(line, "\\") {
			cur.WriteString("\n")
			continue
		}
		if strings.TrimSpace(cur.String()) != "" {
			cmds = append(cmds, cur.String())
		}
		cur.Reset()
	}
	return cmds
}

// runSDKDistGate runs the check-sdk-ts-dist recipe in dir as make would (each
// command in its own shell, stopping at the first that fails) with the fake
// npm first on PATH, and reports its output and whether it passed.
func runSDKDistGate(t *testing.T, dir string) (string, bool) {
	t.Helper()
	recipe := repoMakeDryRun(t, "check-sdk-ts-dist")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(fakeNPM), 0o755); err != nil { // #nosec G306 -- the fake npm must be executable
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CEILING_DIRECTORIES="+filepath.Dir(dir),
	)
	var out strings.Builder
	for _, line := range recipeCommands(recipe) {
		cmd := exec.Command("sh", "-c", line)
		cmd.Dir = dir
		cmd.Env = env
		b, err := cmd.CombinedOutput()
		out.Write(b)
		if err != nil {
			return out.String(), false
		}
	}
	return out.String(), true
}

func TestSDKDistGatePassesWhenDistMatchesTheBuild(t *testing.T) {
	dir := sdkDistTree(t, map[string]string{
		"sdk/typescript/src/a.ts":  "export const a = 1;\n",
		"sdk/typescript/src/b.ts":  "export const b = 2;\n",
		"sdk/typescript/dist/a.js": "export const a = 1;\n",
		"sdk/typescript/dist/b.js": "export const b = 2;\n",
	})
	if out, passed := runSDKDistGate(t, dir); !passed {
		t.Fatalf("check-sdk-ts-dist failed a dist that matches its build:\n%s", out)
	}
}

func TestSDKDistGateNamesAHandEditedFile(t *testing.T) {
	dir := sdkDistTree(t, map[string]string{
		"sdk/typescript/src/a.ts":  "export const a = 1;\n",
		"sdk/typescript/src/b.ts":  "export const b = 2;\n",
		"sdk/typescript/dist/a.js": "export const a = 1;\n",
		"sdk/typescript/dist/b.js": "export const b = 3; // hand-edited\n",
	})
	out, passed := runSDKDistGate(t, dir)
	if passed {
		t.Fatalf("check-sdk-ts-dist passed a hand-edited dist file:\n%s", out)
	}
	if !strings.Contains(out, "sdk/typescript/dist/b.js") || strings.Contains(out, "dist/a.js") {
		t.Errorf("check-sdk-ts-dist did not name exactly sdk/typescript/dist/b.js:\n%s", out)
	}
}

func TestSDKDistGateNamesMissingAndStaleFiles(t *testing.T) {
	dir := sdkDistTree(t, map[string]string{
		"sdk/typescript/src/a.ts":      "export const a = 1;\n",
		"sdk/typescript/src/new.ts":    "export const n = 1;\n",
		"sdk/typescript/dist/a.js":     "export const a = 1;\n",
		"sdk/typescript/dist/stale.js": "export const gone = 1;\n",
	})
	out, passed := runSDKDistGate(t, dir)
	if passed {
		t.Fatalf("check-sdk-ts-dist passed a dist missing new.js and holding stale.js:\n%s", out)
	}
	for _, want := range []string{"sdk/typescript/dist/new.js", "sdk/typescript/dist/stale.js"} {
		if !strings.Contains(out, want) {
			t.Errorf("check-sdk-ts-dist did not name %s:\n%s", want, out)
		}
	}
}
