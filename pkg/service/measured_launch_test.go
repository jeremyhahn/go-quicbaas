package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/provenance"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// quietLogger is an interfaces.Logger that drops what it is given.
type quietLogger struct{ interfaces.Logger }

func (quietLogger) Debug(string, ...any)                              {}
func (quietLogger) Debugf(string, ...interface{})                     {}
func (quietLogger) Info(string, ...any)                               {}
func (quietLogger) Infof(string, ...interface{})                      {}
func (quietLogger) Warn(string, ...any)                               {}
func (quietLogger) Warnf(string, ...interface{})                      {}
func (quietLogger) Error(string, ...any)                              {}
func (quietLogger) Errorf(string, ...interface{})                     {}
func (l quietLogger) WithField(string, interface{}) interfaces.Logger { return l }
func (l quietLogger) WithFields(map[string]interface{}) interfaces.Logger {
	return l
}
func (l quietLogger) WithError(error) interfaces.Logger { return l }

// untouchedTPM is a HostTPM that fails the test if a refused launch
// reaches it.
type untouchedTPM struct {
	orchestration.HostTPM
	t *testing.T
}

func (u untouchedTPM) ExtendPCR(int, string, []byte) error {
	u.t.Fatal("a refused launch must not extend a PCR")
	return nil
}

// unstartedRuntime is a ContainerRuntime that fails the test if a refused
// launch reaches it.
type unstartedRuntime struct {
	orchestration.ContainerRuntime
	t *testing.T
}

func (u unstartedRuntime) Run(context.Context, *orchestration.ContainerSpec) (string, error) {
	u.t.Fatal("a refused launch must not start a container")
	return "", nil
}

