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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/notify"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The first responder — every new attention item reaches an operator
// =============================================================================
//
// The attention inbox lists everything only an operator can clear, but an
// operator must open it to see anything. The first responder sweeps the
// inbox and pushes each new item through the notifier (Signal first, then
// any other channel), once per item and age: an item re-parked later, or
// one that resolves and returns, is announced again. A channel that failed
// is retried on the next sweep, and only that channel: a channel that
// delivered is never sent the same item at the same age again. A sweep
// reads at most a page budget of the inbox and the next sweep resumes
// after it, so every item is eventually announced however long the inbox.
//
// Deduplication is in memory: after a restart every open item is announced
// once more.

// AttentionLister pages the attention inbox. AttentionService implements it.
type AttentionLister interface {
	List(ctx context.Context, tenantID string, filter AttentionFilter, page *orchestration.Page) (*orchestration.PageResult[*AttentionItem], error)
}

var _ AttentionLister = (*AttentionService)(nil)

// ResponderNotifier delivers one notification to the named channels; nil
// channels means every channel. notify.Dispatcher implements it.
type ResponderNotifier interface {
	NotifyChannels(ctx context.Context, n notify.Notification, channels []string) (notify.Report, error)
}

var _ ResponderNotifier = (*notify.Dispatcher)(nil)

const (
	// DefaultFirstResponderInterval is the sweep interval when none is
	// configured.
	DefaultFirstResponderInterval = time.Minute

	// MinFirstResponderInterval is the shortest sweep interval accepted.
	MinFirstResponderInterval = 5 * time.Second

	// maxFirstResponderPages bounds one sweep at this many pages of
	// orchestration.MaxPageLimit items; the next sweep resumes after them.
	maxFirstResponderPages = 50
)

// FirstResponderConfig configures a FirstResponder.
type FirstResponderConfig struct {
	// Attention lists the inbox. Required.
	Attention AttentionLister

	// Notifier delivers the notifications. Required.
	Notifier ResponderNotifier

	// Interval is the time between sweeps in Run. Zero means
	// DefaultFirstResponderInterval; below MinFirstResponderInterval is
	// refused.
	Interval time.Duration

	// OnSweep, when set, is called after each sweep Run performs.
	OnSweep func(tenantID string, result FirstResponderSweep, err error)
}

// FirstResponderSweep counts one sweep's outcome.
type FirstResponderSweep struct {
	// Listed is the number of inbox items seen.
	Listed int
	// Notified is the number of items every attempted channel delivered
	// in this sweep.
	Notified int
	// Failed is the number of items with a channel still owed after this
	// sweep, or whose notification was refused; they are retried next
	// sweep.
	Failed int
	// More is true when the sweep stopped at its page budget; the next
	// sweep continues from there.
	More bool
}

// announcement records an item's announcement at one age. pending holds
// the channels still owed the item at that age; an empty pending means it
// is fully announced.
type announcement struct {
	age     int64
	pending []string
	// seen is the pass in which the item was last listed.
	seen uint64
}

// sweepCursor is where a tenant's next sweep starts. offset is the next
// page offset; cycle numbers the current pass over the inbox, from 1;
// paged is true once the current pass has needed more than one sweep.
type sweepCursor struct {
	offset int
	cycle  uint64
	paged  bool
}

// FirstResponder announces each new attention item through a notifier.
type FirstResponder struct {
	attention AttentionLister
	notifier  ResponderNotifier
	interval  time.Duration
	onSweep   func(tenantID string, result FirstResponderSweep, err error)
	// pagesPerSweep bounds one sweep at this many pages.
	pagesPerSweep int

	mu sync.Mutex
	// notified maps tenant → item ID → its announcement.
	notified map[string]map[string]announcement
	// cursors maps tenant → where its next sweep starts.
	cursors map[string]sweepCursor
	// sweeping maps tenant → a one-slot lock held for a whole sweep, so
	// overlapping sweeps of one tenant run one after the other.
	sweeping map[string]chan struct{}
}

// NewFirstResponder validates cfg and returns a FirstResponder. Refusals
// are *InvalidInputError.
func NewFirstResponder(cfg FirstResponderConfig) (*FirstResponder, error) {
	if cfg.Attention == nil {
		return nil, &InvalidInputError{Field: "attention", Message: "is required"}
	}
	if cfg.Notifier == nil {
		return nil, &InvalidInputError{Field: "notifier", Message: "is required"}
	}
	interval := cfg.Interval
	if interval == 0 {
		interval = DefaultFirstResponderInterval
	}
	if interval < MinFirstResponderInterval {
		return nil, &InvalidInputError{Field: "interval", Message: "must be at least " + MinFirstResponderInterval.String()}
	}
	return &FirstResponder{
		attention:     cfg.Attention,
		notifier:      cfg.Notifier,
		interval:      interval,
		onSweep:       cfg.OnSweep,
		pagesPerSweep: maxFirstResponderPages,
		notified:      make(map[string]map[string]announcement),
		cursors:       make(map[string]sweepCursor),
		sweeping:      make(map[string]chan struct{}),
	}, nil
}

