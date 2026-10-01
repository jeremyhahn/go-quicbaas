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

package registry

import (
	"context"
	stderrors "errors"
	"io"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	// watchBuffer sizes the event channel Watch returns, so a briefly slow
	// consumer does not stall the stream.
	watchBuffer = 64

	// watchMaxReconnects bounds transparent resumption of a dropped watch.
	watchMaxReconnects = 5

	// watchReconnectBase is the first reconnect delay; each attempt doubles
	// it up to watchReconnectMax.
	watchReconnectBase = 250 * time.Millisecond

	// watchReconnectMax caps the reconnect backoff.
	watchReconnectMax = 4 * time.Second
)

// Client is the service registry client: lease-based service-instance
// endpoints scoped to the caller's authenticated tenant.
type Client interface {
	// Register lists an instance under a new lease. Keep the registration
	// alive with Renew; it disappears when the lease lapses.
	Register(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error)

	// Renew refreshes the lease backing one of the caller's registrations.
	Renew(ctx context.Context, leaseID int64) (*RenewResponse, error)

	// Deregister revokes the lease backing one of the caller's
	// registrations.
	Deregister(ctx context.Context, leaseID int64) error

	// Resolve lists a service's instances and the revision they were read
	// at.
	Resolve(ctx context.Context, service string) (*ResolveResponse, error)

	// Watch streams a service's registration changes from req.FromRevision
	// (zero: the changes after the current revision). A dropped connection
	// is resumed from the last delivered revision, so the stream neither
	// skips nor repeats a change. The channel closes when ctx ends, or after
	// a final event whose Err says why the watch could not continue —
	// including a refusal to open it (a compacted start revision, a missing
	// permission). Watch itself returns an error only for an invalid request
	// or when it cannot read the current revision to start "from now".
	Watch(ctx context.Context, req *WatchRequest) (<-chan Event, error)
}

// client is the transport-backed Client.
type client struct {
	t transport.Transport

	// reconnectBase and maxReconnects bound watch resumption. They are
	// fields rather than constants so tests can compress the schedule.
	reconnectBase time.Duration
	maxReconnects int
}

// NewClient creates a registry client over a transport.
func NewClient(t transport.Transport) Client {
	return &client{t: t, reconnectBase: watchReconnectBase, maxReconnects: watchMaxReconnects}
}

// Register lists an instance under a new lease.
func (c *client) Register(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error) {
	const op = "registry.Register"
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if err := validateName(op, "service", req.Instance.Service); err != nil {
		return nil, err
	}
	if err := validateName(op, "id", req.Instance.ID); err != nil {
		return nil, err
	}
	if req.Instance.Address == "" {
		return nil, errors.NewInvalidInput(op, "address is required")
	}
	if req.TTLSeconds < MinTTLSeconds || req.TTLSeconds > MaxTTLSeconds {
		return nil, errors.NewInvalidInput(op, "ttl_seconds must lie between 1 and 86400")
	}
	resp, err := c.t.Request(ctx, ServiceName, "Register", req)
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*RegisterResponse)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return out, nil
}

// Renew refreshes a registration's lease.
func (c *client) Renew(ctx context.Context, leaseID int64) (*RenewResponse, error) {
	const op = "registry.Renew"
	if leaseID <= 0 {
		return nil, errors.NewInvalidInput(op, "lease ID must be positive")
	}
	resp, err := c.t.Request(ctx, ServiceName, "Renew", &RenewRequest{LeaseID: leaseID})
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*RenewResponse)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return out, nil
}

// Deregister revokes a registration's lease.
func (c *client) Deregister(ctx context.Context, leaseID int64) error {
	const op = "registry.Deregister"
	if leaseID <= 0 {
		return errors.NewInvalidInput(op, "lease ID must be positive")
	}
	_, err := c.t.Request(ctx, ServiceName, "Deregister", &DeregisterRequest{LeaseID: leaseID})
	return err
}

// Resolve lists a service's instances.
func (c *client) Resolve(ctx context.Context, service string) (*ResolveResponse, error) {
	const op = "registry.Resolve"
	if err := validateName(op, "service", service); err != nil {
		return nil, err
	}
	resp, err := c.t.Request(ctx, ServiceName, "Resolve", &ResolveRequest{Service: service})
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*ResolveResponse)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return out, nil
}

// Watch streams a service's registration changes.
//
// A watch "from now" (FromRevision zero) is first pinned to a concrete
// revision with Resolve. That is what makes resumption exact on every
// transport: a reconnect always names the revision after the last one
// delivered, never "now", which would silently drop whatever changed while
// the connection was down.
func (c *client) Watch(ctx context.Context, req *WatchRequest) (<-chan Event, error) {
	const op = "registry.Watch"
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if err := validateName(op, "service", req.Service); err != nil {
		return nil, err
	}
	if req.FromRevision < 0 {
		return nil, errors.NewInvalidInput(op, "from revision must not be negative")
	}

	from := req.FromRevision
	if from == 0 {
		snapshot, err := c.Resolve(ctx, req.Service)
		if err != nil {
			return nil, err
		}
		from = snapshot.Revision + 1
	}

	out := make(chan Event, watchBuffer)
	w := &watcher{client: c, ctx: ctx, service: req.Service, next: from, out: out}
	go w.run()
	return out, nil
}

