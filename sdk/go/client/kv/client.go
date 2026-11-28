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

// Package kv provides the KV client for the QuicBaaS SDK.
package kv

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.KV"
)

// Client provides key-value operations.
type Client interface {
	// Put stores a key-value pair.
	Put(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error)

	// Get retrieves a value by key.
	Get(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error)

	// Delete removes a key.
	Delete(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error)

	// List lists entities of a type with pagination.
	List(ctx context.Context, req *service.ListRequest) (*service.ListResponse, error)

	// Watch watches a specific key for changes.
	Watch(ctx context.Context, key string, opts ...WatchOption) (Watcher, error)

	// WatchPrefix watches all keys with a given prefix.
	WatchPrefix(ctx context.Context, prefix string, opts ...WatchOption) (Watcher, error)
}

// EventType represents the type of watch event.
type EventType int

const (
	// EventPut indicates a key was created or updated.
	EventPut EventType = iota
	// EventDelete indicates a key was deleted.
	EventDelete
)

// String returns the string representation of the event type.
func (t EventType) String() string {
	switch t {
	case EventPut:
		return "PUT"
	case EventDelete:
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

// WatchEvent represents a key change notification.
type WatchEvent struct {
	Type      EventType
	Key       string
	Value     []byte
	PrevValue []byte
	Revision  uint64
	Timestamp time.Time
}

// WatchOption configures watch behavior.
type WatchOption func(*watchOptions)

type watchOptions struct {
	startRevision  uint64
	prevKV         bool
	progressNotify bool
	filterPut      bool
	filterDelete   bool
}

// WithStartRevision starts watching from a specific revision.
func WithStartRevision(rev uint64) WatchOption {
	return func(o *watchOptions) {
		o.startRevision = rev
	}
}

// WithPrevKV includes the previous value in watch events.
func WithPrevKV() WatchOption {
	return func(o *watchOptions) {
		o.prevKV = true
	}
}

// WithProgressNotify enables periodic progress notifications.
func WithProgressNotify() WatchOption {
	return func(o *watchOptions) {
		o.progressNotify = true
	}
}

// WithFilterPut filters out PUT events (only receive DELETE).
func WithFilterPut() WatchOption {
	return func(o *watchOptions) {
		o.filterPut = true
	}
}

// WithFilterDelete filters out DELETE events (only receive PUT).
func WithFilterDelete() WatchOption {
	return func(o *watchOptions) {
		o.filterDelete = true
	}
}

// Watcher provides event streaming for key changes.
type Watcher interface {
	// Events returns a channel of watch events.
	Events() <-chan *WatchEvent

	// Err returns the error that caused the watcher to close, if any.
	Err() error

	// Close stops watching and releases resources.
	Close() error
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new KV client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Put stores a key-value pair.
func (c *client) Put(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
	if err := validatePutRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "Put", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("kv.Put", "nil response from server")
	}

	putResp, ok := resp.(*service.PutResponse)
	if !ok {
		return nil, errors.NewInternal("kv.Put", "invalid response type")
	}

	return putResp, nil
}

// Get retrieves a value by key.
func (c *client) Get(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
	if err := validateGetRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "Get", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &service.GetResponse{Found: false}, nil
	}

	getResp, ok := resp.(*service.GetResponse)
	if !ok {
		return nil, errors.NewInternal("kv.Get", "invalid response type")
	}

	return getResp, nil
}

// Delete removes a key.
func (c *client) Delete(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	if err := validateDeleteRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "Delete", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("kv.Delete", "nil response from server")
	}

	deleteResp, ok := resp.(*service.DeleteResponse)
	if !ok {
		return nil, errors.NewInternal("kv.Delete", "invalid response type")
	}

	return deleteResp, nil
}

// List lists entities of a type with pagination.
func (c *client) List(ctx context.Context, req *service.ListRequest) (*service.ListResponse, error) {
	if err := validateListRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "List", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &service.ListResponse{Entities: []*service.Entity{}}, nil
	}

	listResp, ok := resp.(*service.ListResponse)
	if !ok {
		return nil, errors.NewInternal("kv.List", "invalid response type")
	}

	return listResp, nil
}

// Watch watches a specific key for changes.
func (c *client) Watch(ctx context.Context, key string, opts ...WatchOption) (Watcher, error) {
	if key == "" {
		return nil, errors.NewInvalidInput("kv.Watch", "key is required")
	}

	options := &watchOptions{}
	for _, opt := range opts {
		opt(options)
	}

	watchReq := &watchRequest{
		Key:            key,
		IsPrefix:       false,
		StartRevision:  options.startRevision,
		PrevKV:         options.prevKV,
		ProgressNotify: options.progressNotify,
		FilterPut:      options.filterPut,
		FilterDelete:   options.filterDelete,
	}

	return c.startWatch(ctx, watchReq)
}

// WatchPrefix watches all keys with a given prefix.
func (c *client) WatchPrefix(ctx context.Context, prefix string, opts ...WatchOption) (Watcher, error) {
	if prefix == "" {
		return nil, errors.NewInvalidInput("kv.WatchPrefix", "prefix is required")
	}

	options := &watchOptions{}
	for _, opt := range opts {
		opt(options)
	}

	watchReq := &watchRequest{
		Prefix:         prefix,
		IsPrefix:       true,
		StartRevision:  options.startRevision,
		PrevKV:         options.prevKV,
		ProgressNotify: options.progressNotify,
		FilterPut:      options.filterPut,
		FilterDelete:   options.filterDelete,
	}

	return c.startWatch(ctx, watchReq)
}

