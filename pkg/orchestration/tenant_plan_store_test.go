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
	"bytes"
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

func testTenantPlan(rev uint64) *TenantPlan {
	return &TenantPlan{
		Priorities: []TenantPriority{
			{ProjectID: "go-quicbaas", Rank: 1, Note: "ship the tenant agent"},
			{ProjectID: "go-qrdb", Rank: 2},
		},
		Milestones: []TenantMilestone{{
			ID:         "m-1",
			Title:      "first release",
			Due:        time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
			ProjectIDs: []string{"go-quicbaas", "go-qrdb"},
		}},
		Revision:     rev,
		Principal:    "p",
		SigningKeyID: "k",
		Signature:    []byte{1},
	}
}

func TestTenantPlanStore_Lifecycle(t *testing.T) {
	t.Parallel()

	auditor := &recordingAuditor{}
	store := NewTenantPlanStore(kvstore.NewMemory(), WithStoreAuditor(auditor)).(*tenantPlanStore)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	ctx := context.Background()

	if err := store.PutTenantPlan(ctx, "tenant-a", testTenantPlan(1), 0); err != nil {
		t.Fatalf("PutTenantPlan() error = %v", err)
	}
	got, err := store.GetTenantPlan(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("GetTenantPlan() error = %v", err)
	}
	want := testTenantPlan(1)
	want.TenantID = "tenant-a"
	want.CreatedAt = clock
	want.UpdatedAt = clock
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}

	other := testTenantPlan(1)
	other.Priorities[0].Note = "changed"
	err = store.PutTenantPlan(ctx, "tenant-a", other, 0)
	if !errors.Is(err, ErrTenantPlanConflict) {
		t.Fatalf("second put at expected 0 error = %v, want ErrTenantPlanConflict", err)
	}
	var conflict *TenantPlanConflictError
	if !errors.As(err, &conflict) || conflict.Expected != 0 || conflict.Held != 1 || conflict.TenantID != "tenant-a" {
		t.Fatalf("conflict = %#v", err)
	}
	if held, _ := store.GetTenantPlan(ctx, "tenant-a"); !reflect.DeepEqual(held, want) {
		t.Fatalf("a refused put changed the plan: %+v", held)
	}

	later := clock.Add(time.Hour)
	store.now = func() time.Time { return later }
	if err := store.PutTenantPlan(ctx, "tenant-a", testTenantPlan(2), 1); err != nil {
		t.Fatalf("PutTenantPlan(rev 2) error = %v", err)
	}
	got, err = store.GetTenantPlan(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("GetTenantPlan() after update error = %v", err)
	}
	if got.Revision != 2 || !got.CreatedAt.Equal(clock) || !got.UpdatedAt.Equal(later) {
		t.Fatalf("after update = %+v", got)
	}

	if err := store.DeleteTenantPlan(ctx, "tenant-a"); err != nil {
		t.Fatalf("DeleteTenantPlan() error = %v", err)
	}
	if _, err := store.GetTenantPlan(ctx, "tenant-a"); !errors.Is(err, ErrTenantPlanUnknown) {
		t.Fatalf("get after delete error = %v, want ErrTenantPlanUnknown", err)
	}
	if err := store.DeleteTenantPlan(ctx, "tenant-a"); err != nil {
		t.Fatalf("second delete error = %v, want nil", err)
	}

	creates := auditActions(auditor, AuditActionCreate)
	if len(creates) != 2 || !creates[0].Success || creates[1].Success {
		t.Fatalf("create events = %+v, want one success then one refusal", creates)
	}
	if updates := auditActions(auditor, AuditActionUpdate); len(updates) != 1 || !updates[0].Success {
		t.Fatalf("update events = %+v", updates)
	}
	if deletes := auditActions(auditor, AuditActionDelete); len(deletes) != 2 {
		t.Fatalf("delete events = %+v, want 2", deletes)
	}
	for _, event := range auditor.all() {
		if event.Resource != AuditResourceTenantPlan || event.ResourceID != TenantPlanRecordID || event.TenantID != "tenant-a" {
			t.Fatalf("event = %+v", event)
		}
	}
	if n := len(auditor.all()); n != 5 {
		t.Fatalf("recorded %d events, want 5", n)
	}
}

