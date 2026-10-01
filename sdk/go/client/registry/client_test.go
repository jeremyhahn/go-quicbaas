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
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// scriptedStream replays a fixed sequence of Recv results, then blocks until
// its context ends (as a live, idle stream does).
type scriptedStream struct {
	ctx     context.Context
	results []recvResult
	closed  bool
}

type recvResult struct {
	msg interface{}
	err error
}

func (s *scriptedStream) Recv() (interface{}, error) {
	if len(s.results) == 0 {
		<-s.ctx.Done()
		return nil, s.ctx.Err()
	}
	r := s.results[0]
	s.results = s.results[1:]
	return r.msg, r.err
}
func (s *scriptedStream) Send(interface{}) error { return nil }
func (s *scriptedStream) CloseSend() error       { return nil }
func (s *scriptedStream) Close() error {
	s.closed = true
	return nil
}

// fakeTransport answers unary calls from a table and opens scripted streams
// in order, recording every request it saw.
type fakeTransport struct {
	mu        sync.Mutex
	requests  []string
	unary     map[string]func(req interface{}) (interface{}, error)
	streams   [][]recvResult
	openErrs  []error
	watchReqs []*WatchRequest
}

func (f *fakeTransport) Request(_ context.Context, service, method string, req interface{}) (interface{}, error) {
	f.mu.Lock()
	f.requests = append(f.requests, service+"/"+method)
	f.mu.Unlock()
	handler, ok := f.unary[method]
	if !ok {
		return nil, errors.NewUnimplemented("fake", method)
	}
	return handler(req)
}

func (f *fakeTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, service+"/"+method)
	f.watchReqs = append(f.watchReqs, req.(*WatchRequest))
	if len(f.openErrs) > 0 {
		err := f.openErrs[0]
		f.openErrs = f.openErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	if len(f.streams) == 0 {
		return &scriptedStream{ctx: ctx}, nil
	}
	script := f.streams[0]
	f.streams = f.streams[1:]
	return &scriptedStream{ctx: ctx, results: script}, nil
}

func (f *fakeTransport) Conn() interface{} { return nil }
func (f *fakeTransport) Close() error      { return nil }
func (f *fakeTransport) Healthy() bool     { return true }

func (f *fakeTransport) watchFroms() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int64, 0, len(f.watchReqs))
	for _, r := range f.watchReqs {
		out = append(out, r.FromRevision)
	}
	return out
}

func fastClient(t transport.Transport) *client {
	return &client{t: t, reconnectBase: time.Millisecond, maxReconnects: 3}
}

func put(rev int64) recvResult {
	return recvResult{msg: &Event{Type: EventPut, Instance: ServiceInstance{ID: "a", Service: "api"}, Revision: rev}}
}

func next(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("watch channel closed early")
		}
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a watch event")
		return Event{}
	}
}

func closedWithin(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if ok {
			t.Fatalf("expected the channel to close, got %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch channel did not close")
	}
}

func TestClient_RegisterRenewDeregisterResolve(t *testing.T) {
	var gotRegister *RegisterRequest
	var gotRenew, gotDeregister int64
	ft := &fakeTransport{unary: map[string]func(interface{}) (interface{}, error){
		"Register": func(req interface{}) (interface{}, error) {
			gotRegister = req.(*RegisterRequest)
			return &RegisterResponse{LeaseID: 5, TTLSeconds: gotRegister.TTLSeconds}, nil
		},
		"Renew": func(req interface{}) (interface{}, error) {
			gotRenew = req.(*RenewRequest).LeaseID
			return &RenewResponse{LeaseID: gotRenew, TTLSeconds: 10}, nil
		},
		"Deregister": func(req interface{}) (interface{}, error) {
			gotDeregister = req.(*DeregisterRequest).LeaseID
			return nil, nil
		},
		"Resolve": func(req interface{}) (interface{}, error) {
			return &ResolveResponse{Instances: []ServiceInstance{{ID: "a", Service: req.(*ResolveRequest).Service}}, Revision: 3}, nil
		},
	}}
	c := NewClient(ft)
	ctx := context.Background()

	lease, err := c.Register(ctx, &RegisterRequest{Instance: ServiceInstance{Service: "api", ID: "a", Address: "h:1"}, TTLSeconds: 9})
	if err != nil || lease.LeaseID != 5 || gotRegister.Instance.Address != "h:1" {
		t.Fatalf("Register = %+v, %v (sent %+v)", lease, err, gotRegister)
	}
	renewed, err := c.Renew(ctx, 5)
	if err != nil || renewed.TTLSeconds != 10 || gotRenew != 5 {
		t.Fatalf("Renew = %+v, %v", renewed, err)
	}
	if err := c.Deregister(ctx, 6); err != nil || gotDeregister != 6 {
		t.Fatalf("Deregister: %v (sent %d)", err, gotDeregister)
	}
	resolved, err := c.Resolve(ctx, "api")
	if err != nil || resolved.Revision != 3 || len(resolved.Instances) != 1 {
		t.Fatalf("Resolve = %+v, %v", resolved, err)
	}
	for _, r := range ft.requests {
		if r[:len(ServiceName)] != ServiceName {
			t.Fatalf("request %q not routed through %s", r, ServiceName)
		}
	}
}

