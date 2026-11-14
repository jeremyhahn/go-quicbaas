// Package provenance is the software-factory provenance of a container
// image: an in-toto statement carrying a SLSA v1 provenance predicate,
// signed as a DSSE envelope and stored in the registry as an OCI referrer
// of the image it describes.
//
// The package holds the formats and the registry client. Signing takes a
// crypto.Signer (a go-xkms key); verification is the Verifier's, in
// pkg/service, which anchors the signer at the tenant's image-signer
// endorsements and checks the signature with the go-xkms attestation
// verifier.
package provenance

import (
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
)

// Media and payload types.
const (
	// PayloadTypeInToto is the DSSE payload type of an in-toto statement.
	PayloadTypeInToto = "application/vnd.in-toto+json"
	// MediaTypeEnvelope is the OCI media type of a DSSE envelope; it is
	// also the referrer's artifact type.
	MediaTypeEnvelope = "application/vnd.dsse.envelope.v1+json"
	// MediaTypeSignerChain is the OCI media type of the signer's PEM
	// certificate chain, leaf first.
	MediaTypeSignerChain = "application/x-pem-file"
)

// Envelope is a DSSE envelope. Payload and signatures are base64 on the
// wire, as encoding/json encodes byte slices.
type Envelope struct {
	PayloadType string      `json:"payloadType"`
	Payload     []byte      `json:"payload"`
	Signatures  []Signature `json:"signatures"`
}

// Signature is one DSSE signature over the envelope's PAE.
type Signature struct {
	KeyID string `json:"keyid,omitempty"`
	Sig   []byte `json:"sig"`
}

// PAE is the DSSE v1 pre-authentication encoding of a payload: the bytes a
// DSSE signature covers.
func PAE(payloadType string, payload []byte) []byte {
	out := []byte("DSSEv1 ")
	out = strconv.AppendInt(out, int64(len(payloadType)), 10)
	out = append(out, ' ')
	out = append(out, payloadType...)
	out = append(out, ' ')
	out = strconv.AppendInt(out, int64(len(payload)), 10)
	out = append(out, ' ')
	return append(out, payload...)
}

// PAE returns the bytes the envelope's signatures cover.
func (e *Envelope) PAE() []byte { return PAE(e.PayloadType, e.Payload) }

// Signed is a signed provenance envelope and the chain of the key that
// signed it, leaf first, as stored in the registry.
type Signed struct {
	Envelope       Envelope
	SignerChainPEM [][]byte
}

// ErrUnsigned is returned for an envelope with no signature.
var ErrUnsigned = errors.New("provenance: the envelope has no signature")

// Sign signs stmt as a DSSE envelope with signer, a go-xkms key whose
// certificate chain (leaf first) is chain. The signature is over the
// SHA-256 of the PAE.
func Sign(stmt *Statement, signer crypto.Signer, chain []*x509.Certificate) (*Signed, error) {
	if stmt == nil || signer == nil || len(chain) == 0 {
		return nil, errors.New("provenance: a statement, a signer and its certificate chain are required")
	}
	if err := stmt.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(stmt)
	if err != nil {
		return nil, fmt.Errorf("provenance: encode statement: %w", err)
	}
	digest := sha256.Sum256(PAE(PayloadTypeInToto, payload))
	sig, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("provenance: sign: %w", err)
	}
	pems := make([][]byte, len(chain))
	for i, cert := range chain {
		pems[i] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	}
	keyID := sha256.Sum256(chain[0].Raw)
	return &Signed{
		Envelope: Envelope{
			PayloadType: PayloadTypeInToto,
			Payload:     payload,
			Signatures:  []Signature{{KeyID: fmt.Sprintf("sha256:%x", keyID), Sig: sig}},
		},
		SignerChainPEM: pems,
	}, nil
}

// Statement decodes the envelope's in-toto statement. It does not verify
// the signature.
func (s *Signed) Statement() (*Statement, error) {
	if s.Envelope.PayloadType != PayloadTypeInToto {
		return nil, fmt.Errorf("provenance: payload type %q is not %q", s.Envelope.PayloadType, PayloadTypeInToto)
	}
	var stmt Statement
	if err := json.Unmarshal(s.Envelope.Payload, &stmt); err != nil {
		return nil, fmt.Errorf("provenance: decode statement: %w", err)
	}
	if err := stmt.Validate(); err != nil {
		return nil, err
	}
	return &stmt, nil
}
