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
	"net/http"
	"time"
)

// =============================================================================
// Gitea transport: deadlines, retry and write recovery
// =============================================================================
//
// Every call goes through do, and do decides three things from the call alone:
// how long to wait, whether an unanswered call may be sent again, and what an
// unanswered write becomes. See ForgeTransport for why the three are split.

// giteaWriteRecovery is what the adapter does with a write the forge did not
// answer.
type giteaWriteRecovery int

const (
	// recoverUnconfirmed surfaces ForgeWriteUnconfirmedError and never sends
	// the write again. The default: a write nobody has classified is assumed
	// to be one that repeating could apply twice.
	recoverUnconfirmed giteaWriteRecovery = iota

	// recoverReplay sends the write again under the read retry bound. Only
	// for a write that is idempotent on the forge AND whose method verifies
	// the result by reading it back, so a replay can neither double the
	// effect nor hide a write that did not land.
	recoverReplay

	// recoverReconcile hands the unanswered call back to the method, which
	// settles it by reading the forge's state. Only for the writes whose
	// outcome a read can prove: a merge and a pull request's creation.
	recoverReconcile
)

// giteaWritePolicy is how one write is waited on and recovered.
type giteaWritePolicy struct {
	// heavy selects HeavyWriteTimeout: the forge does real work before it
	// answers.
	heavy bool

	// recovery is what an unanswered write becomes.
	recovery giteaWriteRecovery
}

// giteaWritePolicies is the one table of what every write the adapter makes
// does when the forge does not answer, keyed by the request's operation.
//
// A WRITE MISSING FROM IT IS NEVER REPEATED: it takes the zero policy — a
// short write, unconfirmed. So forgetting a new write here fails safe (an
// error the engine attributes to the forge) rather than open (a duplicate).
//
//	operation           deadline  unanswered  why
//	MergePullRequest    heavy     reconcile   re-read the PR: merged with a merge commit is success; open and
//	                                          mergeable is not-ready, so the bounded merge retry decides
//	CreatePullRequest   heavy     reconcile   adopt the open PR for the same head -> base if the create
//	                                          landed; the forge refuses a duplicate, so a lookup is the proof
//	SetIssueLabels      write     replay      PUT replaces the whole set; the method reads it back
//	AddIssueLabels      write     replay      Gitea's POST is a set union; the method reads it back
//	RemoveIssueLabel    write     replay      a replayed DELETE answers 404, which the method accepts
//	CloseIssue          write     replay      PATCH to a state; the response is checked
//	ClosePullRequest    write     replay      PATCH to a state; the method reads the pull request back
//	DeleteBranch        write     replay      a replayed DELETE answers 404, which the method accepts
//	SetCommitStatus     write     replay      the latest status per context wins; read back
//	UpdateWebhook       write     replay      PATCH to a configuration; EnsureWebhook reads it back
//	DeleteWebhook       write     replay      a replayed DELETE answers 404, which the caller accepts
//	CommentOnIssue      write     once        a second post is a second comment; the Blocked hook's own
//	                                          bounded post retry is the one place a duplicate is accepted
//	CreateBranch        write     once        a second create is refused as a conflict, not reconciled
//	CreateIssue         write     once        a second create is a second issue
//	SubmitReview        write     once        a second review is a second verdict
//	RerunCheckRun       write     once        a second rerun is a second pipeline
//	CreateWebhook       write     once        a second hook doubles every delivery
//	CreateLabel         write     once        a second create is refused as a conflict
//	gitea.MintToken     write     once        a second token is a live credential nobody holds
//	gitea.RevokeToken   write     once        a replayed DELETE answers 404, which the method reports
var giteaWritePolicies = map[string]giteaWritePolicy{
	"MergePullRequest":  {heavy: true, recovery: recoverReconcile},
	"CreatePullRequest": {heavy: true, recovery: recoverReconcile},
	"SetIssueLabels":    {recovery: recoverReplay},
	"AddIssueLabels":    {recovery: recoverReplay},
	"RemoveIssueLabel":  {recovery: recoverReplay},
	"CloseIssue":        {recovery: recoverReplay},
	"ClosePullRequest":  {recovery: recoverReplay},
	"DeleteBranch":      {recovery: recoverReplay},
	"SetCommitStatus":   {recovery: recoverReplay},
	"UpdateWebhook":     {recovery: recoverReplay},
	"DeleteWebhook":     {recovery: recoverReplay},
	"CommentOnIssue":    {recovery: recoverUnconfirmed},
	"CreateBranch":      {recovery: recoverUnconfirmed},
	"CreateIssue":       {recovery: recoverUnconfirmed},
	"SubmitReview":      {recovery: recoverUnconfirmed},
	"RerunCheckRun":     {recovery: recoverUnconfirmed},
	"CreateWebhook":     {recovery: recoverUnconfirmed},
	"CreateLabel":       {recovery: recoverUnconfirmed},
	"gitea.MintToken":   {recovery: recoverUnconfirmed},
	"gitea.RevokeToken": {recovery: recoverUnconfirmed},
}

