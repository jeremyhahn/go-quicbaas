//go:build rest

package rest

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// Stopgap until go-quicbaas#1821: enrollment and heartbeat recovery admit only
// a certificate the fleet issuer recorded for the tenant. The generic SignCSR
// path issues certificates of the same shape (OU tenant, SPIFFE kind svc), so
// shape alone must never be enough.

const certRecordTestCN = "agent-record-01"

func newCertRecordStore(t *testing.T) *service.KVFleetNodeCertRecords {
	t.Helper()
	records, err := service.NewKVFleetNodeCertRecords(qrdbsdk.NewMemoryKVStore())
	require.NoError(t, err)
	return records
}

// fleetNodeCertOf returns a certificate shaped as the fleet issuer and the
// generic SignCSR path both issue it: OU and SPIFFE trust domain name the
// tenant, SPIFFE kind svc.
func fleetNodeCertOf(t *testing.T, tenantID string) *x509.Certificate {
	t.Helper()
	return agentSPIFFEPeerCertOfKind(t, tenantID, tenantID, qrdbsdk.SpiffeKindService, certRecordTestCN)
}

func recordCert(t *testing.T, records service.FleetNodeCertRecords, tenantID string, cert *x509.Certificate) {
	t.Helper()
	require.NoError(t, records.RecordFleetNodeCertificate(context.Background(), tenantID, cert))
}

// requireFleetError asserts the status and the OAuth-style error code of a
// fleet enroll error body.
func requireFleetError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	require.Equal(t, status, w.Code, "body: %s", w.Body.String())
	var body fleetEnrollErrorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, code, body.Error)
}

// newCertRecordEnrollFixture returns the enroll fixture with records wired;
// a nil records leaves the server with no store.
func newCertRecordEnrollFixture(t *testing.T, records service.FleetNodeCertRecords) *agentOAuthFixture {
	t.Helper()
	f := newAgentOAuthFixture(t)
	f.server.SetFleetNodeCertRecords(records)
	return f
}

func TestFleetEnrollCertRecord_RecordedCertificateEnrolls(t *testing.T) {
	records := newCertRecordStore(t)
	cert := fleetNodeCertOf(t, agentOAuthTestTenant)
	recordCert(t, records, agentOAuthTestTenant, cert)
	f := newCertRecordEnrollFixture(t, records)

	w := f.postRegister(t, cert)

	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
}

func TestFleetEnrollCertRecord_UnrecordedSVCCertificateIsRefused(t *testing.T) {
	records := newCertRecordStore(t)
	recordCert(t, records, agentOAuthTestTenant, fleetNodeCertOf(t, agentOAuthTestTenant))
	f := newCertRecordEnrollFixture(t, records)

	// Same tenant, same kind, same CN: only the key differs, as for a
	// certificate the generic SignCSR path issued.
	w := f.postRegister(t, fleetNodeCertOf(t, agentOAuthTestTenant))

	requireFleetError(t, w, http.StatusForbidden, "certificate_not_recorded")
	_, err := f.identity.GetUserByUsername(context.Background(), agentOAuthTestTenant, certRecordTestCN)
	require.Error(t, err, "a refused certificate must not leave an enrolled identity behind")
}

func TestFleetEnrollCertRecord_CertificateRecordedForAnotherTenantIsRefused(t *testing.T) {
	records := newCertRecordStore(t)
	cert := fleetNodeCertOf(t, agentOAuthTestTenant)
	recordCert(t, records, "other-tenant", cert)
	f := newCertRecordEnrollFixture(t, records)

	w := f.postRegister(t, cert)

	requireFleetError(t, w, http.StatusForbidden, "certificate_not_recorded")
}

func TestFleetEnrollCertRecord_ServerWithNoStoreFailsClosed(t *testing.T) {
	f := newCertRecordEnrollFixture(t, nil)

	w := f.postRegister(t, fleetNodeCertOf(t, agentOAuthTestTenant))

	requireFleetError(t, w, http.StatusInternalServerError, "server_error")
}

