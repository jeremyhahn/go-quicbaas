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

//go:build integration

package helpers

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// plainHTTPClient is a client that needs no integration CA, for tests whose
// login is refused before any request is sent.
func plainHTTPClient() *http.Client {
	return &http.Client{Transport: http.DefaultTransport, Timeout: 5 * time.Second}
}

// newBearerIssuingNode starts a TLS server standing in for one cluster node:
// its PIV login issues token, and every other request records the
// Authorization header it carried.
func newBearerIssuingNode(t *testing.T, token string) (*httptest.Server, chan string) {
	t.Helper()
	headers := make(chan string, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pivLoginPath {
			headers <- r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := `{"access_token":"` + token + `","token_type":"Bearer","expires_in":900}`
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write login response: %v", err)
		}
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequestClientCert}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, headers
}

// Admin-resource routes reject a request without a bearer, so every request
// the system admin client sends must carry the IAM login's token.
func TestNewSystemAdminHTTPClient_SendsEachNodeItsOwnBearer(t *testing.T) {
	node1, headers1 := newBearerIssuingNode(t, "node1-token")
	node2, headers2 := newBearerIssuingNode(t, "node2-token")
	certPath, keyPath, caPath := pivLoginFiles(t, node1)

	base := node1.Client().Transport
	client, err := newSystemAdminHTTPClient(context.Background(), &http.Client{Transport: base, Timeout: 5 * time.Second},
		node1.URL, certPath, keyPath, caPath)
	if err != nil {
		t.Fatalf("newSystemAdminHTTPClient: %v", err)
	}

	for _, node := range []struct {
		server  *httptest.Server
		headers chan string
		want    string
	}{
		{server: node1, headers: headers1, want: "Bearer node1-token"},
		{server: node2, headers: headers2, want: "Bearer node2-token"},
	} {
		resp, err := client.Get(node.server.URL + "/api/v1/debug/route/users/1")
		if err != nil {
			t.Fatalf("GET %s: %v", node.server.URL, err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("close body: %v", err)
		}
		if got := <-node.headers; got != node.want {
			t.Errorf("Authorization at %s = %q, want %q", node.server.URL, got, node.want)
		}
	}
}

func TestNewSystemAdminHTTPClient_RejectedLoginFails(t *testing.T) {
	login, paths := newPIVLoginServer(t, http.StatusUnauthorized, `{"error":"unauthorized"}`)
	certPath, keyPath, caPath := pivLoginFiles(t, login)

	_, err := newSystemAdminHTTPClient(context.Background(), plainHTTPClient(), login.URL, certPath, keyPath, caPath)
	<-paths
	var authErr *PrincipalAuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %v, want *PrincipalAuthError", err)
	}
	if authErr.Principal != systemAdminPrincipal || authErr.Step != "login" {
		t.Errorf("principal, step = %q, %q; want %q, login", authErr.Principal, authErr.Step, systemAdminPrincipal)
	}
	var badStatus *AuthBadStatusError
	if !errors.As(err, &badStatus) || badStatus.Status != http.StatusUnauthorized {
		t.Errorf("cause = %v, want *AuthBadStatusError with status 401", err)
	}
}

func TestNewSystemAdminGRPCCredentials_CarriesLoginBearer(t *testing.T) {
	login, paths := newPIVLoginServer(t, http.StatusOK,
		`{"access_token":"admin-token","token_type":"Bearer","expires_in":900}`)
	certPath, keyPath, caPath := pivLoginFiles(t, login)

	creds, err := newSystemAdminGRPCCredentials(context.Background(), login.URL, certPath, keyPath, caPath)
	if err != nil {
		t.Fatalf("newSystemAdminGRPCCredentials: %v", err)
	}
	<-paths
	metadata, err := creds.GetRequestMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetRequestMetadata: %v", err)
	}
	if got := metadata["authorization"]; got != "Bearer admin-token" {
		t.Errorf("authorization = %q, want %q", got, "Bearer admin-token")
	}
}

func TestNewSystemAdminGRPCCredentials_RejectedLoginFails(t *testing.T) {
	login, paths := newPIVLoginServer(t, http.StatusForbidden, `{"error":"forbidden"}`)
	certPath, keyPath, caPath := pivLoginFiles(t, login)

	_, err := newSystemAdminGRPCCredentials(context.Background(), login.URL, certPath, keyPath, caPath)
	<-paths
	var badStatus *AuthBadStatusError
	if !errors.As(err, &badStatus) || badStatus.Status != http.StatusForbidden {
		t.Errorf("error = %v, want *AuthBadStatusError with status 403", err)
	}
}
