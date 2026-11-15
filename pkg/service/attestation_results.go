package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	stderrors "errors"
	"fmt"
	"sort"
)

// attestationResultSegment holds the latest appraisal of each attestation
// key, the go-xkms attestation results that replace go-trusted-platform's
// attestation store and reporting cache.
const attestationResultSegment = "result/"

// AttestationResultRecord is the latest appraisal of one attesting key.
type AttestationResultRecord struct {
	// Subject is the hex SHA-256 of the attestation-key certificate (DER).
	Subject string `json:"subject"`
	// Result is the appraisal as AppraiseQuote returned it.
	Result AttestationResult `json:"result"`
}

// ErrAttestationResultsConfig is returned by NewAttestationResults without
// a store.
var ErrAttestationResultsConfig = stderrors.New("attestation results: an attestation store is required")

// AttestationResults reads the attestation results the Verifier records in
// go-qrdb: every AppraiseQuote, and every container attestation, writes the
// result under the tenant and the attestation key it appraised. They replace
// go-trusted-platform's attestation store and reporting cache.
type AttestationResults struct {
	store AttestationStore
}

// NewAttestationResults returns the attestation results kept in store, the
// same store the attestation service was built with.
func NewAttestationResults(store AttestationStore) (*AttestationResults, error) {
	if store == nil {
		return nil, ErrAttestationResultsConfig
	}
	return &AttestationResults{store: store}, nil
}

// AttestationSubject returns the subject an attestation key's results are
// recorded under: the hex SHA-256 of its leaf certificate.
func AttestationSubject(akCertificatePEM []byte) string {
	der := akCertificatePEM
	if block, _ := pem.Decode(akCertificatePEM); block != nil {
		der = block.Bytes
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// Latest returns the latest result recorded for the attestation key whose
// certificate is akCertificatePEM; found is false when none is recorded.
func (r *AttestationResults) Latest(ctx context.Context, akCertificatePEM []byte) (*AttestationResultRecord, bool, error) {
	prefix, err := attestationPrefix(ctx, attestationResultSegment)
	if err != nil {
		return nil, false, err
	}
	data, found, err := r.store.Get(ctx, prefix+AttestationSubject(akCertificatePEM))
	if err != nil || !found {
		return nil, false, err
	}
	var record AttestationResultRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, false, fmt.Errorf("attestation results: decode stored record: %w", err)
	}
	return &record, true, nil
}

// List returns the latest result of every attestation key of the tenant,
// ordered by subject.
func (r *AttestationResults) List(ctx context.Context) ([]AttestationResultRecord, error) {
	prefix, err := attestationPrefix(ctx, attestationResultSegment)
	if err != nil {
		return nil, err
	}
	records, err := r.store.Scan(ctx, prefix)
	if err != nil {
		return nil, err
	}
	out := make([]AttestationResultRecord, 0, len(records))
	for _, data := range records {
		var record AttestationResultRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out, nil
}

// recordAttestationResult writes result as the latest of the attestation
// key in akCertificatePEM.
func recordAttestationResult(ctx context.Context, store AttestationStore, akCertificatePEM []byte, result *AttestationResult) error {
	prefix, err := attestationPrefix(ctx, attestationResultSegment)
	if err != nil {
		return err
	}
	subject := AttestationSubject(akCertificatePEM)
	data, err := json.Marshal(AttestationResultRecord{Subject: subject, Result: *result})
	if err != nil {
		return err
	}
	return store.Put(ctx, prefix+subject, data)
}