// watcher forwards one watch's events, resuming it across disconnects.
type watcher struct {
	client  *client
	ctx     context.Context
	service string
	stream  transport.Stream

	// next is the first revision not yet delivered: a resume asks for it.
	next int64

	// attempts counts consecutive failed resumes; a delivered event resets
	// it, so a long-lived watch is never starved by earlier, healed blips.
	attempts int

	out chan<- Event
}

// run opens the watch, forwards its events until it ends, then closes the
// channel. Opening here rather than in Watch is what gives every transport
// the same contract: gRPC and the embedded transport open lazily and report a
// refusal on the first read, REST reports it on open, and either way the
// caller sees it as the final event.
func (w *watcher) run() {
	defer close(w.out)
	stream, err := w.client.t.RequestStream(w.ctx, ServiceName, "Watch", &WatchRequest{Service: w.service, FromRevision: w.next})
	if err != nil {
		if w.ctx.Err() != nil {
			return
		}
		if final := terminalError(w.service, w.next, err); final != nil {
			w.emit(Event{Err: final})
			return
		}
		resumeErr := w.resume(err)
		if w.ctx.Err() != nil {
			return
		}
		if resumeErr != nil {
			w.emit(Event{Err: resumeErr})
			return
		}
	} else {
		w.stream = stream
	}
	for {
		ev, err := w.recv()
		if err == nil {
			w.attempts = 0
			if ev.Revision >= w.next {
				w.next = ev.Revision + 1
			}
			if !w.emit(*ev) {
				return
			}
			continue
		}
		if w.ctx.Err() != nil {
			// Every transport binds its stream to the watch's context, so
			// cancelling it has already torn the stream down.
			return
		}
		if final := terminalError(w.service, w.next, err); final != nil {
			w.emit(Event{Err: stderrors.Join(final, w.closeStream())})
			return
		}
		resumeErr := w.resume(err)
		if w.ctx.Err() != nil {
			return
		}
		if resumeErr != nil {
			w.emit(Event{Err: resumeErr})
			return
		}
	}
}

// recv reads one change. The embedded transport hands a handler failure
// over as a message, every other transport as Recv's error, so both forms
// are read as the stream's failure.
func (w *watcher) recv() (*Event, error) {
	msg, err := w.stream.Recv()
	if err != nil {
		return nil, err
	}
	switch m := msg.(type) {
	case *Event:
		return m, nil
	case error:
		return nil, m
	default:
		return nil, errors.NewInternal("registry.Watch", "unexpected stream message type")
	}
}

// emit delivers an event unless the watch's context has ended.
func (w *watcher) emit(ev Event) bool {
	select {
	case <-w.ctx.Done():
		return false
	case w.out <- ev:
		return true
	}
}

// resume reopens a dropped watch from the first undelivered revision,
// retrying with exponential backoff. It returns nil once a stream is open (or
// the watch's context ended while waiting, which the caller checks), or the
// error that ends the watch.
func (w *watcher) resume(cause error) error {
	cause = stderrors.Join(cause, w.closeStream())
	for w.attempts < w.client.maxReconnects {
		w.attempts++
		if !w.wait(w.backoff()) {
			return nil
		}
		stream, err := w.client.t.RequestStream(w.ctx, ServiceName, "Watch", &WatchRequest{Service: w.service, FromRevision: w.next})
		if err == nil {
			w.stream = stream
			return nil
		}
		if final := terminalError(w.service, w.next, err); final != nil {
			return final
		}
		cause = err
	}
	return &WatchDisconnectedError{Service: w.service, LastRevision: w.next - 1, Attempts: w.attempts, Cause: cause}
}

// wait blocks for d unless the watch's context ends first.
func (w *watcher) wait(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-w.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// backoff is the delay before the current attempt: exponential from the
// base, capped.
func (w *watcher) backoff() time.Duration {
	delay := w.client.reconnectBase << (w.attempts - 1)
	if delay <= 0 || delay > watchReconnectMax {
		return watchReconnectMax
	}
	return delay
}

// closeStream releases the current stream and returns its close failure,
// which the caller reports with the error that ended or interrupted the
// watch.
func (w *watcher) closeStream() error {
	if w.stream == nil {
		return nil
	}
	err := w.stream.Close()
	w.stream = nil
	return err
}

// terminalError reports the failures a resume cannot fix — compaction, a
// store-ended watch, or a refusal of the request itself — and returns nil for
// a dropped connection worth resuming.
func terminalError(service string, from int64, err error) error {
	var compacted *WatchCompactedError
	if stderrors.As(err, &compacted) {
		if compacted.Service == "" {
			compacted.Service = service
		}
		if compacted.FromRevision == 0 {
			compacted.FromRevision = from
		}
		return compacted
	}
	var ended *WatchEndedError
	if stderrors.As(err, &ended) {
		return ended
	}
	if resumable(err) {
		return nil
	}
	return err
}

// resumable reports a failure of the connection rather than of the request.
func resumable(err error) bool {
	if stderrors.Is(err, io.EOF) || stderrors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.IsRetryable(err) {
		return true
	}
	switch errors.Code(err) {
	case errors.CodeConnection, errors.CodeTransport:
		return true
	default:
		return false
	}
}

// validateName mirrors the server's name rule so an obviously malformed
// request fails before a round trip.
func validateName(op, field, value string) error {
	if value == "" {
		return errors.NewInvalidInput(op, field+" is required")
	}
	if strings.ContainsRune(value, '/') {
		return errors.NewInvalidInput(op, field+" must not contain '/'")
	}
	return nil
}
