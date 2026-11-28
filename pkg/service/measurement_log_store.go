package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

const measurementLogSegment = "measurement-log/"

// ErrMeasurementLogStoreConfig is returned by NewMeasurementLogStore without
// a store or a node name.
var ErrMeasurementLogStoreConfig = stderrors.New("measurement log store: an attestation store and a node name are required")

// MeasurementLogStore keeps a node's measurement log in the Verifier's
// go-qrdb store, one record per node. The log belongs to the node's TPM,
// not to a tenant, so the record is keyed by node alone. The runtime keeps
// it only while it replays to the TPM's PCRs (a log from an earlier boot is
// dropped), so the record holds the current boot's log.
type MeasurementLogStore struct {
	store AttestationStore
	key   string
}

var _ orchestration.MeasurementLogStore = (*MeasurementLogStore)(nil)

// NewMeasurementLogStore returns the measurement log store of node over
// store.
func NewMeasurementLogStore(store AttestationStore, node string) (*MeasurementLogStore, error) {
	if store == nil || node == "" {
		return nil, ErrMeasurementLogStoreConfig
	}
	return &MeasurementLogStore{store: store, key: attestationKeyPrefix + measurementLogSegment + node}, nil
}

// LoadMeasurementLog implements orchestration.MeasurementLogStore.
func (s *MeasurementLogStore) LoadMeasurementLog(ctx context.Context) ([]orchestration.MeasurementLogEntry, error) {
	record, found, err := s.store.Get(ctx, s.key)
	if err != nil || !found {
		return nil, err
	}
	var entries []orchestration.MeasurementLogEntry
	if err := json.Unmarshal(record, &entries); err != nil {
		return nil, fmt.Errorf("measurement log: decode stored record: %w", err)
	}
	return entries, nil
}

// SaveMeasurementLog implements orchestration.MeasurementLogStore.
func (s *MeasurementLogStore) SaveMeasurementLog(ctx context.Context, entries []orchestration.MeasurementLogEntry) error {
	record, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("measurement log: encode: %w", err)
	}
	return s.store.Put(ctx, s.key, record)
}
