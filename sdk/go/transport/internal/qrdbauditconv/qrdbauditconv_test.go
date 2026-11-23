package qrdbauditconv

import (
	"reflect"
	"testing"
	"time"

	qrdbaudit "github.com/jeremyhahn/go-qrdb/pkg/audit"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

func TestPageToTransport_Nil(t *testing.T) {
	if got := PageToTransport(nil); got != nil {
		t.Fatalf("PageToTransport(nil) = %+v, want nil", got)
	}
}

func TestPageToTransport_EmptyEventsNonNil(t *testing.T) {
	got := PageToTransport(&qrdbaudit.Page{})
	if got == nil || got.Events == nil || len(got.Events) != 0 {
		t.Fatalf("PageToTransport(empty) = %+v, want a non-nil empty Events slice", got)
	}
}

func TestPageToTransport_CopiesEveryField(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	in := &qrdbaudit.Page{
		Events: []qrdbaudit.AuditEvent{{
			Timestamp:  ts,
			Actor:      "alice",
			TenantID:   "t1",
			Role:       "admin",
			Resource:   "barrier",
			Action:     "unseal",
			Outcome:    "success",
			Attributes: map[string]any{"k": "v"},
			ClientIP:   "10.0.0.1",
			RequestID:  "r1",
			PrevHash:   "h0",
		}},
		Next:  "n",
		Total: 3,
	}
	want := &qrdbtransport.AuditPage{
		Events: []qrdbtransport.AuditEvent{{
			Timestamp:  ts,
			Actor:      "alice",
			TenantID:   "t1",
			Role:       "admin",
			Resource:   "barrier",
			Action:     "unseal",
			Outcome:    "success",
			Attributes: map[string]any{"k": "v"},
			ClientIP:   "10.0.0.1",
			RequestID:  "r1",
			PrevHash:   "h0",
		}},
		Next:  "n",
		Total: 3,
	}
	got := PageToTransport(in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PageToTransport:\n got %+v\nwant %+v", got, want)
	}

	got.Events[0].Attributes["k"] = "changed"
	if in.Events[0].Attributes["k"] != "v" {
		t.Fatalf("mutating the result's Attributes changed the input: %v", in.Events[0].Attributes)
	}
}

func TestEventToTransport_NilAttributes(t *testing.T) {
	if got := EventToTransport(qrdbaudit.AuditEvent{Actor: "a"}); got.Attributes != nil || got.Actor != "a" {
		t.Fatalf("EventToTransport = %+v", got)
	}
}