// TestNewMeasuredLaunchRuntime proves the production runtime is default
// deny with the Verifier's image verifier, provenance verifier and cluster
// manifest wired in.
func TestNewMeasuredLaunchRuntime(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	store := attestationtest.NewMemoryStore()
	imageCA, err := attestationtest.NewAuthority("Image Signing CA")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAttestationService(nil, nil, WithAttestationStore(store))
	if _, err := svc.PutEndorsement(ctx, &PutEndorsementRequest{Name: "images", Kind: EndorsementImageSigner, CertificatePEM: imageCA.CertificatePEM()}); err != nil {
		t.Fatalf("PutEndorsement: %v", err)
	}
	cfg := MeasuredLaunchConfig{TPM: untouchedTPM{t: t}, Runtime: unstartedRuntime{t: t}}
	spec := func(signed bool) *orchestration.ContainerSpec {
		s := &orchestration.ContainerSpec{
			Image:           "registry.example.com/team/app@" + imageTestDigest,
			TrustLevel:      orchestration.TrustLevelMeasured,
			TrustLabel:      "prod",
			ReferenceValues: "golden",
		}
		if signed {
			s.ImageSignature = signImage(t, imageCA, imageTestDigest)
		}
		return s
	}

	t.Run("NeedsTPMStoreAndLogger", func(t *testing.T) {
		if _, err := NewMeasuredLaunchRuntime(store, MeasuredLaunchConfig{Runtime: cfg.Runtime}, quietLogger{}); err != ErrMeasuredLaunchConfig {
			t.Fatalf("nil TPM: %v", err)
		}
		if _, err := NewMeasuredLaunchRuntime(nil, cfg, quietLogger{}); err != ErrMeasuredLaunchConfig {
			t.Fatalf("nil store: %v", err)
		}
		if _, err := NewMeasuredLaunchRuntime(store, cfg, nil); err != ErrMeasuredLaunchConfig {
			t.Fatalf("nil logger: %v", err)
		}
	})

	t.Run("UnsignedIsRefusedByDefault", func(t *testing.T) {
		rt, err := NewMeasuredLaunchRuntime(store, cfg, quietLogger{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rt.Run(ctx, spec(false)); !stderrors.Is(err, orchestration.ErrImageUnsigned) {
			t.Fatalf("an unsigned image must be refused: %v", err)
		}
	})

	t.Run("ImageVerifierIsWired", func(t *testing.T) {
		rt, err := NewMeasuredLaunchRuntime(store, cfg, quietLogger{})
		if err != nil {
			t.Fatal(err)
		}
		forged := spec(true)
		forged.ImageSignature.Signature[0] ^= 0xff
		if _, err := rt.Run(ctx, forged); !stderrors.Is(err, orchestration.ErrImageSignatureRejected) {
			t.Fatalf("a forged signature must be refused: %v", err)
		}
		if _, err := rt.Run(ctx, spec(true)); !stderrors.Is(err, orchestration.ErrImageProvenance) {
			t.Fatalf("a verified signature with no provenance source must be refused for provenance: %v", err)
		}
	})

	t.Run("ClusterManifestIsWired", func(t *testing.T) {
		source := &fakeProvenanceSource{repository: "team/app", referrers: map[string][]*provenance.Signed{
			imageTestDigest: {signProvenance(t, imageCA, imageTestDigest, "https://factory.example.com")},
		}}
		withProv := cfg
		withProv.Provenance = source
		rt, err := NewMeasuredLaunchRuntime(store, withProv, quietLogger{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rt.Run(ctx, spec(true)); !stderrors.Is(err, orchestration.ErrReleaseNotListed) {
			t.Fatalf("a release no signed manifest lists must be refused: %v", err)
		}
	})
}

func TestResolveContainerAttestor(t *testing.T) {
	store := attestationtest.NewMemoryStore()
	own := NewRuntimeContainerAttestor(&evidenceRuntime{})

	b := &serviceBuilder{containerAttestor: own}
	if got, err := b.resolveContainerAttestor(store, quietLogger{}); err != nil || got != own {
		t.Fatalf("without measured launch the application's attestor is kept: %v, %v", got, err)
	}

	cfg := MeasuredLaunchConfig{TPM: untouchedTPM{t: t}, Runtime: unstartedRuntime{t: t}}
	b = &serviceBuilder{containerAttestor: own, measuredLaunch: &cfg}
	if _, err := b.resolveContainerAttestor(store, quietLogger{}); !stderrors.Is(err, ErrMeasuredLaunchWithAttestor) {
		t.Fatalf("both set: %v", err)
	}

	b = &serviceBuilder{measuredLaunch: &cfg}
	got, err := b.resolveContainerAttestor(store, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	ra, ok := got.(*runtimeContainerAttestor)
	if !ok {
		t.Fatalf("attestor is %T", got)
	}
	if _, ok := ra.rt.(*orchestration.TPMAttestingRuntime); !ok {
		t.Fatalf("measured launch must attest through the TPM runtime, got %T", ra.rt)
	}

	b = &serviceBuilder{measuredLaunch: &MeasuredLaunchConfig{Runtime: cfg.Runtime}}
	if _, err := b.resolveContainerAttestor(store, quietLogger{}); err != ErrMeasuredLaunchConfig {
		t.Fatalf("no TPM: %v", err)
	}
}

// evidenceRuntime attests with a fixed node measurement log.
type evidenceRuntime struct {
	orchestration.ContainerRuntime
}

func (*evidenceRuntime) TrustCapabilities() orchestration.TrustCapabilities {
	return orchestration.TrustCapabilities{}
}

func (*evidenceRuntime) Attest(_ context.Context, spec *orchestration.ContainerSpec, id string) (*orchestration.ContainerAttestation, error) {
	return &orchestration.ContainerAttestation{
		ContainerID: id,
		Image:       spec.Image,
		TrustLevel:  spec.TrustLevel,
		PCRBank:     "sha256",
		MeasurementLog: []orchestration.MeasurementLogEntry{
			{Sequence: 0, Image: spec.Image},
			{Sequence: 1, Image: spec.Image},
		},
	}, nil
}

// TestRuntimeContainerAttestor_CarriesMeasurementLog proves the node
// measurement log and the quoted bank reach the AttestContainer response.
func TestRuntimeContainerAttestor_CarriesMeasurementLog(t *testing.T) {
	resp, err := NewRuntimeContainerAttestor(&evidenceRuntime{}).AttestContainer(context.Background(),
		&AttestContainerRequest{Image: "alpine@" + imageTestDigest, ContainerID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.PCRBank != "sha256" {
		t.Fatalf("PCRBank = %q", resp.PCRBank)
	}
	if len(resp.MeasurementLog) != 2 || resp.MeasurementLog[1].Sequence != 1 || resp.MeasurementLog[1].Image != "alpine@"+imageTestDigest {
		t.Fatalf("MeasurementLog = %+v", resp.MeasurementLog)
	}
}
