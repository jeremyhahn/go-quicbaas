package orchestration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

// memoryLogStore is a MeasurementLogStore that keeps the log as JSON, as a
// real store would, and can fail its saves.
type memoryLogStore struct {
	mu      sync.Mutex
	record  []byte
	saveErr error
	saves   int
}

func (s *memoryLogStore) LoadMeasurementLog(context.Context) ([]MeasurementLogEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil {
		return nil, nil
	}
	var entries []MeasurementLogEntry
	return entries, json.Unmarshal(s.record, &entries)
}

func (s *memoryLogStore) SaveMeasurementLog(_ context.Context, entries []MeasurementLogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saves++
	record, err := json.Marshal(entries)
	s.record = record
	return err
}

func (s *memoryLogStore) stored(t *testing.T) []MeasurementLogEntry {
	t.Helper()
	entries, err := s.LoadMeasurementLog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// nopAppraiser satisfies NewTPMAttestingRuntime; these tests only launch.
type nopAppraiser struct{ HostAppraiser }

func newStoredLogRuntime(t *testing.T, tpm *fakeHostTPM, store MeasurementLogStore) (*TPMAttestingRuntime, *recordingRuntime) {
	t.Helper()
	base := &recordingRuntime{}
	rt, err := NewTPMAttestingRuntime(base, tpm, nopAppraiser{}, newContainerTestLogger(t),
		WithImageVerifier(acceptingImageVerifier{}), WithProvenanceVerifier(acceptingProvenanceVerifier{}),
		WithClusterManifest(listingManifest{}), WithMeasurementLogStore(store))
	if err != nil {
		t.Fatal(err)
	}
	return rt, base
}

// replaysTPM fails the test unless entries replay to the PCR tpm holds.
func replaysTPM(t *testing.T, entries []MeasurementLogEntry, tpm *fakeHostTPM) {
	t.Helper()
	got, err := ReplayMeasurementLog(entries, MeasurementPCRDefault, DefaultMeasurementBank)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if want := tpm.pcr(uint32(MeasurementPCRDefault)); !bytes.Equal(got, want) {
		t.Fatalf("the log replays to %x, the TPM holds %x", got, want)
	}
}

func TestMeasurementLogStore_RestartKeepsTheLog(t *testing.T) {
	ctx := context.Background()
	tpm := newFakeHostTPM()
	store := &memoryLogStore{}
	rt, _ := newStoredLogRuntime(t, tpm, store)
	for _, label := range []string{"a", "b"} {
		if _, err := rt.Run(ctx, measuredTestSpec(label)); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	if stored := store.stored(t); len(stored) != 3 {
		t.Fatalf("the store holds %d entries, want the anchor and two launches", len(stored))
	}

	// A restarted agent on the same boot continues the stored log.
	restarted, _ := newStoredLogRuntime(t, tpm, store)
	if _, err := restarted.Run(ctx, measuredTestSpec("c")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	entries := restarted.log.Entries()
	if len(entries) != 4 || entries[3].Sequence != 3 || entries[3].TrustLabel != "c" {
		t.Fatalf("the restarted log = %+v", entries)
	}
	replaysTPM(t, entries, tpm)
	replaysTPM(t, store.stored(t), tpm)
}

func TestMeasurementLogStore_AttestRestoresBeforeAnyLaunch(t *testing.T) {
	tpm := newFakeHostTPM()
	store := &memoryLogStore{}
	rt, _ := newStoredLogRuntime(t, tpm, store)
	if _, err := rt.Run(context.Background(), measuredTestSpec("a")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	restarted, _ := newStoredLogRuntime(t, tpm, store)
	if err := restarted.restoreMeasurementLog(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(restarted.log.Entries()); n != 2 {
		t.Fatalf("restored %d entries, want 2", n)
	}
}

func TestMeasurementLogStore_RebootDropsTheLog(t *testing.T) {
	ctx := context.Background()
	store := &memoryLogStore{}
	rt, _ := newStoredLogRuntime(t, newFakeHostTPM(), store)
	if _, err := rt.Run(ctx, measuredTestSpec("a")); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// After a reboot the PCRs are reset: the stored log is from another boot.
	rebooted := newFakeHostTPM()
	rt, _ = newStoredLogRuntime(t, rebooted, store)
	if _, err := rt.Run(ctx, measuredTestSpec("b")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	entries := rt.log.Entries()
	if len(entries) != 2 || entries[0].Kind != MeasurementLogAnchor || entries[1].TrustLabel != "b" {
		t.Fatalf("a log from an earlier boot must be dropped: %+v", entries)
	}
	replaysTPM(t, store.stored(t), rebooted)
}

func TestMeasurementLogStore_FailedSaveRefusesTheLaunch(t *testing.T) {
	ctx := context.Background()
	tpm := newFakeHostTPM()
	store := &memoryLogStore{}
	rt, base := newStoredLogRuntime(t, tpm, store)
	if _, err := rt.Run(ctx, measuredTestSpec("a")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	store.saveErr = errors.New("qrdb unavailable")
	if _, err := rt.Run(ctx, measuredTestSpec("b")); err == nil {
		t.Fatal("a launch whose log entry was not stored must be refused")
	}
	if n := len(base.runSpecs); n != 1 {
		t.Fatalf("%d containers started, want only the first", n)
	}
	// The in-memory log still matches the PCR, so evidence replays.
	replaysTPM(t, rt.log.Entries(), tpm)

	// The stored log is behind the PCR; a restart drops it and anchors anew.
	store.saveErr = nil
	restarted, _ := newStoredLogRuntime(t, tpm, store)
	if _, err := restarted.Run(ctx, measuredTestSpec("c")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	entries := restarted.log.Entries()
	if len(entries) != 2 || entries[0].Kind != MeasurementLogAnchor {
		t.Fatalf("a stored log behind the PCR must be dropped: %+v", entries)
	}
	replaysTPM(t, entries, tpm)
}

func TestMeasurementLogStore_TamperedLogIsDropped(t *testing.T) {
	ctx := context.Background()
	tpm := newFakeHostTPM()
	store := &memoryLogStore{}
	rt, _ := newStoredLogRuntime(t, tpm, store)
	if _, err := rt.Run(ctx, measuredTestSpec("a")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	entries := store.stored(t)
	entries[1].TrustLabel = "forged"
	store.record, _ = json.Marshal(entries)

	restarted, _ := newStoredLogRuntime(t, tpm, store)
	if err := restarted.restoreMeasurementLog(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(restarted.log.Entries()); n != 0 {
		t.Fatalf("a stored log whose chain does not verify must be dropped, restored %d entries", n)
	}
}
