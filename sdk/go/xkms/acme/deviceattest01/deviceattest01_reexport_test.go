package deviceattest01_test

import (
	"encoding/base64"
	"errors"
	"reflect"
	"testing"

	da01 "github.com/jeremyhahn/go-xkms/pkg/acme/challenges/device-attest-01"
	"github.com/jeremyhahn/go-xkms/pkg/acme/challenges/validation"
	"github.com/jeremyhahn/go-xkms/pkg/acme/common"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/acme/deviceattest01"
)

// A go-trusted-platform ACME server or device names these through sdk/go;
// each must be go-xkms's own, so a value crosses the seam without conversion.
func TestTypes_AreTheGoXKMSTypes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		local, up reflect.Type
	}{
		{"AttestationFormat", reflect.TypeOf(deviceattest01.AttestationFormat("")), reflect.TypeOf(da01.AttestationFormat(""))},
		{"AttestationObject", reflect.TypeOf(deviceattest01.AttestationObject{}), reflect.TypeOf(da01.AttestationObject{})},
		{"AttestationStatement", reflect.TypeOf(deviceattest01.AttestationStatement{}), reflect.TypeOf(da01.AttestationStatement{})},
		{"AttestationRequest", reflect.TypeOf(deviceattest01.AttestationRequest{}), reflect.TypeOf(da01.AttestationRequest{})},
		{"VerifiedAttestation", reflect.TypeOf(deviceattest01.VerifiedAttestation{}), reflect.TypeOf(da01.VerifiedAttestation{})},
		{"AttestationVerifier", reflect.TypeOf((*deviceattest01.AttestationVerifier)(nil)).Elem(), reflect.TypeOf((*da01.AttestationVerifier)(nil)).Elem()},
		{"TrustedPlatformModule", reflect.TypeOf((*deviceattest01.TrustedPlatformModule)(nil)).Elem(), reflect.TypeOf((*da01.TrustedPlatformModule)(nil)).Elem()},
		{"ChallengeClient", reflect.TypeOf(deviceattest01.ChallengeClient{}), reflect.TypeOf(validation.Client{})},
		{"ChallengeClientConfig", reflect.TypeOf(deviceattest01.ChallengeClientConfig{}), reflect.TypeOf(validation.Config{})},
	} {
		if tc.local != tc.up {
			t.Errorf("%s is %v, want the go-xkms type %v", tc.name, tc.local, tc.up)
		}
	}
}

func TestFunctions_AreTheGoXKMSFunctions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		local, up any
	}{
		{"Verify", deviceattest01.Verify, da01.Verify},
		{"VerifyAttestation", deviceattest01.VerifyAttestation, da01.VerifyAttestation},
		{"ParseAttestationObject", deviceattest01.ParseAttestationObject, da01.ParseAttestationObject},
		{"EncodeAttestationForSubmission", deviceattest01.EncodeAttestationForSubmission, da01.EncodeAttestationForSubmission},
		{"ValidateAttestationFormat", deviceattest01.ValidateAttestationFormat, da01.ValidateAttestationFormat},
		{"Setup", deviceattest01.Setup, da01.Setup},
		{"TPMExtraData", deviceattest01.TPMExtraData, da01.TPMExtraData},
		{"DICENonce", deviceattest01.DICENonce, da01.DICENonce},
		{"NewChallengeClient", deviceattest01.NewChallengeClient, validation.NewClient},
	} {
		if reflect.ValueOf(tc.local).Pointer() != reflect.ValueOf(tc.up).Pointer() {
			t.Errorf("%s is not go-xkms's %s", tc.name, tc.name)
		}
	}
}

func TestConstants_AreTheGoXKMSValues(t *testing.T) {
	if deviceattest01.ChallengeType != common.ChallengeTypeDeviceAttest01 {
		t.Errorf("ChallengeType = %q, want %q", deviceattest01.ChallengeType, common.ChallengeTypeDeviceAttest01)
	}
	if deviceattest01.FormatTPM != da01.FormatTPM || deviceattest01.FormatDICE != da01.FormatDICE {
		t.Errorf("formats = %q, %q, want %q, %q",
			deviceattest01.FormatTPM, deviceattest01.FormatDICE, da01.FormatTPM, da01.FormatDICE)
	}
	for _, f := range []deviceattest01.AttestationFormat{deviceattest01.FormatTPM, deviceattest01.FormatDICE} {
		if !deviceattest01.ValidateAttestationFormat(f) {
			t.Errorf("ValidateAttestationFormat(%q) = false", f)
		}
	}
	if deviceattest01.ValidateAttestationFormat("packed") {
		t.Error(`ValidateAttestationFormat("packed") = true, want a refusal`)
	}
}