// watchRequest is the internal watch request.
type watchRequest struct {
	Key            string
	Prefix         string
	IsPrefix       bool
	StartRevision  uint64
	PrevKV         bool
	ProgressNotify bool
	FilterPut      bool
	FilterDelete   bool
}

// startWatch initiates a watch stream.
func (c *client) startWatch(ctx context.Context, req *watchRequest) (Watcher, error) {
	serviceReq, err := req.serviceRequest()
	if err != nil {
		return nil, err
	}

	stream, err := c.transport.RequestStream(ctx, serviceName, "Watch", serviceReq)
	if err != nil {
		return nil, err
	}

	w := &watcher{
		stream:  stream,
		events:  make(chan *WatchEvent, 100),
		done:    make(chan struct{}),
		options: req,
	}

	go w.receiveLoop()

	return w, nil
}

// serviceRequest maps the watch onto the request the KV Watch handler
// accepts. The put and delete filters travel as service filters and are also
// applied by the watcher, so they hold whatever the server honours.
func (r *watchRequest) serviceRequest() (*service.WatchRequest, error) {
	if r.StartRevision > math.MaxInt64 {
		return nil, errors.NewInvalidInput("kv.Watch", "start revision exceeds the maximum revision")
	}

	key := r.Key
	if r.IsPrefix {
		key = r.Prefix
	}

	var filters []service.WatchEventType
	if r.FilterPut {
		filters = append(filters, service.WatchEventTypePut)
	}
	if r.FilterDelete {
		filters = append(filters, service.WatchEventTypeDelete)
	}

	return &service.WatchRequest{
		Key:            []byte(key),
		IsPrefix:       r.IsPrefix,
		StartRevision:  int64(r.StartRevision),
		PrevKV:         r.PrevKV,
		ProgressNotify: r.ProgressNotify,
		Filters:        filters,
	}, nil
}

// watcher implements the Watcher interface.
type watcher struct {
	stream  transport.Stream
	events  chan *WatchEvent
	done    chan struct{}
	options *watchRequest

	mu     sync.Mutex
	err    error
	closed atomic.Bool
}

// Events returns the event channel.
func (w *watcher) Events() <-chan *WatchEvent {
	return w.events
}

// Err returns any error that caused the watcher to close.
func (w *watcher) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// Close stops the watcher.
func (w *watcher) Close() error {
	if !w.closed.CompareAndSwap(false, true) {
		return nil
	}

	close(w.done)

	if w.stream != nil {
		w.stream.Close()
	}

	return nil
}

// receiveLoop receives events from the stream.
func (w *watcher) receiveLoop() {
	defer func() {
		close(w.events)
	}()

	for {
		select {
		case <-w.done:
			return
		default:
			msg, err := w.stream.Recv()
			if err != nil {
				w.mu.Lock()
				w.err = err
				w.mu.Unlock()
				return
			}

			for _, event := range watchEvents(msg) {
				// Apply filters
				if w.options.FilterPut && event.Type == EventPut {
					continue
				}
				if w.options.FilterDelete && event.Type == EventDelete {
					continue
				}

				select {
				case w.events <- event:
				case <-w.done:
					return
				}
			}
		}
	}
}

// watchEvents extracts the events a stream message carries. The KV Watch
// handler sends *service.WatchResponse batches; the creation confirmation and
// progress notifications carry no events.
func watchEvents(msg interface{}) []*WatchEvent {
	switch m := msg.(type) {
	case *service.WatchResponse:
		events := make([]*WatchEvent, 0, len(m.Events))
		for _, e := range m.Events {
			if event, ok := fromServiceEvent(e); ok {
				events = append(events, event)
			}
		}
		return events
	case *WatchEvent:
		return []*WatchEvent{m}
	default:
		return nil
	}
}

// fromServiceEvent maps a service watch event onto a WatchEvent. An event of
// unspecified type is reported as not mapped rather than guessed.
func fromServiceEvent(e *service.WatchEvent) (*WatchEvent, bool) {
	if e == nil {
		return nil, false
	}

	var eventType EventType
	switch e.Type {
	case service.WatchEventTypePut:
		eventType = EventPut
	case service.WatchEventTypeDelete:
		eventType = EventDelete
	default:
		return nil, false
	}

	var revision uint64
	if e.Revision > 0 {
		revision = uint64(e.Revision)
	}

	return &WatchEvent{
		Type:      eventType,
		Key:       string(e.Key),
		Value:     e.Value,
		PrevValue: e.PrevValue,
		Revision:  revision,
		Timestamp: time.Unix(0, e.Timestamp),
	}, true
}

// Validation functions

func validatePutRequest(req *service.PutRequest) error {
	if req == nil {
		return errors.NewInvalidInput("kv.Put", "request is required")
	}
	if req.EntityType == "" {
		return errors.NewInvalidInput("kv.Put", "entity type is required")
	}
	return nil
}

func validateGetRequest(req *service.GetRequest) error {
	if req == nil {
		return errors.NewInvalidInput("kv.Get", "request is required")
	}
	if req.EntityType == "" {
		return errors.NewInvalidInput("kv.Get", "entity type is required")
	}
	return nil
}

func validateDeleteRequest(req *service.DeleteRequest) error {
	if req == nil {
		return errors.NewInvalidInput("kv.Delete", "request is required")
	}
	if req.EntityType == "" {
		return errors.NewInvalidInput("kv.Delete", "entity type is required")
	}
	return nil
}

func validateListRequest(req *service.ListRequest) error {
	if req == nil {
		return errors.NewInvalidInput("kv.List", "request is required")
	}
	if req.EntityType == "" {
		return errors.NewInvalidInput("kv.List", "entity type is required")
	}
	return nil
}
