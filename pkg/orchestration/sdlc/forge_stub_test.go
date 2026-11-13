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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A stubbed forge REST API the adapters' fixture tests share: a real
// http.Client against an httptest server, served under an API root that is
// not the host's root so every path an adapter builds is checked against it.
// No call leaves the process.

// forgeCall is one request the adapter made.
type forgeCall struct {
	Method string
	Path   string
	Query  string
	Body   string
	Header http.Header
}

// forgeStub is a stubbed forge API.
type forgeStub struct {
	t      *testing.T
	server *httptest.Server

	// root is the path the API is served under, and fixtures the directory
	// below testdata its fixture files live in.
	root     string
	fixtures string

	// escaped keys handlers by the request's path AS SENT, percent-encoding
	// included, for a forge whose paths carry an encoded slash that the
	// decoded path would hide.
	escaped bool

	mu       sync.Mutex
	handlers map[string]http.HandlerFunc
	calls    []forgeCall
}

// newForgeStub starts a stub serving under root, answering from
// testdata/<fixtures>.
func newForgeStub(t *testing.T, root, fixtures string) *forgeStub {
	t.Helper()
	stub := &forgeStub{t: t, root: root, fixtures: fixtures, handlers: map[string]http.HandlerFunc{}}
	stub.server = httptest.NewServer(http.HandlerFunc(stub.serve))
	t.Cleanup(stub.server.Close)
	return stub
}

// serve records a request and answers it from the handler registered for its
// method and path below the API root.
func (s *forgeStub) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("reading request body: %v", err)
	}
	path := r.URL.Path
	if s.escaped {
		path = r.URL.EscapedPath()
	}
	path = strings.TrimPrefix(path, s.root)
	key := r.Method + " " + path

	s.mu.Lock()
	s.calls = append(s.calls, forgeCall{
		Method: r.Method, Path: path, Query: r.URL.RawQuery, Body: string(body), Header: r.Header.Clone(),
	})
	handler, ok := s.handlers[key]
	s.mu.Unlock()

	if !ok {
		forgeJSON(http.StatusNotFound, map[string]string{"message": "Not Found: " + key})(w, r)
		return
	}
	handler(w, r)
}

// api is the adapter's base URL.
func (s *forgeStub) api() string { return s.server.URL + s.root }

// handle registers a handler for "METHOD /path".
func (s *forgeStub) handle(key string, handler http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[key] = handler
}

// json registers a fixed JSON answer.
func (s *forgeStub) json(key string, status int, payload any) {
	s.handle(key, forgeJSON(status, payload))
}

// fixture registers a fixture file as the answer.
func (s *forgeStub) fixture(key string, status int, name string) {
	s.handle(key, forgeRaw(status, s.load(name)))
}

// sequence answers successive requests with successive handlers, repeating
// the last one once they run out.
func (s *forgeStub) sequence(key string, handlers ...http.HandlerFunc) {
	var mu sync.Mutex
	next := 0
	s.handle(key, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		handler := handlers[min(next, len(handlers)-1)]
		next++
		mu.Unlock()
		handler(w, r)
	})
}

// load reads a fixture, naming the stub's API root where it says {{API}}.
func (s *forgeStub) load(name string) string {
	s.t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", s.fixtures, name))
	if err != nil {
		s.t.Fatalf("fixture %s: %v", name, err)
	}
	return strings.ReplaceAll(string(raw), "{{API}}", s.api())
}

// requests returns the calls made to "METHOD /path".
func (s *forgeStub) requests(key string) []forgeCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var matched []forgeCall
	for _, call := range s.calls {
		if call.Method+" "+call.Path == key {
			matched = append(matched, call)
		}
	}
	return matched
}

// writes returns every call that was not a read.
func (s *forgeStub) writes() []forgeCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var matched []forgeCall
	for _, call := range s.calls {
		if call.Method != http.MethodGet {
			matched = append(matched, call)
		}
	}
	return matched
}

// forgeJSON answers with status and payload encoded as JSON.
func forgeJSON(status int, payload any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if payload != nil {
			_ = json.NewEncoder(w).Encode(payload)
		}
	}
}

// forgeRaw answers with status and a body as written.
func forgeRaw(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// decodeBody decodes a recorded JSON request body.
func decodeBody(t *testing.T, call forgeCall) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(call.Body), &body); err != nil {
		t.Fatalf("request body %q: %v", call.Body, err)
	}
	return body
}
