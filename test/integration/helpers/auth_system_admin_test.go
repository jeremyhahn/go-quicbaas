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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newAuthorizationRecorder starts a server that records the Authorization
// header of every request it receives.
func newAuthorizationRecorder(t *testing.T) (*httptest.Server, chan string) {
	t.Helper()
	headers := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, headers
}

// plainHTTPClient is a client for the recorder's plain HTTP server, which
// needs no integration CA.
func plainHTTPClient() *http.Client {
	return &http.Client{Transport: http.DefaultTransport, Timeout: 5 * time.Second}
}

// Admin-resource routes reject a request without a bearer, so every request
// the system admin client sends must carry the IAM login's token.
func TestNewSystemAdminHTTPClient_SendsLoginBearer(t *testing.T) {
	login, paths := newPIVLoginServer(t, http.StatusOK,
		`{"access_token":"admin-token","token_type":"Bearer","expires_in":900}`)
	certPath, keyPath, caPath := pivLoginFiles(t, login)

	client, err := newSystemAdminHTTPClient(context.Background(), plainHTTPClient(), login.URL, certPath, keyPath, caPath)
	if err != nil {
		t.Fatalf("newSystemAdminHTTPClient: %v", err)
	}
	if got := <-paths; got != pivLoginPath {
		t.Errorf("login path = %q, want %q", got, pivLoginPath)
	}

	target, headers := newAuthorizationRecorder(t)
	resp, err := client.Get(target.URL + "/api/v1/cache/stats")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if got := <-headers; got != "Bearer admin-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer admin-token")
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
