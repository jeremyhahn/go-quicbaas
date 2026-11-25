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

package server

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-qrdb/pkg/service"
)

// listOnlyCredentials is a credential servicer double that answers only
// CredentialList and CredentialStrategy.
type listOnlyCredentials struct {
	qrdbsdk.ServiceCredentialServicer
	calls int
}

func (l *listOnlyCredentials) CredentialList(context.Context) ([]string, error) {
	l.calls++
	return []string{"a"}, nil
}

func (l *listOnlyCredentials) CredentialStrategy() string { return "manual" }

func requireCredentialNotConfigured(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.As(err, new(*service.CapabilityUnsupportedError)) {
		t.Fatalf("%s: expected the not-configured CapabilityUnsupportedError, got %v", what, err)
	}
}

// The listeners are built before startService creates the bootstrap
// credential service, so the proxy must serve a servicer published after it.
func TestQRDBCredentialServicerProxy_ServesServicerPublishedAfterRegistration(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	proxy := newQRDBCredentialServicerProxy(s)
	double := &listOnlyCredentials{}
	s.publishCredentialServicer(double)

	got, err := proxy.CredentialList(t.Context())
	if err != nil {
		t.Fatalf("CredentialList: %v", err)
	}
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("CredentialList: expected [a], got %v", got)
	}
	if double.calls != 1 {
		t.Fatalf("expected one delegated CredentialList call, got %d", double.calls)
	}
	if got := proxy.CredentialStrategy(); got != "manual" {
		t.Fatalf("CredentialStrategy: expected manual, got %q", got)
	}
}

func TestQRDBCredentialServicerProxy_NotConfiguredWhenNothingWired(t *testing.T) {
	proxies := map[string]*qrdbCredentialServicerProxy{
		"unwired server": newQRDBCredentialServicerProxy(&Server{logger: buildTestLogger(t)}),
		"nil server":     newQRDBCredentialServicerProxy(nil),
	}
	for name, proxy := range proxies {
		ctx := t.Context()
		_, err := proxy.CredentialList(ctx)
		requireCredentialNotConfigured(t, name+" CredentialList", err)
		requireCredentialNotConfigured(t, name+" CredentialSubmit", proxy.CredentialSubmit(ctx, "n", []byte("v")))
		_, err = proxy.CredentialRetrieveVersion(ctx, "n", 1)
		requireCredentialNotConfigured(t, name+" CredentialRetrieveVersion", err)
		_, err = proxy.CredentialGCExpired(ctx, "n")
		requireCredentialNotConfigured(t, name+" CredentialGCExpired", err)
		if got := proxy.CredentialStrategy(); got != "" {
			t.Fatalf("%s CredentialStrategy: expected empty, got %q", name, got)
		}
	}
}

func TestQRDBCredentialServicerProxy_PublishNilClears(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	proxy := newQRDBCredentialServicerProxy(s)
	s.publishCredentialServicer(&listOnlyCredentials{})
	s.publishCredentialServicer(nil)

	_, err := proxy.CredentialList(t.Context())
	requireCredentialNotConfigured(t, "CredentialList after clearing", err)
}

func TestNodeBarrierServerOptions_SealDisabledCarriesCredentialProxy(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if got := s.nodeBarrierServerOptions(false); len(got) != 3 {
		t.Fatalf("seal disabled: expected the barrier proxy, sealed-window probe and credential proxy options, got %d", len(got))
	}
	if got := s.nodeBarrierServerOptions(true); len(got) != 0 {
		t.Fatalf("seal enabled: expected no options, got %d", len(got))
	}
}
