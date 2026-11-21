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
	"errors"
	"strings"
	"testing"
)

// stubForge is a minimal ForgeBackend for registry tests. It implements the
// interface so the compiler checks the shape, and nothing else.
type stubForge struct {
	forgeType ForgeType
	baseURL   string
	caps      ForgeCapabilities
}

// registerStub registers a factory building a stub for one platform.
func registerStub(t ForgeType) {
	RegisterForgeFactory(t, func(cfg ForgeConfig, _ ForgeCredentials, _ ForgeTransport) (ForgeBackend, error) {
		return &stubForge{forgeType: t, baseURL: cfg.BaseURL}, nil
	})
}

func (s *stubForge) Type() ForgeType                 { return s.forgeType }
func (s *stubForge) Capabilities() ForgeCapabilities { return s.caps }
func (s *stubForge) AuthenticatedUser(context.Context) (*TokenIdentity, error) {
	return nil, nil
}

func (s *stubForge) ListIssues(context.Context, RepoRef, IssueFilter) ([]ForgeIssue, error) {
	return nil, nil
}
func (s *stubForge) GetIssue(context.Context, RepoRef, int) (*ForgeIssue, error) { return nil, nil }
func (s *stubForge) CommentOnIssue(context.Context, RepoRef, int, string) (*ForgeComment, error) {
	return nil, nil
}
func (s *stubForge) ListIssueComments(context.Context, RepoRef, int) ([]ForgeComment, error) {
	return nil, nil
}
func (s *stubForge) CommentOnPullRequest(context.Context, RepoRef, int, string) (*ForgeComment, error) {
	return nil, nil
}
func (s *stubForge) ListPullRequestComments(context.Context, RepoRef, int) ([]ForgeComment, error) {
	return nil, nil
}
func (s *stubForge) AddPullRequestLabels(context.Context, RepoRef, int, []string) error { return nil }
func (s *stubForge) RemovePullRequestLabel(context.Context, RepoRef, int, string) error { return nil }
func (s *stubForge) SetIssueLabels(context.Context, RepoRef, int, []string) error       { return nil }
func (s *stubForge) AddIssueLabels(context.Context, RepoRef, int, []string) error       { return nil }
func (s *stubForge) RemoveIssueLabel(context.Context, RepoRef, int, string) error       { return nil }
func (s *stubForge) CreateBranch(context.Context, RepoRef, string, string) (*ForgeBranch, error) {
	return nil, nil
}
func (s *stubForge) GetBranch(context.Context, RepoRef, string) (*ForgeBranch, error) {
	return nil, nil
}
func (s *stubForge) GetTag(context.Context, RepoRef, string) (*ForgeTag, error) {
	return nil, nil
}
func (s *stubForge) DeleteBranch(context.Context, RepoRef, string) error { return nil }
func (s *stubForge) CreatePullRequest(context.Context, RepoRef, PullRequestSpec) (*ForgePullRequest, error) {
	return nil, nil
}
func (s *stubForge) ListOpenPullRequests(context.Context, RepoRef) ([]ForgePullRequest, error) {
	return nil, nil
}
func (s *stubForge) CloseIssue(context.Context, RepoRef, int) error { return nil }
func (s *stubForge) CreateIssue(context.Context, RepoRef, IssueSpec) (*ForgeIssue, error) {
	return nil, nil
}
func (s *stubForge) ListCollaborators(context.Context, RepoRef) ([]ForgeUser, error) {
	return nil, nil
}

func (s *stubForge) GetPullRequest(context.Context, RepoRef, int) (*ForgePullRequest, error) {
	return nil, nil
}
func (s *stubForge) MergePullRequest(context.Context, RepoRef, int, MergeSpec, string) error {
	return nil
}
func (s *stubForge) ListReviews(context.Context, RepoRef, int) ([]ForgeReview, error) {
	return nil, nil
}
func (s *stubForge) ListCheckRuns(context.Context, RepoRef, string) ([]ForgeCheckRun, error) {
	return nil, nil
}
func (s *stubForge) CheckRunLogs(context.Context, RepoRef, ForgeCheckRun, int) (string, error) {
	return "", nil
}
func (s *stubForge) SubmitReview(context.Context, RepoRef, int, ReviewSubmission) (*ForgeReview, error) {
	return nil, nil
}
func (s *stubForge) SetCommitStatus(context.Context, RepoRef, string, CommitStatus) error {
	return nil
}
func (s *stubForge) ListPullRequestFiles(context.Context, RepoRef, int) ([]ForgeChangedFile, error) {
	return nil, nil
}
func (s *stubForge) ClosePullRequest(context.Context, RepoRef, int) error        { return nil }
func (s *stubForge) CancelCheckRun(context.Context, RepoRef, string) error       { return nil }
func (s *stubForge) RerunCheckRun(context.Context, RepoRef, ForgeCheckRun) error { return nil }
func (s *stubForge) VerifyWebhook([]byte, string, []byte) error                  { return nil }
func (s *stubForge) ParseWebhook(string, []byte) (*ForgeEvent, error)            { return nil, nil }
func (s *stubForge) EnsureWebhook(context.Context, RepoRef, WebhookSpec, []byte) error {
	return nil
}
func (s *stubForge) Labels(context.Context, RepoRef) ([]ForgeLabel, error)     { return nil, nil }
func (s *stubForge) EnsureLabels(context.Context, RepoRef, []LabelSpec) error  { return nil }
func (s *stubForge) Webhooks(context.Context, RepoRef) ([]ForgeWebhook, error) { return nil, nil }
func (s *stubForge) DeleteWebhook(context.Context, RepoRef, string) error      { return nil }
func (s *stubForge) RepoFiles(context.Context, RepoRef, string, string) ([]ForgeFile, error) {
	return nil, nil
}
func (s *stubForge) BranchProtection(context.Context, RepoRef, string) (*ForgeBranchProtection, error) {
	return nil, nil
}

