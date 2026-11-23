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

// Package qrdbauditconv converts go-qrdb's server-side audit types into its
// SDK transport types. It mirrors go-qrdb's own conversion, which is internal
// to that module and cannot be imported here.
package qrdbauditconv

import (
	"maps"

	qrdbaudit "github.com/jeremyhahn/go-qrdb/pkg/audit"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// EventToTransport converts one go-qrdb audit event, copying its attributes.
func EventToTransport(e qrdbaudit.AuditEvent) qrdbtransport.AuditEvent {
	return qrdbtransport.AuditEvent{
		Timestamp:  e.Timestamp,
		Actor:      e.Actor,
		TenantID:   e.TenantID,
		Role:       e.Role,
		Resource:   e.Resource,
		Action:     e.Action,
		Outcome:    e.Outcome,
		Attributes: maps.Clone(e.Attributes),
		ClientIP:   e.ClientIP,
		RequestID:  e.RequestID,
		PrevHash:   e.PrevHash,
	}
}

// PageToTransport converts a go-qrdb audit page. A nil page gives nil; an
// empty page gives a non-nil empty Events slice.
func PageToTransport(p *qrdbaudit.Page) *qrdbtransport.AuditPage {
	if p == nil {
		return nil
	}
	events := make([]qrdbtransport.AuditEvent, 0, len(p.Events))
	for _, e := range p.Events {
		events = append(events, EventToTransport(e))
	}
	return &qrdbtransport.AuditPage{Events: events, Next: p.Next, Total: p.Total}
}
