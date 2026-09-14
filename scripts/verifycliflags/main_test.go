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

package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

func TestDecidePassesWhenTheSubjectCarriesEveryReferenceFlag(t *testing.T) {
	reference := []string{"--config", "--key-dir", "--token", "--verbose"}
	// --token and --verbose are documented differences; the rest are present.
	subject := []string{"--config", "--key-dir", "--log-level", "--transport"}

	var stdout strings.Builder
	if status := decide(reference, subject, true, &stdout); status != gate.ExitClean {
		t.Fatalf("decide exited %d, want %d\nstdout: %s", status, gate.ExitClean, stdout.String())
	}
	if !strings.Contains(stdout.String(), "every xkmsctl init flag is present on quicbaas init") {
		t.Fatalf("the pass was not reported: %q", stdout.String())
	}
}

func TestDecideFailsUnderStrictOnDrift(t *testing.T) {
	// THE CASE THAT MATTERS. An operator flag on the canonical CLI that QuicBaaS
	// omits is UX drift, and under -strict it must stop the build.
	reference := []string{"--config", "--so-pin", "--token"}
	subject := []string{"--config"}

	var stdout strings.Builder
	status := decide(reference, subject, true, &stdout)
	if status != gate.ExitFindings {
		t.Fatalf("decide exited %d, want %d\nstdout: %s", status, gate.ExitFindings, stdout.String())
	}
	report := stdout.String()
	if !strings.Contains(report, "verify-cli-flags FAIL (-strict)") {
		t.Fatalf("the failure was not reported: %s", report)
	}
	if !strings.Contains(report, "--so-pin") {
		t.Fatalf("the report does not name the missing flag: %s", report)
	}
	if strings.Contains(report, "--token") {
		t.Fatalf("a documented difference was reported as drift: %s", report)
	}
	if !strings.Contains(report, "cmd/quicbaas/init.go") {
		t.Fatalf("the report does not say where to fix it: %s", report)
	}
}

func TestDecideIsAdvisoryWithoutStrict(t *testing.T) {
	// The drift is real and reported, but the gate does not block until an
	// operator asks it to — which is the behaviour the shell gate had and CI
	// depends on.
	reference := []string{"--config", "--so-pin"}
	subject := []string{"--config"}

	var stdout strings.Builder
	status := decide(reference, subject, false, &stdout)
	if status != gate.ExitClean {
		t.Fatalf("decide exited %d, want %d (advisory mode must not block)", status, gate.ExitClean)
	}
	report := stdout.String()
	if !strings.Contains(report, "verify-cli-flags WARN") {
		t.Fatalf("the drift was not reported: %s", report)
	}
	if !strings.Contains(report, "--so-pin") {
		t.Fatalf("the advisory does not name the missing flag: %s", report)
	}
	if !strings.Contains(report, "STRICT=1") {
		t.Fatalf("the advisory does not say how to make it blocking: %s", report)
	}
}

