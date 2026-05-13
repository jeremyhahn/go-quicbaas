// gen-certs generates TLS certificates for the QuicBaaS integration test cluster.
//
// Usage:
//
//	go run test/integration/gen-certs/main.go [output-dir]
//
// If output-dir is omitted it defaults to test/integration/certs/.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	nodeCount     = 5
	caValidYears  = 5
	nodeValidDays = 365
)

// CertGenerationError represents a failure during certificate generation.
type CertGenerationError struct {
	Operation string
	Detail    string
	Cause     error
}

func (e *CertGenerationError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Operation, e.Detail, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Operation, e.Detail)
}

func (e *CertGenerationError) Unwrap() error {
	return e.Cause
}

// FileWriteError represents a failure writing a file to disk.
type FileWriteError struct {
	Path  string
	Cause error
}

func (e *FileWriteError) Error() string {
	return fmt.Sprintf("write %s: %v", e.Path, e.Cause)
}

func (e *FileWriteError) Unwrap() error {
	return e.Cause
}

func main() {
	outDir := "test/integration/certs"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil { //nolint:gosec // 0o755 is appropriate for a certificate output directory
		log.Fatalf("Failed to create output directory %s: %v", outDir, err) //nolint:gosec // CLI tool with controlled input
	}

	// Generate CA key pair and self-signed certificate.
	caKey, caCertDER, caCert, err := generateCA()
	if err != nil {
		log.Fatalf("Failed to generate CA: %v", err)
	}

	// Marshal CA private key.
	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		log.Fatalf("Failed to marshal CA key: %v", err)
	}

	// Write CA files.
	caCertPath := filepath.Join(outDir, "ca.crt")
	caKeyPath := filepath.Join(outDir, "ca.key")

	if err := writePEM(caCertPath, "CERTIFICATE", caCertDER, 0o644); err != nil {
		log.Fatalf("Failed to write CA cert: %v", err)
	}
	if err := writePEM(caKeyPath, "EC PRIVATE KEY", caKeyDER, 0o600); err != nil {
		log.Fatalf("Failed to write CA key: %v", err)
	}
	fmt.Printf("Generated CA certificate: %s\n", caCertPath) //nolint:forbidigo // CLI tool output
	fmt.Printf("Generated CA private key: %s\n", caKeyPath)  //nolint:forbidigo // CLI tool output

	// Generate node certificates for nodes 1 through 5.
	var generated []string
	generated = append(generated, caCertPath, caKeyPath)

	for i := 1; i <= nodeCount; i++ {
		hostname := fmt.Sprintf("test-node%d", i)
		alias := fmt.Sprintf("node%d", i)

		nodeCertDER, nodeKeyDER, err := generateNodeCert(caKey, caCert, hostname, alias)
		if err != nil {
			log.Fatalf("Failed to generate node%d certificate: %v", i, err)
		}

		nodeCertPath := filepath.Join(outDir, fmt.Sprintf("node%d.crt", i))
		nodeKeyPath := filepath.Join(outDir, fmt.Sprintf("node%d.key", i))

		if err := writePEM(nodeCertPath, "CERTIFICATE", nodeCertDER, 0o644); err != nil {
			log.Fatalf("Failed to write node%d cert: %v", i, err)
		}
		if err := writePEM(nodeKeyPath, "EC PRIVATE KEY", nodeKeyDER, 0o600); err != nil {
			log.Fatalf("Failed to write node%d key: %v", i, err)
		}

		fmt.Printf("Generated node%d certificate: %s\n", i, nodeCertPath) //nolint:forbidigo // CLI tool output
		fmt.Printf("Generated node%d private key: %s\n", i, nodeKeyPath)  //nolint:forbidigo // CLI tool output

		generated = append(generated, nodeCertPath, nodeKeyPath)
	}

	// Generate an admin client certificate that integration tests use to
	// authenticate via mTLS / PIV against the test cluster. The CN is the
	// bootstrap admin's email; the cluster's bootstrap path computes the
	// SHA-256 thumbprint and registers it on the admin user so the PIV login
	// endpoint can resolve the bearer.
	adminCertDER, adminKeyDER, err := generateClientCert(caKey, caCert, "admin@integration.test", "admin")
	if err != nil {
		log.Fatalf("Failed to generate admin client certificate: %v", err)
	}

	adminCertPath := filepath.Join(outDir, "admin.crt")
	adminKeyPath := filepath.Join(outDir, "admin.key")

	if err := writePEM(adminCertPath, "CERTIFICATE", adminCertDER, 0o644); err != nil {
		log.Fatalf("Failed to write admin cert: %v", err)
	}
	if err := writePEM(adminKeyPath, "EC PRIVATE KEY", adminKeyDER, 0o600); err != nil {
		log.Fatalf("Failed to write admin key: %v", err)
	}

	fmt.Printf("Generated admin client certificate: %s\n", adminCertPath) //nolint:forbidigo // CLI tool output
	fmt.Printf("Generated admin client private key: %s\n", adminKeyPath)  //nolint:forbidigo // CLI tool output
	generated = append(generated, adminCertPath, adminKeyPath)

	// Print summary.
	fmt.Println()                                        //nolint:forbidigo // CLI tool output
	fmt.Println("--- Summary ---")                       //nolint:forbidigo // CLI tool output
	fmt.Printf("Output directory: %s\n", outDir)         //nolint:forbidigo // CLI tool output
	fmt.Printf("Files generated:  %d\n", len(generated)) //nolint:forbidigo // CLI tool output
	for _, f := range generated {
		fmt.Printf("  %s\n", f) //nolint:forbidigo // CLI tool output
	}
}

