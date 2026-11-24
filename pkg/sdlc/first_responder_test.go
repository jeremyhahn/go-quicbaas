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
	"slices"
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
	mu      sync.Mutex
	items   []*AttentionItem
	err     error
	calls   int
	offsets []int
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
	f.offsets = append(f.offsets, page.Offset)
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
	channels [][]string
	failKeys map[string]bool
}

func (f *fakeResponderNotifier) NotifyChannels(_ context.Context, n notify.Notification, channels []string) (notify.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.channels = append(f.channels, append([]string(nil), channels...))
	if f.failKeys[n.Key] {
		failed := []notify.ChannelFailure{{Channel: "signal", Err: errors.New("signal unavailable")}}
		return notify.Report{Failed: failed}, &notify.DeliveryError{Key: n.Key, Failures: failed}
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
	notifier.mu.Lock()
	calls := notifier.channels
	notifier.mu.Unlock()
	if len(calls) != 3 || len(calls[2]) != 1 || calls[2][0] != "signal" {
		t.Fatalf("retry channels: %v", calls)
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
	if res.Listed != 250 || res.Notified != 250 || res.More {
		t.Fatalf("sweep: %+v", res)
	}
	lister.mu.Lock()
	calls := lister.calls
	lister.mu.Unlock()
	if calls != 3 {
		t.Fatalf("List calls: want 3, got %d", calls)
	}
}

// numberedItems returns n parked items with ids t0..t(n-1).
func numberedItems(n int) []*AttentionItem {
	items := make([]*AttentionItem, n)
	for i := range items {
		items[i] = parkedItem("t"+strconv.Itoa(i), int64(i+1))
	}
	return items
}

// listedOffsets returns the page offsets the lister has been asked for.
func (f *fakeAttentionLister) listedOffsets() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.offsets...)
}

func TestFirstResponder_ContinuesPastPageBudget(t *testing.T) {
	lister := &fakeAttentionLister{}
	lister.set(numberedItems(250)...)
	notifier := &fakeResponderNotifier{}
	r := newTestResponder(t, lister, notifier)
	r.pagesPerSweep = 1

	want := []FirstResponderSweep{
		{Listed: 100, Notified: 100, More: true},
		{Listed: 100, Notified: 100, More: true},
		{Listed: 50, Notified: 50, More: false},
	}
	for i, w := range want {
		if res := mustSweep(t, r); res != w {
			t.Fatalf("sweep %d: want %+v, got %+v", i+1, w, res)
		}
	}
	got := notifier.received()
	keys := make(map[string]struct{}, len(got))
	for _, n := range got {
		keys[n.Key] = struct{}{}
	}
	if len(got) != 250 || len(keys) != 250 {
		t.Fatalf("notifications: %d, distinct keys: %d", len(got), len(keys))
	}

	res := mustSweep(t, r)
	if res != (FirstResponderSweep{Listed: 100, More: true}) {
		t.Fatalf("sweep 4: %+v", res)
	}
	if offsets := lister.listedOffsets(); !slices.Equal(offsets, []int{0, 100, 200, 0}) {
		t.Fatalf("offsets: %v", offsets)
	}
}

func TestFirstResponder_ListErrorMidCycleKeepsCursor(t *testing.T) {
	lister := &fakeAttentionLister{}
	lister.set(numberedItems(250)...)
	notifier := &fakeResponderNotifier{}
	r := newTestResponder(t, lister, notifier)
	r.pagesPerSweep = 1
	if res := mustSweep(t, r); res.Notified != 100 || !res.More {
		t.Fatalf("sweep 1: %+v", res)
	}

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
	if res := mustSweep(t, r); res.Listed != 100 || res.Notified != 100 || !res.More {
		t.Fatalf("sweep 3: %+v", res)
	}
	offsets := lister.listedOffsets()
	if last := offsets[len(offsets)-1]; last != 100 {
		t.Fatalf("sweep 3 offset: want 100, got %d (all %v)", last, offsets)
	}
	if got := notifier.received(); len(got) != 200 {
		t.Fatalf("notifications: %d", len(got))
	}
}

