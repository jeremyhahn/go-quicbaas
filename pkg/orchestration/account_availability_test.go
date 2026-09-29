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
	"strings"
	"testing"
	"time"
)

// availabilityAccount creates one codex account in state status.
func (r *headroomRig) availabilityAccount(t *testing.T, id string, status AccountStatus, lastError string) {
	t.Helper()
	account := newTestAccount(admissionTenant, id, AccountProviderCodex)
	account.Status = status
	account.LastError = lastError
	if status == AccountStatusLimited {
		account.AvailableAt = time.Now().UTC().Add(time.Hour)
	}
	if err := r.accounts.Create(context.Background(), account); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
}

func codexQuery() AccountAvailabilityQuery {
	return AccountAvailabilityQuery{TenantID: admissionTenant, ProjectID: "proj-a", Provider: AccountProviderCodex}
}

func TestAccountAvailability_ADeadLoginPoolIsUnusableAndSaysSo(t *testing.T) {
	rig := newHeadroomRig(t)
	rig.availabilityAccount(t, "codex-reviewer", AccountStatusError, AccountReloginNeededPrefix+"401 Unauthorized")
	rig.availabilityAccount(t, "codex-old", AccountStatusRevoked, "")

	got, err := rig.reader.(AccountAvailabilityReader).AccountAvailability(context.Background(), codexQuery(), time.Now().UTC())
	if err != nil {
		t.Fatalf("AccountAvailability: %v", err)
	}
	if got.Usable || !strings.Contains(got.Reason, "re-login needed") || !strings.Contains(got.Reason, "1 revoked") {
		t.Fatalf("availability = %+v, want unusable naming the re-login and the revoked account", got)
	}
}

// TestAccountAvailability_AnAccountThatReturnsByItselfIsUsable: a cooling-down
// account comes back on its own, so work waits for it rather than being held.
func TestAccountAvailability_AnAccountThatReturnsByItselfIsUsable(t *testing.T) {
	rig := newHeadroomRig(t)
	rig.availabilityAccount(t, "codex-reviewer", AccountStatusError, AccountReloginNeededPrefix+"401")
	rig.availabilityAccount(t, "codex-2", AccountStatusLimited, "rate limited")

	got, err := rig.reader.(AccountAvailabilityReader).AccountAvailability(context.Background(), codexQuery(), time.Now().UTC())
	if err != nil || !got.Usable {
		t.Fatalf("availability = (%+v, %v), want usable", got, err)
	}

	narrowed := codexQuery()
	narrowed.Allow = []string{"codex-reviewer"}
	got, err = rig.reader.(AccountAvailabilityReader).AccountAvailability(context.Background(), narrowed, time.Now().UTC())
	if err != nil || got.Usable {
		t.Fatalf("availability over the allow list = (%+v, %v), want unusable", got, err)
	}
}

func TestAccountAvailability_RefusesBadInputAndNamesAnEmptyPool(t *testing.T) {
	rig := newHeadroomRig(t)
	reader := rig.reader.(AccountAvailabilityReader)
	if _, err := reader.AccountAvailability(context.Background(), AccountAvailabilityQuery{Provider: AccountProviderCodex}, time.Now()); err == nil {
		t.Fatal("a query with no tenant was answered")
	}
	got, err := reader.AccountAvailability(context.Background(), codexQuery(), time.Now())
	if err != nil || got.Usable || !strings.Contains(got.Reason, "no pooled codex account") {
		t.Fatalf("empty pool = (%+v, %v), want unusable and named", got, err)
	}
}

// TestAgentAccountAvailability_AsksThePoolTheAgentDrawsOn: the Agent's backend
// picks the provider; an Agent on no pool is usable; no reader wired answers
// usable, as dispatch always behaved.
func TestAgentAccountAvailability_AsksThePoolTheAgentDrawsOn(t *testing.T) {
	ctx := context.Background()
	dir := newTestDirectory(t)
	reviewer := testAgent("codex-reviewer", "codex", "gpt-5.5", AgentRoleReview)
	if _, err := dir.CreateAgent(ctx, admissionTenant, &reviewer); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	local := testAgent("npu", "openvino", "qwen3-8b", AgentRoleReview)
	if _, err := dir.CreateAgent(ctx, admissionTenant, &local); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	rig := newHeadroomRig(t)
	rig.availabilityAccount(t, "codex-1", AccountStatusError, AccountReloginNeededPrefix+"401")

	unwired := &agentServiceImpl{agentDirectory: dir}
	if got, err := unwired.AgentAccountAvailability(ctx, admissionTenant, "proj-a", AgentRefTo("codex-reviewer"), AgentRoleReview); err != nil || !got.Usable {
		t.Fatalf("unwired = (%+v, %v), want usable", got, err)
	}

	svc := &agentServiceImpl{agentDirectory: dir}
	svc.SetAccountAvailability(rig.reader.(AccountAvailabilityReader))
	got, err := svc.AgentAccountAvailability(ctx, admissionTenant, "proj-a", AgentRefTo("codex-reviewer"), AgentRoleReview)
	if err != nil || got.Usable || !strings.Contains(got.Reason, "re-login needed") {
		t.Fatalf("codex reviewer = (%+v, %v), want unusable with a re-login reason", got, err)
	}
	got, err = svc.AgentAccountAvailability(ctx, admissionTenant, "proj-a", AgentRefTo("npu"), AgentRoleReview)
	if err != nil || !got.Usable {
		t.Fatalf("local-model reviewer = (%+v, %v), want usable: it draws on no pool", got, err)
	}
	if _, err := svc.AgentAccountAvailability(ctx, admissionTenant, "proj-a", AgentRefTo("missing"), AgentRoleReview); err == nil {
		t.Fatal("an Agent the catalog does not define was answered")
	}
}
