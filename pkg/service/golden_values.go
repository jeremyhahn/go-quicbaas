package service

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	stderrors "errors"
	"fmt"
	"sort"

	"github.com/veraison/corim/comid"
	"github.com/veraison/corim/corim"
	cose "github.com/veraison/go-cose"
	"github.com/veraison/swid"
)

// GoldenPCR is one golden PCR value: the digest PCR Index holds in the bank
// of Hash on a known-good host.
type GoldenPCR struct {
	Index uint32      `json:"index"`
	Hash  crypto.Hash `json:"hash"`
	Value []byte      `json:"value"`
}

// GoldenValues is a set of golden PCR values as go-trusted-platform keeps
// them (the profile baseline, the TPM golden PCRs and the registry's
// required PCRs), to be imported once into the Verifier's go-xkms
// reference-values store as a signed CoRIM.
type GoldenValues struct {
	// ID names the CoRIM; it must be unique per import.
	ID string `json:"id"`
	// Profile is the CoRIM profile; empty selects GoldenValuesProfile.
	Profile string `json:"profile,omitempty"`
	// Vendor and Model name the platform class the values describe.
	Vendor string `json:"vendor,omitempty"`
	Model  string `json:"model,omitempty"`
	// Signer names the reference-value provider in the CoRIM meta.
	Signer string `json:"signer,omitempty"`
	// PCRs are the golden values; a PCR may carry one value per bank.
	PCRs []GoldenPCR `json:"pcrs"`
}

// GoldenValuesProfile is the CoRIM profile golden PCR values are signed
// under when GoldenValues names none.
const GoldenValuesProfile = "tag:quicbaas.dev,2026:tpm-pcr"

// goldenValuesClassOID is the platform class of imported golden values.
const goldenValuesClassOID = "1.3.6.1.4.1.29377.200.1"

// goldenDigestAlgs maps a PCR bank's hash to its CoRIM digest algorithm.
var goldenDigestAlgs = map[crypto.Hash]uint64{
	crypto.SHA256: swid.Sha256,
	crypto.SHA384: swid.Sha384,
	crypto.SHA512: swid.Sha512,
}

// ErrGoldenValues is returned for golden values that cannot be signed.
var ErrGoldenValues = stderrors.New("golden values")

