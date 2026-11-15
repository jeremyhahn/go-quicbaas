package service

import (
	"context"
	"crypto/sha256"
	"sync"
	"testing"

	xkmstpm2 "github.com/jeremyhahn/go-xkms/pkg/tpm2"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// extendingTPM is a HostTPM whose sha256 PCRs extend as a TPM's do.
type extendingTPM struct {
	orchestration.HostTPM
	mu   sync.Mutex
	pcrs map[int][]byte
}

func (f *extendingTPM) value(pcr int) []byte {
	if v, ok := f.pcrs[pcr]; ok {
		return v
	}
	return make([]byte, sha256.Size)
}

func (f *extendingTPM) ReadPCRs(list []uint) ([]xkmstpm2.PCRBank, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bank := xkmstpm2.PCRBank{Algorithm: "sha256"}
	for _, i := range list {
		bank.PCRs = append(bank.PCRs, xkmstpm2.PCR{ID: int32(i), Value: append([]byte(nil), f.value(int(i))...)})
	}
	return []xkmstpm2.PCRBank{bank}, nil
}

func (f *extendingTPM) ExtendPCR(pcr int, _ string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	digest := sha256.Sum256(data)
	next := sha256.Sum256(append(append([]byte(nil), f.value(pcr)...), digest[:]...))
	if f.pcrs == nil {
		f.pcrs = map[int][]byte{}
	}
	f.pcrs[pcr] = next[:]
	return nil
}

// startingRuntime is a ContainerRuntime that starts every container.
type startingRuntime struct{ orchestration.ContainerRuntime }

func (startingRuntime) Run(context.Context, *orchestration.ContainerSpec) (string, error) {
	return "c1", nil
}

// TestMeasuredLaunchRuntime_PersistsTheLog proves the measured-launch
// runtime writes the node measurement log, the audited allow-unsigned grant
// included, to the Verifier's go-qrdb store and restores it on restart.
func TestMeasuredLaunchRuntime_PersistsTheLog(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	store := attestationtest.NewMemoryStore()
	tpm := &extendingTPM{}
	cfg := MeasuredLaunchConfig{
		TPM:      tpm,
		Runtime:  startingRuntime{},
		Unsigned: orchestration.UnsignedImageGrants{"dev": "TICKET-7"},
		Node:     "node-a",
	}
	spec := &orchestration.ContainerSpec{
		Image:           "registry.example.com/team/app@" + imageTestDigest,
		TrustLevel:      orchestration.TrustLevelMeasured,
		TrustLabel:      "dev",
		ReferenceValues: "golden",
	}
	rt, err := NewMeasuredLaunchRuntime(store, cfg, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Run(ctx, spec); err != nil {
		t.Fatalf("Run: %v", err)
	}

	logs, err := NewMeasurementLogStore(store, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := logs.LoadMeasurementLog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[1].AllowUnsigned != "TICKET-7" {
		t.Fatalf("stored log = %+v", stored)
	}
	if err := orchestration.VerifyMeasurementLog(stored); err != nil {
		t.Fatalf("the stored log must verify after the round trip: %v", err)
	}

	restarted, err := NewMeasuredLaunchRuntime(store, cfg, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Run(ctx, spec); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stored, _ = logs.LoadMeasurementLog(ctx); len(stored) != 3 {
		t.Fatalf("a restart on the same boot continues the log; stored %d entries", len(stored))
	}
	other, _ := NewMeasurementLogStore(store, "node-b")
	if entries, err := other.LoadMeasurementLog(ctx); err != nil || entries != nil {
		t.Fatalf("each node has its own record: %v, %v", entries, err)
	}
}

func TestNewMeasurementLogStore_Config(t *testing.T) {
	if _, err := NewMeasurementLogStore(nil, "n"); err != ErrMeasurementLogStoreConfig {
		t.Fatalf("nil store: %v", err)
	}
	if _, err := NewMeasurementLogStore(attestationtest.NewMemoryStore(), ""); err != ErrMeasurementLogStoreConfig {
		t.Fatalf("no node: %v", err)
	}
}
