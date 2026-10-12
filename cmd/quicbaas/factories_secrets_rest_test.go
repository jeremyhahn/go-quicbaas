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

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// recordedRequest is one request the REST test server saw.
type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Body   []byte
}

// restSecretsServer answers the secrets routes with the server's wire shapes
// and records every request, so a test asserts the method and path the CLI
// reached. It keeps stored payloads so a put followed by a get round-trips.
type restSecretsServer struct {
	mu       sync.Mutex
	requests []recordedRequest
	stored   map[string][]byte
}

func (s *restSecretsServer) last(t *testing.T) recordedRequest {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.requests)
	return s.requests[len(s.requests)-1]
}

func (s *restSecretsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, recordedRequest{r.Method, r.URL.Path, r.URL.RawQuery, body})
	s.mu.Unlock()

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/secrets/")
	var resp interface{}
	status := http.StatusOK
	switch {
	case r.URL.Path == "/api/v1/secrets":
		resp = &service.ListSecretsResponse{Secrets: []*service.Secret{{SecretID: "agents/a", Name: "agents/a"}}}
	case r.Method == http.MethodPost && r.URL.Query().Get("action") == "rotate":
		resp = &service.RotateSecretResponse{SecretID: id, OldVersion: 1, NewVersion: 2}
	case r.Method == http.MethodPost && r.URL.Query().Get("action") == "rollback":
		resp = &service.RollbackSecretResponse{SecretID: id, NewVersion: 3}
	case r.Method == http.MethodPost && strings.HasSuffix(id, "/disable"):
		resp = &service.UpdateSecretStatusResponse{SecretID: strings.TrimSuffix(id, "/disable")}
	case r.Method == http.MethodPost:
		var wire struct {
			Data []byte `json:"data"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.stored[id] = wire.Data
		s.mu.Unlock()
		status = http.StatusCreated
		resp = &service.StoreSecretResponse{SecretID: id, Version: 1}
	case r.Method == http.MethodGet && r.URL.Query().Get("versions") == "true":
		resp = &service.ListSecretVersionsResponse{Versions: []*service.SecretVersion{{SecretID: id, Version: 1}}}
	case r.Method == http.MethodGet:
		s.mu.Lock()
		data := s.stored[id]
		s.mu.Unlock()
		resp = &service.GetSecretResponse{SecretID: id, Version: 1, Data: data}
	case r.Method == http.MethodDelete:
		resp = &service.DeleteSecretResponse{SecretID: id, Deleted: true}
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// newRESTSecretsTestClient returns a restSecretsClient backed by a real SDK
// REST transport pointed at a recording TLS test server.
func newRESTSecretsTestClient(t *testing.T) (*restSecretsClient, *restSecretsServer) {
	t.Helper()
	backend := &restSecretsServer{stored: map[string][]byte{}}
	srv := httptest.NewTLSServer(backend)
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	sdk, err := quicbaas.NewClient(strings.TrimPrefix(srv.URL, "https://"),
		quicbaas.WithREST(),
		quicbaas.WithTLSConfig(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}),
	)
	require.NoError(t, err)
	client := &restSecretsClient{sdk: sdk}
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	return client, backend
}

// TestRESTSecretsClient_VerbsHitRealRoutes asserts every verb reaches the
// REST route the server serves, not the old "REST" pseudo-service URL.
func TestRESTSecretsClient_VerbsHitRealRoutes(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		call       func(c *restSecretsClient) error
		wantMethod string
		wantPath   string
		wantQuery  string
	}{
		{"store", func(c *restSecretsClient) error {
			_, err := c.StoreSecret(ctx, &service.StoreSecretRequest{SecretID: "agents/probe/check", Name: "agents/probe/check", Data: []byte("v")})
			return err
		}, http.MethodPost, "/api/v1/secrets/agents/probe/check", ""},
		{"get", func(c *restSecretsClient) error {
			_, err := c.GetSecret(ctx, &service.GetSecretRequest{SecretID: "agents/probe/check"})
			return err
		}, http.MethodGet, "/api/v1/secrets/agents/probe/check", ""},
		{"get version", func(c *restSecretsClient) error {
			_, err := c.GetSecret(ctx, &service.GetSecretRequest{SecretID: "agents/probe/check", Version: 2})
			return err
		}, http.MethodGet, "/api/v1/secrets/agents/probe/check", "version=2"},
		{"delete", func(c *restSecretsClient) error {
			_, err := c.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: "agents/probe/check"})
			return err
		}, http.MethodDelete, "/api/v1/secrets/agents/probe/check", ""},
		{"list", func(c *restSecretsClient) error {
			_, err := c.ListSecrets(ctx, &service.ListSecretsRequest{Prefix: "agents/"})
			return err
		}, http.MethodGet, "/api/v1/secrets", "prefix=agents%2F"},
		{"rotate", func(c *restSecretsClient) error {
			_, err := c.RotateSecret(ctx, &service.RotateSecretRequest{SecretID: "agents/probe/check", NewData: []byte("n")})
			return err
		}, http.MethodPost, "/api/v1/secrets/agents/probe/check", "action=rotate"},
		{"list versions", func(c *restSecretsClient) error {
			_, err := c.ListSecretVersions(ctx, &service.ListSecretVersionsRequest{SecretID: "agents/probe/check"})
			return err
		}, http.MethodGet, "/api/v1/secrets/agents/probe/check", "versions=true"},
		{"rollback", func(c *restSecretsClient) error {
			_, err := c.RollbackSecret(ctx, &service.RollbackSecretRequest{SecretID: "agents/probe/check", TargetVersion: 1})
			return err
		}, http.MethodPost, "/api/v1/secrets/agents/probe/check", "action=rollback"},
		{"disable", func(c *restSecretsClient) error {
			_, err := c.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{SecretID: "agents/probe/check", Status: service.SecretStatusDisabled})
			return err
		}, http.MethodPost, "/api/v1/secrets/agents/probe/check/disable", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, backend := newRESTSecretsTestClient(t)
			backend.stored["agents/probe/check"] = []byte("v")
			require.NoError(t, tc.call(client))
			got := backend.last(t)
			assert.Equal(t, tc.wantMethod, got.Method)
			assert.Equal(t, tc.wantPath, got.Path)
			assert.Equal(t, tc.wantQuery, got.Query)
		})
	}
}

// TestRESTSecretsClient_RawPutGetRoundTrip proves a put stores the bytes as
// given: non-UTF-8 and JSON-looking payloads come back identical.
func TestRESTSecretsClient_RawPutGetRoundTrip(t *testing.T) {
	payloads := map[string][]byte{
		"token":      []byte("ghp_abc123\n"),
		"binary":     {0x00, 0xff, 0xfe, 0x80, 'a'},
		"json-shape": []byte(`{"not":"re-encoded"}`),
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			client, _ := newRESTSecretsTestClient(t)
			ctx := context.Background()

			stored, err := client.StoreSecret(ctx, &service.StoreSecretRequest{SecretID: "pool/" + name, Name: "pool/" + name, Data: payload})
			require.NoError(t, err)
			assert.Equal(t, uint64(1), stored.Version)

			got, err := client.GetSecret(ctx, &service.GetSecretRequest{SecretID: "pool/" + name})
			require.NoError(t, err)
			assert.Equal(t, payload, got.Data)
		})
	}
}

// TestRESTSecretsClient_UnsupportedVerbs checks the verbs with no REST route
// return the typed unsupported error instead of hitting the network.
func TestRESTSecretsClient_UnsupportedVerbs(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		call func(c *restSecretsClient) error
	}{
		{"prefix get", func(c *restSecretsClient) error {
			_, err := c.GetSecretsByPrefix(ctx, &service.GetSecretsByPrefixRequest{Prefix: "a/"})
			return err
		}},
		{"watch", func(c *restSecretsClient) error {
			_, err := c.WatchSecretsByPrefix(ctx, &service.WatchSecretsRequest{})
			return err
		}},
		{"permanent delete", func(c *restSecretsClient) error {
			_, err := c.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: "a", PermanentlyDelete: true})
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, backend := newRESTSecretsTestClient(t)
			err := tc.call(client)
			var unsupported *UnsupportedTransportError
			require.True(t, errors.As(err, &unsupported), "got %v", err)
			assert.Empty(t, backend.requests)
		})
	}
}

// TestRESTSecretsClient_InvalidRequestRejected checks an empty secret ID is
// refused before any request is sent.
func TestRESTSecretsClient_InvalidRequestRejected(t *testing.T) {
	client, backend := newRESTSecretsTestClient(t)
	_, err := client.GetSecret(context.Background(), &service.GetSecretRequest{SecretID: ""})
	require.Error(t, err)
	assert.Empty(t, backend.requests)
}
