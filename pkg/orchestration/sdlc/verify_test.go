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

package sdlc

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// container_command
// =============================================================================

// verifyRig is a JobContext carrying an allowlist and a scripted container,
// which is the whole world the validator has.
type verifyRig struct {
	job     *orchestration.JobContext
	execs   []*orchestration.ExecSpec
	result  *orchestration.ExecResult
	execErr error
}

// newVerifyRig admits every command in allowed and binds a container whose
// checkout is the one path a command may name.
func newVerifyRig(t *testing.T, checkout string, allowed ...string) *verifyRig {
	t.Helper()
	guard, err := orchestration.NewExecutionGuard(allowed)
	if err != nil {
		t.Fatal(err)
	}
	rig := &verifyRig{result: &orchestration.ExecResult{}}
	container, err := orchestration.NewJobContainer("session-1", checkout,
		func(_ context.Context, spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
			rig.execs = append(rig.execs, spec)
			if rig.execErr != nil {
				return nil, rig.execErr
			}
			return rig.result, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	rig.job = &orchestration.JobContext{TaskID: testTask, Guard: guard, Container: container}
	return rig
}

// TestValidateInContainer_RunsTheCommandInTheContainersCheckout: the command
// is handed to the container's exec seam, as a shell command line, in the
// directory the CONTAINER calls its checkout — never a daemon path.
func TestValidateInContainer_RunsTheCommandInTheContainersCheckout(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make build")
	rig.result = &orchestration.ExecResult{Stdout: "build ok\n"}

	result, err := ValidateInContainer(context.Background(), rig.job,
		orchestration.ReviewCriterion{Name: "make build", Validator: ValidatorKindContainerCommand, Config: "make build"})
	if err != nil {
		t.Fatalf("ValidateInContainer: %v", err)
	}
	if !result.Passed || result.Message != "build ok" {
		t.Fatalf("result = %+v, want a pass carrying the command's output", result)
	}
	if len(rig.execs) != 1 {
		t.Fatalf("execs = %d, want exactly one command in the container", len(rig.execs))
	}
	spec := rig.execs[0]
	if spec.WorkDir != "/workspace/api" {
		t.Errorf("work dir = %q, want the container's checkout", spec.WorkDir)
	}
	if len(spec.Cmd) != 3 || spec.Cmd[0] != verificationShell || spec.Cmd[1] != "-c" || spec.Cmd[2] != "make build" {
		t.Errorf("cmd = %v, want the shell asked for by name with the command line", spec.Cmd)
	}
}

// TestValidateInContainer_AFailureCarriesBothStreamsAsEvidence: the exit
// status is the verdict, and what the command said on either stream is what
// the worker is sent back with. A build tool that reports on stdout must not
// send the worker an exit code and nothing else.
func TestValidateInContainer_AFailureCarriesBothStreamsAsEvidence(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make test")
	rig.result = &orchestration.ExecResult{
		ExitCode: 2,
		Stdout:   "--- FAIL: TestThing\n",
		Stderr:   "make: *** [test] Error 1\n",
	}

	result, err := ValidateInContainer(context.Background(), rig.job,
		orchestration.ReviewCriterion{Name: "make test", Config: "make test"})
	if err != nil {
		t.Fatalf("a non-zero exit is a verdict, not an error: %v", err)
	}
	if result.Passed {
		t.Fatal("a command that exited non-zero passed")
	}
	if result.Message != "make: *** [test] Error 1" {
		t.Errorf("message = %q, want the command's stderr", result.Message)
	}
	if !strings.Contains(result.Details, "exit code: 2") || !strings.Contains(result.Details, "TestThing") {
		t.Errorf("details = %q, want the exit code and the stdout evidence", result.Details)
	}
	if result.ExitCode != 2 {
		t.Errorf("exit code = %d, want the command's 2 carried as a field, not only as text", result.ExitCode)
	}
}

// TestValidateInContainer_TheEvidenceIsBoundedToTheOutputsTail: megabytes of
// test output do not go back to the worker; its last VerificationEvidenceBytes
// do, whole lines, with the omission stated — and the lines that NAME the
// failure are gathered from the whole output first, because `go test -v`
// prints tens of thousands of PASS lines after the failing package and the
// tail alone showed the worker a bare `FAIL` (go-quicbaas#62, 2026-09-21).
func TestValidateInContainer_TheEvidenceIsBoundedToTheOutputsTail(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make test")
	var out strings.Builder
	out.WriteString("# pkg/broken\npkg/broken/x.go:12:5: undefined: Thing\n")
	out.WriteString("--- FAIL: TestThing\nFAIL\tpkg/last\t1.2s\n")
	for i := 0; i < 20000; i++ {
		out.WriteString("ok  \tpkg/thing" + strconv.Itoa(i) + "\t0.01s\n")
	}
	out.WriteString("FAIL\nmake: *** [test] Error 1\n")
	rig.result = &orchestration.ExecResult{ExitCode: 2, Stdout: out.String(), Stderr: "make: *** [test] Error 1\n"}

	result, err := ValidateInContainer(context.Background(), rig.job,
		orchestration.ReviewCriterion{Name: "make test", Config: "make test"})
	if err != nil {
		t.Fatalf("ValidateInContainer: %v", err)
	}
	if len(result.Details) > VerificationEvidenceBytes+512 {
		t.Errorf("details = %d bytes, want at most the bound plus its header", len(result.Details))
	}
	for _, want := range []string{"exit code: 2", "--- FAIL: TestThing", "FAIL\tpkg/last", "# pkg/broken",
		"pkg/broken/x.go:12:5: undefined: Thing", "make: *** [test] Error 1", "lines naming the failure", "bytes of output are omitted"} {
		if !strings.Contains(result.Details, want) {
			t.Errorf("details lack %q", want)
		}
	}
	if strings.Contains(result.Details, "pkg/thing0\t") {
		t.Error("the head of the output was kept; the tail is what carries the failure")
	}
	if salient := result.Details[:strings.Index(result.Details, "bytes of output are omitted")]; strings.Contains(salient, "ok  \t") {
		t.Error("passing packages were gathered as failure lines")
	}
	// The window opens on a whole line.
	body := strings.TrimSpace(result.Details[strings.Index(result.Details, "bytes)")+len("bytes)"):])
	if first := strings.SplitN(body, "\n", 2)[0]; !strings.HasPrefix(first, "ok  \t") {
		t.Errorf("the excerpt starts mid-line: %q", first)
	}
}

// TestValidateInContainer_ShortOutputIsHandedOverWhole: nothing under the
// bound is touched or annotated.
func TestValidateInContainer_ShortOutputIsHandedOverWhole(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make lint")
	rig.result = &orchestration.ExecResult{ExitCode: 1, Stdout: "pkg/a.go:1:1: unused variable\n"}
	result, err := ValidateInContainer(context.Background(), rig.job,
		orchestration.ReviewCriterion{Name: "make lint", Config: "make lint"})
	if err != nil {
		t.Fatalf("ValidateInContainer: %v", err)
	}
	if result.Details != "exit code: 1\n\npkg/a.go:1:1: unused variable" {
		t.Errorf("details = %q", result.Details)
	}
}

// TestValidateInContainer_ACommandTheOperatorNeverAllowedIsRefused: the
// allowlist is checked on the DAEMON, before anything reaches a container,
// and the refusal is the guard's own typed error.
func TestValidateInContainer_ACommandTheOperatorNeverAllowedIsRefused(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make build")

	result, err := ValidateInContainer(context.Background(), rig.job,
		orchestration.ReviewCriterion{Name: "rm", Config: "rm -rf /"})
	var refused *orchestration.CommandRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want a *orchestration.CommandRefusedError", err)
	}
	if !errors.Is(err, orchestration.ErrCommandRefused) {
		t.Errorf("err = %v, does not unwrap to the guard's sentinel", err)
	}
	if refused.Command != "rm -rf /" {
		t.Errorf("the refused command was not carried on the error: %q", refused.Command)
	}
	if strings.Contains(refused.Error(), "rm -rf /") {
		t.Error("the refusal rendered tenant-supplied argv into its message")
	}
	if result.Passed {
		t.Error("a refused command passed")
	}
	if result.ExitCode != 0 {
		t.Errorf("a command that never ran reports exit code %d", result.ExitCode)
	}
	if len(rig.execs) != 0 {
		t.Fatalf("a refused command reached the container: %v", rig.execs)
	}
}

// TestValidateInContainer_AJobWithNoContainerIsRefused: verification runs
// where the work is or not at all. There is no daemon path to fall back to,
// and the refusal is typed so a caller can tell it from a failing build.
func TestValidateInContainer_AJobWithNoContainerIsRefused(t *testing.T) {
	guard, err := orchestration.NewExecutionGuard([]string{"make build"})
	if err != nil {
		t.Fatal(err)
	}
	job := &orchestration.JobContext{TaskID: testTask, Guard: guard}

	result, err := ValidateInContainer(context.Background(), job,
		orchestration.ReviewCriterion{Name: "make build", Config: "make build"})
	var refused *VerificationNotContainerizedError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want a *VerificationNotContainerizedError", err)
	}
	if refused.TaskID != testTask || refused.Criterion != "make build" {
		t.Errorf("refusal = %+v, want the task and the criterion named", refused)
	}
	if !errors.Is(err, ErrRunner) {
		t.Errorf("err = %v, does not unwrap to the runner sentinel", err)
	}
	if result.Passed {
		t.Error("a job with no container passed a verification command")
	}
}