// withCleanForgeRegistry swaps in an empty registry for one test and restores
// the real one afterwards, so a test cannot see or disturb the adapters a
// production build registered.
func withCleanForgeRegistry(t *testing.T) {
	t.Helper()
	forgeMu.Lock()
	saved := forgeByType
	forgeByType = map[ForgeType]ForgeFactory{}
	forgeMu.Unlock()

	t.Cleanup(func() {
		forgeMu.Lock()
		forgeByType = saved
		forgeMu.Unlock()
	})
}

// =============================================================================
// Registry
// =============================================================================

// TestLookupForgeBackend_UnregisteredFailsClosed is the selection rule.
//
// A project pointed at a platform nothing implements must fail, never fall back
// to whichever adapter happens to be registered: the fallback would push a
// project's work to a repository on the wrong forge, and the first symptom
// would be commits appearing somewhere nobody was looking.
func TestLookupForgeBackend_UnregisteredFailsClosed(t *testing.T) {
	withCleanForgeRegistry(t)
	registerStub(ForgeGitea)

	backend, err := NewForgeBackend(ForgeConfig{Type: ForgeGitHub}, ForgeCredentials{Token: "token"}, ForgeTransport{})
	if backend != nil {
		t.Fatal("an unregistered forge resolved to an adapter")
	}
	if !errors.Is(err, ErrForgeNotRegistered) {
		t.Fatalf("error = %v, want ErrForgeNotRegistered", err)
	}

	var typed *ForgeNotRegisteredError
	if !errors.As(err, &typed) || typed.Type != ForgeGitHub {
		t.Errorf("error does not name the forge that was asked for: %v", err)
	}
}

