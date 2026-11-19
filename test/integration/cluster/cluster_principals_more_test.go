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

package cluster

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// tenantOperatorEnrolTimeout bounds the tenant operator's enrolment and login
// on a client's first request. It is independent of that request's deadline, so
// a short poll cannot abandon an enrolment half way.
const tenantOperatorEnrolTimeout = 2 * time.Minute

// deferredTenantOperatorTransport sends every request with the tenant
// operator's bearer, enrolling the operator on the first request rather than
// when the client is built. It serves clients built with no test to fail: a
// package-level client or one built on another goroutine. A failed enrolment
// is returned as that request's error and retried on the next request.
type deferredTenantOperatorTransport struct {
	sync.Mutex
	timeout time.Duration
	base    http.RoundTripper
}

// RoundTrip forwards req through the tenant operator's transport.
func (d *deferredTenantOperatorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base, err := d.transport(req.Context())
	if err != nil {
		return nil, err
	}
	return base.RoundTrip(req)
}

// transport returns the transport every request is forwarded to, building it
// on first use. On the plain-HTTP cluster stack, which runs without IAM and so
// has no principal to enrol, it is newInsecureHTTPClient's transport as before.
func (d *deferredTenantOperatorTransport) transport(ctx context.Context) (http.RoundTripper, error) {
	d.Lock()
	defer d.Unlock()

	if d.base != nil {
		return d.base, nil
	}
	if _, err := helpers.IntegrationTLSConfig(); err != nil {
		d.base = newInsecureHTTPClient(d.timeout).Transport
		return d.base, nil
	}

	enrolCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tenantOperatorEnrolTimeout)
	defer cancel()
	client, err := helpers.TenantOperatorHTTPClient(enrolCtx, d.timeout)
	if err != nil {
		return nil, err
	}
	d.base = client.Transport
	return d.base, nil
}

// newDeferredTenantOperatorRESTClient returns an HTTP client whose every
// request carries the tenant operator's bearer, for code with no test in hand
// when the client is built. A test that has one uses
// newTenantOperatorRESTClient, which fails the test at once if the operator
// cannot be enrolled.
func newDeferredTenantOperatorRESTClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &deferredTenantOperatorTransport{timeout: timeout},
	}
}