// TestValidateInContainer_RefusesAnEmptyCriterion: a criterion IS its
// command, so one carrying none is an input error rather than a silent pass.
func TestValidateInContainer_RefusesAnEmptyCriterion(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make build")

	for _, config := range []string{"", "   "} {
		result, err := ValidateInContainer(context.Background(), rig.job,
			orchestration.ReviewCriterion{Name: "blank", Config: config})
		var invalid *InvalidInputError
		if !errors.As(err, &invalid) {
			t.Fatalf("config %q: err = %v, want an *InvalidInputError", config, err)
		}
		if result.Passed {
			t.Errorf("config %q passed", config)
		}
	}
	if len(rig.execs) != 0 {
		t.Fatalf("an empty criterion reached the container: %v", rig.execs)
	}
}

// TestValidateInContainer_RefusesANilJob: the validator judges a running job
// and has nothing to judge without one.
func TestValidateInContainer_RefusesANilJob(t *testing.T) {
	result, err := ValidateInContainer(context.Background(), nil,
		orchestration.ReviewCriterion{Name: "make build", Config: "make build"})
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want an *InvalidInputError", err)
	}
	if result.Passed {
		t.Error("a nil job passed")
	}
}

// TestValidateInContainer_AContainerThatCouldNotRunTheCommandIsAnError: a
// command that never ran is not a failing build. It is reported as an error
// so the pipeline stops rather than sending the worker a round of feedback
// about a container that is gone.
func TestValidateInContainer_AContainerThatCouldNotRunTheCommandIsAnError(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make build")
	rig.execErr = &orchestration.ContainerNotFoundError{ContainerID: "container-1"}

	result, err := ValidateInContainer(context.Background(), rig.job,
		orchestration.ReviewCriterion{Name: "make build", Config: "make build"})
	var missing *orchestration.ContainerNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want the runtime's own error", err)
	}
	if result.Passed {
		t.Error("a command that never ran passed")
	}
}

