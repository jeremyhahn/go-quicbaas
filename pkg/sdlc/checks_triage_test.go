// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cifault"
)

// The log tails below are the distinguishing lines of real Gitea Actions job
// logs from 2026-09-12, the same corpus pkg/cifault/testdata holds whole. They
// are quoted here rather than read from that package's testdata because what
// this file tests is the SEAM — the module's records becoming the classifier's
// inputs — and a fixture large enough to be realistic would hide which line
// the assertion depends on.
// stamp is the RFC3339Nano prefix act_runner puts on every log line. The
// fixtures carry it because a real log does, and because the tail a forge
// serves is byte-bounded: an UNSTAMPED first line is a line cut in half, and
// cifault.StripTimestamps drops it rather than quote half a sentence. A
// fixture written without stamps therefore loses its own first line, which is
// usually the decisive one.
const stamp = "2026-09-12T17:58:40.5536932Z "

const (
	// logGenuine is a red the change owns: a failed recipe, then the runner's
	// own end-of-job marker. From job 77312.
	logGenuine = stamp + "FAIL\tgithub.com/jeremyhahn/go-quicbaas/pkg/agent\t1.161s\n" +
		stamp + "make: *** [Makefile:516: vet] Error 1\n" +
		stamp + "Cleaning up container for job Lint\n" +
		stamp + "Job failed\n"

	// logRunnerDied is a suite that simply stops: no marker, nothing failing.
	// From job 77459, which carried zero FAIL lines in 43,787.
	logRunnerDied = stamp + "=== RUN   TestSession_ConcurrentCloseDetection\n" +
		stamp + "--- PASS: TestSession_ConcurrentCloseDetection (0.00s)\n" +
		stamp + "PASS\n" +
		stamp + "ok  \tgithub.com/jeremyhahn/go-quicbaas/pkg/api/websocket\t35.451s\n"

	// logHostDefect is a container the OCI runtime refused. From job 77604.
	logHostDefect = stamp + "docker run image=forge.internal:3001/jeremyhahn/ci-runner:latest\n" +
		stamp + "failed to start container: Error response from daemon: failed to create task for container: " +
		"failed to create shim task: OCI runtime create failed: runc create failed: unable to start " +
		"container process: error during container init: error mounting \"/var/lib/lxcfs/proc/diskstats\"\n"

	// logModuleResolution is a private module the toolchain could not resolve.
	// From job 77532, with the fixture binding's forge owner in place of the
	// real one. "acme" is what testBinding declares.
	logModuleResolution = stamp + "Running go vet...\n" +
		stamp + "go: github.com/acme/go-auth@v0.2.0-alpha: reading github.com/acme/go-auth/go.mod " +
		"at revision v0.2.0-alpha: unknown revision v0.2.0-alpha\n" +
		stamp + "make: *** [Makefile:516: vet] Error 1\n" +
		stamp + "Job failed\n"

	// logRegistryUnreachable is a job image that never arrived. From job 77520.
	logRegistryUnreachable = stamp + "docker pull image=forge.internal:3001/jeremyhahn/ci-runner:latest\n" +
		stamp + "failed to pull image 'forge.internal:3001/jeremyhahn/ci-runner:latest' (): Error response " +
		"from daemon: http: server gave HTTP response to HTTPS client\n"
)

// failedChecks builds a settled failure over the named checks.
func failedChecks(head string, names ...string) ChecksFailure {
	failure := ChecksFailure{HeadSHA: head, SettledAt: time.Unix(1000, 0)}
	for _, name := range names {
		failure.Failed = append(failure.Failed, CheckRecord{
			Name: name, HeadSHA: head, Status: CheckFailure, FinishedAt: time.Unix(1000, 0),
		})
	}
	return failure
}

