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
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/cifault"
)

// =============================================================================
// Triage — what each red check on one head actually means
// =============================================================================
//
// pkg/cifault answers for ONE check and knows nothing about forges,
// deliveries or reviews. This is the seam: it turns the module's records into
// that package's inputs, keeps the answers beside the checks they are about,
// and reduces them to the single thing this round does.
//
// Nothing here decides; see forge_review.go for who acts on what.

// CheckTriage is one failed check and what its red means.
type CheckTriage struct {
	// Check is the failed check as the delivery records it.
	Check CheckRecord

	// Finding is pkg/cifault's answer about it.
	Finding cifault.Finding
}

// ChecksTriage is the triage of every failed check on one head, ordered as
// ChecksFailure.Failed is.
type ChecksTriage []CheckTriage

// Verdict returns the verdict recorded for a check by name, and genuine for a
// name that was never triaged — the same fail-toward-the-agent default the
// classifier itself takes.
func (t ChecksTriage) Verdict(name string) cifault.Verdict {
	for _, entry := range t {
		if entry.Check.Name == name {
			return entry.Finding.Verdict
		}
	}
	return cifault.VerdictGenuine
}

// Aggregate reduces the triage to the one verdict this round acts on.
func (t ChecksTriage) Aggregate() cifault.Verdict {
	findings := make([]cifault.Finding, 0, len(t))
	for _, entry := range t {
		findings = append(findings, entry.Finding)
	}
	return cifault.Aggregate(findings)
}

// Of returns the entries carrying one verdict, in the triage's own order.
func (t ChecksTriage) Of(verdict cifault.Verdict) ChecksTriage {
	out := make(ChecksTriage, 0, len(t))
	for _, entry := range t {
		if entry.Finding.Verdict == verdict {
			out = append(out, entry)
		}
	}
	return out
}

// NotTheChanges names the checks whose red is not this change's, for the
// worker's prompt.
func (t ChecksTriage) NotTheChanges() []string {
	names := make([]string, 0, len(t))
	for _, entry := range t {
		if entry.Finding.Verdict.ReachesAgent() {
			continue
		}
		names = append(names, entry.Check.Name+" ("+entry.Finding.Signature.String()+")")
	}
	sort.Strings(names)
	return names
}

