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

package sealedwindow

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// loggerAuditSink is the canonical AuditSink: a structured daemon-log event
// per sealed-window authentication bypass. The structured log is the correct
// trail for this event class — while the barrier is sealed the IAM audit
// store itself sits behind the sealed data plane, so a database-backed sink
// cannot exist yet. The emitted shape mirrors the audited ceremony bypass on
// go-xkms's QUIC transport (transport + operation + fixed reason).
type loggerAuditSink struct {
	log *logger.Logger
}

// NewLoggerAuditSink returns an AuditSink that emits one structured Warn
// event per sealed-window authentication bypass on log. A nil log yields a
// nil sink (no auditing), which AuthBypass treats as a no-op.
func NewLoggerAuditSink(log *logger.Logger) AuditSink {
	if log == nil {
		return nil
	}
	return &loggerAuditSink{log: log}
}

// SealedWindowAuthBypass implements AuditSink.
func (s *loggerAuditSink) SealedWindowAuthBypass(transport, operation string) {
	s.log.WithFields(map[string]interface{}{
		"transport": transport,
		"operation": operation,
		"reason":    "sealed-window allowlist",
	}).Warn("sealed-window authentication bypass")
}
