package embedded

import (
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestAttestContainerWireResponse_MeasurementLog proves the embedded
// transport carries the quoted bank and the node measurement log.
func TestAttestContainerWireResponse_MeasurementLog(t *testing.T) {
	at := time.Date(2026, 11, 14, 12, 0, 0, 0, time.FixedZone("x", 3600))
	resp := attestContainerWireResponse(&service.AttestContainerResponse{
		Attested: true,
		PCRBank:  "sha256",
		MeasurementLog: []orchestration.MeasurementLogEntry{
			{Sequence: 0, Kind: "anchor", PCR: 11, Bank: "sha256"},
			{Sequence: 1, Kind: "launch", Image: "alpine", AllowUnsigned: "grant-1", PCR: 11, RecordedAt: at},
		},
	})
	if resp.PCRBank != "sha256" {
		t.Fatalf("PCRBank = %q", resp.PCRBank)
	}
	if len(resp.MeasurementLog) != 2 {
		t.Fatalf("MeasurementLog = %+v", resp.MeasurementLog)
	}
	if e := resp.MeasurementLog[0]; e.Kind != "anchor" || e.RecordedAt != "" {
		t.Fatalf("anchor = %+v", e)
	}
	e := resp.MeasurementLog[1]
	if e.Sequence != 1 || e.Image != "alpine" || e.AllowUnsigned != "grant-1" || e.PCR != 11 || e.RecordedAt != "2026-11-14T11:00:00Z" {
		t.Fatalf("launch = %+v", e)
	}
}