func TestTenantPlanStore_RefusesBadWrites(t *testing.T) {
	t.Parallel()

	with := func(edit func(p *TenantPlan)) *TenantPlan {
		p := testTenantPlan(1)
		edit(p)
		return p
	}
	tooMany := make([]TenantPriority, MaxTenantPlanPriorities+1)
	for i := range tooMany {
		tooMany[i] = TenantPriority{ProjectID: "p-" + strconv.Itoa(i), Rank: i + 1}
	}
	tests := []struct {
		name     string
		plan     *TenantPlan
		expected uint64
	}{
		{name: "nil", plan: nil},
		{name: "rank 0", plan: with(func(p *TenantPlan) { p.Priorities[0].Rank = 0 })},
		{name: "duplicate rank", plan: with(func(p *TenantPlan) { p.Priorities[1].Rank = 1 })},
		{name: "duplicate project", plan: with(func(p *TenantPlan) { p.Priorities[1].ProjectID = "go-quicbaas" })},
		{name: "blank project", plan: with(func(p *TenantPlan) { p.Priorities[0].ProjectID = " " })},
		{name: "note over the limit", plan: with(func(p *TenantPlan) {
			p.Priorities[0].Note = strings.Repeat("x", MaxTenantPlanNoteLen+1)
		})},
		{name: "too many priorities", plan: with(func(p *TenantPlan) { p.Priorities = tooMany })},
		{name: "blank milestone id", plan: with(func(p *TenantPlan) { p.Milestones[0].ID = "" })},
		{name: "blank title", plan: with(func(p *TenantPlan) { p.Milestones[0].Title = " " })},
		{name: "duplicate milestone id", plan: with(func(p *TenantPlan) {
			p.Milestones = append(p.Milestones, TenantMilestone{ID: "m-1", Title: "again"})
		})},
		{name: "blank milestone project", plan: with(func(p *TenantPlan) { p.Milestones[0].ProjectIDs[1] = "" })},
		{name: "duplicate milestone project", plan: with(func(p *TenantPlan) { p.Milestones[0].ProjectIDs[1] = "go-quicbaas" })},
		{name: "blank principal", plan: with(func(p *TenantPlan) { p.Principal = " " })},
		{name: "blank key", plan: with(func(p *TenantPlan) { p.SigningKeyID = "" })},
		{name: "empty signature", plan: with(func(p *TenantPlan) { p.Signature = nil })},
		{name: "revision not expected+1", plan: testTenantPlan(2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := NewInMemoryTenantPlanStore()
			ctx := context.Background()
			err := store.PutTenantPlan(ctx, "tenant-a", tt.plan, tt.expected)
			var invalid *InputValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %v, want *InputValidationError", err)
			}
			if _, err := store.GetTenantPlan(ctx, "tenant-a"); !errors.Is(err, ErrTenantPlanUnknown) {
				t.Fatalf("a refused write stored a plan: %v", err)
			}
		})
	}
}

func TestTenantPlanStore_EmptyPlanIsValid(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTenantPlanStore()
	ctx := context.Background()
	plan := testTenantPlan(1)
	plan.Priorities, plan.Milestones = nil, nil
	if err := store.PutTenantPlan(ctx, "tenant-a", plan, 0); err != nil {
		t.Fatalf("PutTenantPlan(empty) error = %v", err)
	}
}

func TestTenantPlanStore_TenantsAreIsolated(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTenantPlanStore()
	ctx := context.Background()

	if err := store.PutTenantPlan(ctx, "tenant-a", testTenantPlan(1), 0); err != nil {
		t.Fatalf("put in tenant-a: %v", err)
	}
	if _, err := store.GetTenantPlan(ctx, "tenant-b"); !errors.Is(err, ErrTenantPlanUnknown) {
		t.Fatalf("tenant-b sees a plan: %v", err)
	}
	planB := testTenantPlan(1)
	planB.Principal = "p-b"
	if err := store.PutTenantPlan(ctx, "tenant-b", planB, 0); err != nil {
		t.Fatalf("put in tenant-b: %v", err)
	}
	if err := store.DeleteTenantPlan(ctx, "tenant-b"); err != nil {
		t.Fatalf("delete tenant-b: %v", err)
	}
	a, err := store.GetTenantPlan(ctx, "tenant-a")
	if err != nil || a.Principal != "p" {
		t.Fatalf("tenant-a after tenant-b's delete = %+v, %v", a, err)
	}

	other := NewInMemoryTenantPlanStore()
	forged := testTenantPlan(1)
	forged.TenantID = "tenant-b"
	if err := other.PutTenantPlan(ctx, "tenant-a", forged, 0); err != nil {
		t.Fatalf("put with a foreign body tenant: %v", err)
	}
	if forged.TenantID != "tenant-a" {
		t.Fatalf("stored body names tenant %q, want tenant-a", forged.TenantID)
	}
	got, err := other.GetTenantPlan(ctx, "tenant-a")
	if err != nil || got.TenantID != "tenant-a" {
		t.Fatalf("read back = %+v, %v; want tenant-a", got, err)
	}
	if _, err := other.GetTenantPlan(ctx, "tenant-b"); !errors.Is(err, ErrTenantPlanUnknown) {
		t.Fatalf("the body's tenant gained a plan: %v", err)
	}
}

