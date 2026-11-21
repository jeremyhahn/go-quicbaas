package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSPIFFEWorkloadName(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"web-01.prod":       "web-01.prod",
		"admin@example.com": "admin_40example.com",
		"a_b":               "a_5fb",
		"a b/c":             "a_20b_2fc",
		".":                 "_2e",
		"..":                "_2e_2e",
		"é":                 "_c3_a9",
	}
	for in, want := range cases {
		assert.Equal(t, want, SPIFFEWorkloadName(in), "SPIFFEWorkloadName(%q)", in)
	}
	// Injective: a CN that already looks escaped does not collide.
	assert.NotEqual(t, SPIFFEWorkloadName("a@b"), SPIFFEWorkloadName("a_40b"))
}

func TestCSRCommonName(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "admin@example.com"},
	}, key)
	require.NoError(t, err)
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})

	assert.Equal(t, "admin@example.com", CSRCommonName(csrPEM))
	assert.Equal(t, "", CSRCommonName([]byte("not pem")))
	assert.Equal(t, "", CSRCommonName(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: []byte{1, 2}})))
}
