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
// delivered is never sent the same item at the same age again.
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
	// orchestration.MaxPageLimit items.
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
}

// announcement records an item's announcement at one age. pending holds
// the channels still owed the item at that age; an empty pending means it
// is fully announced.
type announcement struct {
	age     int64
	pending []string
}

// FirstResponder announces each new attention item through a notifier.
type FirstResponder struct {
	attention AttentionLister
	notifier  ResponderNotifier
	interval  time.Duration
	onSweep   func(tenantID string, result FirstResponderSweep, err error)

	mu sync.Mutex
	// notified maps tenant → item ID → its announcement.
	notified map[string]map[string]announcement
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
		attention: cfg.Attention,
		notifier:  cfg.Notifier,
		interval:  interval,
		onSweep:   cfg.OnSweep,
		notified:  make(map[string]map[string]announcement),
	}, nil
}

// Sweep lists the tenant's attention inbox and announces every item not
// yet announced at its current age, and retries each channel an earlier
// sweep failed for that age, only that channel. A List error returns it
// and leaves the record untouched; a delivery failure is counted and
// retried next sweep.
func (r *FirstResponder) Sweep(ctx context.Context, tenantID string) (FirstResponderSweep, error) {
	if tenantID == "" {
		return FirstResponderSweep{}, &InvalidInputError{Field: "tenant_id", Message: "is required"}
	}
	var items []*AttentionItem
	complete := false
	offset := 0
	for range maxFirstResponderPages {
		page, err := r.attention.List(ctx, tenantID, AttentionFilter{},
			&orchestration.Page{Limit: orchestration.MaxPageLimit, Offset: offset})
		if err != nil {
			return FirstResponderSweep{}, err
		}
		if page == nil {
			complete = true
			break
		}
		items = append(items, page.Items...)
		if !page.HasNext || page.NextOffset <= offset {
			complete = true
			break
		}
		offset = page.NextOffset
	}

	var result FirstResponderSweep
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		result.Listed++
		seen[item.ID] = struct{}{}
		age := attentionAge(item)
		prev, ok := r.lookup(tenantID, item.ID)
		retry := ok && prev.age == age
		if retry && len(prev.pending) == 0 {
			continue
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
			continue
		}
		var pending []string
		for _, f := range report.Failed {
			pending = append(pending, f.Channel)
		}
		r.record(tenantID, item.ID, announcement{age: age, pending: pending})
		if len(pending) > 0 {
			result.Failed++
			continue
		}
		result.Notified++
	}
	if complete {
		r.prune(tenantID, seen)
	}
	return result, nil
}

// lookup returns the item's recorded announcement, if any.
func (r *FirstResponder) lookup(tenantID, itemID string) (announcement, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	got, ok := r.notified[tenantID][itemID]
	return got, ok
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

// prune forgets every recorded item not in seen, so an item that resolves
// and later returns is announced again.
func (r *FirstResponder) prune(tenantID string, seen map[string]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id := range r.notified[tenantID] {
		if _, ok := seen[id]; !ok {
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
// alert is not.
func notificationFor(tenantID string, item *AttentionItem) notify.Notification {
	label := item.ProjectName
	if label == "" {
		label = item.ProjectID
	}
	if label == "" {
		label = string(item.Kind)
	}
	subject := strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(item.Subject)), " ")
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
