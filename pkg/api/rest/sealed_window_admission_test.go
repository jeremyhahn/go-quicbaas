//go:build rest

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

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsealedwindow "github.com/jeremyhahn/go-qrdb/pkg/api/sealedwindow"
)

// recordingListener is a sealedListenerProbe that records every Admit.
type recordingListener struct {
	sealed atomic.Bool
	mu     sync.Mutex
	admits []bool
}

func (l *recordingListener) IsSealed() bool                      { return l.sealed.Load() }
func (l *recordingListener) EnrollmentOpen(context.Context) bool { return true }
func (l *recordingListener) Admit(transition bool) func() {
	l.mu.Lock()
	l.admits = append(l.admits, transition)
	l.mu.Unlock()
	return func() {}
}

func (l *recordingListener) take() []bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.admits
	l.admits = nil
	return out
}

// admissionListener is a sealedListenerProbe over a real Admission.
type admissionListener struct {
	sealed    atomic.Bool
	admission *qrdbsealedwindow.Admission
}

func (l *admissionListener) IsSealed() bool                      { return l.sealed.Load() }
func (l *admissionListener) EnrollmentOpen(context.Context) bool { return true }
func (l *admissionListener) Admit(transition bool) func()        { return l.admission.Admit(transition) }

func waitBriefly() <-chan time.Time { return time.After(150 * time.Millisecond) }

func serveSealed(s *Server, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestRESTSealedWindowAdmission_NativeRoutesShareTheListenerAdmission(t *testing.T) {
	s := newSealedGateRESTServer(t, &restSealedProbe{sealed: true})
	s.SetSealedWindowHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(sealedWindowStubHeader, "1")
		w.WriteHeader(http.StatusOK)
	}))
	listener := &recordingListener{}
	listener.sealed.Store(true)
	s.SetSealedWindowAdmission(listener)

	// A native unseal holds the admission exclusively.
	rec := serveSealed(s, http.MethodPost, "/v1/barrier/unseal")
	assert.Empty(t, rec.Header().Get(sealedWindowStubHeader))
	assert.Equal(t, []bool{true}, listener.take())

	// A native non-transition route holds it shared.
	serveSealed(s, http.MethodGet, "/v1/barrier/status")
	assert.Equal(t, []bool{false}, listener.take())

	// The mounted qrdb handler admits on the same probe itself; the native
	// middleware must not admit it a second time.
	rec = serveSealed(s, http.MethodPost, "/v1/barrier/officers")
	assert.Equal(t, "1", rec.Header().Get(sealedWindowStubHeader))
	assert.Empty(t, listener.take())
}

func TestRESTSealedWindowAdmission_RefusesNativeRoutesOnceUnsealed(t *testing.T) {
	s := newSealedGateRESTServer(t, &restSealedProbe{sealed: true})
	listener := &recordingListener{}
	s.SetSealedWindowAdmission(listener) // sealed is false: another transport unsealed

	for _, path := range []string{"/v1/barrier/status", "/v1/barrier/unseal"} {
		rec := serveSealed(s, http.MethodPost, path)
		require.Equal(t, http.StatusServiceUnavailable, rec.Code, path)
		assert.Contains(t, rec.Body.String(), qrdbsealedwindow.ClosedMessage, path)
	}
}

func TestRESTSealedWindowAdmission_UnsealWaitsForRequestsInFlight(t *testing.T) {
	s := newSealedGateRESTServer(t, &restSealedProbe{sealed: true})
	var admission qrdbsealedwindow.Admission
	listener := &admissionListener{admission: &admission}
	listener.sealed.Store(true)
	s.SetSealedWindowAdmission(listener)

	// An anonymous request on another transport is in flight.
	releaseOther := admission.Admit(false)

	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- serveSealed(s, http.MethodPost, "/v1/barrier/unseal") }()
	select {
	case <-done:
		t.Fatal("native unseal ran while another transport's request was in flight")
	case <-waitBriefly():
	}
	releaseOther()
	<-done
}