// generateCA creates a self-signed ECDSA P-256 CA certificate.
func generateCA() (*ecdsa.PrivateKey, []byte, *x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, &CertGenerationError{
			Operation: "generate CA key",
			Detail:    "ecdsa.GenerateKey failed",
			Cause:     err,
		}
	}

	serialNumber, err := randomSerialNumber()
	if err != nil {
		return nil, nil, nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"QuicBaaS Integration Tests"},
			CommonName:   "QuicBaaS Test CA",
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(caValidYears, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, &CertGenerationError{
			Operation: "create CA certificate",
			Detail:    "x509.CreateCertificate failed",
			Cause:     err,
		}
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, nil, &CertGenerationError{
			Operation: "parse CA certificate",
			Detail:    "x509.ParseCertificate failed",
			Cause:     err,
		}
	}

	return key, certDER, cert, nil
}

// generateNodeCert creates a leaf certificate signed by the CA for a cluster node.
// It returns the DER-encoded certificate and the DER-encoded private key.
func generateNodeCert(caKey *ecdsa.PrivateKey, caCert *x509.Certificate, hostname, alias string) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, &CertGenerationError{
			Operation: "generate node key",
			Detail:    fmt.Sprintf("ecdsa.GenerateKey failed for %s", hostname),
			Cause:     err,
		}
	}

	serialNumber, err := randomSerialNumber()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"QuicBaaS Integration Tests"},
			CommonName:   hostname,
		},
		NotBefore: now,
		NotAfter:  now.AddDate(0, 0, nodeValidDays),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		DNSNames: []string{
			"localhost",
			hostname,
			alias,
		},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("::1"),
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, &CertGenerationError{
			Operation: "create node certificate",
			Detail:    fmt.Sprintf("x509.CreateCertificate failed for %s", hostname),
			Cause:     err,
		}
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, &CertGenerationError{
			Operation: "marshal node key",
			Detail:    fmt.Sprintf("x509.MarshalECPrivateKey failed for %s", hostname),
			Cause:     err,
		}
	}

	return certDER, keyDER, nil
}

// generateClientCert creates a leaf certificate signed by the CA for a
// client identity (e.g. integration-test admin). The cert is suitable for
// TLS client auth and PIV login: ClientAuth ExtKeyUsage, the principal's
// email/UPN in Subject CommonName, and the email also placed in the SAN
// EmailAddresses list so server-side identity resolvers can read it.
func generateClientCert(caKey *ecdsa.PrivateKey, caCert *x509.Certificate, email, friendlyName string) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, &CertGenerationError{
			Operation: "generate client key",
			Detail:    fmt.Sprintf("ecdsa.GenerateKey failed for %s", friendlyName),
			Cause:     err,
		}
	}

	serialNumber, err := randomSerialNumber()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"QuicBaaS Integration Tests"},
			CommonName:   email,
		},
		NotBefore: now,
		NotAfter:  now.AddDate(0, 0, nodeValidDays),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
		},
		EmailAddresses: []string{email},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, &CertGenerationError{
			Operation: "create client certificate",
			Detail:    fmt.Sprintf("x509.CreateCertificate failed for %s", friendlyName),
			Cause:     err,
		}
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, &CertGenerationError{
			Operation: "marshal client key",
			Detail:    fmt.Sprintf("x509.MarshalECPrivateKey failed for %s", friendlyName),
			Cause:     err,
		}
	}

	return certDER, keyDER, nil
}

// writePEM encodes data as a PEM block and writes it to path with the given permissions.
func writePEM(path, blockType string, data []byte, perm os.FileMode) error {
	block := &pem.Block{
		Type:  blockType,
		Bytes: data,
	}

	encoded := pem.EncodeToMemory(block)
	if encoded == nil {
		return &FileWriteError{
			Path:  path,
			Cause: fmt.Errorf("pem.EncodeToMemory returned nil"),
		}
	}

	if err := os.WriteFile(path, encoded, perm); err != nil {
		return &FileWriteError{
			Path:  path,
			Cause: err,
		}
	}

	return nil
}

// randomSerialNumber generates a random serial number for an X.509 certificate.
// Serial numbers are 128-bit random values as recommended by RFC 5280.
func randomSerialNumber() (*big.Int, error) {
	// 128 bits provides sufficient uniqueness for test certificates.
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, &CertGenerationError{
			Operation: "generate serial number",
			Detail:    "crypto/rand.Int failed",
			Cause:     err,
		}
	}
	return serial, nil
}