func TestTenantPlanStore_RefusesAContextScopedToAnotherTenant(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTenantPlanStore()
	ctx := ContextWithTenant(context.Background(), "tenant-b")

	checks := map[string]error{
		"put":    store.PutTenantPlan(ctx, "tenant-a", testTenantPlan(1), 0),
		"delete": store.DeleteTenantPlan(ctx, "tenant-a"),
	}
	_, checks["get"] = store.GetTenantPlan(ctx, "tenant-a")
	for name, err := range checks {
		if !errors.Is(err, ErrAccountTenantScope) {
			t.Errorf("%s error = %v, want ErrAccountTenantScope", name, err)
		}
	}
}

func TestTenantPlanStore_CorruptRecordIsAStoreError(t *testing.T) {
	t.Parallel()

	kv := kvstore.NewMemory()
	store := NewTenantPlanStore(kv)
	ctx := context.Background()
	key, err := tenantPlanKey("tenant-a")
	if err != nil {
		t.Fatalf("tenantPlanKey() error = %v", err)
	}
	if err := kv.Put(ctx, key, []byte("{not json")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if _, err := store.GetTenantPlan(ctx, "tenant-a"); !errors.Is(err, ErrAgentCatalogStore) {
		t.Fatalf("error = %v, want ErrAgentCatalogStore", err)
	}
	if err := store.PutTenantPlan(ctx, "tenant-a", testTenantPlan(1), 0); !errors.Is(err, ErrAgentCatalogStore) {
		t.Fatalf("put over a corrupt record error = %v, want ErrAgentCatalogStore", err)
	}
}

func TestTenantPlan_SigningPayloadCoversTheContent(t *testing.T) {
	t.Parallel()

	payload := func(p *TenantPlan) []byte {
		t.Helper()
		data, err := p.SigningPayload()
		if err != nil {
			t.Fatalf("SigningPayload() error = %v", err)
		}
		return data
	}
	base := func() *TenantPlan {
		p := testTenantPlan(1)
		p.TenantID = "tenant-a"
		return p
	}
	want := payload(base())

	unchanged := map[string]func(p *TenantPlan){
		"signature":  func(p *TenantPlan) { p.Signature = []byte{9, 9} },
		"created at": func(p *TenantPlan) { p.CreatedAt = time.Now() },
		"updated at": func(p *TenantPlan) { p.UpdatedAt = time.Now() },
	}
	for name, edit := range unchanged {
		p := base()
		edit(p)
		if !bytes.Equal(payload(p), want) {
			t.Errorf("payload changed with %s", name)
		}
	}

	changed := map[string]func(p *TenantPlan){
		"tenant":            func(p *TenantPlan) { p.TenantID = "tenant-b" },
		"revision":          func(p *TenantPlan) { p.Revision = 2 },
		"principal":         func(p *TenantPlan) { p.Principal = "q" },
		"signing key":       func(p *TenantPlan) { p.SigningKeyID = "k2" },
		"rank":              func(p *TenantPlan) { p.Priorities[1].Rank = 3 },
		"note":              func(p *TenantPlan) { p.Priorities[1].Note = "n" },
		"milestone title":   func(p *TenantPlan) { p.Milestones[0].Title = "second release" },
		"milestone due":     func(p *TenantPlan) { p.Milestones[0].Due = p.Milestones[0].Due.Add(time.Hour) },
		"milestone project": func(p *TenantPlan) { p.Milestones[0].ProjectIDs[1] = "go-xkms" },
		"milestone done":    func(p *TenantPlan) { p.Milestones[0].Done = true },
	}
	for name, edit := range changed {
		p := base()
		edit(p)
		if bytes.Equal(payload(p), want) {
			t.Errorf("payload unchanged with %s", name)
		}
	}
}