// giteaReadMethods are the HTTP methods that only read, and are therefore
// always safe to send again.
var giteaReadMethods = map[string]bool{
	http.MethodGet:  true,
	http.MethodHead: true,
}

// giteaGatewayStatuses are the answers that come from IN FRONT of the forge
// rather than from it: the gateway could not reach Gitea, or gave up waiting.
// They say nothing about the request, so they are treated as no answer at all.
var giteaGatewayStatuses = map[int]bool{
	http.StatusBadGateway:         true,
	http.StatusServiceUnavailable: true,
	http.StatusGatewayTimeout:     true,
}

// giteaCallPolicy is what do applies to one call.
type giteaCallPolicy struct {
	deadline time.Duration
	attempts int
	recovery giteaWriteRecovery
}

// callPolicy derives the policy for one request: a read retries under the read
// deadline; a write takes its entry in giteaWritePolicies.
func (g *GiteaBackend) callPolicy(req giteaRequest) giteaCallPolicy {
	if giteaReadMethods[req.method] {
		return giteaCallPolicy{
			deadline: g.transport.ReadTimeout,
			attempts: g.transport.ReadAttempts,
			recovery: recoverReplay,
		}
	}
	write := giteaWritePolicies[req.operation]
	policy := giteaCallPolicy{deadline: g.transport.WriteTimeout, attempts: 1, recovery: write.recovery}
	if write.heavy {
		policy.deadline = g.transport.HeavyWriteTimeout
	}
	if write.recovery == recoverReplay {
		policy.attempts = g.transport.ReadAttempts
	}
	return policy
}

// do performs one API call under its policy.
//
// ONLY AN UNANSWERED CALL IS EVER SENT AGAIN. Any answer the forge actually
// gave — a 404, a 409, a 422, Gitea's own 5xx — is final and returned as it
// was, because the forge has said what it did.
func (g *GiteaBackend) do(ctx context.Context, req giteaRequest) error {
	policy := g.callPolicy(req)
	var last error
	for attempt := 1; attempt <= policy.attempts; attempt++ {
		err := g.send(ctx, req, policy.deadline)
		if err == nil || !errors.Is(err, ErrForgeUnanswered) {
			return err
		}
		last = err
		if attempt == policy.attempts {
			break
		}
		if waitErr := g.waiter.Wait(ctx, attempt); waitErr != nil {
			return waitErr
		}
	}
	switch policy.recovery {
	case recoverReconcile:
		// The method settles it by reading the forge's state.
		return last
	case recoverReplay:
		return &ForgeRetriesExhaustedError{Type: ForgeGitea, Operation: req.operation, Attempts: policy.attempts, Last: last}
	default:
		return &ForgeWriteUnconfirmedError{Type: ForgeGitea, Operation: req.operation, Subject: req.path, Cause: last}
	}
}

// unanswered classifies a transport failure on one attempt.
//
// A failure the CALLER caused — its context cancelled or past its own deadline
// — is not the forge failing to answer, and is reported as it was: a job being
// cancelled must not be retried, and must not be attributed to the forge.
func unanswered(ctx context.Context, operation string, cause error) error {
	if ctx.Err() != nil {
		return backendError(operation, cause)
	}
	return &ForgeUnansweredError{Type: ForgeGitea, Operation: operation, Cause: cause}
}
