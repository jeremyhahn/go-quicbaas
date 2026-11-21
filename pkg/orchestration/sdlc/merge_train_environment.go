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
	"sort"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// Environmental reds
// =============================================================================
//
// A TRAIN'S RED IS NOT ALWAYS ITS CARS'. The classifier (TriageChecksFailure)
// sets aside a runner that died and a host that is broken, but a check that
// stalls on a loaded runner until its job limit kills it reads as an ordinary
// failure. Ejecting on it charges the change a review round for something no
// change to the code could fix, and a change already at its round limit is
// escalated to a person (go-objstore#52: approved and green, ejected because
// its Security Scan ran 1h45m on the train against 31m on its own head).
//
// THE RULE (owner's lead, 2026-09-28). A red is environmental when, for every
// failed check, BOTH hold:
//
//   - the check passed on each car's own head in that car's pull request CI;
//   - the train's run of it timed out, was cancelled, or ran more than
//     EnvironmentalSlowdown times as long as the slowest of those passing runs.
//
// An environmental red re-runs the failed checks on the same commit — the
// train retried with the same cars — once per train, with no ejection and no
// review round. A second red ejects as any red does, and the ejection names
// the suspicion so the person reading it sees why.

// EnvironmentalSlowdown is how many times as long as its passing run on the
// car's own head a train's run of a check must take before its red is judged
// the environment's rather than the change's.
const EnvironmentalSlowdown = 3

// auditTrainEnvironmentRetry records every environmental retry.
const auditTrainEnvironmentRetry = "sdlc:train_environment_retry"

// checkRunTimedOut is the conclusion a forge gives a run its job limit ended.
const checkRunTimedOut = "timed_out"

// latestCheckRun returns the newest finished run of the named check and how
// long it took, zero when the records do not say when it started.
//
// A RUN MAY SPAN SEVERAL RECORDS. A check-run forge (GitHub) gives one record
// with both times; Gitea posts a pending status when the job is queued and a
// second status when it ends, each stamped with its own creation time. So a
// run starts at the first record after the previous run finished, and ends at
// the record that finished it — which covers both shapes and a re-run.
func latestCheckRun(runs []ForgeCheckRun, name string) (ForgeCheckRun, time.Duration, bool) {
	var named []ForgeCheckRun
	for _, run := range runs {
		if run.Name == name {
			named = append(named, run)
		}
	}
	sort.SliceStable(named, func(i, j int) bool { return named[i].Started.Before(named[j].Started) })

	var (
		last     ForgeCheckRun
		took     time.Duration
		found    bool
		runStart time.Time
		inRun    bool
	)
	for _, run := range named {
		if !inRun {
			runStart, inRun = run.Started, true
		}
		if !strings.EqualFold(run.Status, "completed") {
			continue
		}
		last, found, inRun = run, true, false
		took = 0
		if !runStart.IsZero() && !run.Completed.IsZero() && run.Completed.After(runStart) {
			took = run.Completed.Sub(runStart)
		}
	}
	return last, took, found
}

// environmentalRed reports whether the train's red on check is the
// environment's by the rule above, and the one-line account a person reads.
// carRuns holds the check runs of each car head in carHeads; a car the caller
// read nothing for is not judged in the train's favour.
func environmentalRed(check string, trainRuns []ForgeCheckRun, carRuns map[string][]ForgeCheckRun, carHeads []string) (string, bool) {
	if strings.TrimSpace(check) == "" || len(carHeads) == 0 {
		return "", false
	}
	trainRun, trainTook, ok := latestCheckRun(trainRuns, check)
	if !ok {
		return "", false
	}
	var passedTook time.Duration
	for _, head := range carHeads {
		runs, read := carRuns[head]
		if !read {
			return "", false
		}
		carRun, took, ok := latestCheckRun(runs, check)
		if !ok || checkStatusFor(carRun) != CheckSuccess {
			return "", false
		}
		passedTook = max(passedTook, took)
	}

	passed := check + " passed on the change's own head"
	if passedTook > 0 {
		passed += " in " + passedTook.String()
	}
	switch {
	case strings.EqualFold(trainRun.Conclusion, checkRunTimedOut):
		return check + " timed out on the train; " + passed, true
	case checkStatusFor(trainRun) == CheckCancelled:
		return check + " was cancelled on the train; " + passed, true
	case passedTook > 0 && trainTook > EnvironmentalSlowdown*passedTook:
		return check + " ran " + trainTook.String() + " on the train, more than " + strconv.Itoa(EnvironmentalSlowdown) +
			" times as long; " + passed, true
	}
	return "", false
}

