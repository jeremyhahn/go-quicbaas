package service

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// STOPGAP, removed by go-quicbaas#1821. A fleet node certificate carries
// SPIFFE kind "svc", and so does every certificate the generic CA SignCSR path
// issues for a non-client profile, so the kind cannot tell the two apart. Until
// go-xkms stamps an issuer-owned agent claim into the certificate (the re-pin
// in #1821), fleet enrollment and heartbeat recovery accept only a certificate
// the fleet issuer recorded here when it issued it. Delete this file, its
// wiring and its checks when #1821 lands.

// fleetNodeCertRecordPrefix is the KV keyspace of the issued-certificate
// records. The key is the certificate's SPKI thumbprint; the store routes it
// into the record tenant's namespace by the context tenant.
//
// The records live in qrdb's reserved "__sys:" system keyspace, inside the
// tenant's namespace ("<tenant>/__sys:fleet/node-cert/<SPKI>"). The data plane
// composes every caller key as "<tenant>/<database>/<key>", and qrdb refuses a
// database name containing ':', so no caller-created database can reach a
// record. A record under a plain "fleet/" prefix would be the data plane key of
// a database named "fleet", which any caller with KV write could forge.
const fleetNodeCertRecordPrefix = SystemKeyspacePrefix + "fleet/node-cert/"

// FleetNodeCertRecord is what the fleet issuer remembers about a certificate
// it issued.
type FleetNodeCertRecord struct {
	TenantID string    `json:"tenant_id"`
	NotAfter time.Time `json:"not_after"`
}

// FleetNodeCertRecords records the certificates the fleet issuer issued and
// answers whether a presented certificate is one of them. The issuer writes
// it; the enrollment and heartbeat recovery handlers read it.
type FleetNodeCertRecords interface {
	// RecordFleetNodeCertificate persists the record of cert, issued for
	// tenantID.
	RecordFleetNodeCertificate(ctx context.Context, tenantID string, cert *x509.Certificate) error

	// VerifyFleetNodeCertificate returns nil when cert was issued by the
	// fleet issuer for tenantID, and *FleetNodeCertNotRecordedError when it
	// was not.
	VerifyFleetNodeCertificate(ctx context.Context, tenantID string, cert *x509.Certificate) error
}

// KVFleetNodeCertRecords keeps the records in the server's durable KV, so a
// node issued a certificate before a restart still enrolls after it.
type KVFleetNodeCertRecords struct {
	kv interfaces.KVStore
}

var _ FleetNodeCertRecords = (*KVFleetNodeCertRecords)(nil)

// NewKVFleetNodeCertRecords builds the records over kv.
func NewKVFleetNodeCertRecords(kv interfaces.KVStore) (*KVFleetNodeCertRecords, error) {
	if kv == nil {
		return nil, ErrFleetNodeCertRecordsNilStore
	}
	return &KVFleetNodeCertRecords{kv: kv}, nil
}

// RecordFleetNodeCertificate implements FleetNodeCertRecords.
func (r *KVFleetNodeCertRecords) RecordFleetNodeCertificate(ctx context.Context, tenantID string, cert *x509.Certificate) error {
	key, err := fleetNodeCertRecordKey(tenantID, cert)
	if err != nil {
		return err
	}
	data, err := json.Marshal(&FleetNodeCertRecord{TenantID: tenantID, NotAfter: cert.NotAfter.UTC()})
	if err != nil {
		return &FleetNodeCertRecordStoreError{Op: "encode", Cause: err}
	}
	if err := r.kv.Put(accountTenantContext(ctx, tenantID), key, data); err != nil {
		return &FleetNodeCertRecordStoreError{Op: "put", Cause: err}
	}
	return nil
}

// VerifyFleetNodeCertificate implements FleetNodeCertRecords. The record is
// read in tenantID's namespace, so a certificate recorded for another tenant
// is not found there; the tenant in the record is compared as well, for a
// store that does not route by tenant.
func (r *KVFleetNodeCertRecords) VerifyFleetNodeCertificate(ctx context.Context, tenantID string, cert *x509.Certificate) error {
	key, err := fleetNodeCertRecordKey(tenantID, cert)
	if err != nil {
		return err
	}
	thumbprint := strings.TrimPrefix(key, fleetNodeCertRecordPrefix)
	data, err := r.kv.Get(accountTenantContext(ctx, tenantID), key)
	if err != nil && !qerrors.IsNotFound(err) {
		return &FleetNodeCertRecordStoreError{Op: "get", Cause: err}
	}
	if err != nil || data == nil {
		return &FleetNodeCertNotRecordedError{TenantID: tenantID, Thumbprint: thumbprint}
	}
	var record FleetNodeCertRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return &FleetNodeCertRecordStoreError{Op: "decode", Cause: err}
	}
	if record.TenantID != tenantID {
		return &FleetNodeCertNotRecordedError{TenantID: tenantID, Thumbprint: thumbprint, RecordTenantID: record.TenantID}
	}
	return nil
}

func fleetNodeCertRecordKey(tenantID string, cert *x509.Certificate) (string, error) {
	if cert == nil {
		return "", ErrFleetNodeCertRecordNilCert
	}
	if strings.TrimSpace(tenantID) == "" {
		return "", ErrFleetNodeCertRecordNoTenant
	}
	return fleetNodeCertRecordPrefix + qrdbsdk.ComputeSPKIThumbprint(cert), nil
}

// ErrFleetNodeCertRecordsNilStore is returned by NewKVFleetNodeCertRecords
// when called with a nil KV store.
var ErrFleetNodeCertRecordsNilStore = errors.New("fleet node cert records: kv store is required")

// ErrFleetNodeCertRecordsRequired is returned by NewXKMSFleetNodeCertIssuer
// when called without records: a certificate nobody recorded can never
// enroll.
var ErrFleetNodeCertRecordsRequired = errors.New("agent cert issuer: certificate records are required")

// ErrFleetNodeCertRecordNilCert is returned when no certificate is given.
var ErrFleetNodeCertRecordNilCert = errors.New("fleet node cert records: certificate is nil")

// ErrFleetNodeCertRecordNoTenant is returned when no tenant is given.
var ErrFleetNodeCertRecordNoTenant = errors.New("fleet node cert records: tenant is required")

// FleetNodeCertNotRecordedError reports a certificate the fleet issuer did
// not issue for the tenant presenting it. Handlers answer it 403.
type FleetNodeCertNotRecordedError struct {
	TenantID   string
	Thumbprint string
	// RecordTenantID is set when the certificate was recorded for another
	// tenant.
	RecordTenantID string
}

func (e *FleetNodeCertNotRecordedError) Error() string {
	if e.RecordTenantID != "" {
		return "fleet node certificate " + e.Thumbprint + " was issued for tenant " + e.RecordTenantID + ", not " + e.TenantID
	}
	return "fleet node certificate " + e.Thumbprint + " was not issued by the fleet issuer for tenant " + e.TenantID
}

// FleetNodeCertRecordStoreError wraps a KV failure reading or writing a
// record.
type FleetNodeCertRecordStoreError struct {
	Op    string
	Cause error
}

func (e *FleetNodeCertRecordStoreError) Error() string {
	return "fleet node cert records: " + e.Op + ": " + e.Cause.Error()
}

func (e *FleetNodeCertRecordStoreError) Unwrap() error { return e.Cause }