func TestClient_ValidationFailsBeforeARoundTrip(t *testing.T) {
	ft := &fakeTransport{}
	c := NewClient(ft)
	ctx := context.Background()
	valid := ServiceInstance{Service: "api", ID: "a", Address: "h:1"}

	registerCases := map[string]*RegisterRequest{
		"nil":          nil,
		"no service":   {Instance: ServiceInstance{ID: "a", Address: "h:1"}, TTLSeconds: 5},
		"slash id":     {Instance: ServiceInstance{Service: "api", ID: "a/b", Address: "h:1"}, TTLSeconds: 5},
		"no address":   {Instance: ServiceInstance{Service: "api", ID: "a"}, TTLSeconds: 5},
		"ttl too low":  {Instance: valid, TTLSeconds: 0},
		"ttl too high": {Instance: valid, TTLSeconds: MaxTTLSeconds + 1},
	}
	for name, req := range registerCases {
		if _, err := c.Register(ctx, req); !errors.IsInvalidInput(err) {
			t.Fatalf("Register %s = %v, want invalid input", name, err)
		}
	}
	if _, err := c.Renew(ctx, 0); !errors.IsInvalidInput(err) {
		t.Fatalf("Renew(0) = %v", err)
	}
	if err := c.Deregister(ctx, -1); !errors.IsInvalidInput(err) {
		t.Fatalf("Deregister(-1) = %v", err)
	}
	if _, err := c.Resolve(ctx, ""); !errors.IsInvalidInput(err) {
		t.Fatalf("Resolve('') = %v", err)
	}
	if _, err := c.Watch(ctx, nil); !errors.IsInvalidInput(err) {
		t.Fatalf("Watch(nil) = %v", err)
	}
	if _, err := c.Watch(ctx, &WatchRequest{Service: "api", FromRevision: -1}); !errors.IsInvalidInput(err) {
		t.Fatalf("Watch(-1) = %v", err)
	}
	if len(ft.requests) != 0 {
		t.Fatalf("invalid requests reached the transport: %v", ft.requests)
	}
}

func TestClient_UnaryErrorsAndBadResponses(t *testing.T) {
	boom := errors.New(errors.CodeNotFound, "srv", "lease gone")
	failing := &fakeTransport{unary: map[string]func(interface{}) (interface{}, error){
		"Register":   func(interface{}) (interface{}, error) { return nil, boom },
		"Renew":      func(interface{}) (interface{}, error) { return nil, boom },
		"Deregister": func(interface{}) (interface{}, error) { return nil, boom },
		"Resolve":    func(interface{}) (interface{}, error) { return nil, boom },
	}}
	c := NewClient(failing)
	ctx := context.Background()
	if _, err := c.Register(ctx, &RegisterRequest{Instance: ServiceInstance{Service: "s", ID: "i", Address: "h:1"}, TTLSeconds: 5}); err != boom {
		t.Fatalf("Register err = %v", err)
	}
	if _, err := c.Renew(ctx, 1); !errors.IsNotFound(err) {
		t.Fatalf("Renew err = %v", err)
	}
	if err := c.Deregister(ctx, 1); err != boom {
		t.Fatalf("Deregister err = %v", err)
	}
	if _, err := c.Resolve(ctx, "s"); err != boom {
		t.Fatalf("Resolve err = %v", err)
	}

	wrongType := &fakeTransport{unary: map[string]func(interface{}) (interface{}, error){
		"Register": func(interface{}) (interface{}, error) { return "nope", nil },
		"Renew":    func(interface{}) (interface{}, error) { return "nope", nil },
		"Resolve":  func(interface{}) (interface{}, error) { return "nope", nil },
	}}
	c = NewClient(wrongType)
	if _, err := c.Register(ctx, &RegisterRequest{Instance: ServiceInstance{Service: "s", ID: "i", Address: "h:1"}, TTLSeconds: 5}); errors.Code(err) != errors.CodeInternal {
		t.Fatalf("Register wrong type = %v", err)
	}
	if _, err := c.Renew(ctx, 1); errors.Code(err) != errors.CodeInternal {
		t.Fatalf("Renew wrong type = %v", err)
	}
	if _, err := c.Resolve(ctx, "s"); errors.Code(err) != errors.CodeInternal {
		t.Fatalf("Resolve wrong type = %v", err)
	}
}

