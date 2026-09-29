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
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Environmental reds: the rule
// =============================================================================

// envT0 anchors every timeline below.
var envT0 = time.Date(2026, 9, 25, 18, 15, 0, 0, time.UTC)

// finishedRun is one check run that started at start and finished took later.
func finishedRun(name, sha, conclusion string, start time.Time, took time.Duration) ForgeCheckRun {
	return ForgeCheckRun{ID: name + "@" + sha + "@" + start.String(), Name: name, HeadSHA: sha, Status: "completed",
		Conclusion: conclusion, Started: start, Completed: start.Add(took)}
}

// giteaRun is the record trail Gitea leaves for one run: a pending status
// when the job is queued, and a finished status posted when it ends. Each
// record's Started is the status's own creation time.
func giteaRun(name, sha, conclusion string, start time.Time, took time.Duration) []ForgeCheckRun {
	end := start.Add(took)
	return []ForgeCheckRun{
		{ID: name + "-p-" + start.String(), Name: name, HeadSHA: sha, Status: "pending", Started: start, Completed: start},
		{ID: name + "-f-" + end.String(), Name: name, HeadSHA: sha, Status: "completed", Conclusion: conclusion, Started: end, Completed: end},
	}
}

func TestEnvironmentalRed_TheRule(t *testing.T) {
	const check = "CI / Security Scan"
	carHead := "car-head"
	tests := []struct {
		name  string
		train []ForgeCheckRun
		car   []ForgeCheckRun
		want  bool
		cause string
	}{
		{
			name:  "slow on the train and passed on the car's head (the go-objstore#52 case)",
			train: giteaRun(check, "train", "failure", envT0, 105*time.Minute+51*time.Second),
			car:   giteaRun(check, carHead, "success", envT0.Add(-2*time.Hour), 31*time.Minute+37*time.Second),
			want:  true, cause: "ran 1h45m51s",
		},
		{
			name:  "cancelled on the train and passed on the car's head",
			train: []ForgeCheckRun{finishedRun(check, "train", "cancelled", envT0, time.Minute)},
			car:   []ForgeCheckRun{finishedRun(check, carHead, "success", envT0, 10*time.Minute)},
			want:  true, cause: "was cancelled",
		},
		{
			name:  "timed out on the train and passed on the car's head",
			train: []ForgeCheckRun{finishedRun(check, "train", "timed_out", envT0, 6*time.Minute)},
			car:   []ForgeCheckRun{finishedRun(check, carHead, "success", envT0, 5*time.Minute)},
			want:  true, cause: "timed out",
		},
		{
			name:  "failed on the train in ordinary time",
			train: []ForgeCheckRun{finishedRun(check, "train", "failure", envT0, 20*time.Minute)},
			car:   []ForgeCheckRun{finishedRun(check, carHead, "success", envT0, 10*time.Minute)},
		},
		{
			name:  "exactly three times as slow is not more than three times",
			train: []ForgeCheckRun{finishedRun(check, "train", "failure", envT0, 30*time.Minute)},
			car:   []ForgeCheckRun{finishedRun(check, carHead, "success", envT0, 10*time.Minute)},
		},
		{
			name:  "slow on the train but it failed on the car's head too",
			train: []ForgeCheckRun{finishedRun(check, "train", "failure", envT0, 2*time.Hour)},
			car:   []ForgeCheckRun{finishedRun(check, carHead, "failure", envT0, 10*time.Minute)},
		},
		{
			name:  "slow on the train but the car's head never ran the check",
			train: []ForgeCheckRun{finishedRun(check, "train", "failure", envT0, 2*time.Hour)},
			car:   []ForgeCheckRun{finishedRun("CI / Build", carHead, "success", envT0, 10*time.Minute)},
		},
		{
			name:  "no start time on the train run proves no slowness",
			train: []ForgeCheckRun{{Name: check, HeadSHA: "train", Status: "completed", Conclusion: "failure", Completed: envT0}},
			car:   []ForgeCheckRun{finishedRun(check, carHead, "success", envT0, 10*time.Minute)},
		},
		{
			name:  "the newest train run is the one judged, after a re-run",
			train: append(giteaRun(check, "train", "failure", envT0, 2*time.Hour), giteaRun(check, "train", "failure", envT0.Add(3*time.Hour), 12*time.Minute)...),
			car:   []ForgeCheckRun{finishedRun(check, carHead, "success", envT0, 10*time.Minute)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suspicion, ok := environmentalRed(check, tt.train, map[string][]ForgeCheckRun{carHead: tt.car}, []string{carHead})
			if ok != tt.want {
				t.Fatalf("environmentalRed = %v (%q), want %v", ok, suspicion, tt.want)
			}
			if tt.want && !strings.Contains(suspicion, tt.cause) {
				t.Fatalf("suspicion %q does not say the train's run %s", suspicion, tt.cause)
			}
		})
	}
}

