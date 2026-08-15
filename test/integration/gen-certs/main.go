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
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const (
	nodeCount     = 5
	caValidYears  = 5
	nodeValidDays = 365

	// spiffeTrustDomain is the trust domain embedded in the tenant-scoped SPIFFE
	// URI SAN of integration client certificates. After the login hard cutover,
	// PIV/bearer login derives the tenant from this SAN
	// (spiffe://<td>/tenant/<id>/<kind>/<name>); the daemon leaves its pinned
	// trust domain empty in tests, so any trust domain here is accepted.
	spiffeTrustDomain = "quicbaas.integration"
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

	// Regenerating over a complete set would mint a second CA under the same
	// subject name, leaving nodes that already loaded the first one presenting
	// leaves the new CA cannot verify. That reads as "x509: certificate signed
	// by unknown authority" against an authority the client can name, which
	// looks like a broken cluster rather than rotated trust. Compose starts
	// this service once for `up` and again for the later `run`, so the second
	// execution has to be a no-op. Material stays per-run because the volume
	// is destroyed with `down --volumes` before each run, not because this
	// program rewrites it.
	if files := expectedFiles(outDir); allPresent(files) {
		fmt.Printf("Certificates already present in %s (%d files); leaving them alone\n", outDir, len(files)) //nolint:forbidigo // CLI tool output
		return
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
		dockerAlias := fmt.Sprintf("quicbaas-node%d", i)

		nodeCertDER, nodeKeyDER, err := generateNodeCert(caKey, caCert, hostname, alias, dockerAlias)
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
	adminCertDER, adminKeyDER, err := generateClientCert(caKey, caCert, "admin@integration.test", "admin", "system")
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
// expectedFiles returns every path a complete generation writes into outDir.
func expectedFiles(outDir string) []string {
	files := []string{
		filepath.Join(outDir, "ca.crt"),
		filepath.Join(outDir, "ca.key"),
	}
	for i := 1; i <= nodeCount; i++ {
		files = append(files,
			filepath.Join(outDir, fmt.Sprintf("node%d.crt", i)),
			filepath.Join(outDir, fmt.Sprintf("node%d.key", i)),
		)
	}
	return append(files,
		filepath.Join(outDir, "admin.crt"),
		filepath.Join(outDir, "admin.key"),
	)
}

// allPresent reports whether every path exists. A partial set is treated as
// absent so that an interrupted generation is redone rather than half-used.
func allPresent(files []string) bool {
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			return false
		}
	}
	return true
}

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
// hostname is the canonical hostname (e.g. "test-node1"), alias is the short name
// (e.g. "node1"), and dockerAlias is the Docker service name (e.g. "quicbaas-node1").
// All three are added as SANs so the certificate is valid for both local testing
// and Docker Compose deployments.
// It returns the DER-encoded certificate and the DER-encoded private key.
func generateNodeCert(caKey *ecdsa.PrivateKey, caCert *x509.Certificate, hostname, alias, dockerAlias string) ([]byte, []byte, error) {
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
			dockerAlias,
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
func generateClientCert(caKey *ecdsa.PrivateKey, caCert *x509.Certificate, email, friendlyName, tenantID string) ([]byte, []byte, error) {
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

	// After the login hard cutover, the tenant is derived from a tenant-scoped
	// SPIFFE URI SAN. Embedding it here lets PIV/bearer login (e.g. the MCP
	// multiprotocol client) resolve the tenant from the client certificate.
	// Adding a URI SAN does not change the SubjectPublicKeyInfo, so the cert's
	// SPKI thumbprint (the cert-binding identifier) is unchanged.
	if tenantID != "" {
		template.URIs = []*url.URL{{
			Scheme: "spiffe",
			Host:   spiffeTrustDomain,
			Path:   "/tenant/" + tenantID + "/user/" + friendlyName,
		}}
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
