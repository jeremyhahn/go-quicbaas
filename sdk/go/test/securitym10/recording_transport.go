// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package securitym10

import (
	"context"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// recordingTransport is a minimal transport.Transport that captures every
// (service, method, request) call and returns canned responses. It is
// dedicated to the M10 SDK conformance tests.
type recordingTransport struct {
	mu sync.Mutex

	// responses maps service -> method -> {response, error}.
	responses map[string]map[string]any

	// errs maps service -> method -> error.
	errs map[string]map[string]error

	// calls captures every Request invocation.
	calls []recordedCall

	closed bool
}

type recordedCall struct {
	Service string
	Method  string
	Request interface{}
}

// setResponse installs a canned (response, error) pair for the given
// service/method tuple.
func (rt *recordingTransport) setResponse(service, method string, resp any, err error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.responses == nil {
		rt.responses = map[string]map[string]any{}
	}
	if rt.responses[service] == nil {
		rt.responses[service] = map[string]any{}
	}
	rt.responses[service][method] = resp

	if err != nil {
		if rt.errs == nil {
			rt.errs = map[string]map[string]error{}
		}
		if rt.errs[service] == nil {
			rt.errs[service] = map[string]error{}
		}
		rt.errs[service][method] = err
	}
}

// callsFor returns the number of recorded calls for a (service, method) pair.
func (rt *recordingTransport) callsFor(service, method string) int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	count := 0
	for _, c := range rt.calls {
		if c.Service == service && c.Method == method {
			count++
		}
	}
	return count
}

// totalCalls returns the number of Request and RequestStream invocations
// recorded on any service.
func (rt *recordingTransport) totalCalls() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.calls)
}

// Request implements transport.Transport.Request.
func (rt *recordingTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	rt.mu.Lock()
	rt.calls = append(rt.calls, recordedCall{Service: service, Method: method, Request: req})
	resp, hasResp := rt.responses[service][method]
	err, hasErr := rt.errs[service][method]
	rt.mu.Unlock()

	if hasErr {
		return nil, err
	}
	if hasResp {
		return resp, nil
	}
	return nil, nil
}

// RequestStream implements transport.Transport.RequestStream. It returns
// an error since the M10 conformance tests do not exercise streaming.
func (rt *recordingTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	rt.mu.Lock()
	rt.calls = append(rt.calls, recordedCall{Service: service, Method: method, Request: req})
	rt.mu.Unlock()
	return nil, nil
}

// Conn implements transport.Transport.Conn.
func (rt *recordingTransport) Conn() interface{} { return nil }

// Close implements transport.Transport.Close.
func (rt *recordingTransport) Close() error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.closed = true
	return nil
}

// Healthy implements transport.Transport.Healthy.
func (rt *recordingTransport) Healthy() bool { return !rt.closed }
