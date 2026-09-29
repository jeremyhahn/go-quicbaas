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

package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// stubModels is a live catalog: backend -> models; a missing backend is not
// served.
type stubModels map[string][]string

func (m stubModels) ListBackendModels(_ context.Context, backend string) ([]string, error) {
	models, ok := m[backend]
	if !ok {
		return nil, &BackendNotFoundError{Backend: backend}
	}
	return models, nil
}

// stubReporter reports canned references.
type stubReporter struct {
	refs []AgentReference
	err  error
}

func (r *stubReporter) AgentReferences(context.Context, string) ([]AgentReference, error) {
	return r.refs, r.err
}

// stubAccounts knows one account per tenant.
// stubAccounts maps each account the tenant holds to its provider.
type stubAccounts map[string]AccountProvider

func (a stubAccounts) Get(_ context.Context, tenantID, accountID string) (*Account, error) {
	provider, ok := a[accountID]
	if !ok {
		return nil, &AccountNotFoundError{AccountID: accountID}
	}
	return &Account{ID: accountID, TenantID: tenantID, Provider: provider}, nil
}

func liveModels() stubModels {
	return stubModels{
		"claude":   {"claude-opus-5-5", "claude-fable-5"},
		"codex":    {"gpt-5.5"},
		"openvino": {},
	}
}

func newTestDirectory(t *testing.T) *AgentDirectory {
	t.Helper()
	dir, err := NewAgentDirectory(AgentDirectoryConfig{
		Store:    NewInMemoryAgentCatalogStore(),
		Models:   liveModels(),
		Accounts: stubAccounts{"acct-1": AccountProviderClaude, "codex-1": AccountProviderCodex},
	})
	if err != nil {
		t.Fatalf("NewAgentDirectory: %v", err)
	}
	return dir
}

func TestNewAgentDirectory_RequiresStoreAndModels(t *testing.T) {
	if _, err := NewAgentDirectory(AgentDirectoryConfig{Models: liveModels()}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no store: %v", err)
	}
	if _, err := NewAgentDirectory(AgentDirectoryConfig{Store: NewInMemoryAgentCatalogStore()}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no models: %v", err)
	}
}

func TestAgentDirectory_CreateAgentChecksTheLiveCatalog(t *testing.T) {
	ctx := context.Background()
	dir := newTestDirectory(t)

	good := testAgent("claude-coder", "claude", "claude-opus-5-5")
	if _, err := dir.CreateAgent(ctx, "acme", &good); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	// A backend with no curated catalog accepts any model.
	npu := testAgent("npu", "openvino", "qwen3-8b", AgentRoleTriage)
	if _, err := dir.CreateAgent(ctx, "acme", &npu); err != nil {
		t.Fatalf("an uncatalogued backend's model: %v", err)
	}

	unserved := testAgent("x", "vllm", "m")
	_, err := dir.CreateAgent(ctx, "acme", &unserved)
	var invalid *AgentCatalogInvalidError
	if !errors.As(err, &invalid) || invalid.Field != "agent.backend" {
		t.Fatalf("an unserved backend: %v", err)
	}
	wrongModel := testAgent("y", "codex", "gpt-4")
	_, err = dir.CreateAgent(ctx, "acme", &wrongModel)
	if !errors.As(err, &invalid) || invalid.Field != "agent.model" || len(invalid.Known) != 1 {
		t.Fatalf("a model the backend does not offer: %v", err)
	}
	if _, err := dir.CreateAgent(ctx, "acme", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a nil agent: %v", err)
	}
}

func TestAgentDirectory_CreateAgentChecksAccounts(t *testing.T) {
	ctx := context.Background()
	dir := newTestDirectory(t)
	agent := testAgent("a", "claude", "claude-opus-5-5")
	agent.Account = AgentAccountPolicy{Strategy: LeastUsageAccountStrategyName, Accounts: []string{"acct-1"}}
	if _, err := dir.CreateAgent(ctx, "acme", &agent); err != nil {
		t.Fatalf("a known account and strategy: %v", err)
	}
	bad := testAgent("b", "claude", "claude-opus-5-5")
	bad.Account = AgentAccountPolicy{Strategy: "round-robin"}
	if _, err := dir.CreateAgent(ctx, "acme", &bad); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an unknown strategy: %v", err)
	}
	bad.Account = AgentAccountPolicy{Accounts: []string{"ghost"}}
	if _, err := dir.CreateAgent(ctx, "acme", &bad); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("an unknown account: %v", err)
	}
	bad.Account = AgentAccountPolicy{Accounts: []string{"codex-1"}}
	if _, err := dir.CreateAgent(ctx, "acme", &bad); !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "codex-1") {
		t.Fatalf("another provider's account, which the backend would never draw on: %v", err)
	}
	local := testAgent("c", "openvino", "qwen")
	local.Account = AgentAccountPolicy{Accounts: []string{"acct-1"}}
	if _, err := dir.CreateAgent(ctx, "acme", &local); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("accounts on a backend that pools none: %v", err)
	}
	noAccounts, _ := NewAgentDirectory(AgentDirectoryConfig{Store: NewInMemoryAgentCatalogStore(), Models: liveModels()})
	if _, err := noAccounts.CreateAgent(ctx, "acme", &agent); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("naming accounts where none are kept: %v", err)
	}
}

