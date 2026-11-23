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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/notify"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

const frTenant = "tenant-fr"

type fakeAttentionLister struct {
	mu    sync.Mutex
	items []*AttentionItem
	err   error
	calls int
}

func (f *fakeAttentionLister) set(items ...*AttentionItem) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = items
}

func (f *fakeAttentionLister) List(_ context.Context, _ string, _ AttentionFilter, page *orchestration.Page) (*orchestration.PageResult[*AttentionItem], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	start := min(page.Offset, len(f.items))
	end := min(start+page.Limit, len(f.items))
	out := &orchestration.PageResult[*AttentionItem]{
		Items:  append([]*AttentionItem(nil), f.items[start:end]...),
		Total:  len(f.items),
		Limit:  page.Limit,
		Offset: page.Offset,
	}
	if end < len(f.items) {
		out.HasNext = true
		out.NextOffset = end
	}
	return out, nil
}

type fakeResponderNotifier struct {
	mu       sync.Mutex
	got      []notify.Notification
	failKeys map[string]bool
}

func (f *fakeResponderNotifier) Notify(_ context.Context, n notify.Notification) (notify.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failKeys[n.Key] {
		return notify.Report{}, &notify.DeliveryError{Key: n.Key}
	}
	f.got = append(f.got, n)
	return notify.Report{Delivered: []string{"signal"}}, nil
}

func (f *fakeResponderNotifier) received() []notify.Notification {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]notify.Notification(nil), f.got...)
}

func parkedItem(id string, changedAt int64) *AttentionItem {
	return &AttentionItem{
		ID:              id,
		Kind:            AttentionParkedTask,
		ProjectID:       "proj-1",
		ProjectName:     "Project One",
		Subject:         "task " + id,
		BlockKind:       orchestration.TaskBlockKindNeedsInfo,
		Question:        "which branch?",
		IssueURL:        "https://forge.example/owner/repo/issues/1",
		CreatedAt:       1,
		StatusChangedAt: changedAt,
	}
}

func alertItem(id, severity string) *AttentionItem {
	return &AttentionItem{
		ID:        id,
		Kind:      AttentionFleetNodeOffline,
		Subject:   "agent offline",
		Message:   "the agent stopped reporting",
		Severity:  severity,
		CreatedAt: 7,
	}
}

func newTestResponder(t *testing.T, lister *fakeAttentionLister, notifier *fakeResponderNotifier) *FirstResponder {
	t.Helper()
	r, err := NewFirstResponder(FirstResponderConfig{Attention: lister, Notifier: notifier})
	if err != nil {
		t.Fatalf("NewFirstResponder: %v", err)
	}
	return r
}

func mustSweep(t *testing.T, r *FirstResponder) FirstResponderSweep {
	t.Helper()
	res, err := r.Sweep(context.Background(), frTenant)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	return res
}

func TestNewFirstResponder_Refuses(t *testing.T) {
	lister := &fakeAttentionLister{}
	notifier := &fakeResponderNotifier{}
	cases := []struct {
		name  string
		cfg   FirstResponderConfig
		field string
	}{
		{"nil attention", FirstResponderConfig{Notifier: notifier}, "attention"},
		{"nil notifier", FirstResponderConfig{Attention: lister}, "notifier"},
		{"short interval", FirstResponderConfig{Attention: lister, Notifier: notifier, Interval: time.Second}, "interval"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewFirstResponder(tc.cfg)
			if r != nil || !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
			var inv *InvalidInputError
			if !errors.As(err, &inv) || inv.Field != tc.field {
				t.Fatalf("want field %q, got %v", tc.field, err)
			}
		})
	}
	r, err := NewFirstResponder(FirstResponderConfig{Attention: lister, Notifier: notifier})
	if err != nil || r.interval != DefaultFirstResponderInterval {
		t.Fatalf("default interval: %v, %v", r, err)
	}
	if _, err := r.Sweep(context.Background(), ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty tenant: want ErrInvalidInput, got %v", err)
	}
}