// TestTriageChecksFailure_ClassifiesEachCheck covers the seam over one real log
// per verdict.
func TestTriageChecksFailure_ClassifiesEachCheck(t *testing.T) {
	failure := failedChecks("h1", "build", "lint", "test", "security")
	triage := TriageChecksFailure(failure, map[string]string{
		"build":    logGenuine,
		"lint":     logModuleResolution,
		"test":     logRunnerDied,
		"security": logHostDefect,
	}, []string{fixtureOwner})

	want := map[string]cifault.Verdict{
		"build":    cifault.VerdictGenuine,
		"lint":     cifault.VerdictHostDefect,
		"test":     cifault.VerdictVoid,
		"security": cifault.VerdictHostDefect,
	}
	for name, verdict := range want {
		if got := triage.Verdict(name); got != verdict {
			t.Errorf("%s: verdict = %q, want %q", name, got, verdict)
		}
	}
	// Genuine outranks everything: one real failure among three faults still
	// reaches the agent.
	if got := triage.Aggregate(); got != cifault.VerdictGenuine {
		t.Fatalf("Aggregate = %q, want genuine", got)
	}
	if got := len(triage.Of(cifault.VerdictHostDefect)); got != 2 {
		t.Errorf("Of(host_defect) returned %d entries, want 2", got)
	}
	if got := len(triage.Of(cifault.VerdictVoid)); got != 1 {
		t.Errorf("Of(void) returned %d entries, want 1", got)
	}
	infrastructure := triage.NotTheChanges()
	if len(infrastructure) != 3 {
		t.Fatalf("NotTheChanges = %v, want the three faults", infrastructure)
	}
	if strings.Contains(strings.Join(infrastructure, " "), "build") {
		t.Errorf("the change's own failure was named as infrastructure: %v", infrastructure)
	}
}

// TestTriageChecksFailure_BadInput covers the inputs a caller can get wrong: no
// failed checks, no logs at all, a status cifault refuses, and a nil owner
// list.
func TestTriageChecksFailure_BadInput(t *testing.T) {
	if got := TriageChecksFailure(ChecksFailure{}, nil, nil); len(got) != 0 {
		t.Errorf("an empty failure triaged %d checks", len(got))
	}

	// No log read at all: absence of evidence reaches the agent.
	noLogs := TriageChecksFailure(failedChecks("h1", "test"), nil, nil)
	if got := noLogs.Verdict("test"); got != cifault.VerdictGenuine {
		t.Errorf("with no log: verdict = %q, want genuine", got)
	}

	// A status the classifier has no rule for. Reaching here is a module bug,
	// and the safe reading of a module bug is the pre-classifier behaviour.
	odd := ChecksFailure{HeadSHA: "h1", Failed: []CheckRecord{{Name: "test", HeadSHA: "h1", Status: CheckPending}}}
	refused := TriageChecksFailure(odd, map[string]string{"test": logRunnerDied}, nil)
	if got := refused.Verdict("test"); got != cifault.VerdictGenuine {
		t.Errorf("an unclassifiable status: verdict = %q, want genuine", got)
	}
	if !strings.Contains(refused[0].Finding.Evidence, "could not be classified") {
		t.Errorf("evidence = %q, want it to say the classification was refused", refused[0].Finding.Evidence)
	}

	// A name nothing triaged answers genuine rather than the zero verdict.
	if got := ChecksTriage(nil).Verdict("absent"); got != cifault.VerdictGenuine {
		t.Errorf("an untriaged name: verdict = %q, want genuine", got)
	}
	if got := ChecksTriage(nil).Aggregate(); got != cifault.VerdictGenuine {
		t.Errorf("an empty triage: Aggregate = %q, want genuine", got)
	}
}

// TestTriageChecksFailure_PrivateOwnerDecidesTheModuleVerdict pins the one rule
// whose answer depends on the project: the same log is a host defect for the
// owner whose modules only a provisioned host resolves, and the change's own
// mistake for anybody else.
func TestTriageChecksFailure_PrivateOwnerDecidesTheModuleVerdict(t *testing.T) {
	failure := failedChecks("h1", "lint")
	logs := map[string]string{"lint": logModuleResolution}

	mine := TriageChecksFailure(failure, logs, []string{fixtureOwner})
	if got := mine.Verdict("lint"); got != cifault.VerdictHostDefect {
		t.Errorf("for the project's own owner: verdict = %q, want host_defect", got)
	}
	theirs := TriageChecksFailure(failure, logs, []string{"somebody-else"})
	if got := theirs.Verdict("lint"); got != cifault.VerdictGenuine {
		t.Errorf("for an unrelated owner: verdict = %q, want genuine", got)
	}
}