// TestClient_WatchFromNowPinsARevision proves a "from now" watch is anchored
// to the revision Resolve reports, so a later resume has a concrete cursor.
func TestClient_WatchFromNowPinsARevision(t *testing.T) {
	ft := &fakeTransport{
		unary: map[string]func(interface{}) (interface{}, error){
			"Resolve": func(interface{}) (interface{}, error) { return &ResolveResponse{Revision: 40}, nil },
		},
		streams: [][]recvResult{{put(41)}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := fastClient(ft).Watch(ctx, &WatchRequest{Service: "api"})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if ev := next(t, events); ev.Revision != 41 {
		t.Fatalf("event = %+v, want revision 41", ev)
	}
	if froms := ft.watchFroms(); len(froms) != 1 || froms[0] != 41 {
		t.Fatalf("watch opened from %v, want [41]", froms)
	}
}

// TestClient_WatchResumesFromTheNextUndeliveredRevision proves a dropped
// stream is reopened at last-delivered+1, so nothing is skipped or repeated.
func TestClient_WatchResumesFromTheNextUndeliveredRevision(t *testing.T) {
	ft := &fakeTransport{streams: [][]recvResult{
		{put(10), put(12), {err: io.EOF}},
		{{err: errors.New(errors.CodeUnavailable, "net", "reset")}},
		{put(15)},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := fastClient(ft).Watch(ctx, &WatchRequest{Service: "api", FromRevision: 10})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	for _, want := range []int64{10, 12, 15} {
		if ev := next(t, events); ev.Revision != want || ev.Err != nil {
			t.Fatalf("event = %+v, want revision %d", ev, want)
		}
	}
	if froms := ft.watchFroms(); len(froms) != 3 || froms[0] != 10 || froms[1] != 13 || froms[2] != 13 {
		t.Fatalf("watch opened from %v, want [10 13 13]", froms)
	}
}

func TestClient_WatchEndsOnCompaction(t *testing.T) {
	ft := &fakeTransport{streams: [][]recvResult{{put(3), {err: &WatchCompactedError{Message: "gone"}}}}}
	events, err := fastClient(ft).Watch(context.Background(), &WatchRequest{Service: "api", FromRevision: 3})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	next(t, events)
	final := next(t, events)
	var compacted *WatchCompactedError
	if !stderrors.As(final.Err, &compacted) || compacted.Service != "api" || compacted.FromRevision != 4 {
		t.Fatalf("final = %+v, want *WatchCompactedError for api from 4", final)
	}
	closedWithin(t, events)
}

// TestClient_WatchReadsAnErrorMessageAsTheFailure covers the embedded
// transport, which hands a handler failure over as a stream message.
func TestClient_WatchReadsAnErrorMessageAsTheFailure(t *testing.T) {
	ft := &fakeTransport{streams: [][]recvResult{{{msg: &WatchEndedError{Service: "api", Message: "evicted"}}}}}
	events, err := fastClient(ft).Watch(context.Background(), &WatchRequest{Service: "api", FromRevision: 1})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	var ended *WatchEndedError
	if final := next(t, events); !stderrors.As(final.Err, &ended) {
		t.Fatalf("final = %+v, want *WatchEndedError", final)
	}
	closedWithin(t, events)

	odd := &fakeTransport{streams: [][]recvResult{{{msg: 42}}}}
	events, err = fastClient(odd).Watch(context.Background(), &WatchRequest{Service: "api", FromRevision: 1})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if final := next(t, events); errors.Code(final.Err) != errors.CodeInternal {
		t.Fatalf("final = %+v, want an internal error for an unknown message", final)
	}
}

func TestClient_WatchGivesUpAfterTheRetryBudget(t *testing.T) {
	down := errors.New(errors.CodeConnection, "net", "refused")
	ft := &fakeTransport{
		streams:  [][]recvResult{{put(7), {err: io.EOF}}},
		openErrs: []error{nil, down, down, down},
	}
	events, err := fastClient(ft).Watch(context.Background(), &WatchRequest{Service: "api", FromRevision: 7})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	next(t, events)
	final := next(t, events)
	var disconnected *WatchDisconnectedError
	if !stderrors.As(final.Err, &disconnected) || disconnected.LastRevision != 7 || disconnected.Attempts != 3 {
		t.Fatalf("final = %+v, want *WatchDisconnectedError after revision 7 in 3 attempts", final)
	}
	if !stderrors.Is(final.Err, down) {
		t.Fatalf("final %v does not carry the last failure", final.Err)
	}
	closedWithin(t, events)
}

func TestClient_WatchStopsOnARefusedResume(t *testing.T) {
	forbidden := errors.New(errors.CodeForbidden, "srv", "token revoked")
	ft := &fakeTransport{
		streams:  [][]recvResult{{{err: io.EOF}}},
		openErrs: []error{nil, forbidden},
	}
	events, err := fastClient(ft).Watch(context.Background(), &WatchRequest{Service: "api", FromRevision: 2})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if final := next(t, events); !errors.IsForbidden(final.Err) {
		t.Fatalf("final = %+v, want the forbidden refusal", final)
	}
	closedWithin(t, events)
}

// TestClient_WatchOpenFailuresArriveAsTheFinalEvent pins the uniform
// contract: a refusal to open the stream is the final event, on every
// transport, while only the "from now" pin can fail Watch itself.
func TestClient_WatchOpenFailuresArriveAsTheFinalEvent(t *testing.T) {
	refused := errors.New(errors.CodeForbidden, "srv", "no")
	events, err := fastClient(&fakeTransport{openErrs: []error{refused}}).Watch(context.Background(), &WatchRequest{Service: "api", FromRevision: 1})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if final := next(t, events); final.Err != refused {
		t.Fatalf("final = %+v, want the refusal", final)
	}
	closedWithin(t, events)

	compacted := &WatchCompactedError{Message: "gone"}
	events, err = fastClient(&fakeTransport{openErrs: []error{compacted}}).Watch(context.Background(), &WatchRequest{Service: "api", FromRevision: 4})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	var c *WatchCompactedError
	if final := next(t, events); !stderrors.As(final.Err, &c) || c.FromRevision != 4 || c.Service != "api" {
		t.Fatalf("final = %+v, want the compaction for api from 4", final)
	}

	// A connection failure at open is resumed like any other.
	down := errors.New(errors.CodeUnavailable, "net", "reset")
	ft := &fakeTransport{openErrs: []error{down, nil}, streams: [][]recvResult{{put(4)}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err = fastClient(ft).Watch(ctx, &WatchRequest{Service: "api", FromRevision: 4})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if ev := next(t, events); ev.Revision != 4 || ev.Err != nil {
		t.Fatalf("event = %+v, want revision 4 after the reconnect", ev)
	}

	resolveFails := &fakeTransport{unary: map[string]func(interface{}) (interface{}, error){
		"Resolve": func(interface{}) (interface{}, error) { return nil, refused },
	}}
	if _, err := fastClient(resolveFails).Watch(context.Background(), &WatchRequest{Service: "api"}); err != refused {
		t.Fatalf("Watch pin err = %v", err)
	}
}

func TestClient_WatchEndsQuietlyWithItsContext(t *testing.T) {
	ft := &fakeTransport{streams: [][]recvResult{{put(1)}}}
	ctx, cancel := context.WithCancel(context.Background())
	events, err := fastClient(ft).Watch(ctx, &WatchRequest{Service: "api", FromRevision: 1})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	next(t, events)
	cancel()
	closedWithin(t, events)
}

func TestWatchErrors_Messages(t *testing.T) {
	cause := stderrors.New("refused")
	d := &WatchDisconnectedError{Service: "api", LastRevision: 9, Attempts: 2, Cause: cause}
	if !stderrors.Is(d, cause) || d.Error() == "" {
		t.Fatalf("WatchDisconnectedError = %q", d.Error())
	}
	if (&WatchCompactedError{Service: "api", FromRevision: 3}).Error() == "" || (&WatchEndedError{Service: "api"}).Error() == "" {
		t.Fatal("watch error messages are empty")
	}
}
