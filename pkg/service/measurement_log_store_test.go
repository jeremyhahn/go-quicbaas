package service

import (
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"sync"
	"testing"

	"github.com/google/go-tpm/tpm2"
	xkmstpm2 "github.com/jeremyhahn/go-xkms/pkg/tpm2"
	"github.com/jeremyhahn/go-xkms/pkg/types"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// extendingTPM is a HostTPM whose sha256 PCRs extend as a TPM's do.
type extendingTPM struct {
	orchestration.HostTPM
	mu   sync.Mutex
	pcrs map[int][]byte
	ak   *attestationtest.AttestationKey
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

// Quote signs the SHA-256 values of pcrs with the test attestation key, as
// TPM2_Quote does: a measured launch starts only after a verified host
// appraisal, so the fixture host must produce real evidence.
func (f *extendingTPM) Quote(pcrs []uint, nonce []byte) (xkmstpm2.Quote, error) {
	f.mu.Lock()
	values := make(map[uint32][]byte, len(pcrs))
	for _, i := range pcrs {
		values[uint32(i)] = append([]byte(nil), f.value(int(i))...)
	}
	f.mu.Unlock()
	quote, err := attestationtest.SignQuote(f.ak, values, nonce)
	if err != nil {
		return xkmstpm2.Quote{}, err
	}
	return xkmstpm2.Quote{Quoted: quote.Attest, Signature: quote.Signature, Nonce: nonce}, nil
}

func (f *extendingTPM) IAKCertificate() (*x509.Certificate, error) {
	return f.ak.Certificate, nil
}

func (f *extendingTPM) IAKAttributes() (*types.KeyAttributes, error) {
	return &types.KeyAttributes{TPMAttributes: &types.TPMAttributes{HashAlg: tpm2.TPMAlgSHA256}}, nil
}

// trustHost stores the attestation key root and reference values "golden"
// admitting PCR 7 = zero and every value in launches for PCR 16.
func trustHost(t *testing.T, ctx context.Context, store AttestationStore, akRoot *attestationtest.Authority, launches [][]byte) {
	t.Helper()
	svc := NewAttestationService(nil, nil, WithAttestationStore(store))
	rvCA, err := attestationtest.NewAuthority("Reference Value CA")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutEndorsement(ctx, &PutEndorsementRequest{Name: "ak-ca", Kind: EndorsementAKRoot, CertificatePEM: akRoot.CertificatePEM()}); err != nil {
		t.Fatalf("PutEndorsement ak-root: %v", err)
	}
	if _, err := svc.PutEndorsement(ctx, &PutEndorsementRequest{Name: "rv-ca", Kind: EndorsementReferenceSigner, CertificatePEM: rvCA.CertificatePEM()}); err != nil {
		t.Fatalf("PutEndorsement reference-signer: %v", err)
	}
	digests := []attestationtest.PCRDigest{{Index: 7, Hash: crypto.SHA256, Value: make([]byte, sha256.Size)}}
	for _, value := range launches {
		digests = append(digests, attestationtest.PCRDigest{Index: uint32(orchestration.MeasurementPCRDefault), Hash: crypto.SHA256, Value: value})
	}
	signerCert, signerKey, err := attestationtest.SignerUnder(rvCA)
	if err != nil {
		t.Fatal(err)
	}
	corim, err := attestationtest.SignCoRIMDigests(digests, signerKey, []*x509.Certificate{signerCert})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutReferenceValues(ctx, &PutReferenceValuesRequest{Name: "golden", SignedCoRIM: corim}); err != nil {
		t.Fatalf("PutReferenceValues: %v", err)
	}
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
	akRoot, err := attestationtest.NewAuthority("AK CA")
	if err != nil {
		t.Fatal(err)
	}
	ak, err := attestationtest.IssueAttestationKey(akRoot, true)
	if err != nil {
		t.Fatal(err)
	}
	tpm := &extendingTPM{ak: ak}
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
	// The launch register after the first launch and after the restarted
	// one, each the measurement under the granted policy.
	policy, err := orchestration.LaunchPolicyOf(spec)
	if err != nil {
		t.Fatal(err)
	}
	policy.AllowUnsigned = "TICKET-7"
	granted, err := orchestration.LaunchMeasurementUnder(spec, policy)
	if err != nil {
		t.Fatal(err)
	}
	scratch := &extendingTPM{}
	var launches [][]byte
	for range 2 {
		if err := scratch.ExtendPCR(orchestration.MeasurementPCRDefault, "sha256", granted); err != nil {
			t.Fatal(err)
		}
		launches = append(launches, scratch.value(orchestration.MeasurementPCRDefault))
	}
	trustHost(t, ctx, store, akRoot, launches)

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