// sweepPass runs one pass of two sweeps and checks its boundaries: the
// first sweep stops at the budget, the second reaches the end. It returns
// the number of items notified in the pass.
func sweepPass(t *testing.T, r *FirstResponder, pass int) int {
	t.Helper()
	first := mustSweep(t, r)
	if !first.More {
		t.Fatalf("pass %d sweep 1 reached the end: %+v", pass, first)
	}
	second := mustSweep(t, r)
	if second.More {
		t.Fatalf("pass %d sweep 2 stopped early: %+v", pass, second)
	}
	return first.Notified + second.Notified
}

func TestFirstResponder_PagedCyclePrunesAfterTwoMisses(t *testing.T) {
	items := numberedItems(150)
	lister := &fakeAttentionLister{}
	lister.set(items...)
	notifier := &fakeResponderNotifier{}
	r := newTestResponder(t, lister, notifier)
	r.pagesPerSweep = 1

	if n := sweepPass(t, r, 1); n != 150 {
		t.Fatalf("pass 1 notified %d", n)
	}
	lister.set(items[1:]...)
	if n := sweepPass(t, r, 2); n != 0 {
		t.Fatalf("pass 2 notified %d", n)
	}
	lister.set(items...)
	if n := sweepPass(t, r, 3); n != 0 {
		t.Fatalf("pass 3: item missed once was announced again (%d)", n)
	}
	lister.set(items[1:]...)
	for pass := 4; pass <= 5; pass++ {
		if n := sweepPass(t, r, pass); n != 0 {
			t.Fatalf("pass %d notified %d", pass, n)
		}
	}
	lister.set(items...)
	if n := sweepPass(t, r, 6); n != 1 {
		t.Fatalf("pass 6: item missed twice not announced again (%d)", n)
	}
	got := notifier.received()
	if last := got[len(got)-1]; last.Key != "t0@1" {
		t.Fatalf("pass 6 announced %q", last.Key)
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
	for _, tc := range []struct {
		projectName string
		want        string
	}{
		{"Project\r\nOne\n", "Project One: line one line two"},
		{"\n \r", "proj-1: line one line two"},
	} {
		item := parkedItem("t1", 10)
		item.ProjectName = tc.projectName
		item.Subject = "line one\nline two"
		n := notificationFor(frTenant, item)
		if n.Subject != tc.want {
			t.Fatalf("project name %q: subject %q, want %q", tc.projectName, n.Subject, tc.want)
		}
		if err := n.Validate(); err != nil {
			t.Fatalf("project name %q: validate: %v", tc.projectName, err)
		}
	}

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

// frTestChannel is a notify.Channel that records what it receives and
// fails while failing is set.
type frTestChannel struct {
	name    string
	secure  bool
	mu      sync.Mutex
	failing bool
	got     []notify.Notification
}

func (c *frTestChannel) Name() string { return c.name }

func (c *frTestChannel) Secure() bool { return c.secure }

func (c *frTestChannel) Deliver(_ context.Context, n notify.Notification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failing {
		return errors.New(c.name + " unavailable")
	}
	c.got = append(c.got, n)
	return nil
}

func (c *frTestChannel) setFailing(failing bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failing = failing
}

func (c *frTestChannel) received() []notify.Notification {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]notify.Notification(nil), c.got...)
}

// newDispatchedResponder wires a FirstResponder to a real notify.Dispatcher
// over a secure "signal" channel, failing at first, and an insecure "slack"
// channel.
func newDispatchedResponder(t *testing.T, lister *fakeAttentionLister) (*FirstResponder, *frTestChannel, *frTestChannel) {
	t.Helper()
	signal := &frTestChannel{name: "signal", secure: true, failing: true}
	slack := &frTestChannel{name: "slack"}
	d, err := notify.NewDispatcher(notify.DispatcherConfig{Channels: []notify.Channel{signal, slack}})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	r, err := NewFirstResponder(FirstResponderConfig{Attention: lister, Notifier: d})
	if err != nil {
		t.Fatalf("NewFirstResponder: %v", err)
	}
	return r, signal, slack
}

