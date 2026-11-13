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

// interopLegsScript is the table of external-implementation interop legs
// (go-quicbaas#203) that `make interop-test-<leg>` runs.
var interopLegsScript = filepath.Join(repoRoot, "scripts", "interop-legs.sh")

// wantInteropLegs are the wire surfaces #203 names a compatibility claim for.
var wantInteropLegs = []string{"oidc", "http3", "grpc", "mcp"}

var testFuncRe = regexp.MustCompile(`(?m)^func (Test\w+)\(t \*testing\.T\)`)

type interopLeg struct {
	name, pkg, baseline, tests string
}

func listInteropLegs(t *testing.T) []interopLeg {
	t.Helper()
	out, err := exec.Command("bash", interopLegsScript, "list").CombinedOutput()
	if err != nil {
		t.Fatalf("interop-legs.sh list: %v\n%s", err, out)
	}
	var legs []interopLeg
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Fatalf("interop-legs.sh list: want 4 fields, got %q", line)
		}
		legs = append(legs, interopLeg{f[0], f[1], f[2], f[3]})
	}
	return legs
}

// packageTests returns the Test functions declared in a package directory.
func packageTests(t *testing.T, pkg string) []string {
	t.Helper()
	dir := filepath.Join(repoRoot, filepath.FromSlash(strings.TrimPrefix(pkg, "./")))
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("%s: no test files (%v)", pkg, err)
	}
	var names []string
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range testFuncRe.FindAllSubmatch(src, -1) {
			names = append(names, string(m[1]))
		}
	}
	return names
}

func selected(t *testing.T, pattern string, names []string) []string {
	t.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("pattern %q: %v", pattern, err)
	}
	var out []string
	for _, n := range names {
		if re.MatchString(n) {
			out = append(out, n)
		}
	}
	return out
}

// TestInteropLegsCoverEverySurface fails when a surface #203 names has no
// leg, or a leg's baseline or test pattern selects no test, so a renamed
// test cannot turn a leg into a vacuous pass.
func TestInteropLegsCoverEverySurface(t *testing.T) {
	legs := listInteropLegs(t)
	var names []string
	for _, l := range legs {
		names = append(names, l.name)
	}
	for _, want := range wantInteropLegs {
		if !slices.Contains(names, want) {
			t.Errorf("no interop leg %q; legs: %v", want, names)
		}
	}
	for _, l := range legs {
		tests := packageTests(t, l.pkg)
		base := selected(t, l.baseline, tests)
		if len(base) != 1 {
			t.Errorf("leg %s: baseline %q must select exactly one test in %s, selects %v", l.name, l.baseline, l.pkg, base)
		}
		all := selected(t, l.tests, tests)
		if len(all) < 2 {
			t.Errorf("leg %s: tests %q select %v in %s; a leg needs its baseline and at least one operation", l.name, l.tests, all, l.pkg)
		}
		for _, b := range base {
			if !slices.Contains(all, b) {
				t.Errorf("leg %s: baseline %s is not one of the leg's tests %v", l.name, b, all)
			}
		}
	}
}

// TestInteropLegsMakeTargets fails when a leg has no make target that runs it.
func TestInteropLegsMakeTargets(t *testing.T) {
	for _, l := range listInteropLegs(t) {
		target := "interop-test-" + l.name
		out, err := exec.Command("make", "-n", "--no-print-directory", "-C", repoRoot, target).CombinedOutput()
		if err != nil {
			t.Errorf("make -n %s: %v\n%s", target, err, out)
			continue
		}
		if !strings.Contains(string(out), "interop-legs.sh run "+l.name) {
			t.Errorf("make -n %s does not run interop-legs.sh run %s:\n%s", target, l.name, out)
		}
	}
}

// TestInteropLegRefusesAPatternThatRunsNothing proves the script's vacuous
// pass guard: a go test that passes without running a test is a failure.
func TestInteropLegRefusesAPatternThatRunsNothing(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "go")
	// "testing: warning: no tests to run" then ok: what go test prints when
	// -run matches nothing.
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'testing: warning: no tests to run'\necho PASS\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", interopLegsScript, "run", "grpc")
	cmd.Env = append(os.Environ(), "GO="+fake)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a leg whose baseline ran no test passed:\n%s", out)
	}
	if !strings.Contains(string(out), "INTEROP-FINDING grpc baseline") {
		t.Errorf("no baseline finding:\n%s", out)
	}
}

// TestInteropLegReportsAFailingBaseline proves a baseline failure stops the
// leg before its operations run.
func TestInteropLegReportsAFailingBaseline(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho '--- FAIL: TestGRPCInterop_BaselineHealth (0.01s)'\necho FAIL\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", interopLegsScript, "run", "grpc")
	cmd.Env = append(os.Environ(), "GO="+fake)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a leg with a failing baseline passed:\n%s", out)
	}
	s := string(out)
	if !strings.Contains(s, "INTEROP-FINDING grpc baseline --- FAIL: TestGRPCInterop_BaselineHealth") {
		t.Errorf("no baseline failure finding:\n%s", s)
	}
	if strings.Contains(s, "interop grpc: tests") {
		t.Errorf("the leg's operations ran after a failed baseline:\n%s", s)
	}
}

// TestInteropLegRefusesAnUnknownLeg keeps a typo from passing silently.
func TestInteropLegRefusesAnUnknownLeg(t *testing.T) {
	out, err := exec.Command("bash", interopLegsScript, "run", "nope").CombinedOutput()
	if err == nil {
		t.Fatalf("an unknown leg passed:\n%s", out)
	}
}