func TestFirstResponder_NotifiesEachItemOnce(t *testing.T) {
	lister := &fakeAttentionLister{}
	lister.set(parkedItem("t1", 10), alertItem("agent_offline:a1", "warning"))
	notifier := &fakeResponderNotifier{}
	r := newTestResponder(t, lister, notifier)

	first := mustSweep(t, r)
	if first.Listed != 2 || first.Notified != 2 || first.Failed != 0 {
		t.Fatalf("first sweep: %+v", first)
	}
	second := mustSweep(t, r)
	if second.Listed != 2 || second.Notified != 0 {
		t.Fatalf("second sweep: %+v", second)
	}
	got := notifier.received()
	if len(got) != 2 || got[0].Key != "t1@10" || got[1].Key != "agent_offline:a1@7" {
		t.Fatalf("notifications: %+v", got)
	}
	if got[0].TenantID != frTenant || got[0].Subject != "Project One: task t1" || got[0].Link != "https://forge.example/owner/repo/issues/1" {
		t.Fatalf("parked notification: %+v", got[0])
	}
	if got[1].Subject != "agent_offline: agent offline" || got[1].Body != "the agent stopped reporting" {
		t.Fatalf("alert notification: %+v", got[1])
	}
}

func TestFirstResponder_RetriesFailedDelivery(t *testing.T) {
	lister := &fakeAttentionLister{}
	lister.set(parkedItem("t1", 10), parkedItem("t2", 20))
	notifier := &fakeResponderNotifier{failKeys: map[string]bool{"t2@20": true}}
	r := newTestResponder(t, lister, notifier)

	first := mustSweep(t, r)
	if first.Notified != 1 || first.Failed != 1 {
		t.Fatalf("first sweep: %+v", first)
	}
	notifier.mu.Lock()
	notifier.failKeys = nil
	notifier.mu.Unlock()
	second := mustSweep(t, r)
	if second.Notified != 1 || second.Failed != 0 {
		t.Fatalf("second sweep: %+v", second)
	}
	got := notifier.received()
	if len(got) != 2 || got[1].Key != "t2@20" {
		t.Fatalf("notifications: %+v", got)
	}
}

func TestFirstResponder_ReparkedItemIsAnnouncedAgain(t *testing.T) {
	lister := &fakeAttentionLister{}
	lister.set(parkedItem("t1", 10))
	notifier := &fakeResponderNotifier{}
	r := newTestResponder(t, lister, notifier)
	mustSweep(t, r)
	lister.set(parkedItem("t1", 30))
	if res := mustSweep(t, r); res.Notified != 1 {
		t.Fatalf("re-parked sweep: %+v", res)
	}
	got := notifier.received()
	if len(got) != 2 || got[1].Key != "t1@30" {
		t.Fatalf("notifications: %+v", got)
	}
}

func TestFirstResponder_ResolvedThenReturnedIsAnnouncedAgain(t *testing.T) {
	lister := &fakeAttentionLister{}
	lister.set(parkedItem("t1", 10))
	notifier := &fakeResponderNotifier{}
	r := newTestResponder(t, lister, notifier)
	mustSweep(t, r)
	lister.set()
	if res := mustSweep(t, r); res.Listed != 0 || res.Notified != 0 {
		t.Fatalf("empty sweep: %+v", res)
	}
	lister.set(parkedItem("t1", 10))
	if res := mustSweep(t, r); res.Notified != 1 {
		t.Fatalf("returned sweep: %+v", res)
	}
	if got := notifier.received(); len(got) != 2 {
		t.Fatalf("notifications: %+v", got)
	}
}

func TestFirstResponder_ListErrorKeepsState(t *testing.T) {
	lister := &fakeAttentionLister{}
	lister.set(parkedItem("t1", 10))
	notifier := &fakeResponderNotifier{}
	r := newTestResponder(t, lister, notifier)
	mustSweep(t, r)

	errList := errors.New("store unavailable")
	lister.mu.Lock()
	lister.err = errList
	lister.mu.Unlock()
	res, err := r.Sweep(context.Background(), frTenant)
	if !errors.Is(err, errList) || res != (FirstResponderSweep{}) {
		t.Fatalf("list error sweep: %+v, %v", res, err)
	}

	lister.mu.Lock()
	lister.err = nil
	lister.mu.Unlock()
	if res := mustSweep(t, r); res.Notified != 0 {
		t.Fatalf("state lost after list error: %+v", res)
	}
	if got := notifier.received(); len(got) != 1 {
		t.Fatalf("notifications: %+v", got)
	}
}

func TestFirstResponder_PagesThroughEveryItem(t *testing.T) {
	items := make([]*AttentionItem, 250)
	for i := range items {
		items[i] = parkedItem("t"+strconv.Itoa(i), int64(i+1))
	}
	lister := &fakeAttentionLister{}
	lister.set(items...)
	notifier := &fakeResponderNotifier{}
	r := newTestResponder(t, lister, notifier)
	res := mustSweep(t, r)
	if res.Listed != 250 || res.Notified != 250 {
		t.Fatalf("sweep: %+v", res)
	}
	lister.mu.Lock()
	calls := lister.calls
	lister.mu.Unlock()
	if calls != 3 {
		t.Fatalf("List calls: want 3, got %d", calls)
	}
}