// TestEnvironmentalRed_RefusesWhatItCannotJudge covers the bad inputs: no
// check name, no car, and a car the caller read nothing for.
func TestEnvironmentalRed_RefusesWhatItCannotJudge(t *testing.T) {
	train := []ForgeCheckRun{finishedRun("ci", "train", "failure", envT0, 2*time.Hour)}
	car := map[string][]ForgeCheckRun{"h": {finishedRun("ci", "h", "success", envT0, time.Minute)}}
	if _, ok := environmentalRed("", train, car, []string{"h"}); ok {
		t.Fatal("a nameless check was judged environmental")
	}
	if _, ok := environmentalRed("ci", train, car, nil); ok {
		t.Fatal("a red with no car was judged environmental")
	}
	if _, ok := environmentalRed("ci", train, car, []string{"unread"}); ok {
		t.Fatal("a car whose checks were never read was judged environmental")
	}
	if _, ok := environmentalRed("ci", nil, car, []string{"h"}); ok {
		t.Fatal("a check the train never ran was judged environmental")
	}
}

// =============================================================================
// Environmental reds: the train
// =============================================================================

// envRig is a one-car train whose car passed "ci" in ten minutes on its own
// head, and whose train run of "ci" failed after forty: environmental by the
// rule. The clock is the test's, so a re-run can settle.
type envRig struct {
	*trainRig
	head string

	clockMu sync.Mutex
	clock   time.Time
}

func newEnvRig(t *testing.T) *envRig {
	rig := &envRig{trainRig: newTrainRig(t), clock: envT0.Add(2 * time.Hour)}
	rig.runner.now = func() time.Time {
		rig.clockMu.Lock()
		defer rig.clockMu.Unlock()
		return rig.clock
	}
	rig.queue(101, "task-a")
	rig.forge.mu.Lock()
	rig.forge.checks["head-101"] = append(rig.forge.checks["head-101"],
		finishedRun("ci", "head-101", "success", envT0.Add(-time.Hour), 10*time.Minute))
	rig.forge.mu.Unlock()
	rig.head = trainCommit(1, 101)
	rig.redTrain(envT0, 40*time.Minute)
	return rig
}

// redTrain scripts the train's run of "ci" as a genuine-looking red. Callers
// inside onListCheckRuns hold the forge lock already, so this writes directly.
func (r *envRig) redTrain(start time.Time, took time.Duration) {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	r.setTrainRun(start, took, "failure")
}

func (r *envRig) setTrainRun(start time.Time, took time.Duration, conclusion string) {
	r.forge.checks[r.head] = []ForgeCheckRun{finishedRun("ci", r.head, conclusion, start, took)}
	r.forge.logs["ci"] = genuineRed
}

// afterRerun scripts what the train reads once it asked for a re-run: the
// clock moves past the re-run, and the new run ends as conclusion after took.
func (r *envRig) afterRerun(conclusion string, took time.Duration) {
	r.forge.onListCheckRuns = func(sha string) {
		if sha != r.head || len(r.forge.reruns) == 0 {
			return
		}
		r.clockMu.Lock()
		if r.forge.checks[sha][0].Started.Before(r.clock) {
			start := r.clock.Add(time.Minute)
			r.clock = start.Add(took + time.Hour)
			r.setTrainRun(start, took, conclusion)
		}
		r.clockMu.Unlock()
	}
}

