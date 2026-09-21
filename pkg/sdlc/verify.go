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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// container_command — the validating-phase validator
// =============================================================================
//
// A binding's verification commands are the project's own build and tests,
// which is to say ARBITRARY CODE FROM A TENANT'S REPOSITORY. The engine's
// built-in "command" kind runs a criterion on the daemon; for an sdlc
// attempt that is wrong twice over.
//
// IT IS THE TRUST BOUNDARY THE CONTAINER EXISTS TO HOLD. A tenant's `make
// build` running as the daemon user, on the daemon host, is the thing #608
// closed for the delivery's git. Verification is the same class.
//
// AND IT IS NOT THE ENVIRONMENT THE WORK WAS DONE IN. The worker built and
// tested inside its image; a daemon that re-runs those commands answers
// about a host with a different toolchain, or none, and sends the worker a
// round of feedback about a difference it cannot see or fix.
//
// So the module registers its own kind, and it keeps the command validator's
// contract exactly: the operator's ExecutionGuard decides, ON THE DAEMON,
// whether a command may run at all — the allowlist is operator policy and
// belongs where the operator's configuration is — and only a command it
// admits runs, inside the worker's container, in the checkout the
// container's own entrypoint made. There is no second path: a job with no
// container is refused rather than judged somewhere else.

// ValidatorKindContainerCommand is the criterion kind this validator
// registers.
const ValidatorKindContainerCommand = "container_command"

// verificationShell is what a criterion's command line is run by.
//
// ExecSpec executes an argument vector DIRECTLY, never through a shell, and
// says a caller wanting shell semantics asks for a shell by name. A
// verification command is a shell command line — "make build && make test"
// is one string with an operator in it — so this asks.
const verificationShell = "sh"

// ValidateInContainer is the container_command validator: an
// orchestration.ValidatorFunc that runs one verification command inside the
// job's worker container and reports its exit status as the verdict.
func ValidateInContainer(ctx context.Context, job *orchestration.JobContext, criterion orchestration.ReviewCriterion) (orchestration.ValidationResult, error) {
	failed := orchestration.ValidationResult{Name: criterion.Name, Passed: false}
	if job == nil {
		return failed, &InvalidInputError{
			Field:   "job",
			Message: "a verification command is judged against a running job and this call carries none",
		}
	}
	command := strings.TrimSpace(criterion.Config)
	if command == "" {
		return failed, &InvalidInputError{
			Field:   "config",
			Message: "a verification criterion IS its command; there is nothing to run",
		}
	}
	// The allowlist check stays on the daemon. It is the OPERATOR's gate over
	// tenant data, built from daemon configuration, and it decides whether
	// the command runs at all; the container decides nothing about it.
	if err := job.Guard.CheckCommand(criterion.Name, command); err != nil {
		failed.Message = err.Error()
		return failed, err
	}
	if job.Container == nil {
		return failed, &VerificationNotContainerizedError{TaskID: job.TaskID, Criterion: criterion.Name}
	}
	result, err := job.Container.Exec(ctx, []string{verificationShell, "-c", command})
	if err != nil {
		return failed, err
	}
	if result.Succeeded() {
		return orchestration.ValidationResult{
			Name:    criterion.Name,
			Passed:  true,
			Message: strings.TrimSpace(result.Stdout),
		}, nil
	}
	failed.Message = strings.TrimSpace(result.Stderr)
	failed.Details = verificationEvidence(result)
	return failed, nil
}

// verificationEvidence is what a failing command said, for the round of
// feedback the worker gets.
//
// BOTH STREAMS ARE EVIDENCE. A build tool that reports its failure on stdout
// — go test does, make does — would otherwise send the worker back with an
// exit code and nothing to act on.
// VerificationEvidenceBytes bounds how much of a failed verification
// command's output is handed back to the worker: its LAST bytes, because that
// is where a build error, a `--- FAIL` and a `make: *** Error` land.
//
// THE OUTPUT USED TO BE HANDED OVER WHOLE, and `make test` on a large project
// prints megabytes. On the burn-in fleet (go-quicbaas #51 and #965,
// 2026-09-21) the round-1 prompt that carried it was refused by the CLI's
// context guard (`blocking_limit`) after three minutes, and once the session
// was rolled, by the API itself (`prompt_too_long`) in under a minute — a
// fresh session with nothing in it but the contract and this feedback. Every
// attempt that reached verification and failed it died there, whatever the
// failure was.
//
// 16 KiB is roughly a hundred lint findings or a few failing tests with
// their assertions, and it is a bound on the PROMPT, not on the evidence: the
// full output stays on the job's verification record.
const VerificationEvidenceBytes = 16 * 1024

func verificationEvidence(result *orchestration.ExecResult) string {
	evidence := "exit code: " + strconv.Itoa(result.ExitCode)
	stdout, truncated := boundedTail(result.Stdout, VerificationEvidenceBytes)
	if stdout == "" {
		return evidence
	}
	if !truncated {
		return evidence + "\n\n" + stdout
	}
	// THE LINES THAT NAME THE FAILURE COME FIRST, gathered from the whole
	// output, because the tail alone loses them: `go test -v` on this
	// repository prints tens of thousands of PASS lines after the failing
	// package, and three rounds on go-quicbaas#62 showed the worker a bare
	// trailing `FAIL` with no package name behind it.
	if salient := salientFailureLines(result.Stdout, VerificationSalientLines); salient != "" {
		evidence += "\n\nlines naming the failure, from the whole output:\n\n" + salient
	}
	evidence += "\n\n(the first " + strconv.Itoa(len(result.Stdout)-len(stdout)) +
		" bytes of output are omitted; this is its last " + strconv.Itoa(VerificationEvidenceBytes) + " bytes)"
	return evidence + "\n\n" + stdout
}

// VerificationSalientLines bounds how many failure-naming lines are gathered
// from a failed command's whole output ahead of its tail.
const VerificationSalientLines = 200

// salientFailurePrefixes are the line starts that name a failure in the tools
// this factory verifies with: Go's test and build reporters and make. A line
// is kept for the prefix alone; nothing is parsed out of it.
var salientFailurePrefixes = []string{"--- FAIL", "FAIL", "panic:", "# ", "make: ***", "ok  \t"}

// salientFailureLines returns, in order, the lines of output that name a
// failure — a failed test, a failed package, a panic, a compiler's package
// header, make's own error — up to limit lines. Passing packages (`ok`) are
// excluded again here: their prefix is listed above only so the check reads
// as the reporter's vocabulary, and they are what the tail already drowns in.
func salientFailureLines(output string, limit int) string {
	var b strings.Builder
	kept := 0
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if !salientFailureLine(trimmed) {
			continue
		}
		if kept == limit {
			b.WriteString("… (more lines omitted)\n")
			break
		}
		b.WriteString(trimmed + "\n")
		kept++
	}
	return strings.TrimSpace(b.String())
}

// salientFailureLine reports whether one line names a failure.
func salientFailureLine(line string) bool {
	if strings.HasPrefix(line, "ok  \t") {
		return false
	}
	for _, prefix := range salientFailurePrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	// A compiler diagnostic: path.go:line:col: message.
	if i := strings.Index(line, ".go:"); i > 0 {
		rest := line[i+len(".go:"):]
		if j := strings.IndexByte(rest, ':'); j > 0 {
			if _, err := strconv.Atoi(rest[:j]); err == nil {
				return true
			}
		}
	}
	return false
}