func TestFirstResponder_RedactsSecretsInBody(t *testing.T) {
	token := "ghp_" + strings.Repeat("x", 20)
	item := parkedItem("t1", 10)
	item.Question = "use the token " + token + " to push?"
	item.Recommendation = "rotate it"
	lister := &fakeAttentionLister{}
	lister.set(item)
	notifier := &fakeResponderNotifier{}
	r := newTestResponder(t, lister, notifier)
	mustSweep(t, r)
	got := notifier.received()
	if len(got) != 1 {
		t.Fatalf("notifications: %+v", got)
	}
	body := got[0].Body
	if strings.Contains(body, token) || !strings.Contains(body, "[redacted]") {
		t.Fatalf("body not redacted: %q", body)
	}
	if !strings.HasPrefix(body, "Question: ") || !strings.Contains(body, "\n\nRecommendation: rotate it") {
		t.Fatalf("body layout: %q", body)
	}
}

func TestFirstResponder_ParkedTaskIsSensitiveAlertIsNot(t *testing.T) {
	parked := notificationFor(frTenant, parkedItem("t1", 10))
	if !parked.Sensitive {
		t.Fatalf("parked task notification is not sensitive")
	}
	alert := notificationFor(frTenant, alertItem("agent_offline:a1", "warning"))
	if alert.Sensitive {
		t.Fatalf("alert notification is sensitive")
	}
	if err := parked.Validate(); err != nil {
		t.Fatalf("parked notification invalid: %v", err)
	}
	if err := alert.Validate(); err != nil {
		t.Fatalf("alert notification invalid: %v", err)
	}
}

func TestFirstResponder_SeverityMapping(t *testing.T) {
	escalated := parkedItem("t1", 10)
	escalated.BlockKind = orchestration.TaskBlockKindEscalated
	blocked := parkedItem("t2", 10)
	blocked.BlockKind = orchestration.TaskBlockKindBlocked
	cases := []struct {
		name string
		item *AttentionItem
		want notify.Severity
	}{
		{"critical alert", alertItem("a1", "critical"), notify.SeverityCritical},
		{"warning alert", alertItem("a2", "warning"), notify.SeverityWarning},
		{"unlabelled alert", alertItem("a3", ""), notify.SeverityWarning},
		{"escalated park", escalated, notify.SeverityCritical},
		{"blocked park", blocked, notify.SeverityWarning},
		{"needs-info park", parkedItem("t3", 10), notify.SeverityWarning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := notificationFor(frTenant, tc.item).Severity; got != tc.want {
				t.Fatalf("severity: want %s, got %s", tc.want, got)
			}
		})
	}
}

func TestFirstResponder_SubjectIsOneLine(t *testing.T) {
	item := parkedItem("t1", 10)
	item.ProjectName = ""
	item.Subject = "line one\nline two"
	n := notificationFor(frTenant, item)
	if n.Subject != "proj-1: line one line two" {
		t.Fatalf("subject: %q", n.Subject)
	}
	item.ProjectID = ""
	item.Subject = ""
	n = notificationFor(frTenant, item)
	if n.Subject != "parked_task: parked_task" {
		t.Fatalf("empty subject: %q", n.Subject)
	}
}

func TestFirstResponder_RunSweepsUntilCancelled(t *testing.T) {
	lister := &fakeAttentionLister{}
	lister.set(parkedItem("t1", 10))
	notifier := &fakeResponderNotifier{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sweeps []FirstResponderSweep
	r, err := NewFirstResponder(FirstResponderConfig{
		Attention: lister,
		Notifier:  notifier,
		Interval:  MinFirstResponderInterval,
		OnSweep: func(tenantID string, result FirstResponderSweep, err error) {
			if tenantID != frTenant || err != nil {
				t.Errorf("OnSweep(%q, %v)", tenantID, err)
			}
			sweeps = append(sweeps, result)
			cancel()
		},
	})
	if err != nil {
		t.Fatalf("NewFirstResponder: %v", err)
	}
	if err := r.Run(ctx, frTenant); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: want context.Canceled, got %v", err)
	}
	if len(sweeps) != 1 || sweeps[0].Notified != 1 {
		t.Fatalf("sweeps: %+v", sweeps)
	}
}