func TestFirstResponder_PartialFailureRetriesOnlyFailedChannel(t *testing.T) {
	lister := &fakeAttentionLister{}
	lister.set(alertItem("agent_offline:a1", "warning"))
	r, signal, slack := newDispatchedResponder(t, lister)

	if res := mustSweep(t, r); res.Notified != 0 || res.Failed != 1 {
		t.Fatalf("sweep 1: %+v", res)
	}
	if got := slack.received(); len(got) != 1 || got[0].Key != "agent_offline:a1@7" {
		t.Fatalf("slack after sweep 1: %+v", got)
	}
	if got := signal.received(); len(got) != 0 {
		t.Fatalf("signal after sweep 1: %+v", got)
	}

	signal.setFailing(false)
	if res := mustSweep(t, r); res.Notified != 1 || res.Failed != 0 {
		t.Fatalf("sweep 2: %+v", res)
	}
	if got := signal.received(); len(got) != 1 || got[0].Key != "agent_offline:a1@7" {
		t.Fatalf("signal after sweep 2: %+v", got)
	}
	if got := slack.received(); len(got) != 1 {
		t.Fatalf("slack re-sent in sweep 2: %+v", got)
	}

	if res := mustSweep(t, r); res.Notified != 0 || res.Failed != 0 {
		t.Fatalf("sweep 3: %+v", res)
	}
	if len(signal.received()) != 1 || len(slack.received()) != 1 {
		t.Fatalf("sweep 3 sent again: signal %d, slack %d", len(signal.received()), len(slack.received()))
	}
}

func TestFirstResponder_SensitiveBodyReachesSignalAfterRecovery(t *testing.T) {
	item := parkedItem("t1", 10)
	want := notificationFor(frTenant, item).Body
	if want == "" {
		t.Fatalf("parked item has no body")
	}
	lister := &fakeAttentionLister{}
	lister.set(item)
	r, signal, slack := newDispatchedResponder(t, lister)

	if res := mustSweep(t, r); res.Failed != 1 {
		t.Fatalf("sweep 1: %+v", res)
	}
	got := slack.received()
	if len(got) != 1 || got[0].Body == want || strings.Contains(got[0].Body, "which branch?") || !strings.Contains(got[0].Body, "withheld") {
		t.Fatalf("slack after sweep 1: %+v", got)
	}

	signal.setFailing(false)
	if res := mustSweep(t, r); res.Notified != 1 {
		t.Fatalf("sweep 2: %+v", res)
	}
	sig := signal.received()
	if len(sig) != 1 || sig[0].Body != want {
		t.Fatalf("signal after sweep 2: %+v", sig)
	}
	if len(slack.received()) != 1 {
		t.Fatalf("slack re-sent in sweep 2: %+v", slack.received())
	}
}

func TestFirstResponder_MultilineProjectNameIsDelivered(t *testing.T) {
	item := parkedItem("t1", 10)
	item.ProjectName = "Project\nOne"
	lister := &fakeAttentionLister{}
	lister.set(item)
	r, signal, slack := newDispatchedResponder(t, lister)
	signal.setFailing(false)

	if res := mustSweep(t, r); res.Notified != 1 || res.Failed != 0 {
		t.Fatalf("sweep 1: %+v", res)
	}
	for name, ch := range map[string]*frTestChannel{"signal": signal, "slack": slack} {
		if got := ch.received(); len(got) != 1 || got[0].Subject != "Project One: task t1" {
			t.Fatalf("%s after sweep 1: %+v", name, got)
		}
	}

	if res := mustSweep(t, r); res.Notified != 0 || res.Failed != 0 {
		t.Fatalf("sweep 2: %+v", res)
	}
	if len(signal.received()) != 1 || len(slack.received()) != 1 {
		t.Fatalf("sweep 2 sent again: signal %d, slack %d", len(signal.received()), len(slack.received()))
	}
}