// TestChecksTriage_AccountRedacts covers the operator's account, including the
// redaction it owes the forge issue it is posted to.
func TestChecksTriage_AccountRedacts(t *testing.T) {
	triage := TriageChecksFailure(failedChecks("h1", "security"), map[string]string{
		"security": logRegistryUnreachable + stamp + "token=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ\n",
	}, nil)
	account := triage.Account("h1")
	for _, want := range []string{"h1", "security", "host_defect", "registry_unreachable", "failed to pull image"} {
		if !strings.Contains(account, want) {
			t.Errorf("account lacks %q:\n%s", want, account)
		}
	}
	if strings.Contains(account, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Fatal("a credential reached the operator's account, which is posted to the forge")
	}
}

// TestChecksTriage_AccountBadInput covers an empty triage and an empty head.
func TestChecksTriage_AccountBadInput(t *testing.T) {
	if got := ChecksTriage(nil).Account(""); got == "" {
		t.Fatal("an empty triage rendered nothing; an operator must still learn a decision was made")
	}
}

// TestChecksFailure_FindingsQuoteOnlyTheChangesOwnFailures covers the rendering
// the worker is handed: the genuine reds are named and quoted, the
// infrastructure ones are named as not its problem and not quoted.
func TestChecksFailure_FindingsQuoteOnlyTheChangesOwnFailures(t *testing.T) {
	failure := failedChecks("h1", "build", "test")
	logs := map[string]string{"build": logGenuine, "test": logRunnerDied}
	triage := TriageChecksFailure(failure, logs, nil)

	text := failure.Findings(triage, logs, DefaultChecksLogBytes)
	for _, want := range []string{"FAILED: build", "Makefile:516", "test (runner_died)", "Do not try to fix them"} {
		if !strings.Contains(text, want) {
			t.Errorf("findings lack %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "FAILED: test") {
		t.Errorf("an infrastructure fault was handed to the worker as its own:\n%s", text)
	}
	if strings.Contains(text, "pkg/api/websocket") {
		t.Errorf("the void check's log was quoted to the worker:\n%s", text)
	}
}

// TestChecksFailure_FindingsBadInput covers a nil triage, a nil log map and a
// zero quote budget.
func TestChecksFailure_FindingsBadInput(t *testing.T) {
	failure := failedChecks("h1", "build")
	// A nil triage answers genuine for every name, so the check is still named.
	if text := failure.Findings(nil, nil, DefaultChecksLogBytes); !strings.Contains(text, "FAILED: build") {
		t.Fatalf("a nil triage must still name the failed check:\n%s", text)
	}
	// A zero budget is boundedTail's "unbounded", so the excerpt is whole
	// rather than absent.
	text := failure.Findings(nil, map[string]string{"build": logGenuine}, 0)
	if !strings.Contains(text, "Makefile:516") {
		t.Fatalf("a zero quote budget dropped the excerpt:\n%s", text)
	}
}

// TestDelivery_RerunBookkeeping covers the budget and the pending test the
// whole bound rests on.
func TestDelivery_RerunBookkeeping(t *testing.T) {
	finished := time.Unix(2000, 0)
	check := CheckRecord{Name: "test", HeadSHA: "h1", Status: CheckFailure, FinishedAt: finished}
	delivery := &Delivery{TaskID: testTask}

	if got := delivery.RerunAttempts("h1", "test"); got != 0 {
		t.Errorf("a fresh delivery has spent %d attempts", got)
	}
	if delivery.RerunPending(check) {
		t.Error("a fresh delivery reported a re-run pending")
	}

	// An ask made BEFORE this result is the ask this result answers, so it is
	// not pending.
	delivery.RecordRerun(CIRerunRecord{HeadSHA: "h1", Check: "test", Signature: "runner_died", At: finished.Add(-time.Minute)})
	if delivery.RerunPending(check) {
		t.Error("an ask older than the result read as pending; the engine would wait on an answer it already has")
	}
	if got := delivery.RerunAttempts("h1", "test"); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}

	// An ask made AFTER it is one the forge has not answered.
	delivery.RecordRerun(CIRerunRecord{HeadSHA: "h1", Check: "test", Signature: "runner_died", At: finished.Add(time.Minute)})
	if !delivery.RerunPending(check) {
		t.Error("an ask newer than the result did not read as pending")
	}
	if got := delivery.RerunAttempts("h1", "test"); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

// TestDelivery_RerunBookkeepingBadInput covers the records that must NOT count:
// another head, another check, and a re-run the forge refused.
func TestDelivery_RerunBookkeepingBadInput(t *testing.T) {
	finished := time.Unix(2000, 0)
	check := CheckRecord{Name: "test", HeadSHA: "h1", FinishedAt: finished}
	delivery := &Delivery{TaskID: testTask, CIReruns: []CIRerunRecord{
		{HeadSHA: "h2", Check: "test", At: finished.Add(time.Minute)},
		{HeadSHA: "h1", Check: "lint", At: finished.Add(time.Minute)},
		{HeadSHA: "h1", Check: "test", At: finished.Add(time.Minute), Failure: "the forge refused"},
	}}
	if got := delivery.RerunAttempts("h1", "test"); got != 1 {
		t.Errorf("attempts = %d, want 1: only this head's own check counts", got)
	}
	if delivery.RerunPending(check) {
		t.Error("a re-run the forge REFUSED read as pending; nothing was restarted to wait for")
	}
}

// TestMergeRerunRecords_IsAppendOnlyAndExactlyOnce covers the conflict path.
// The budget is a count, so a record applied twice escalates a healthy fleet
// and one dropped never bounds anything.
func TestMergeRerunRecords_IsAppendOnlyAndExactlyOnce(t *testing.T) {
	at := time.Unix(3000, 0)
	mine := CIRerunRecord{HeadSHA: "h1", Check: "test", Signature: "runner_died", At: at}
	theirs := CIRerunRecord{HeadSHA: "h1", Check: "lint", Signature: "runner_died", At: at}

	merged := mergeRerunRecords([]CIRerunRecord{theirs}, []CIRerunRecord{mine})
	if len(merged) != 2 {
		t.Fatalf("merged = %+v, want both records", merged)
	}
	again := mergeRerunRecords(merged, []CIRerunRecord{mine})
	if len(again) != 2 {
		t.Fatalf("re-applying a written record added it twice: %+v", again)
	}
	if got := mergeRerunRecords(nil, nil); len(got) != 0 {
		t.Errorf("merging nothing produced %+v", got)
	}
}

// TestCIHostDefectError_Reports covers the error the run parks on.
func TestCIHostDefectError_Reports(t *testing.T) {
	err := &CIHostDefectError{HeadSHA: "0123456789abcdef", Reason: "the registry is unreachable"}
	if !strings.Contains(err.Error(), "the registry is unreachable") {
		t.Errorf("Error() = %q", err.Error())
	}
	if !errors.Is(err, ErrCIHostDefect) {
		t.Error("the error does not unwrap to ErrCIHostDefect")
	}
	if err.ParkReason() != "the registry is unreachable" {
		t.Errorf("ParkReason() = %q", err.ParkReason())
	}
}

// TestCIHostDefectError_EmptyStillReports covers the bad-input half.
func TestCIHostDefectError_EmptyStillReports(t *testing.T) {
	err := &CIHostDefectError{}
	if err.Error() == "" {
		t.Fatal("an empty host-defect error rendered as nothing")
	}
	if !errors.Is(err, ErrCIHostDefect) {
		t.Error("the error does not unwrap to ErrCIHostDefect")
	}
}