// TestTrain_AnEnvironmentalRedRetriesTheTrainOnceAndLands is the
// go-objstore#52 case: the car passed the check on its own head, the train's
// run of it ran far longer and failed. The train re-runs it on the same
// commit rather than ejecting the car, audits the retry, and lands.
func TestTrain_AnEnvironmentalRedRetriesTheTrainOnceAndLands(t *testing.T) {
	rig := newEnvRig(t)
	rig.afterRerun("success", 10*time.Minute)

	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reruns := rig.forge.rerunCalls(); len(reruns) != 1 || reruns[0].HeadSHA != rig.head || reruns[0].Name != "ci" {
		t.Fatalf("reruns = %+v, want one of ci on %s", reruns, rig.head)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded {
		t.Fatalf("entry = %+v, want landed once the retry went green", got)
	}
	if ejected := rig.audited(auditTrainEjectedPR); len(ejected) != 0 {
		t.Fatalf("an environmental red ejected the change: %v", ejected)
	}
	retries := rig.audited(auditTrainEnvironmentRetry)
	if len(retries) != 1 || !strings.Contains(retries[0], "train=1") || !strings.Contains(retries[0], "check=ci") ||
		!strings.Contains(retries[0], "ran 40m0s") {
		t.Fatalf("retry audits = %v, want one naming the train, the check and why", retries)
	}
}

// TestTrain_ASecondEnvironmentalRedEjectsAndSaysWhy proves the retry is
// spent once: the same red again ejects the car as any red does, and the
// ejection tells the person reading it the failure looked environmental.
func TestTrain_ASecondEnvironmentalRedEjectsAndSaysWhy(t *testing.T) {
	rig := newEnvRig(t)
	rig.afterRerun("failure", 45*time.Minute)

	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reruns := rig.forge.rerunCalls(); len(reruns) != 1 {
		t.Fatalf("reruns = %+v, want exactly one retry", reruns)
	}
	got := rig.entry("task-a")
	if got.State != TrainEntryEjected || got.Ejection == nil || got.Ejection.Reason != TrainEjectChecksFailed {
		t.Fatalf("entry = %+v, want ejected for its checks", got)
	}
	for _, want := range []string{"environment", "ci ran 45m0s", "retried"} {
		if !strings.Contains(got.Ejection.Detail, want) {
			t.Fatalf("ejection detail %q does not say %q", got.Ejection.Detail, want)
		}
	}
	if !strings.Contains(got.Ejection.Findings, "got 1, want 2") {
		t.Fatalf("the worker is not told what failed:\n%s", got.Ejection.Findings)
	}
}

// TestTrain_AnOrdinaryRedIsNotRetried proves the rule is narrow: a red in
// ordinary time on a check the car passed is the change's, ejected at once.
func TestTrain_AnOrdinaryRedIsNotRetried(t *testing.T) {
	rig := newEnvRig(t)
	rig.redTrain(envT0, 12*time.Minute)

	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reruns := rig.forge.rerunCalls(); len(reruns) != 0 {
		t.Fatalf("an ordinary red was retried: %+v", reruns)
	}
	got := rig.entry("task-a")
	if got.State != TrainEntryEjected || strings.Contains(got.Ejection.Detail, "environment") {
		t.Fatalf("entry = %+v, want ejected with no environmental suspicion", got)
	}
	if retries := rig.audited(auditTrainEnvironmentRetry); len(retries) != 0 {
		t.Fatalf("an ordinary red audited a retry: %v", retries)
	}
}

// TestTrain_AnEnvironmentalRedTheForgeWillNotRerunStopsTheTrain proves a
// refused retry ejects no one: like a refused void re-run, it is a host
// defect for an operator.
func TestTrain_AnEnvironmentalRedTheForgeWillNotRerunStopsTheTrain(t *testing.T) {
	rig := newEnvRig(t)
	rig.forge.rerunErr = errors.New("rerun refused")

	err := rig.run(orchestration.JobStateInitializing)
	if !errors.Is(err, ErrCIHostDefect) {
		t.Fatalf("Run = %v, want a CI host defect", err)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryQueued {
		t.Fatalf("entry = %+v, want back in the queue", got)
	}
}

// TestTrain_AnEnvironmentalRedOnAForgeThatCannotRerunEjectsAndSaysWhy proves
// a forge with no re-run is not waited on: the car is ejected as today, and
// the ejection names the suspicion and why no retry was made.
func TestTrain_AnEnvironmentalRedOnAForgeThatCannotRerunEjectsAndSaysWhy(t *testing.T) {
	rig := newEnvRig(t)
	rig.forge.caps.RerunCheckRuns = false

	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := rig.entry("task-a")
	if got.State != TrainEntryEjected {
		t.Fatalf("entry = %+v, want ejected", got)
	}
	for _, want := range []string{"environment", "cannot re-run"} {
		if !strings.Contains(got.Ejection.Detail, want) {
			t.Fatalf("ejection detail %q does not say %q", got.Ejection.Detail, want)
		}
	}
	if n := len(rig.forge.rerunCalls()); n != 0 {
		t.Fatalf("reruns = %d on a forge that cannot re-run", n)
	}
}
