//go:build rest

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

package rest

// SetEventIngestURL records the gRPC URL the daemon must dial for the
// PushEvents bidi-stream. The mTLS agent-register handler returns this
// URL in its response under the "event_ingest_addr" field; the daemon
// then uses it to wire its EventForwarder.
//
// Why this is required: the REST TLS listener intentionally advertises
// only http/1.1 in NextProtos to avoid a Go HTTP/2 record-MAC bug
// triggered when clients downgrade. gRPC requires h2 ALPN, so a daemon
// that dials the REST host for PushEvents fails the TLS handshake with
// "remote error: tls: no application protocol". This setter is the
// single contract by which the daemon learns the correct gRPC dial
// target (host + port) — no environment variables, no CLI flags, no
// hostname guessing.
//
// Pass an empty string when no gRPC listener is exposed; the handler
// will then omit "event_ingest_addr" from the response and the daemon
// disables event forwarding. Wired from pkg/server.lifecycle_rest.go
// once the gRPC listener is configured.
//
// Safe to call before or after Start. Concurrent reads from the
// register handler take the same RWMutex used by other server setters.
func (s *Server) SetEventIngestURL(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventIngestURL = url
}

// eventIngestAddr returns the configured gRPC ingest URL under the
// server's read lock so a SetEventIngestURL call from a different
// goroutine cannot race the handler reading it.
func (s *Server) eventIngestAddr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.eventIngestURL
}