// =============================================================================
// verificationSummary — what the record says (go-quicbaas#1037)
// =============================================================================

// makeEnvNoise is GNU make 4.4's harmless first line on go-luks2
// (go-luks2#205), which the record used to report as the failure.
const makeEnvNoise = "env: can't execute 'GOPATH': No such file or directory"

// TestVerificationSummary_AFailureCarriesItsFailingLinesNotItsFirst: the
// recorded output of go-luks2#12 — noise first, the failure at the end.
func TestVerificationSummary_AFailureCarriesItsFailingLinesNotItsFirst(t *testing.T) {
	var stdout strings.Builder
	stdout.WriteString("exit code: 2\n\n")
	stdout.WriteString("--- FAIL: TestOpenVolume (0.01s)\n")
	for i := 0; i < 50; i++ {
		stdout.WriteString("=== RUN   TestPassing" + strconv.Itoa(i) + "\n")
	}
	stdout.WriteString("FAIL\tgithub.com/jeremyhahn/go-luks2/pkg/luks2\t0.412s\n")
	stdout.WriteString("FAIL\n")
	var stderr strings.Builder
	stderr.WriteString(makeEnvNoise + "\n")
	for i := 0; i < 20; i++ {
		stderr.WriteString("go: downloading example.com/mod" + strconv.Itoa(i) + " v1.0.0\n")
	}
	stderr.WriteString("make: *** [Makefile:12: test] Error 1\n")

	summary := verificationSummary(orchestration.ValidationResult{
		Name: "make test", Message: strings.TrimSpace(stderr.String()), Details: stdout.String(),
	})
	for _, want := range []string{
		"make: *** [Makefile:12: test] Error 1",
		"--- FAIL: TestOpenVolume (0.01s)",
		"FAIL\tgithub.com/jeremyhahn/go-luks2/pkg/luks2\t0.412s",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("the summary lacks the failing line %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, makeEnvNoise) {
		t.Errorf("the summary still carries the harmless first line:\n%s", summary)
	}
	if strings.HasPrefix(summary, "env:") {
		t.Errorf("the summary leads with the noise:\n%s", summary)
	}
}

// TestVerificationSummary_MarkersAnywhereInALineAreFailures: a compiler's
// `error:` and make's `Error 2` name the failure mid-line, ahead of the tail.
func TestVerificationSummary_MarkersAnywhereInALineAreFailures(t *testing.T) {
	var stderr strings.Builder
	stderr.WriteString(makeEnvNoise + "\n")
	stderr.WriteString("src/luks.c:10:3: error: unknown type name 'foo'\n")
	stderr.WriteString("Makefile:4: recipe failed; Error 2 from sub-make\n")
	for i := 0; i < 30; i++ {
		stderr.WriteString("note: context " + strconv.Itoa(i) + "\n")
	}
	summary := verificationSummary(orchestration.ValidationResult{Message: stderr.String()})
	for _, want := range []string{"error: unknown type name 'foo'", "Error 2 from sub-make"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the summary lacks %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, makeEnvNoise) || strings.Contains(summary, "note: context 0\n") {
		t.Errorf("the summary carries lines that name no failure:\n%s", summary)
	}
}

// TestVerificationSummary_APassingCommandKeepsTheShortForm: one line.
func TestVerificationSummary_APassingCommandKeepsTheShortForm(t *testing.T) {
	summary := verificationSummary(orchestration.ValidationResult{
		Passed: true, Message: makeEnvNoise + "\nok  \tgithub.com/acme/api\t0.1s",
	})
	if summary != makeEnvNoise {
		t.Fatalf("summary = %q, want the first line alone", summary)
	}
}

// TestVerificationSummary_IsBounded: however much fails, the summary is a
// record, not the output.
func TestVerificationSummary_IsBounded(t *testing.T) {
	var stdout strings.Builder
	// Enough to exceed every bound — more failing lines than the summary keeps,
	// each longer than a summary line — and no more: production input reaches
	// here already cut to VerificationEvidenceBytes, and redaction is linear, so
	// megabytes here would only spend minutes proving the same thing.
	long := strings.Repeat("x", 2*VerificationSummaryLineBytes)
	for i := 0; i < 4*(VerificationSummaryMarkedLines+VerificationSummaryTailLines); i++ {
		stdout.WriteString("--- FAIL: TestCase" + strconv.Itoa(i) + " " + long + "\n")
	}
	summary := verificationSummary(orchestration.ValidationResult{Message: stdout.String(), Details: stdout.String()})
	lines := strings.Split(summary, "\n")
	if max := VerificationSummaryMarkedLines + 2*VerificationSummaryTailLines; len(lines) > max {
		t.Errorf("summary lines = %d, want at most %d", len(lines), max)
	}
	for _, line := range lines {
		if len(line) > VerificationSummaryLineBytes+len(" …") {
			t.Fatalf("a summary line is %d bytes, want at most %d", len(line), VerificationSummaryLineBytes)
		}
	}
}

// TestBoundedLine_CutsOnARuneBoundary: a cut never splits a character.
func TestBoundedLine_CutsOnARuneBoundary(t *testing.T) {
	got := boundedLine("aé", 2)
	if got != "a …" {
		t.Fatalf("boundedLine = %q, want the whole rune dropped", got)
	}
	if got := boundedLine("short", 10); got != "short" {
		t.Fatalf("boundedLine = %q, want a short line untouched", got)
	}
}

// TestValidateInContainer_ASummaryKeepsAMidLineErrorBeyondTheEvidenceTail is
// the path the summary actually reads: stdout reaches it only through the
// evidence, which keeps the tail and the failure-naming lines. A compiler's
// mid-line `error:` on stdout, followed by more than the evidence bound of
// ordinary output, must survive into both the evidence and the summary.
func TestValidateInContainer_ASummaryKeepsAMidLineErrorBeyondTheEvidenceTail(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make test")
	var out strings.Builder
	out.WriteString(makeEnvNoise + "\n")
	out.WriteString("src/luks.c:10:3: error: unknown type name 'foo'\n")
	for out.Len() < 2*VerificationEvidenceBytes {
		out.WriteString("cc -O2 -c src/other.c -o build/other.o\n")
	}
	rig.result = &orchestration.ExecResult{ExitCode: 2, Stdout: out.String(), Stderr: "make: *** [build] Error 2\n"}

	result, err := ValidateInContainer(context.Background(), rig.job,
		orchestration.ReviewCriterion{Name: "make test", Config: "make test"})
	if err != nil {
		t.Fatalf("ValidateInContainer: %v", err)
	}
	const failing = "src/luks.c:10:3: error: unknown type name 'foo'"
	if !strings.Contains(result.Details, failing) {
		t.Fatalf("the evidence dropped the failing line:\n%s", result.Details[:512])
	}
	summary := verificationSummary(result)
	for _, want := range []string{failing, "make: *** [build] Error 2"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the summary lacks %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, makeEnvNoise) {
		t.Errorf("the summary carries the harmless first line:\n%s", summary)
	}
}

// TestVerificationSummary_RedactsBeforeItCuts: a credential that straddles
// the summary's line cut is redacted whole, not cut to a fragment redaction
// no longer recognises; so is a PEM block whose delimiters no selection keeps.
func TestVerificationSummary_RedactsBeforeItCuts(t *testing.T) {
	const token = "ghp_" + "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"
	const pemBody = "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC7"
	stderr := strings.Repeat("w", VerificationSummaryLineBytes-10) + " " + token + "\n" +
		"-----BEGIN PRIVATE KEY-----\n" + pemBody + "\n-----END PRIVATE KEY-----\n" +
		"make: *** [test] Error 1\n"
	summary := verificationSummary(orchestration.ValidationResult{Message: stderr})
	for _, leaked := range []string{"ghp_", token[4:14], pemBody[:16]} {
		if strings.Contains(summary, leaked) {
			t.Errorf("the summary carries a credential fragment %q:\n%s", leaked, summary)
		}
	}
	if !strings.Contains(summary, "make: *** [test] Error 1") {
		t.Errorf("the summary lost the failure:\n%s", summary)
	}
}

// TestVerificationExcerpt_KeepsTheFailingLinesWithinItsBudget: long harmless
// tail lines give up their room before a line that names the failure does.
func TestVerificationExcerpt_KeepsTheFailingLinesWithinItsBudget(t *testing.T) {
	var stderr strings.Builder
	stderr.WriteString("src/luks.c:10:3: error: unknown type name 'foo'\n")
	for i := 0; i < 20; i++ {
		stderr.WriteString("warning: harmless " + strconv.Itoa(i) + " " + strings.Repeat("h", 400) + "\n")
	}
	stderr.WriteString("make: *** [build] Error 2\n")
	result := orchestration.ValidationResult{Message: stderr.String()}
	const budget = 600
	excerpt := verificationExcerpt(result, budget)
	if len(excerpt) > budget {
		t.Errorf("excerpt = %d bytes, want at most %d", len(excerpt), budget)
	}
	for _, want := range []string{"error: unknown type name 'foo'", "make: *** [build] Error 2"} {
		if !strings.Contains(excerpt, want) {
			t.Errorf("the excerpt lacks %q:\n%s", want, excerpt)
		}
	}
	if !strings.HasPrefix(excerpt, "src/luks.c") {
		t.Errorf("the excerpt is out of the summary's order:\n%s", excerpt)
	}
	if got := verificationExcerpt(result, 20); len(got) > 20 || got == "" {
		t.Errorf("a budget no whole line fits = %q, want a cut failing line within it", got)
	}
}

// TestVerificationVerdict_NamesTheExitStatusWhenThereIsOne: an escalation
// says `make lint` exited 2 when the command ran, and falls back to plain
// "failed" only for a command that has no exit status because it never ran.
func TestVerificationVerdict_NamesTheExitStatusWhenThereIsOne(t *testing.T) {
	for name, tc := range map[string]struct {
		result orchestration.ValidationResult
		want   string
	}{
		"ran and exited non-zero": {orchestration.ValidationResult{Name: "make lint", ExitCode: 2}, "exited 2"},
		"refused before it ran":   {orchestration.ValidationResult{Name: "rm", Message: "refused"}, "failed"},
	} {
		if got := verificationVerdict(tc.result); got != tc.want {
			t.Errorf("%s: verdict = %q, want %q", name, got, tc.want)
		}
	}
}
