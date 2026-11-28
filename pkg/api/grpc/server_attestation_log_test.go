package grpc

import (
	"bytes"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestMeasurementLogToProto proves the node measurement log crosses the
// gRPC AttestContainerResponse entry for entry.
func TestMeasurementLogToProto(t *testing.T) {
	if got := measurementLogToProto(nil); got != nil {
		t.Fatalf("an empty log maps to nil, got %v", got)
	}
	at := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	log := []orchestration.MeasurementLogEntry{
		{Sequence: 0, Kind: "anchor", PCR: 11, Bank: "sha256", Base: []byte{1}},
		{Sequence: 1, Kind: "launch", Image: "alpine@sha256:aa", ImageDigest: "sha256:aa", TrustLabel: "prod",
			AllowUnsigned: "grant-1", PCR: 11, Bank: "sha256", Measurement: []byte{2, 3}, RecordedAt: at},
	}
	got := measurementLogToProto(log)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].GetKind() != "anchor" || !bytes.Equal(got[0].GetBase(), []byte{1}) || got[0].GetRecordedAt() != nil {
		t.Fatalf("anchor = %+v", got[0])
	}
	e := got[1]
	if e.GetSequence() != 1 || e.GetImage() != "alpine@sha256:aa" || e.GetAllowUnsigned() != "grant-1" ||
		e.GetPcr() != 11 || e.GetBank() != "sha256" || !bytes.Equal(e.GetMeasurement(), []byte{2, 3}) ||
		!e.GetRecordedAt().AsTime().Equal(at) {
		t.Fatalf("launch = %+v", e)
	}
}