// TestLookupForgeBackend_ReturnsTheRegisteredAdapter covers the ordinary path.
func TestLookupForgeBackend_ReturnsTheRegisteredAdapter(t *testing.T) {
	withCleanForgeRegistry(t)
	registerStub(ForgeGitea)

	got, err := NewForgeBackend(ForgeConfig{Type: ForgeGitea, BaseURL: "http://a"}, ForgeCredentials{Token: "token"}, ForgeTransport{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Type() != ForgeGitea {
		t.Error("the factory built an adapter for a different platform")
	}
}

// TestNewForgeBackend_BuildsOnePerProject is why the registry holds factories
// rather than instances.
//
// Two projects may sit on different Gitea instances, or on one instance under
// different credentials. A single registered instance could serve only whichever
// project configured it first, and the rest would silently talk to the wrong
// server with the wrong token.
func TestNewForgeBackend_BuildsOnePerProject(t *testing.T) {
	withCleanForgeRegistry(t)
	registerStub(ForgeGitea)

	first, err := NewForgeBackend(ForgeConfig{Type: ForgeGitea, BaseURL: "http://gitea-a/api/v1"}, ForgeCredentials{Token: "token-a"}, ForgeTransport{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := NewForgeBackend(ForgeConfig{Type: ForgeGitea, BaseURL: "http://gitea-b/api/v1"}, ForgeCredentials{Token: "token-b"}, ForgeTransport{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first == second {
		t.Fatal("two projects were handed the same adapter; one is talking to the wrong forge")
	}
	if first.(*stubForge).baseURL == second.(*stubForge).baseURL {
		t.Error("the factory ignored the project's own base URL")
	}
}

// TestRegisterForgeBackend_RefusesNilAndDuplicates proves both wiring errors
// fail at init rather than at the first dispatch.
//
// A duplicate is the dangerous one: silently replacing an adapter would route a
// project's work to a different platform than the one its config names, and
// nothing downstream would report the substitution.
func TestRegisterForgeBackend_RefusesNilAndDuplicates(t *testing.T) {
	withCleanForgeRegistry(t)

	t.Run("nil", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("registering a nil factory did not panic")
			}
		}()
		RegisterForgeFactory(ForgeGitHub, nil)
	})

	t.Run("duplicate", func(t *testing.T) {
		registerStub(ForgeGitLab)
		defer func() {
			if recover() == nil {
				t.Error("registering a second adapter for one forge did not panic")
			}
		}()
		registerStub(ForgeGitLab)
	})
}

// TestRegisteredForgeTypes_ReportsWhatThisBuildCanTalkTo covers the diagnostic
// an operator reads before wondering why a project will not register.
func TestRegisteredForgeTypes_ReportsWhatThisBuildCanTalkTo(t *testing.T) {
	withCleanForgeRegistry(t)
	registerStub(ForgeGitea)

	types := RegisteredForgeTypes()
	if len(types) != 1 || types[0] != ForgeGitea {
		t.Fatalf("registered types = %v, want exactly [gitea]", types)
	}
}

// =============================================================================
// Capabilities
// =============================================================================

// TestForgeCapabilities_DescribeThePlatformNotTheAdapter pins the meaning of a
// false flag.
//
// The engine plans around these: an uncancellable run has to be RECORDED as an
// orphan rather than assumed stopped. That only works if false means "this
// platform cannot", so an adapter is registered complete or not at all — there
// is deliberately no field for "not implemented yet", because a caller cannot
// distinguish it from the platform's own limit and would plan wrongly either
// way.
func TestForgeCapabilities_DescribeThePlatformNotTheAdapter(t *testing.T) {
	withCleanForgeRegistry(t)
	// Gitea's real shape: no cancel route, unreliable label deliveries.
	RegisterForgeFactory(ForgeGitea, func(cfg ForgeConfig, _ ForgeCredentials, _ ForgeTransport) (ForgeBackend, error) {
		return &stubForge{forgeType: ForgeGitea, baseURL: cfg.BaseURL, caps: ForgeCapabilities{
			CancelCheckRuns:     false,
			ReliableLabelEvents: false,
			SquashMerge:         true,
			DeleteBranchOnMerge: true,
		}}, nil
	})

	backend, err := NewForgeBackend(ForgeConfig{Type: ForgeGitea}, ForgeCredentials{Token: "token"}, ForgeTransport{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	caps := backend.Capabilities()

	if caps.CancelCheckRuns {
		t.Error("gitea reports it can cancel check runs; it exposes no such route")
	}
	if !caps.SquashMerge {
		t.Error("gitea reports it cannot squash-merge, which would refuse the default merge strategy")
	}
}

// TestForgeUnsupportedError_NamesTheCapability proves the error tells a caller
// what the platform lacks rather than which call was attempted.
//
// The caller's next move depends on the capability: no cancel route means
// record an orphan and surface it, which is a different response from a
// transient API failure on the same method.
func TestForgeUnsupportedError_NamesTheCapability(t *testing.T) {
	err := &ForgeUnsupportedError{Type: ForgeGitea, Capability: "cancelling check runs"}

	if !errors.Is(err, ErrForgeUnsupported) {
		t.Fatalf("error does not match ErrForgeUnsupported: %v", err)
	}
	if got := err.Error(); got != "sdlc: gitea does not support cancelling check runs" {
		t.Errorf("message = %q, which does not name the capability", got)
	}
}

// =============================================================================
// Read-back and webhook errors
// =============================================================================

// TestForgeWriteNotAppliedError_CarriesWhatItCompared is the diagnosis a
// read-back failure has to deliver.
//
// "A write failed" sends an operator to the API logs. "Wanted these labels, saw
// those" names the 200-that-did-nothing immediately, which is the failure mode
// read-back exists to catch.
func TestForgeWriteNotAppliedError_CarriesWhatItCompared(t *testing.T) {
	err := &ForgeWriteNotAppliedError{
		Type:      ForgeGitea,
		Operation: "SetIssueLabels",
		Subject:   "jeremyhahn/go-quicbaas#32",
		Want:      "[in-progress]",
		Got:       "[ready]",
	}

	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error does not match ErrForgeWriteNotApplied: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"SetIssueLabels", "jeremyhahn/go-quicbaas#32", "[in-progress]", "[ready]"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q omits %q", msg, want)
		}
	}
}

// TestWebhookUnverifiedError_QuotesNothingFromThePayload is a security
// property, not a formatting preference.
//
// The payload is unauthenticated by definition at the point this error is
// raised, so anything quoted from it into a log is attacker-controlled text
// landing in an operator's terminal.
func TestWebhookUnverifiedError_QuotesNothingFromThePayload(t *testing.T) {
	err := &WebhookUnverifiedError{Type: ForgeGitea, Reason: "signature length mismatch"}

	if !errors.Is(err, ErrWebhookUnverified) {
		t.Fatalf("error does not match ErrWebhookUnverified: %v", err)
	}
	if strings.Contains(err.Error(), "payload") {
		t.Error("the error message references payload content")
	}
}

// =============================================================================
// RepoRef
// =============================================================================

// TestRepoRef_RendersAsOwnerSlashRepo covers the form every adapter builds URLs
// and error subjects from.
func TestRepoRef_RendersAsOwnerSlashRepo(t *testing.T) {
	if got := (RepoRef{Owner: "jeremyhahn", Repo: "go-quicbaas"}).String(); got != "jeremyhahn/go-quicbaas" {
		t.Errorf("String() = %q, want jeremyhahn/go-quicbaas", got)
	}
}
