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

package clientip

// InvalidProxyError reports a trusted-proxy entry that is neither a CIDR nor a
// bare address.
//
// It is a typed error rather than a formatted string because the caller is
// configuration loading, which should fail startup on it: a misspelled proxy
// entry silently dropped would leave the deployment trusting fewer proxies
// than the operator wrote, and the symptom -- every client attributed to the
// load balancer -- looks like a different bug entirely.
type InvalidProxyError struct {
	// Entry is the configured value that could not be parsed, as written.
	Entry string

	// Cause is the parse failure underneath.
	Cause error
}

// Error implements the error interface.
func (e *InvalidProxyError) Error() string {
	return "clientip: trusted proxy " + e.Entry + " is neither a CIDR nor an IP address: " + e.Cause.Error()
}

// Unwrap returns the underlying parse failure.
func (e *InvalidProxyError) Unwrap() error { return e.Cause }
