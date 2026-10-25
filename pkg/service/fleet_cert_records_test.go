package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

const fleetCertRecordTestTenant = "acme"

// fleetCertRecordTestCert returns a parsed self-signed certificate with its
// own key, so each call has a distinct SPKI thumbprint.
func fleetCertRecordTestCert(t *testing.T, notAfter time.Time) *x509.Certificate {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "node-01", OrganizationalUnit: []string{fleetCertRecordTestTenant}},
		NotBefore:    notAfter.Add(-48 * time.Hour),
		NotAfter:     notAfter,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

func newFleetCertRecordsOver(t *testing.T, kv interfaces.KVStore) *KVFleetNodeCertRecords {
	t.Helper()
	records, err := NewKVFleetNodeCertRecords(kv)
	require.NoError(t, err)
	return records
}

func TestKVFleetNodeCertRecords_RecordStoresTenantAndExpiry(t *testing.T) {
	ctx := context.Background()
	kv := qrdbsdk.NewMemoryKVStore()
	records := newFleetCertRecordsOver(t, kv)
	notAfter := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	cert := fleetCertRecordTestCert(t, notAfter)

	require.NoError(t, records.RecordFleetNodeCertificate(ctx, fleetCertRecordTestTenant, cert))
	require.NoError(t, records.VerifyFleetNodeCertificate(ctx, fleetCertRecordTestTenant, cert))

	data, err := kv.Get(accountTenantContext(ctx, fleetCertRecordTestTenant),
		fleetNodeCertRecordPrefix+qrdbsdk.ComputeSPKIThumbprint(cert))
	require.NoError(t, err)
	var record FleetNodeCertRecord
	require.NoError(t, json.Unmarshal(data, &record))
	require.Equal(t, fleetCertRecordTestTenant, record.TenantID)
	require.True(t, notAfter.Equal(record.NotAfter), "record expiry %v, want %v", record.NotAfter, notAfter)
}

func TestKVFleetNodeCertRecords_UnknownThumbprintIsNotRecorded(t *testing.T) {
	ctx := context.Background()
	records := newFleetCertRecordsOver(t, qrdbsdk.NewMemoryKVStore())
	recorded := fleetCertRecordTestCert(t, time.Now().Add(time.Hour))
	unrecorded := fleetCertRecordTestCert(t, time.Now().Add(time.Hour))
	require.NoError(t, records.RecordFleetNodeCertificate(ctx, fleetCertRecordTestTenant, recorded))

	err := records.VerifyFleetNodeCertificate(ctx, fleetCertRecordTestTenant, unrecorded)

	var notRecorded *FleetNodeCertNotRecordedError
	require.True(t, errors.As(err, &notRecorded), "want *FleetNodeCertNotRecordedError, got %v", err)
	require.Equal(t, fleetCertRecordTestTenant, notRecorded.TenantID)
	require.Equal(t, qrdbsdk.ComputeSPKIThumbprint(unrecorded), notRecorded.Thumbprint)
	require.Empty(t, notRecorded.RecordTenantID)
}

func TestKVFleetNodeCertRecords_OtherTenantIsNotRecorded(t *testing.T) {
	ctx := context.Background()
	records := newFleetCertRecordsOver(t, qrdbsdk.NewMemoryKVStore())
	cert := fleetCertRecordTestCert(t, time.Now().Add(time.Hour))
	require.NoError(t, records.RecordFleetNodeCertificate(ctx, "other", cert))

	err := records.VerifyFleetNodeCertificate(ctx, fleetCertRecordTestTenant, cert)

	var notRecorded *FleetNodeCertNotRecordedError
	require.True(t, errors.As(err, &notRecorded), "want *FleetNodeCertNotRecordedError, got %v", err)
	require.Equal(t, fleetCertRecordTestTenant, notRecorded.TenantID)
}

func TestKVFleetNodeCertRecords_RefusesNilCertificateAndEmptyTenant(t *testing.T) {
	ctx := context.Background()
	records := newFleetCertRecordsOver(t, qrdbsdk.NewMemoryKVStore())
	cert := fleetCertRecordTestCert(t, time.Now().Add(time.Hour))

	require.ErrorIs(t, records.RecordFleetNodeCertificate(ctx, fleetCertRecordTestTenant, nil), ErrFleetNodeCertRecordNilCert)
	require.ErrorIs(t, records.VerifyFleetNodeCertificate(ctx, fleetCertRecordTestTenant, nil), ErrFleetNodeCertRecordNilCert)
	require.ErrorIs(t, records.RecordFleetNodeCertificate(ctx, "", cert), ErrFleetNodeCertRecordNoTenant)
	require.ErrorIs(t, records.RecordFleetNodeCertificate(ctx, "  ", cert), ErrFleetNodeCertRecordNoTenant)
	require.ErrorIs(t, records.VerifyFleetNodeCertificate(ctx, "", cert), ErrFleetNodeCertRecordNoTenant)
}

func TestNewKVFleetNodeCertRecords_RefusesNilStore(t *testing.T) {
	records, err := NewKVFleetNodeCertRecords(nil)
	require.ErrorIs(t, err, ErrFleetNodeCertRecordsNilStore)
	require.Nil(t, records)
}

func TestKVFleetNodeCertRecords_RecordSurvivesANewStoreOverTheSameKV(t *testing.T) {
	ctx := context.Background()
	kv := qrdbsdk.NewMemoryKVStore()
	cert := fleetCertRecordTestCert(t, time.Now().Add(time.Hour))
	require.NoError(t, newFleetCertRecordsOver(t, kv).RecordFleetNodeCertificate(ctx, fleetCertRecordTestTenant, cert))

	reopened := newFleetCertRecordsOver(t, kv)

	require.NoError(t, reopened.VerifyFleetNodeCertificate(ctx, fleetCertRecordTestTenant, cert),
		"a record must outlive the store value that wrote it (server restart)")
}

// TestKVFleetNodeCertRecords_DataPlaneForgeryIsNotARecord writes a record
// through the qrdb data plane the way a caller with KV write on a database
// named "fleet" would: the data plane composes "<tenant>/<database>/<key>", so
// inside the tenant's namespace the forged entry lands at
// "fleet/node-cert/<SPKI>". The fleet issuer's records live in the reserved
// "__sys:" keyspace, which no database name can reach, so the forgery must not
// verify.
func TestKVFleetNodeCertRecords_DataPlaneForgeryIsNotARecord(t *testing.T) {
	ctx := context.Background()
	kv := newTenantNamespacedKV()
	records := newFleetCertRecordsOver(t, kv)
	cert := fleetCertRecordTestCert(t, time.Now().Add(time.Hour))
	forged, err := json.Marshal(&FleetNodeCertRecord{TenantID: fleetCertRecordTestTenant, NotAfter: cert.NotAfter})
	require.NoError(t, err)
	dataPlaneKey := "fleet" + "/" + "node-cert/" + qrdbsdk.ComputeSPKIThumbprint(cert)
	require.NoError(t, kv.Put(accountTenantContext(ctx, fleetCertRecordTestTenant), dataPlaneKey, forged))

	err = records.VerifyFleetNodeCertificate(ctx, fleetCertRecordTestTenant, cert)

	var notRecorded *FleetNodeCertNotRecordedError
	require.True(t, errors.As(err, &notRecorded), "a record forged through the data plane must not verify, got %v", err)
	require.True(t, strings.HasPrefix(fleetNodeCertRecordPrefix, SystemKeyspacePrefix),
		"the records must live in qrdb's reserved system keyspace")
}