// environmentalSuspicion judges a probe's settled, genuine-looking red by the
// rule: the account when every failed check is environmental, empty when any
// is not. It reads each car's own head once.
func (s *trainRun) environmentalSuspicion(ctx context.Context, t *MergeTrain, probe *TrainProbe, failure ChecksFailure, runs []ForgeCheckRun) (string, error) {
	if len(failure.Failed) == 0 || probe.Length <= 0 || probe.Length > len(t.Cars) {
		return "", nil
	}
	cars := t.Cars[:probe.Length]
	heads := make([]string, 0, len(cars))
	carRuns := make(map[string][]ForgeCheckRun, len(cars))
	for _, car := range cars {
		heads = append(heads, car.HeadSHA)
		if _, read := carRuns[car.HeadSHA]; read {
			continue
		}
		read, err := s.forge.ListCheckRuns(ctx, s.repo, car.HeadSHA)
		if err != nil {
			return "", err
		}
		carRuns[car.HeadSHA] = read
	}
	accounts := make([]string, 0, len(failure.Failed))
	for _, failed := range failure.Failed {
		account, ok := environmentalRed(failed.Name, runs, carRuns, heads)
		if !ok {
			return "", nil
		}
		accounts = append(accounts, account)
	}
	return strings.Join(accounts, "; "), nil
}

// retryEnvironmental re-runs the failed checks of an environmental red on the
// probe's commit — the same cars — and answers pending so the wait continues.
//
// ONCE PER TRAIN. A spent retry, or a forge with no re-run, answers the red as
// genuine with the suspicion kept on the probe for the ejection to name. A
// forge that refuses the re-run it advertises is a host defect, as it is for
// a void red: no change is ejected for it.
func (s *trainRun) retryEnvironmental(ctx context.Context, t *MergeTrain, probe *TrainProbe, suspicion string, failure ChecksFailure, runs []ForgeCheckRun) (probeVerdict, bool, error) {
	switch {
	case t.EnvironmentRetried:
		probe.EnvironmentSuspicion = suspicion + ". The train already retried once with the same changes"
		return probeVerdict{}, false, nil
	case !s.forge.Capabilities().RerunCheckRuns:
		probe.EnvironmentSuspicion = suspicion + ". " + string(s.forge.Type()) + " cannot re-run a finished check, so the train could not retry it"
		return probeVerdict{}, false, nil
	}
	byName := make(map[string]ForgeCheckRun, len(runs))
	for _, run := range latestCheckPerContext(runs) {
		byName[run.Name] = run
	}
	for _, failed := range failure.Failed {
		if err := s.forge.RerunCheckRun(ctx, s.repo, byName[failed.Name]); err != nil {
			return probeVerdict{}, true, s.hostDefect(ctx, t, probe, "the forge refused to re-run "+failed.Name+
				" for an environmental retry: "+err.Error())
		}
	}
	t.EnvironmentRetried = true
	t.EnvironmentSuspicion = suspicion
	probe.RerunAt = s.runner.now()
	if err := s.saveLedger(ctx); err != nil {
		return probeVerdict{}, true, err
	}
	names := make([]string, 0, len(failure.Failed))
	for _, failed := range failure.Failed {
		names = append(names, failed.Name)
	}
	s.job.Audit(auditTrainEnvironmentRetry, "train="+strconv.Itoa(t.Number)+" prefix="+strconv.Itoa(probe.Length)+
		" head="+probe.HeadSHA+" check="+strings.Join(names, ",")+" reason="+suspicion)
	return probeVerdict{pending: true}, true, nil
}

// environmentalDetail is what an ejection for checks adds when the red, or an
// earlier red of the same train, looked like the environment's; empty when
// neither did.
func environmentalDetail(t *MergeTrain, probe *TrainProbe) string {
	switch {
	case probe != nil && probe.EnvironmentSuspicion != "":
		return " It looked like the CI environment rather than this change: " + probe.EnvironmentSuspicion + "."
	case t.EnvironmentRetried:
		return " An earlier red of this train looked like the CI environment rather than this change (" +
			t.EnvironmentSuspicion + "), so the train retried once with the same changes; it failed again."
	}
	return ""
}
