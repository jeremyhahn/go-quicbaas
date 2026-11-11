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

package scheduler

import (
	"net/http"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// operatorTransport sends every request as the tenant operator, the
// principal whose built-in Operator role holds the scheduler job
// permissions. The REST client constructors of this package take no test and
// return no error, so the authenticated client is built on the first request
// and an enrolment failure comes back as that request's error.
type operatorTransport struct {
	timeout time.Duration

	mu     sync.Mutex
	client *http.Client
}

// RoundTrip forwards req through the tenant operator's HTTP client.
func (o *operatorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	client, err := o.operatorClient(req)
	if err != nil {
		return nil, err
	}
	return client.Transport.RoundTrip(req)
}

// operatorClient returns the tenant operator's HTTP client, building it on
// first use. A failed build is not kept, so the next request tries again.
func (o *operatorTransport) operatorClient(req *http.Request) (*http.Client, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.client != nil {
		return o.client, nil
	}
	client, err := helpers.TenantOperatorHTTPClient(req.Context(), o.timeout)
	if err != nil {
		return nil, err
	}
	o.client = client
	return client, nil
}

// newOperatorHTTPClient returns an HTTP client whose requests carry the
// tenant operator's bearer.
func newOperatorHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &operatorTransport{timeout: timeout}}
}