func TestSentinels_AreTheGoXKMSErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		local, up error
	}{
		{"ErrInvalidKeyAlgorithm", deviceattest01.ErrInvalidKeyAlgorithm, da01.ErrInvalidKeyAlgorithm},
		{"ErrInvalidAttestationFormat", deviceattest01.ErrInvalidAttestationFormat, da01.ErrInvalidAttestationFormat},
		{"ErrInvalidAttestationObject", deviceattest01.ErrInvalidAttestationObject, da01.ErrInvalidAttestationObject},
		{"ErrTPMAttestationInvalid", deviceattest01.ErrTPMAttestationInvalid, da01.ErrTPMAttestationInvalid},
		{"ErrAIKCertificateInvalid", deviceattest01.ErrAIKCertificateInvalid, da01.ErrAIKCertificateInvalid},
		{"ErrCertificateChainInvalid", deviceattest01.ErrCertificateChainInvalid, da01.ErrCertificateChainInvalid},
		{"ErrKeyBindingRequired", deviceattest01.ErrKeyBindingRequired, da01.ErrKeyBindingRequired},
		{"ErrKeyBindingUnavailable", deviceattest01.ErrKeyBindingUnavailable, da01.ErrKeyBindingUnavailable},
		{"ErrNoVerifiedAttestation", deviceattest01.ErrNoVerifiedAttestation, da01.ErrNoVerifiedAttestation},
		{"ErrAttestationKeyUnavailable", deviceattest01.ErrAttestationKeyUnavailable, da01.ErrAttestationKeyUnavailable},
		{"ErrNoCertificateKey", deviceattest01.ErrNoCertificateKey, da01.ErrNoCertificateKey},
		{"ErrCertifyFailed", deviceattest01.ErrCertifyFailed, da01.ErrCertifyFailed},
	} {
		if tc.local == nil || !errors.Is(tc.local, tc.up) {
			t.Errorf("%s is not go-xkms's sentinel", tc.name)
		}
	}
}

// The verifier a server reaches through sdk/go behaves as go-xkms's: an
// encoded attestation object parses back, and a malformed one is refused.
func TestAttestationObject_RoundTripsThroughTheReexport(t *testing.T) {
	encoded, err := deviceattest01.EncodeAttestationForSubmission(&deviceattest01.AttestationObject{
		Format: string(deviceattest01.FormatDICE),
	})
	if err != nil {
		t.Fatalf("EncodeAttestationForSubmission: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode submission %q: %v", encoded, err)
	}
	obj, err := deviceattest01.ParseAttestationObject(raw)
	if err != nil {
		t.Fatalf("ParseAttestationObject: %v", err)
	}
	if obj.Format != string(deviceattest01.FormatDICE) {
		t.Errorf("Format = %q, want %q", obj.Format, deviceattest01.FormatDICE)
	}
	if _, err := deviceattest01.ParseAttestationObject([]byte("not cbor")); err == nil {
		t.Error("ParseAttestationObject accepted a malformed object")
	}
}

// With no CSR in front of it a challenge verifier cannot bind the attested
// key, so a VerifiedAttestation with nothing verified binds nothing.
func TestVerifiedAttestation_ZeroValueBindsNothing(t *testing.T) {
	var v *deviceattest01.VerifiedAttestation
	if err := v.BindCSRKey(nil); err == nil {
		t.Error("BindCSRKey on no verified attestation succeeded")
	}
	if _, err := deviceattest01.VerifyAttestation(nil, nil, "device.example", "80", "token", "keyauth"); err == nil {
		t.Error("VerifyAttestation with no challenge client succeeded")
	}
}
