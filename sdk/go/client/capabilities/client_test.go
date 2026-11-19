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

package capabilities

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

type fakeTransport struct {
	service, method string
	resp            interface{}
	err             error
}

func (f *fakeTransport) Request(_ context.Context, service, method string, _ interface{}) (interface{}, error) {
	f.service, f.method = service, method
	return f.resp, f.err
}

func (f *fakeTransport) RequestStream(context.Context, string, string, interface{}) (transport.Stream, error) {
	return nil, nil
}

func (f *fakeTransport) Conn() interface{} { return nil }
func (f *fakeTransport) Close() error      { return nil }
func (f *fakeTransport) Healthy() bool     { return true }

func TestNewClient_NilTransport(t *testing.T) {
	if NewClient(nil) != nil {
		t.Fatal("NewClient(nil) returned a client")
	}
}

func TestGet_SendsCapabilitiesGet(t *testing.T) {
	ft := &fakeTransport{resp: &Report{Subsystems: map[string]Subsystem{
		"messaging": {Served: true, Configured: true},
	}}}
	report, err := NewClient(ft).Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ft.service != "quicbaas.Capabilities" || ft.method != "Get" {
		t.Errorf("request = %s/%s, want quicbaas.Capabilities/Get", ft.service, ft.method)
	}
	if !report.Available("messaging") {
		t.Errorf("messaging not available in %+v", report)
	}
}

func TestGet_DecodesJSONMap(t *testing.T) {
	ft := &fakeTransport{resp: map[string]interface{}{
		"subsystems": map[string]interface{}{
			"timeseries": map[string]interface{}{"served": false, "configured": false, "reason": "build with -tags timeseries"},
		},
		"families": []interface{}{"kv"},
	}}
	report, err := NewClient(ft).Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	sub := report.Subsystems["timeseries"]
	if sub.Served || sub.Configured || sub.Reason != "build with -tags timeseries" {
		t.Errorf("timeseries = %+v", sub)
	}
	if report.Available("timeseries") {
		t.Error("timeseries reported available")
	}
	if len(report.Families) != 1 || report.Families[0] != "kv" {
		t.Errorf("families = %v, want [kv]", report.Families)
	}
}

func TestGet_Errors(t *testing.T) {
	wantErr := errors.New("boom")
	if _, err := NewClient(&fakeTransport{err: wantErr}).Get(context.Background()); !errors.Is(err, wantErr) {
		t.Errorf("transport error = %v, want %v", err, wantErr)
	}
	if _, err := NewClient(&fakeTransport{}).Get(context.Background()); err == nil {
		t.Error("nil response: want an error")
	}
	if _, err := NewClient(&fakeTransport{resp: map[string]interface{}{"x": 1}}).Get(context.Background()); err == nil {
		t.Error("response without subsystems: want an error")
	}
	//nolint:staticcheck // a nil context is the case under test
	if _, err := NewClient(&fakeTransport{}).Get(nil); err == nil {
		t.Error("nil context: want an error")
	}
	var nilReport *Report
	if nilReport.Available("messaging") {
		t.Error("nil report reported a subsystem available")
	}
}