// newCertRecordRecoveryServer returns a server whose first heartbeat misses
// the registry, so the request goes through recovery with records.
func newCertRecordRecoveryServer(t *testing.T, records service.FleetNodeCertRecords) (*Server, *recoverTestRegistrar) {
	t.Helper()

	agentSvc := &recoveryAgentService{mockAgentService: &mockAgentService{}}
	agentSvc.failsRemaining.Store(1)
	wrapper := &recoveryServer{MockQuicBaaS: testutil.NewMockQuicBaaS(), agentService: agentSvc}

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	require.NoError(t, err)
	cfg := DefaultConfig()
	cfg.Address = ":0"
	srv, err := NewServer(wrapper, cfg, log)
	require.NoError(t, err)

	registrar := &recoverTestRegistrar{failNth: -1}
	recoverer, err := NewIAMHeartbeatRecoverer(IAMHeartbeatRecovererDeps{
		Registrar:   registrar,
		Logger:      &recoverTestLogger{t: t},
		TrustDomain: testFleetTrustDomain,
		CertRecords: records,
	})
	require.NoError(t, err)
	srv.SetFleetHeartbeatRecoverer(recoverer)
	return srv, registrar
}

func TestFleetHeartbeatCertRecord_RecordedCertificateRecovers(t *testing.T) {
	records := newCertRecordStore(t)
	cert := fleetNodeCertOf(t, agentOAuthTestTenant)
	recordCert(t, records, agentOAuthTestTenant, cert)
	srv, registrar := newCertRecordRecoveryServer(t, records)
	nodeID := iam.FleetNodeAppName(agentOAuthTestTenant, certRecordTestCN)

	w := makeHeartbeatRequest(t, srv, agentOAuthTestTenant, nodeID, cert)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	require.Len(t, registrar.snapshot(), 1)
}

func TestFleetHeartbeatCertRecord_UnrecordedSVCCertificateIsRefused(t *testing.T) {
	records := newCertRecordStore(t)
	recordCert(t, records, agentOAuthTestTenant, fleetNodeCertOf(t, agentOAuthTestTenant))
	srv, registrar := newCertRecordRecoveryServer(t, records)
	nodeID := iam.FleetNodeAppName(agentOAuthTestTenant, certRecordTestCN)

	w := makeHeartbeatRequest(t, srv, agentOAuthTestTenant, nodeID, fleetNodeCertOf(t, agentOAuthTestTenant))

	requireFleetError(t, w, http.StatusForbidden, "certificate_not_recorded")
	require.Empty(t, registrar.snapshot(), "a refused certificate must not be re-published")
}

func TestFleetHeartbeatCertRecord_CertificateRecordedForAnotherTenantIsRefused(t *testing.T) {
	records := newCertRecordStore(t)
	cert := fleetNodeCertOf(t, agentOAuthTestTenant)
	recordCert(t, records, "other-tenant", cert)
	srv, registrar := newCertRecordRecoveryServer(t, records)
	nodeID := iam.FleetNodeAppName(agentOAuthTestTenant, certRecordTestCN)

	w := makeHeartbeatRequest(t, srv, agentOAuthTestTenant, nodeID, cert)

	requireFleetError(t, w, http.StatusForbidden, "certificate_not_recorded")
	require.Empty(t, registrar.snapshot())
}

func TestNewIAMHeartbeatRecovererCertRecord_RequiresAStore(t *testing.T) {
	recoverer, err := NewIAMHeartbeatRecoverer(IAMHeartbeatRecovererDeps{
		Registrar:   &recoverTestRegistrar{failNth: -1},
		Logger:      &recoverTestLogger{t: t},
		TrustDomain: testFleetTrustDomain,
	})

	require.ErrorIs(t, err, ErrHeartbeatRecoverNoCertRecords)
	require.Nil(t, recoverer)
}
