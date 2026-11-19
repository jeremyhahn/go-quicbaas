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

//go:build integration
// +build integration

package iam_test

import (
	"context"
	"io"
	"net/http"
)

// rawDo sends req with tc's HTTP client and, when tc is a view with a
// principal (asSystemAdmin, asTenantAdmin), the view's bearer, the way
// doHTTPRequest does for the JSON helpers. It is for tests that build their
// own requests and read the raw response. On the unauthenticated cluster it
// is tc.client.Do.
func (tc *testCluster) rawDo(req *http.Request) (*http.Response, error) {
	if tc.bearer != nil {
		ctx, cancel := context.WithTimeout(req.Context(), defaultTimeout)
		defer cancel()
		token, err := tc.bearer(ctx)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return tc.client.Do(req)
}

// rawGet is http.Client.Get through rawDo.
func (tc *testCluster) rawGet(rawURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return tc.rawDo(req)
}

// rawPost is http.Client.Post through rawDo.
func (tc *testCluster) rawPost(rawURL, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return tc.rawDo(req)
}