// Account renders the triage for an operator: every check, its signature, and
// the evidence the verdict rests on.
//
// REDACTED, because Evidence is a line quoted out of a job log and this text
// is posted to a forge issue. pkg/cifault does not redact — it does not own
// the policy — so the module does it at the one point the text leaves.
func (t ChecksTriage) Account(headSHA string) string {
	var b strings.Builder
	b.WriteString("Continuous integration is red on ")
	b.WriteString(headSHA)
	b.WriteString(", and the failure is not this change's — it is the machine that ran it. ")
	b.WriteString("No change to the code would turn it green, so this task is parked for an operator ")
	b.WriteString("rather than sent back to the agent.\n")
	for _, entry := range t {
		b.WriteString("\n")
		b.WriteString(entry.Check.Name)
		b.WriteString(": ")
		b.WriteString(entry.Finding.Verdict.String())
		b.WriteString(" / ")
		b.WriteString(entry.Finding.Signature.String())
		if evidence := strings.TrimSpace(redactSecrets(entry.Finding.Evidence)); evidence != "" {
			b.WriteString("\n  ")
			b.WriteString(evidence)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// TriageChecksFailure classifies every failed check on one head.
//
// privateModuleOwners is the project's own forge owner: the modules the
// toolchain can only resolve with credentials an operator provisions. It is
// what turns "unknown revision" from a tag the change forgot to push into a
// host that was never given GOPRIVATE. See cifault.Input.PrivateModuleOwners.
func TriageChecksFailure(failure ChecksFailure, logs map[string]string, privateModuleOwners []string) ChecksTriage {
	triage := make(ChecksTriage, 0, len(failure.Failed))
	for _, check := range failure.Failed {
		finding, err := cifault.Classify(cifault.Input{
			Conclusion:          checkConclusions[check.Status],
			Log:                 logs[check.Name],
			PrivateModuleOwners: privateModuleOwners,
		})
		if err != nil {
			// A CHECK THE CLASSIFIER WILL NOT ANSWER REACHES THE AGENT. The
			// only way here is a status ChecksFailure collected but cifault
			// has no rule for, which is a module bug rather than a fact about
			// the change — and the safe reading of a module bug is the one
			// that behaves exactly as this module did before the classifier
			// existed.
			finding = cifault.Finding{
				Verdict:   cifault.VerdictGenuine,
				Signature: cifault.SignatureUnrecognised,
				Evidence:  "this check's status could not be classified: " + err.Error(),
			}
		}
		triage = append(triage, CheckTriage{Check: check, Finding: finding})
	}
	return triage
}

// checkConclusions maps the module's check status onto the classifier's
// conclusion. Only the terminal, non-green statuses are here: a pending or
// successful check is never triaged, and cifault refuses both.
var checkConclusions = map[CheckStatus]cifault.Conclusion{
	CheckFailure:   cifault.ConclusionFailure,
	CheckCancelled: cifault.ConclusionCancelled,
}

// DefaultChecksRerunBudget is how many times ONE check on ONE head is
// re-run automatically before the fleet is escalated to an operator.
//
// THE BOUND IS THE WHOLE SAFETY OF THE VOID VERDICT. "Void" means "that red
// was not about the change; ask the machine again", and a host that is
// permanently broken answers void every time. Unbounded, the engine would
// spend CI forever and nobody would be told. Three is enough to ride out a
// runner that died once — the case this exists for — and small enough that a
// genuinely broken fleet reaches an operator within one review cycle.
const DefaultChecksRerunBudget = 3

// rerunBudgetSpent renders the escalation a spent budget becomes.
func rerunBudgetSpent(triage ChecksTriage, headSHA string, attempts int) string {
	var b strings.Builder
	b.WriteString("Continuous integration on ")
	b.WriteString(headSHA)
	b.WriteString(" has been re-run automatically ")
	b.WriteString(strconv.Itoa(attempts))
	b.WriteString(" times and came back with the same infrastructure fault every time, ")
	b.WriteString("so the runner fleet needs an operator rather than another attempt.\n")
	b.WriteString(triage.Account(headSHA))
	return b.String()
}

// rerunUnsupported renders the escalation a forge that cannot re-run becomes.
func rerunUnsupported(triage ChecksTriage, headSHA, why string) string {
	var b strings.Builder
	b.WriteString("Continuous integration on ")
	b.WriteString(headSHA)
	b.WriteString(" failed for a reason that is not this change's, and the engine could not re-run it: ")
	b.WriteString(why)
	b.WriteString("\n")
	b.WriteString(triage.Account(headSHA))
	return b.String()
}

// auditCIRerun is the job audit event of one automatic re-run the engine asked
// the forge for (go-quicbaas#1011): which check, on which head, why, and how
// much of the budget it spent. The durable record is the delivery's
// CIReruns; this is the line a reader of the job's own trail sees.
const auditCIRerun = "sdlc:ci_rerun"

// ciRerunAuditKey carries the running job's audit sink to the review gate.
type ciRerunAuditKey struct{}

// withCIRerunAudit hands the review gate the job's audit sink for the rounds
// run under ctx.
//
// THROUGH THE CONTEXT, because the gate is one validator shared by every job
// and a job's audit trail belongs to its run: the runner is the only party
// holding both, and it holds them exactly for the duration of the call.
func withCIRerunAudit(ctx context.Context, audit func(eventType, details string)) context.Context {
	if audit == nil {
		return ctx
	}
	return context.WithValue(ctx, ciRerunAuditKey{}, audit)
}

// auditRerun records one re-run on the job's audit trail, when the job that
// asked for it supplied one.
func auditRerun(ctx context.Context, record CIRerunRecord, attempt, budget int) {
	audit, ok := ctx.Value(ciRerunAuditKey{}).(func(eventType, details string))
	if !ok {
		return
	}
	details := "check=" + record.Check +
		" head=" + shortSHA(record.HeadSHA) +
		" run=" + record.ForgeRunID +
		" signature=" + record.Signature +
		" attempt=" + strconv.Itoa(attempt) + "/" + strconv.Itoa(budget)
	if record.Failure != "" {
		details += " refused=" + record.Failure
	}
	audit(auditCIRerun, details)
}
