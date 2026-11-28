// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import (
	"context"
	"reflect"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// qrdbAuditSink is a go-qrdb AuditClient: it must satisfy the re-export
// with no adapter, because the re-export is go-qrdb's own interface.
type qrdbAuditSink struct{ emitted []qrdbtransport.AuditEvent }

func (s *qrdbAuditSink) EmitAudit(_ context.Context, ev qrdbtransport.AuditEvent) error {
	s.emitted = append(s.emitted, ev)
	return nil
}

func (s *qrdbAuditSink) QueryAudit(context.Context, qrdbtransport.AuditFilter) (*qrdbtransport.AuditPage, error) {
	return &qrdbtransport.AuditPage{}, nil
}

// TestQRDBAuditClient_IsGoQRDBAuditService pins SPEC row audit-logging
// (#1659): the SDK's audit service is a re-export of go-qrdb's AuditClient,
// so the audit sink behind it is go-qrdb's, not a parallel QuicBaaS type.
func TestQRDBAuditClient_IsGoQRDBAuditService(t *testing.T) {
	pairs := []struct {
		name     string
		reexport reflect.Type
		qrdb     reflect.Type
	}{
		{"AuditClient", reflect.TypeOf((*QRDBAuditClient)(nil)).Elem(), reflect.TypeOf((*qrdbtransport.AuditClient)(nil)).Elem()},
		{"AuditEvent", reflect.TypeOf(QRDBAuditEvent{}), reflect.TypeOf(qrdbtransport.AuditEvent{})},
		{"AuditFilter", reflect.TypeOf(QRDBAuditFilter{}), reflect.TypeOf(qrdbtransport.AuditFilter{})},
		{"AuditPage", reflect.TypeOf(QRDBAuditPage{}), reflect.TypeOf(qrdbtransport.AuditPage{})},
	}
	for _, p := range pairs {
		if p.reexport != p.qrdb {
			t.Errorf("QRDB%s is %v, want go-qrdb's %v", p.name, p.reexport, p.qrdb)
		}
	}

	sink := &qrdbAuditSink{}
	var client QRDBAuditClient = sink
	if err := client.EmitAudit(context.Background(), QRDBAuditEvent{}); err != nil {
		t.Fatalf("EmitAudit: %v", err)
	}
	if len(sink.emitted) != 1 {
		t.Fatalf("go-qrdb sink got %d events, want 1", len(sink.emitted))
	}
}