// SignGoldenValues signs gv as a COSE_Sign1 CoRIM with signer, a go-xkms
// key whose certificate chain (leaf first) is chain. The Verifier accepts
// it when chain terminates at a reference-signer endorsement.
func SignGoldenValues(gv *GoldenValues, signer crypto.Signer, chain []*x509.Certificate) ([]byte, error) {
	if gv == nil || gv.ID == "" || len(gv.PCRs) == 0 {
		return nil, fmt.Errorf("%w: an ID and at least one PCR are required", ErrGoldenValues)
	}
	if signer == nil {
		return nil, fmt.Errorf("%w: a signer is required", ErrGoldenValues)
	}
	alg, err := coseAlgorithmOf(signer.Public())
	if err != nil {
		return nil, err
	}
	byIndex := map[uint32][]GoldenPCR{}
	for _, p := range gv.PCRs {
		if _, ok := goldenDigestAlgs[p.Hash]; !ok {
			return nil, fmt.Errorf("%w: PCR %d: bank %v is not supported", ErrGoldenValues, p.Index, p.Hash)
		}
		if len(p.Value) != p.Hash.Size() {
			return nil, fmt.Errorf("%w: PCR %d: a %v value is %d bytes, not %d", ErrGoldenValues, p.Index, p.Hash, p.Hash.Size(), len(p.Value))
		}
		byIndex[p.Index] = append(byIndex[p.Index], p)
	}
	indices := make([]uint32, 0, len(byIndex))
	for idx := range byIndex {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })

	measurements := comid.NewMeasurements()
	for _, idx := range indices {
		m, err := comid.NewMeasurement(uint64(idx), comid.UintType)
		if err != nil {
			return nil, fmt.Errorf("%w: measurement: %w", ErrGoldenValues, err)
		}
		for _, p := range byIndex[idx] {
			m.AddDigest(goldenDigestAlgs[p.Hash], p.Value)
		}
		measurements.AddMeasurement(m)
	}
	class := comid.NewClassOID(goldenValuesClassOID)
	if gv.Vendor != "" {
		class.SetVendor(gv.Vendor)
	}
	if gv.Model != "" {
		class.SetModel(gv.Model)
	}
	c := comid.NewComid().SetTagIdentity(gv.ID, 0).AddReferenceValue(comid.ReferenceValue{
		Environment:  comid.Environment{Class: class},
		Measurements: *measurements,
	})
	if c == nil {
		return nil, fmt.Errorf("%w: comid", ErrGoldenValues)
	}
	if err := c.Valid(); err != nil {
		return nil, fmt.Errorf("%w: comid: %w", ErrGoldenValues, err)
	}
	profile := gv.Profile
	if profile == "" {
		profile = GoldenValuesProfile
	}
	unsigned := corim.NewUnsignedCorim().SetID(gv.ID).AddComid(*c).AddProfile(profile)
	if unsigned == nil {
		return nil, fmt.Errorf("%w: corim", ErrGoldenValues)
	}
	payload, err := unsigned.ToCBOR()
	if err != nil {
		return nil, fmt.Errorf("%w: corim: %w", ErrGoldenValues, err)
	}
	provider := gv.Signer
	if provider == "" {
		provider = "go-trusted-platform golden values"
	}
	meta, err := corim.NewMeta().SetSigner(provider, nil).ToCBOR()
	if err != nil {
		return nil, fmt.Errorf("%w: corim meta: %w", ErrGoldenValues, err)
	}

	msg := cose.NewSign1Message()
	msg.Payload = payload
	msg.Headers.Protected.SetAlgorithm(alg)
	msg.Headers.Protected[cose.HeaderLabelContentType] = corim.ContentType
	msg.Headers.Protected[corim.HeaderLabelCorimMeta] = meta
	if len(chain) > 0 {
		ders := make([]any, len(chain))
		for i, cert := range chain {
			ders[i] = cert.Raw
		}
		msg.Headers.Protected[cose.HeaderLabelX5Chain] = ders
	}
	coseSigner, err := cose.NewSigner(alg, signer)
	if err != nil {
		return nil, fmt.Errorf("%w: cose signer: %w", ErrGoldenValues, err)
	}
	if err := msg.Sign(rand.Reader, nil, coseSigner); err != nil {
		return nil, fmt.Errorf("%w: cose sign: %w", ErrGoldenValues, err)
	}
	return msg.MarshalCBOR()
}

// coseAlgorithmOf returns the COSE signature algorithm for a public key.
func coseAlgorithmOf(pub crypto.PublicKey) (cose.Algorithm, error) {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		switch k.Curve {
		case elliptic.P256():
			return cose.AlgorithmES256, nil
		case elliptic.P384():
			return cose.AlgorithmES384, nil
		case elliptic.P521():
			return cose.AlgorithmES512, nil
		}
	case *rsa.PublicKey:
		return cose.AlgorithmPS256, nil
	case ed25519.PublicKey:
		return cose.AlgorithmEdDSA, nil
	}
	return 0, fmt.Errorf("%w: unsupported signer key %T", ErrGoldenValues, pub)
}

// ImportGoldenValues is the one-time import of go-trusted-platform's golden
// values: it signs gv and stores it in the tenant's go-xkms reference-values
// store under name, where AppraiseQuote and the launch-time PCR checks read
// it and go-trusted-platform reads it back through SDK().Security().
func ImportGoldenValues(ctx context.Context, svc AttestationService, name string, gv *GoldenValues, signer crypto.Signer, chain []*x509.Certificate) (*ReferenceValues, error) {
	if svc == nil {
		return nil, fmt.Errorf("%w: an attestation service is required", ErrGoldenValues)
	}
	signed, err := SignGoldenValues(gv, signer, chain)
	if err != nil {
		return nil, err
	}
	return svc.PutReferenceValues(ctx, &PutReferenceValuesRequest{Name: name, SignedCoRIM: signed})
}