func TestAgentDirectory_DeleteRefusesWhileReferenced(t *testing.T) {
	ctx := context.Background()
	dir := newTestDirectory(t)
	coder := testAgent("coder", "claude", "claude-opus-5-5")
	if _, err := dir.CreateAgent(ctx, "acme", &coder); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	pool := AgentPool{ID: "coders", Name: "Coders", Agents: []string{"coder"}}
	if _, err := dir.CreatePool(ctx, "acme", &pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	reporter := &stubReporter{refs: []AgentReference{
		{Ref: AgentRefTo("coder"), Usage: AgentUsage{Kind: AgentUsageKindBinding, ID: "proj-1", Slot: AgentUsageSlotWorker}},
		{Ref: PoolRefTo("coders"), Usage: AgentUsage{Kind: AgentUsageKindBinding, ID: "proj-2", Slot: AgentUsageSlotWorker}},
	}}
	dir.RegisterUsageReporter(reporter)
	dir.RegisterUsageReporter(nil)

	used, err := dir.UsedBy(ctx, "acme", AgentRefTo("coder"))
	if err != nil || len(used) != 2 {
		t.Fatalf("UsedBy(agent) = %v, %v; want the pool and the binding", used, err)
	}
	err = dir.DeleteAgent(ctx, "acme", "coder")
	var inUse *AgentInUseError
	if !errors.As(err, &inUse) || len(inUse.UsedBy) != 2 {
		t.Fatalf("DeleteAgent of a used agent = %v", err)
	}
	if err := dir.DeletePool(ctx, "acme", "coders"); !errors.Is(err, ErrAgentInUse) {
		t.Fatalf("DeletePool of a used pool = %v", err)
	}

	reporter.refs = nil
	if err := dir.DeletePool(ctx, "acme", "coders"); err != nil {
		t.Fatalf("DeletePool once unused: %v", err)
	}
	if err := dir.DeleteAgent(ctx, "acme", "coder"); err != nil {
		t.Fatalf("DeleteAgent once unused: %v", err)
	}
}

func TestAgentDirectory_UsedByRefusesUnknownAndPropagatesReporterErrors(t *testing.T) {
	ctx := context.Background()
	dir := newTestDirectory(t)
	if _, err := dir.UsedBy(ctx, "acme", AgentRefTo("ghost")); !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("UsedBy(unknown agent) = %v", err)
	}
	if _, err := dir.UsedBy(ctx, "acme", PoolRefTo("ghost")); !errors.Is(err, ErrAgentPoolUnknown) {
		t.Fatalf("UsedBy(unknown pool) = %v", err)
	}
	if _, err := dir.UsedBy(ctx, "acme", AgentRef{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UsedBy(zero) = %v", err)
	}
	agent := testAgent("a", "claude", "claude-opus-5-5")
	if _, err := dir.CreateAgent(ctx, "acme", &agent); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	boom := errors.New("bindings unreadable")
	dir.RegisterUsageReporter(&stubReporter{err: boom})
	if err := dir.DeleteAgent(ctx, "acme", "a"); !errors.Is(err, boom) {
		t.Fatalf("an unreadable owner must refuse the delete, got %v", err)
	}
}

// TestAgentDirectory_UpdateKeepsRolesItsSlotsNeed: dropping the review role
// from an Agent a reviewer seat uses would leave the seat bound to something
// that is not a reviewer.
func TestAgentDirectory_UpdateKeepsRolesItsSlotsNeed(t *testing.T) {
	ctx := context.Background()
	dir := newTestDirectory(t)
	reviewer := testAgent("codex-reviewer", "codex", "gpt-5.5", AgentRoleReview, AgentRoleTriage)
	if _, err := dir.CreateAgent(ctx, "acme", &reviewer); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	dir.RegisterUsageReporter(&stubReporter{refs: []AgentReference{{
		Ref:   AgentRefTo("codex-reviewer"),
		Usage: AgentUsage{Kind: AgentUsageKindBinding, ID: "proj", Slot: AgentUsageSlotReviewer + ":default"},
	}}})

	reviewer.Roles = []AgentRole{AgentRoleTriage}
	if _, err := dir.UpdateAgent(ctx, "acme", &reviewer); !errors.Is(err, ErrAgentRoleMismatch) {
		t.Fatalf("dropping a used role = %v", err)
	}
	reviewer.Roles = []AgentRole{AgentRoleReview}
	if _, err := dir.UpdateAgent(ctx, "acme", &reviewer); err != nil {
		t.Fatalf("dropping an unused role: %v", err)
	}
	unserved := reviewer
	unserved.Backend = "vllm"
	if _, err := dir.UpdateAgent(ctx, "acme", &unserved); !errors.Is(err, ErrAgentCatalogInvalid) {
		t.Fatalf("an update is checked against the live catalog: %v", err)
	}
}

func TestAgentDirectory_UpdatePoolKeepsMembersFitForItsSlots(t *testing.T) {
	ctx := context.Background()
	dir := newTestDirectory(t)
	triage := testAgent("npu", "openvino", "qwen3-8b", AgentRoleTriage)
	coder := testAgent("coder", "claude", "claude-opus-5-5")
	for _, agent := range []*Agent{&triage, &coder} {
		if _, err := dir.CreateAgent(ctx, "acme", agent); err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}
	}
	pool := AgentPool{ID: "triagers", Name: "Triagers", Agents: []string{"npu"}}
	if _, err := dir.CreatePool(ctx, "acme", &pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	dir.RegisterUsageReporter(&stubReporter{refs: []AgentReference{{
		Ref: PoolRefTo("triagers"), Usage: AgentUsage{Kind: AgentUsageKindBinding, ID: "proj", Slot: AgentUsageSlotTriage},
	}}})
	pool.Agents = []string{"npu", "coder"}
	if _, err := dir.UpdatePool(ctx, "acme", &pool); !errors.Is(err, ErrAgentRoleMismatch) {
		t.Fatalf("a coder in a triage slot's pool = %v", err)
	}
	if _, err := dir.UpdatePool(ctx, "acme", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a nil pool = %v", err)
	}
	got, err := dir.GetPool(ctx, "acme", "triagers")
	if err != nil || len(got.Agents) != 1 {
		t.Fatalf("the refused update left the pool alone: %v, %v", got, err)
	}
	pools, _ := dir.ListPools(ctx, "acme")
	agents, _ := dir.ListAgents(ctx, "acme")
	if len(pools) != 1 || len(agents) != 2 {
		t.Fatalf("listing = %d pools, %d agents", len(pools), len(agents))
	}
	if a, err := dir.GetAgent(ctx, "acme", "npu"); err != nil || a.Backend != "openvino" {
		t.Fatalf("GetAgent = %v, %v", a, err)
	}
	if dir.Store() == nil {
		t.Fatal("Store must return the backing store")
	}
}

func TestAgentDirectory_CheckRefAndResolve(t *testing.T) {
	ctx := context.Background()
	dir := newTestDirectory(t)
	triage := testAgent("npu", "openvino", "qwen3-8b", AgentRoleTriage)
	triage.MaxConcurrent = 1
	codex := testAgent("codex", "codex", "gpt-5.5", AgentRoleTriage)
	for _, agent := range []*Agent{&triage, &codex} {
		if _, err := dir.CreateAgent(ctx, "acme", agent); err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}
	}
	pool := AgentPool{ID: "triage", Name: "Triage", Agents: []string{"npu", "codex"}}
	if _, err := dir.CreatePool(ctx, "acme", &pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	if err := dir.CheckRef(ctx, "acme", PoolRefTo("triage"), "triage", AgentRoleTriage); err != nil {
		t.Fatalf("CheckRef: %v", err)
	}
	if err := dir.CheckRef(ctx, "acme", AgentRefTo("npu"), "worker", AgentRoleCoding); !errors.Is(err, ErrAgentRoleMismatch) {
		t.Fatalf("CheckRef(role) = %v", err)
	}
	if err := dir.CheckRef(ctx, "acme", AgentRef{}, "worker", AgentRoleCoding); err != nil {
		t.Fatalf("a zero reference is not checked: %v", err)
	}
	admitted, passed, err := dir.Resolve(ctx, "acme", PoolRefTo("triage"), AgentRoleTriage)
	if err != nil || len(admitted) != 2 || len(passed) != 0 {
		t.Fatalf("Resolve = %v, %v, %v", admitted, passed, err)
	}
	if _, _, err := dir.Resolve(ctx, "acme", PoolRefTo("ghost"), AgentRoleTriage); !errors.Is(err, ErrAgentPoolUnknown) {
		t.Fatalf("Resolve(unknown) = %v", err)
	}
}

func TestProjectAgentUsageReporter_ReportsDefaultLeads(t *testing.T) {
	ctx := context.Background()
	projects := NewInMemoryProjectStore()
	for _, project := range []*Project{
		{ID: "p1", TenantID: "acme", Name: "one", DefaultLeadAgent: PoolRefTo("leads")},
		{ID: "p2", TenantID: "acme", Name: "two"},
	} {
		if err := projects.Create(ctx, project); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	refs, err := NewProjectAgentUsageReporter(projects).AgentReferences(ctx, "acme")
	if err != nil || len(refs) != 1 || refs[0].Usage.ID != "p1" || refs[0].Usage.Slot != AgentUsageSlotDefaultLead {
		t.Fatalf("AgentReferences = %+v, %v", refs, err)
	}
	if refs, err := NewProjectAgentUsageReporter(nil).AgentReferences(ctx, "acme"); err != nil || refs != nil {
		t.Fatalf("no store reports nothing: %v, %v", refs, err)
	}
}

func TestStaticAgentUsageReporter_ReportsOnlyItsTenant(t *testing.T) {
	reporter := StaticAgentUsageReporter{TenantID: "", References: []AgentReference{{
		Ref: AgentRefTo("lead"), Usage: AgentUsage{Kind: AgentUsageKindConfig, ID: "orchestration.lead_agent", Slot: AgentUsageSlotLead},
	}}}
	if refs, _ := reporter.AgentReferences(context.Background(), SystemTenantID); len(refs) != 1 {
		t.Fatalf("the system tenant sees the configuration's references, got %v", refs)
	}
	if refs, _ := reporter.AgentReferences(context.Background(), "acme"); len(refs) != 0 {
		t.Fatalf("another tenant sees none, got %v", refs)
	}
}

func TestAgentDirectory_ReplaceAgentImageRepointsOnlyAStaleImage(t *testing.T) {
	ctx := context.Background()
	auditor := &recordingAuditor{}
	dir, err := NewAgentDirectory(AgentDirectoryConfig{
		Store: NewAgentCatalogStore(kvstore.NewMemory(), WithStoreAuditor(auditor)), Models: liveModels(),
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := testAgent("coder", "claude", "claude-opus-5-5")
	agent.Image = "worker:1"
	if _, err := dir.CreateAgent(ctx, "acme", &agent); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	changed, err := dir.ReplaceAgentImage(ctx, "acme", "coder", []string{"worker:0", "worker:1"}, "worker:2")
	if err != nil || !changed {
		t.Fatalf("ReplaceAgentImage = %v, %v", changed, err)
	}
	if got, _ := dir.GetAgent(ctx, "acme", "coder"); got.Image != "worker:2" {
		t.Fatalf("image = %q", got.Image)
	}
	changed, err = dir.ReplaceAgentImage(ctx, "acme", "coder", []string{"worker:1"}, "worker:3")
	if err != nil || changed {
		t.Fatalf("an image that is not stale must be left alone: %v, %v", changed, err)
	}
	if updates := auditActions(auditor, AuditActionUpdate); len(updates) != 1 {
		t.Fatalf("only the real change is audited: %+v", updates)
	}
}

func TestAgentDirectory_ReplaceAgentImageRefusesBadInput(t *testing.T) {
	ctx := context.Background()
	dir := newTestDirectory(t)
	if _, err := dir.ReplaceAgentImage(ctx, "acme", "coder", []string{"a"}, " "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no image = %v", err)
	}
	if _, err := dir.ReplaceAgentImage(ctx, "acme", "coder", nil, "b"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no stale images = %v", err)
	}
	if _, err := dir.ReplaceAgentImage(ctx, "acme", "ghost", []string{"a"}, "b"); !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("an unknown agent = %v", err)
	}
	if _, err := dir.Store().ModifyAgent(ctx, "acme", "ghost", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a nil modify = %v", err)
	}
}

func TestPooledAccountProvider(t *testing.T) {
	for backend, want := range map[string]AccountProvider{
		"claude": AccountProviderClaude, "claude-code": AccountProviderClaude,
		"codex": AccountProviderCodex, "opencode": AccountProviderOpenCode,
	} {
		if got, ok := PooledAccountProvider(backend); !ok || got != want {
			t.Fatalf("PooledAccountProvider(%s) = %q, %v; want %q", backend, got, ok, want)
		}
	}
	if got, ok := PooledAccountProvider("openvino"); ok || got != "" {
		t.Fatalf("a backend that pools no accounts reported %q", got)
	}
}
