// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package quicbaas

import (
	"context"
	"errors"
	"testing"
	"time"

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// Stub qrdb servicers
// =============================================================================
//
// These stubs are the smallest set required to satisfy the qrdb service
// interfaces used by the embedded transport. They return deterministic
// values from the accessor methods exercised by these tests and a sentinel
// "stub: not implemented" error for everything else.
//
// The stubs are intentionally separate from the qrdb embedded transport's
// own mocks (which live in go-qrdb/sdk/go/transport/embedded/transport_test.go
// behind an unexported package) so the quicbaas SDK can verify the wiring
// without a transitive dependency on those internal mocks.

// stubErr is the sentinel error returned by every unused stub method.
var stubErr = errors.New("qrdb_embedded_test stub: method not implemented")

// stubKVService is a no-op KVServicer used purely so qrdbembedded.New
// accepts the constructor. None of its methods are exercised by these tests.
type stubKVService struct{}

func (stubKVService) Put(context.Context, string, []byte) error   { return stubErr }
func (stubKVService) Get(context.Context, string) ([]byte, error) { return nil, stubErr }
func (stubKVService) Delete(context.Context, string) error        { return stubErr }
func (stubKVService) PutIfAbsent(context.Context, string, []byte) (bool, error) {
	return false, stubErr
}
func (stubKVService) CompareAndDelete(context.Context, string, []byte) (bool, error) {
	return false, stubErr
}
func (stubKVService) CompareAndSwap(context.Context, string, []byte, []byte) (bool, error) {
	return false, stubErr
}
func (stubKVService) List(context.Context, string) ([]string, error) {
	return nil, stubErr
}
func (stubKVService) Scan(context.Context, string) (map[string][]byte, error) {
	return nil, stubErr
}
func (stubKVService) PutEntity(context.Context, *qrdbsdk.PutEntityRequest) (*qrdbsdk.PutEntityResponse, error) {
	return nil, stubErr
}
func (stubKVService) GetEntity(context.Context, *qrdbsdk.GetEntityRequest) (*qrdbsdk.GetEntityResponse, error) {
	return nil, stubErr
}
func (stubKVService) DeleteEntity(context.Context, *qrdbsdk.DeleteEntityRequest) (*qrdbsdk.DeleteEntityResponse, error) {
	return nil, stubErr
}
func (stubKVService) ListEntities(context.Context, *qrdbsdk.ListEntitiesRequest) (*qrdbsdk.ListEntitiesResponse, error) {
	return nil, stubErr
}
func (stubKVService) GetStale(context.Context, string) ([]byte, error)    { return nil, stubErr }
func (stubKVService) ListStale(context.Context, string) ([]string, error) { return nil, stubErr }
func (stubKVService) ScanStale(context.Context, string) (map[string][]byte, error) {
	return nil, stubErr
}
func (stubKVService) PutWithMode(context.Context, string, []byte, qrdbsdk.WriteMode) error {
	return stubErr
}
func (stubKVService) DeleteWithMode(context.Context, string, qrdbsdk.WriteMode) error {
	return stubErr
}
func (stubKVService) GetWithConsistency(context.Context, string, qrdbsdk.ReadOptions) ([]byte, error) {
	return nil, stubErr
}
func (stubKVService) ListWithConsistency(context.Context, string, qrdbsdk.ReadOptions) ([]string, error) {
	return nil, stubErr
}
func (stubKVService) ScanWithConsistency(context.Context, string, qrdbsdk.ReadOptions) (map[string][]byte, error) {
	return nil, stubErr
}
func (stubKVService) ExistsWithConsistency(context.Context, string, qrdbsdk.ReadOptions) (bool, error) {
	return false, stubErr
}

// stubAdminService is a no-op AdminServicer used purely so qrdbembedded.New
// accepts the constructor. None of its methods are exercised by these tests.
type stubAdminService struct{}

func (stubAdminService) Health(context.Context) (*qrdbsdk.HealthStatus, error) {
	return &qrdbsdk.HealthStatus{Status: "ok"}, nil
}
func (stubAdminService) Status(context.Context) (*qrdbsdk.StatusInfo, error) {
	return &qrdbsdk.StatusInfo{}, nil
}
func (stubAdminService) Peers(context.Context) (*qrdbsdk.PeersInfo, error) {
	return &qrdbsdk.PeersInfo{}, nil
}
func (stubAdminService) AddMember(context.Context, uint64, uint64, string) error { return stubErr }
func (stubAdminService) RemoveMember(context.Context, uint64, uint64) error      { return stubErr }
func (stubAdminService) RequestSnapshot(context.Context, uint64) error           { return stubErr }
func (stubAdminService) Drain(context.Context) error                             { return stubErr }
func (stubAdminService) Capabilities(context.Context) (*qrdbsdk.Capabilities, error) {
	return &qrdbsdk.Capabilities{}, nil
}
func (stubAdminService) StartShard(context.Context, *qrdbsdk.StartShardRequest) error {
	return stubErr
}
func (stubAdminService) StopShard(context.Context, uint64) error { return stubErr }
func (stubAdminService) GetShardState(context.Context, uint64) (*qrdbsdk.ShardStateResponse, error) {
	return nil, stubErr
}
func (stubAdminService) GetTopology(context.Context) (*qrdbsdk.TopologyResponse, error) {
	return nil, stubErr
}

// stubBarrierService implements service.BarrierServicer with a single
// deterministic BarrierStatus and stub errors for every other method.
type stubBarrierService struct {
	status *qrdbsdk.BarrierStatusInfo
}

func (s *stubBarrierService) BarrierInitialize(context.Context, string) error { return stubErr }
func (s *stubBarrierService) BarrierUnseal(context.Context, string) error     { return stubErr }
func (s *stubBarrierService) BarrierSeal(context.Context) error               { return stubErr }
func (s *stubBarrierService) BarrierStatus(context.Context) (*qrdbsdk.BarrierStatusInfo, error) {
	return s.status, nil
}
func (s *stubBarrierService) BarrierInitializeShamir(context.Context, string, int, int) (*qrdbsdk.ShamirInitInfo, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierUnsealWithShare(context.Context, string) (*qrdbsdk.UnsealShareProgress, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierUnsealWithShares(context.Context, []string) error { return stubErr }
func (s *stubBarrierService) BarrierRekey(context.Context, int, int) (*qrdbsdk.ShamirInitInfo, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierGenerateRecoveryKeys(context.Context, int, int) (*qrdbsdk.RecoveryKeysInfo, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierRecoverWithKeys(context.Context, []string) error { return stubErr }
func (s *stubBarrierService) BarrierDeleteRecoveryKeys(context.Context) error        { return stubErr }
func (s *stubBarrierService) BarrierHasRecoveryKeys(context.Context) (bool, error) {
	return false, stubErr
}
func (s *stubBarrierService) BarrierGenerateRootToken(context.Context, []string) (*qrdbsdk.RootTokenInfo, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierOfficerRegister(context.Context, *qrdbsdk.BarrierOfficerRegisterRequest) (*qrdbsdk.BarrierOfficerRegisterResult, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierOfficerList(context.Context) ([]*qrdbsdk.BarrierOfficerStatus, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierOfficerClaimStatus(context.Context) ([]*qrdbsdk.BarrierOfficerStatus, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierShareClaimBegin(context.Context, string, string) (*qrdbsdk.BarrierShareClaimChallenge, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierShareClaimComplete(context.Context, string, string, []byte, string) (*qrdbsdk.BarrierShareClaimResult, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierShamirSealVote(context.Context, string) (*qrdbsdk.UnsealShareProgress, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierRecoveryOfficerRegister(context.Context, *qrdbsdk.BarrierOfficerRegisterRequest) (*qrdbsdk.BarrierOfficerRegisterResult, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierRecoveryShareClaimBegin(context.Context, string, string) (*qrdbsdk.BarrierShareClaimChallenge, error) {
	return nil, stubErr
}
func (s *stubBarrierService) BarrierRecoveryShareClaimComplete(context.Context, string, string, []byte, string) (*qrdbsdk.BarrierShareClaimResult, error) {
	return nil, stubErr
}

// stubCeremonyService implements service.CeremonyServicer with a single
// deterministic CeremonyStatus and stub errors for every other method.
type stubCeremonyService struct {
	status *qrdbsdk.CeremonyStatusInfo
}

func (s *stubCeremonyService) CeremonyInitialize(context.Context, qrdbsdk.CeremonyCA, *qrdbservice.CeremonyInitializeOptions) (*qrdbsdk.CeremonyInitResult, error) {
	return nil, stubErr
}
func (s *stubCeremonyService) CeremonyBeginClaimCert(context.Context, string) (*qrdbsdk.CeremonyChallenge, error) {
	return nil, stubErr
}
func (s *stubCeremonyService) CeremonyCompleteClaimCert(context.Context, string, string, []byte) (*qrdbsdk.CeremonyClaimCertResult, error) {
	return nil, stubErr
}
func (s *stubCeremonyService) CeremonyBeginClaimShare(context.Context, string, string) (*qrdbsdk.CeremonyChallenge, error) {
	return nil, stubErr
}
func (s *stubCeremonyService) CeremonyCompleteClaimShare(context.Context, string, string, []byte, string) (*qrdbservice.CeremonyShareClaimResult, error) {
	return nil, stubErr
}
func (s *stubCeremonyService) CeremonySignCSR(context.Context, string, string, string, string) (*qrdbsdk.CeremonySignCSRResult, error) {
	return nil, stubErr
}
func (s *stubCeremonyService) CeremonyStatus(context.Context) (*qrdbsdk.CeremonyStatusInfo, error) {
	return s.status, nil
}
func (s *stubCeremonyService) CeremonyClaimShareBegin(context.Context, string) (*qrdbsdk.CeremonyCustodianChallenge, error) {
	return nil, stubErr
}
func (s *stubCeremonyService) CeremonyClaimShareComplete(context.Context, string, string, []byte) (*qrdbsdk.Custodian, error) {
	return nil, stubErr
}
func (s *stubCeremonyService) CeremonySubmitUnsealShare(context.Context, string, string, []byte, []byte) (*qrdbsdk.CeremonyUnsealShareResult, error) {
	return nil, stubErr
}
func (s *stubCeremonyService) CeremonyEnrollExternalOfficer(context.Context, *qrdbsdk.CeremonyEnrollExternalOfficerRequest) (*qrdbsdk.CeremonyEnrollExternalOfficerResponse, error) {
	return nil, stubErr
}

// stubCredentialService implements service.CredentialServicer with a
// deterministic CredentialStrategy() and stub errors for every other method.
type stubCredentialService struct {
	strategy string
}

func (s *stubCredentialService) CredentialStore(context.Context, string, []byte) error {
	return stubErr
}
func (s *stubCredentialService) CredentialRetrieve(context.Context, string) ([]byte, error) {
	return nil, stubErr
}
func (s *stubCredentialService) CredentialSubmit(context.Context, string, []byte) error {
	return stubErr
}
func (s *stubCredentialService) CredentialDelete(context.Context, string) error { return stubErr }
func (s *stubCredentialService) CredentialStrategy() string                     { return s.strategy }
func (s *stubCredentialService) CredentialList(context.Context) ([]string, error) {
	return nil, stubErr
}
func (s *stubCredentialService) CredentialRotate(context.Context, string, qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error) {
	return nil, stubErr
}
func (s *stubCredentialService) CredentialRetrieveVersion(context.Context, string, uint64) ([]byte, error) {
	return nil, stubErr
}
func (s *stubCredentialService) CredentialListVersions(context.Context, string) ([]qrdbsdk.CredentialVersion, error) {
	return nil, stubErr
}
func (s *stubCredentialService) CredentialGCExpired(context.Context, string) (int, error) {
	return 0, stubErr
}

// =============================================================================
// WithEmbeddedQRDB option tests
// =============================================================================

func TestWithEmbeddedQRDB_RequiresKV(t *testing.T) {
	opts := defaultOptions()
	err := WithEmbeddedQRDB(QRDBEmbeddedServicers{
		Admin: stubAdminService{},
	})(opts)
	if err == nil {
		t.Fatal("expected error when KV servicer is nil")
	}
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %T: %v", err, err)
	}
}

func TestWithEmbeddedQRDB_RequiresAdmin(t *testing.T) {
	opts := defaultOptions()
	err := WithEmbeddedQRDB(QRDBEmbeddedServicers{
		KV: stubKVService{},
	})(opts)
	if err == nil {
		t.Fatal("expected error when Admin servicer is nil")
	}
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %T: %v", err, err)
	}
}

func TestWithEmbeddedQRDB_Success(t *testing.T) {
	opts := defaultOptions()
	servicers := QRDBEmbeddedServicers{
		KV:    stubKVService{},
		Admin: stubAdminService{},
	}
	if err := WithEmbeddedQRDB(servicers)(opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.embeddedQRDB == nil {
		t.Fatal("expected embeddedQRDB to be set on clientOptions")
	}
	if opts.embeddedQRDB.KV == nil || opts.embeddedQRDB.Admin == nil {
		t.Error("expected KV and Admin servicers to round-trip onto clientOptions")
	}
}

// =============================================================================
// TransportEmbedded without WithEmbeddedQRDB returns a typed error
// =============================================================================

func TestBuildQRDBClient_EmbeddedWithoutServicers_ReturnsError(t *testing.T) {
	c := &client{
		opts:    defaultOptions(),
		address: "embedded://local",
	}
	c.opts.transportType = TransportEmbedded
	// Intentionally do NOT install qrdb servicers.

	_, err := c.buildQRDBClient()
	if err == nil {
		t.Fatal("expected error when TransportEmbedded but no qrdb servicers configured")
	}
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %T: %v", err, err)
	}
}

// =============================================================================
// Round-trip tests: SDK -> qrdb embedded transport -> stub servicers
// =============================================================================

// buildEmbeddedQRDBClient constructs a quicbaas SDK client wired to the
// supplied qrdb stubs. The quicbaas transport selection is irrelevant for
// these tests: only the qrdb sub-client is exercised, and the embedded
// dispatch path is taken whenever embeddedQRDB is set.
func buildEmbeddedQRDBClient(t *testing.T, servicers QRDBEmbeddedServicers) *client {
	t.Helper()
	opts := defaultOptions()
	if err := WithEmbeddedQRDB(servicers)(opts); err != nil {
		t.Fatalf("WithEmbeddedQRDB: %v", err)
	}
	return &client{
		opts:    opts,
		address: "embedded://local",
	}
}

func TestSecurityBarrier_RoutesThroughEmbeddedTransport(t *testing.T) {
	want := &qrdbsdk.BarrierStatusInfo{
		Sealed:         false,
		Strategy:       "software-stub",
		HardwareBacked: false,
		InitializedAt:  time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
	}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:      stubKVService{},
		Admin:   stubAdminService{},
		Barrier: &stubBarrierService{status: want},
	})

	barrier, err := c.Security().Barrier()
	if err != nil {
		t.Fatalf("Security().Barrier(): %v", err)
	}
	if barrier == nil {
		t.Fatal("expected non-nil BarrierClient")
	}

	resp, err := barrier.BarrierStatus(context.Background())
	if err != nil {
		t.Fatalf("BarrierStatus: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil BarrierStatusResponse")
	}
	if resp.Sealed != want.Sealed {
		t.Errorf("Sealed: got %v, want %v", resp.Sealed, want.Sealed)
	}
	if resp.Strategy != want.Strategy {
		t.Errorf("Strategy: got %q, want %q", resp.Strategy, want.Strategy)
	}
	if resp.HardwareBacked != want.HardwareBacked {
		t.Errorf("HardwareBacked: got %v, want %v", resp.HardwareBacked, want.HardwareBacked)
	}
	if resp.InitializedAt == "" {
		t.Error("expected InitializedAt to round-trip through the embedded transport")
	}
}

func TestSecurityCeremony_RoutesThroughEmbeddedTransport(t *testing.T) {
	want := &qrdbsdk.CeremonyStatusInfo{
		State:   "awaiting-officers",
		SPKIPin: "stub-spki-pin",
	}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:       stubKVService{},
		Admin:    stubAdminService{},
		Ceremony: &stubCeremonyService{status: want},
	})

	ceremony, err := c.Security().Ceremony()
	if err != nil {
		t.Fatalf("Security().Ceremony(): %v", err)
	}
	if ceremony == nil {
		t.Fatal("expected non-nil CeremonyClient")
	}

	resp, err := ceremony.CeremonyStatus(context.Background())
	if err != nil {
		t.Fatalf("CeremonyStatus: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil CeremonyStatusResponse")
	}
	if resp.State != want.State {
		t.Errorf("State: got %q, want %q", resp.State, want.State)
	}
	if resp.SPKIPin != want.SPKIPin {
		t.Errorf("SPKIPin: got %q, want %q", resp.SPKIPin, want.SPKIPin)
	}
}

func TestSecurityCredentials_RoutesThroughEmbeddedTransport(t *testing.T) {
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:         stubKVService{},
		Admin:      stubAdminService{},
		Credential: &stubCredentialService{strategy: "stub-strategy"},
	})

	credentials, err := c.Security().Credentials()
	if err != nil {
		t.Fatalf("Security().Credentials(): %v", err)
	}
	if credentials == nil {
		t.Fatal("expected non-nil CredentialClient")
	}

	resp, err := credentials.CredentialStrategy(context.Background())
	if err != nil {
		t.Fatalf("CredentialStrategy: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil CredentialStrategyResponse")
	}
	if resp.Strategy != "stub-strategy" {
		t.Errorf("Strategy: got %q, want %q", resp.Strategy, "stub-strategy")
	}
}

// =============================================================================
// Embedded qrdb path is taken even when a non-embedded quicbaas transport is
// configured. This locks in the design that the qrdb sub-client dispatches
// on embeddedQRDB presence, independent of the quicbaas transport selection.
// =============================================================================

func TestBuildQRDBClient_EmbeddedQRDBOverridesNetworkTransport(t *testing.T) {
	opts := defaultOptions()
	opts.transportType = TransportGRPC // network transport
	if err := WithEmbeddedQRDB(QRDBEmbeddedServicers{
		KV:    stubKVService{},
		Admin: stubAdminService{},
	})(opts); err != nil {
		t.Fatalf("WithEmbeddedQRDB: %v", err)
	}
	c := &client{opts: opts, address: "localhost:50051"}

	qc, err := c.buildQRDBClient()
	if err != nil {
		t.Fatalf("buildQRDBClient: %v", err)
	}
	if qc == nil {
		t.Fatal("expected non-nil qrdb client")
	}
}