func TestDriftComparesTheSetsAndNotTheOrder(t *testing.T) {
	cases := []struct {
		name      string
		reference []string
		subject   []string
		want      []string
	}{
		{
			name:      "nothing missing",
			reference: []string{"--a", "--b"},
			subject:   []string{"--b", "--a", "--c"},
			want:      nil,
		},
		{
			name:      "reported sorted",
			reference: []string{"--z", "--a"},
			subject:   nil,
			want:      []string{"--a", "--z"},
		},
		{
			name:      "every documented difference excluded",
			reference: []string{"--token", "--user-pin", "--verbose"},
			subject:   nil,
			want:      nil,
		},
		{
			// The subject may carry flags of its own: QuicBaaS has verbs xkmsctl
			// does not.
			name:      "a superset subject is not drift",
			reference: []string{"--a"},
			subject:   []string{"--a", "--transport", "--mcp"},
			want:      nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := drift(tc.reference, tc.subject); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("drift = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractFlagsReadsTheOperatorSurface(t *testing.T) {
	cases := []struct {
		name string
		help string
		want []string
	}{
		{
			name: "a cobra flag block",
			help: "Usage:\n  xkmsctl init [flags]\n\nFlags:\n" +
				"      --config string     config file\n" +
				"  -h, --help              help for init\n" +
				"      --so-pin string     security officer PIN\n",
			want: []string{"--config", "--help", "--so-pin"},
		},
		{
			name: "duplicates collapse and the result is sorted",
			help: "--b --a --b\n--a\n",
			want: []string{"--a", "--b"},
		},
		{
			name: "short flags and prose are not long flags",
			help: "  -h, -v  see the docs -- really\n",
			want: nil,
		},
		{
			name: "a flag named in a description is still a flag an operator reads",
			help: "      --log-level string  one of debug|info (replaces --verbose)\n",
			want: []string{"--log-level", "--verbose"},
		},
		{
			name: "no help output at all",
			help: "",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractFlags(tc.help)
			if len(got) == 0 {
				got = nil
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("extractFlags = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFlagSetOfRefusesABinaryItCannotBuild(t *testing.T) {
	// THE SILENT SKIP THIS REPLACES. The shell gate discarded the build error
	// and exited 0, so a compile error in the canonical CLI read as a pass.
	_, err := flagSetOf(".", t.TempDir(), "nonesuch", "github.com/jeremyhahn/go-quicbaas/cmd/nonesuch-does-not-exist")
	var build *BuildError
	if !errors.As(err, &build) {
		t.Fatalf("flagSetOf on an unbuildable package = %v, want *BuildError", err)
	}
	if !strings.Contains(build.Error(), "cmd/nonesuch-does-not-exist") {
		t.Fatalf("the error does not name the package: %s", build.Error())
	}
	if build.Unwrap() == nil {
		t.Fatal("BuildError must carry the underlying failure")
	}
}

func TestRunRejectsAnUnknownFlag(t *testing.T) {
	var stdout, stderr strings.Builder
	if status := run([]string{"-nonesuch"}, &stdout, &stderr); status != gate.ExitToolError {
		t.Fatalf("run exited %d, want %d", status, gate.ExitToolError)
	}
}

func TestEveryDocumentedDifferenceCarriesAReason(t *testing.T) {
	// Each entry is an operator flag nobody is checking for.
	for name, reason := range allowedDifferences {
		if strings.TrimSpace(reason) == "" {
			t.Fatalf("%s is allowed to differ with no reason", name)
		}
	}
}

func TestEmptyFlagSetErrorAccountsForWhatHappened(t *testing.T) {
	// When the extraction is empty, why the invocation stopped and what it
	// printed are the only account of it, so neither may be dropped.
	bare := &EmptyFlagSetError{Package: referencePackage}
	if !strings.Contains(bare.Error(), referencePackage) {
		t.Fatalf("the error does not name the package: %s", bare.Error())
	}
	if !strings.Contains(bare.Error(), subcommand) {
		t.Fatalf("the error does not name the subcommand: %s", bare.Error())
	}
	if strings.Contains(bare.Error(), "()") {
		t.Fatalf("the error has an empty aside: %s", bare.Error())
	}

	cause := errors.New("permission denied")
	full := &EmptyFlagSetError{Package: referencePackage, Output: "bash: cannot execute\n", Err: cause}
	if !errors.Is(full, cause) {
		t.Fatal("EmptyFlagSetError must unwrap to the underlying failure")
	}
	for _, want := range []string{"permission denied", "cannot execute"} {
		if !strings.Contains(full.Error(), want) {
			t.Fatalf("the error omits %q: %s", want, full.Error())
		}
	}
}

// writeFixtureCLI writes a throwaway module whose binary answers
// `init --help` with the given text, so the build-and-read path is exercised
// against a real binary without building the repository's own CLIs.
func writeFixtureCLI(t *testing.T, help string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module fixturecli\n\ngo 1.24\n",
		"main.go": "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n" +
			"func main() {\n" +
			"\tif len(os.Args) > 2 && os.Args[1] == \"init\" && os.Args[2] == \"--help\" {\n" +
			"\t\tfmt.Print(" + quoteGo(help) + ")\n" +
			"\t}\n" +
			"}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

// quoteGo renders s as a Go string literal for the fixture's source.
func quoteGo(s string) string {
	return strconv.Quote(s)
}

func TestFlagSetOfReadsTheHelpOfABinaryItBuilt(t *testing.T) {
	module := writeFixtureCLI(t, "Flags:\n      --config string   config file\n      --so-pin string   SO PIN\n")
	got, err := flagSetOf(module, t.TempDir(), "fixturecli", ".")
	if err != nil {
		t.Fatalf("flagSetOf: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"--config", "--so-pin"}) {
		t.Fatalf("flagSetOf = %q", got)
	}
}

func TestFlagSetOfRefusesABinaryThatAdvertisesNoFlags(t *testing.T) {
	// An empty reference set makes the comparison vacuously true, which is the
	// shape of a gate that has stopped reading its input.
	module := writeFixtureCLI(t, "no flags here\n")
	_, err := flagSetOf(module, t.TempDir(), "fixturecli", ".")
	var empty *EmptyFlagSetError
	if !errors.As(err, &empty) {
		t.Fatalf("flagSetOf on a flagless binary = %v, want *EmptyFlagSetError", err)
	}
}

func TestRunRefusesATreeItCannotBuildFrom(t *testing.T) {
	// The shell gate discarded the build error and exited 0 here.
	var stdout, stderr strings.Builder
	status := run([]string{"-root", t.TempDir()}, &stdout, &stderr)
	if status != gate.ExitToolError {
		t.Fatalf("run exited %d, want %d", status, gate.ExitToolError)
	}
	if !strings.Contains(stderr.String(), "verify-cli-flags FAIL") {
		t.Fatalf("the failure was not reported: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "cannot build") {
		t.Fatalf("the failure does not say what could not be built: %q", stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("a gate that could not compare printed a verdict: %q", stdout.String())
	}
}