// lockSweep takes the tenant's sweep lock, waiting for a running sweep of
// the same tenant to finish. It returns ctx's error if ctx ends first.
func (r *FirstResponder) lockSweep(ctx context.Context, tenantID string) (func(), error) {
	r.mu.Lock()
	lock, ok := r.sweeping[tenantID]
	if !ok {
		lock = make(chan struct{}, 1)
		r.sweeping[tenantID] = lock
	}
	r.mu.Unlock()
	select {
	case lock <- struct{}{}:
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Sweep lists the tenant's attention inbox and announces every item not
// yet announced at its current age, and retries each channel an earlier
// sweep failed for that age, only that channel. A List error returns it
// and leaves the record untouched; a delivery failure is counted and
// retried next sweep. A sweep reads at most the page budget and the next
// sweep resumes after it, reporting More, so every item is eventually
// announced however long the inbox is. Sweeps of one tenant are serialized:
// an overlapping call waits for the running sweep (or for ctx), so the
// read, deliver and record steps of two sweeps never interleave and an item
// is announced once per age. Sweeps of different tenants run concurrently.
func (r *FirstResponder) Sweep(ctx context.Context, tenantID string) (FirstResponderSweep, error) {
	if tenantID == "" {
		return FirstResponderSweep{}, &InvalidInputError{Field: "tenant_id", Message: "is required"}
	}
	unlock, err := r.lockSweep(ctx, tenantID)
	if err != nil {
		return FirstResponderSweep{}, err
	}
	defer unlock()
	cur := r.cursor(tenantID)
	items, next, complete, err := r.listFrom(ctx, tenantID, cur.offset)
	if err != nil {
		return FirstResponderSweep{}, err
	}

	var result FirstResponderSweep
	for _, item := range items {
		if item == nil {
			continue
		}
		result.Listed++
		r.announce(ctx, tenantID, item, cur.cycle, &result)
	}
	if complete {
		r.endCycle(tenantID, cur)
		return result, nil
	}
	r.setCursor(tenantID, sweepCursor{offset: next, cycle: cur.cycle, paged: true})
	result.More = true
	return result, nil
}

// listFrom reads at most the page budget of the tenant's inbox from
// offset. It returns the items, the offset the next sweep starts at, and
// whether the read reached the end of the inbox.
func (r *FirstResponder) listFrom(ctx context.Context, tenantID string, offset int) ([]*AttentionItem, int, bool, error) {
	var items []*AttentionItem
	for range r.pagesPerSweep {
		page, err := r.attention.List(ctx, tenantID, AttentionFilter{},
			&orchestration.Page{Limit: orchestration.MaxPageLimit, Offset: offset})
		if err != nil {
			return nil, 0, false, err
		}
		if page == nil {
			return items, 0, true, nil
		}
		items = append(items, page.Items...)
		if !page.HasNext || page.NextOffset <= offset {
			return items, 0, true, nil
		}
		offset = page.NextOffset
	}
	return items, offset, false, nil
}

// announce notifies one listed item unless it is already announced at
// its age, retrying only the channels still owed, and counts the outcome.
func (r *FirstResponder) announce(ctx context.Context, tenantID string, item *AttentionItem, cycle uint64, result *FirstResponderSweep) {
	age := attentionAge(item)
	prev, ok := r.touch(tenantID, item.ID, cycle)
	retry := ok && prev.age == age
	if retry && len(prev.pending) == 0 {
		return
	}
	var channels []string
	if retry {
		channels = prev.pending
	}
	report, err := r.notifier.NotifyChannels(ctx, notificationFor(tenantID, item), channels)
	if len(report.Delivered) == 0 && len(report.Failed) == 0 {
		// Nothing was attempted: the notification or the channel
		// list was refused. Owed channels that no longer exist are
		// forgotten, so the next sweep sends to every channel.
		var cfgErr *notify.ConfigError
		if retry && errors.As(err, &cfgErr) {
			r.forget(tenantID, item.ID)
		}
		result.Failed++
		return
	}
	var pending []string
	for _, f := range report.Failed {
		pending = append(pending, f.Channel)
	}
	r.record(tenantID, item.ID, announcement{age: age, pending: pending, seen: cycle})
	if len(pending) > 0 {
		result.Failed++
		return
	}
	result.Notified++
}

// touch returns the item's recorded announcement, if any, and marks it
// seen in pass cycle.
func (r *FirstResponder) touch(tenantID, itemID string, cycle uint64) (announcement, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	got, ok := r.notified[tenantID][itemID]
	if ok {
		got.seen = cycle
		r.notified[tenantID][itemID] = got
	}
	return got, ok
}

// cursor returns where the tenant's next sweep starts: offset 0 of pass 1
// for a tenant not swept yet.
func (r *FirstResponder) cursor(tenantID string) sweepCursor {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.cursors[tenantID]
	if !ok {
		cur = sweepCursor{cycle: 1}
	}
	return cur
}

// setCursor stores where the tenant's next sweep starts.
func (r *FirstResponder) setCursor(tenantID string, cur sweepCursor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cursors[tenantID] = cur
}

// endCycle closes the tenant's pass cur.cycle: it prunes the records the
// pass did not see and starts the next pass at offset 0.
func (r *FirstResponder) endCycle(tenantID string, cur sweepCursor) {
	r.prune(tenantID, cur.cycle, cur.paged)
	r.setCursor(tenantID, sweepCursor{cycle: cur.cycle + 1})
}

// record notes the item's announcement.
func (r *FirstResponder) record(tenantID, itemID string, a announcement) {
	r.mu.Lock()
	defer r.mu.Unlock()
	byItem := r.notified[tenantID]
	if byItem == nil {
		byItem = make(map[string]announcement)
		r.notified[tenantID] = byItem
	}
	byItem[itemID] = a
}

// forget drops the item's announcement, so the next sweep sends it to
// every channel.
func (r *FirstResponder) forget(tenantID, itemID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.notified[tenantID], itemID)
	if len(r.notified[tenantID]) == 0 {
		delete(r.notified, tenantID)
	}
}

// prune runs at the end of pass cycle and forgets the records the pass
// did not see, so an item that resolves and later returns is announced
// again. A pass that spanned several sweeps keeps a record missed once:
// offset paging can skip an item at a page boundary when an earlier item
// resolves between sweeps, so a record is dropped only after the item is
// missed in two passes in a row.
func (r *FirstResponder) prune(tenantID string, cycle uint64, paged bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, a := range r.notified[tenantID] {
		missedOnce := a.seen+1 == cycle
		if a.seen != cycle && (!paged || !missedOnce) {
			delete(r.notified[tenantID], id)
		}
	}
	if len(r.notified[tenantID]) == 0 {
		delete(r.notified, tenantID)
	}
}

// Run sweeps the tenant's inbox at once and then every Interval until ctx
// is done, calling OnSweep after each sweep. It returns ctx.Err().
func (r *FirstResponder) Run(ctx context.Context, tenantID string) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		result, err := r.Sweep(ctx, tenantID)
		if r.onSweep != nil {
			r.onSweep(tenantID, result, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// notificationFor renders one attention item as a notification. A parked
// task carries the worker's own words, so it is Sensitive; an environmental
// alert is not. The headline is one line, project label included, because
// the notifier refuses a multi-line subject.
func notificationFor(tenantID string, item *AttentionItem) notify.Notification {
	label := oneLine(item.ProjectName)
	if label == "" {
		label = oneLine(item.ProjectID)
	}
	if label == "" {
		label = string(item.Kind)
	}
	subject := oneLine(item.Subject)
	if subject == "" {
		subject = string(item.Kind)
	}

	var parts []string
	for _, part := range []string{
		prefixed("Question: ", item.Question),
		prefixed("Recommendation: ", item.Recommendation),
		item.Message,
	} {
		if text := publicOutput(part); text != "" {
			parts = append(parts, text)
		}
	}

	return notify.Notification{
		TenantID:  tenantID,
		Key:       item.ID + "@" + strconv.FormatInt(attentionAge(item), 10),
		Severity:  attentionSeverity(item),
		Subject:   label + ": " + subject,
		Body:      strings.Join(parts, "\n\n"),
		Link:      item.IssueURL,
		Sensitive: item.Kind == AttentionParkedTask,
	}
}

// prefixed returns prefix+text, or "" when text is blank.
func prefixed(prefix, text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return prefix + text
}

// oneLine collapses every run of whitespace, line breaks included, to a
// single space and trims the ends.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// attentionSeverity grades an item: a critical alert or an escalated park
// is critical, everything else a warning.
func attentionSeverity(item *AttentionItem) notify.Severity {
	if item.Kind == AttentionParkedTask {
		if item.BlockKind == orchestration.TaskBlockKindEscalated {
			return notify.SeverityCritical
		}
		return notify.SeverityWarning
	}
	if item.Severity == string(notify.SeverityCritical) {
		return notify.SeverityCritical
	}
	return notify.SeverityWarning
}
