package orchestration

import (
	"bytes"
	"context"
	"fmt"
)

// MeasurementLogStore persists a node's measurement log so that a restarted
// node agent keeps the launches it measured before the restart. go-quicbaas
// keeps it in the Verifier's go-qrdb store (service.NewMeasurementLogStore).
type MeasurementLogStore interface {
	// LoadMeasurementLog returns the stored log, or nil when none is stored.
	LoadMeasurementLog(ctx context.Context) ([]MeasurementLogEntry, error)

	// SaveMeasurementLog replaces the stored log with entries.
	SaveMeasurementLog(ctx context.Context, entries []MeasurementLogEntry) error
}

// WithMeasurementLogStore persists the node measurement log in store. The
// log is written after every measured launch and before the launch returns;
// a failed write refuses the launch. The stored log is restored on the
// first measured launch only if it still replays to the PCR values the TPM
// holds: a log from an earlier boot (the PCRs were reset) or one left
// behind by a failed write is dropped, and the log starts again from an
// anchor read from the TPM.
func WithMeasurementLogStore(store MeasurementLogStore) TPMAttestingOption {
	return func(r *TPMAttestingRuntime) { r.logStore = store }
}

// readRegister reads the current value of the PCR at pcr in bank.
func (r *TPMAttestingRuntime) readRegister(pcr int, bank string) ([]byte, error) {
	banks, err := r.tpm.ReadPCRs([]uint{uint(pcr)})
	if err != nil {
		return nil, err
	}
	values, err := flattenPCRBanks(banks, bank)
	if err != nil {
		return nil, err
	}
	value, ok := values[uint32(pcr)]
	if !ok {
		return nil, &PCRBankUnavailableError{Requested: bank}
	}
	return value, nil
}

// restoreMeasurementLog adopts the stored log once, if it replays to the
// TPM's current PCR values. A load error is returned and retried by the
// next launch; a log that does not verify or replay is dropped.
func (r *TPMAttestingRuntime) restoreMeasurementLog(ctx context.Context) error {
	if r.logStore == nil {
		return nil
	}
	r.logRestore.Lock()
	defer r.logRestore.Unlock()
	if r.logLoaded {
		return nil
	}
	entries, err := r.logStore.LoadMeasurementLog(ctx)
	if err != nil {
		return fmt.Errorf("load the measurement log: %w", err)
	}
	if len(entries) > 0 {
		if reason := r.replaysCurrentPCRs(entries); reason != nil {
			r.logger.Warnf("measured launch: stored measurement log dropped (%v); the log starts again from the TPM's PCRs", reason)
		} else if !r.log.adopt(entries) {
			r.logger.Warnf("measured launch: stored measurement log not restored: the node log already holds entries")
		}
	}
	r.logLoaded = true
	return nil
}

// replaysCurrentPCRs returns nil when entries verify and replay, register
// by register, to the values the TPM holds now.
func (r *TPMAttestingRuntime) replaysCurrentPCRs(entries []MeasurementLogEntry) error {
	if err := VerifyMeasurementLog(entries); err != nil {
		return err
	}
	type register struct {
		pcr  int
		bank string
	}
	seen := map[register]bool{}
	for i := range entries {
		reg := register{entries[i].PCR, entries[i].Bank}
		if seen[reg] {
			continue
		}
		seen[reg] = true
		want, err := ReplayMeasurementLog(entries, reg.pcr, reg.bank)
		if err != nil {
			return err
		}
		got, err := r.readRegister(reg.pcr, reg.bank)
		if err != nil {
			return err
		}
		if !bytes.Equal(want, got) {
			return fmt.Errorf("PCR %d (%s) does not replay: a reboot or an unsaved launch", reg.pcr, reg.bank)
		}
	}
	return nil
}

// saveMeasurementLog writes the whole log. Saves are serialized and each
// takes its snapshot inside the lock, so a later save never writes an
// older log.
func (r *TPMAttestingRuntime) saveMeasurementLog(ctx context.Context) error {
	if r.logStore == nil {
		return nil
	}
	r.logSave.Lock()
	defer r.logSave.Unlock()
	if err := r.logStore.SaveMeasurementLog(ctx, r.log.Entries()); err != nil {
		return fmt.Errorf("save the measurement log: %w", err)
	}
	return nil
}

// adopt replaces an empty log with entries, which the caller has verified.
// It reports false, and changes nothing, when the log already has entries.
func (l *MeasurementLog) adopt(entries []MeasurementLogEntry) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) > 0 {
		return false
	}
	l.entries = make([]MeasurementLogEntry, len(entries))
	for i, e := range entries {
		e.Measurement = append([]byte(nil), e.Measurement...)
		e.Base = append([]byte(nil), e.Base...)
		l.entries[i] = e
	}
	return true
}
