package server

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// serverCertSource says where the server TLS certificate comes from.
type serverCertSource int

const (
	// serverCertUnconfigured: neither security.tls_cert_file nor
	// security.tls_key_file is set, so the server issues its own
	// certificate into the default certs directory.
	serverCertUnconfigured serverCertSource = iota
	// serverCertOperator: the configured files hold a certificate this
	// server's CA did not issue. It is served as given.
	serverCertOperator
	// serverCertSelfIssued: the configured files hold the certificate this
	// server's CA stores for the server CN. It stays managed: the SAN check
	// runs on it and a re-issued certificate is written back to the
	// configured paths.
	serverCertSelfIssued
)

// Config keys named in ServerCertPathError.
const (
	tlsCertFileKey = "security.tls_cert_file"
	tlsKeyFileKey  = "security.tls_key_file"
)

// serverCertProvenance decides how the server TLS certificate is sourced
// from the configured security.tls_cert_file and security.tls_key_file.
//
//   - neither path configured: serverCertUnconfigured;
//   - one path configured: a ServerCertPathError (unset);
//   - a configured path absent: a ServerCertPathError (absent);
//   - both files present: serverCertSelfIssued when the leaf equals, byte
//     for byte, the certificate ca stores for cn and chains to ca, otherwise
//     serverCertOperator.
//
// The stored certificate is read through the same TrustedCA calls
// issueServerCert uses, so provenance needs no record beyond the CA's own.
func serverCertProvenance(ca xkmssdk.TrustedCA, certFile, keyFile, cn string) (serverCertSource, error) {
	if certFile == "" && keyFile == "" {
		return serverCertUnconfigured, nil
	}
	if missing := unsetCertPaths(certFile, keyFile); len(missing) > 0 {
		return 0, &ServerCertPathError{CertFile: certFile, KeyFile: keyFile, Problem: ServerCertPathUnset, Keys: missing}
	}
	missing, err := absentCertPaths(certFile, keyFile)
	if err != nil {
		return 0, err
	}
	if len(missing) > 0 {
		return 0, &ServerCertPathError{CertFile: certFile, KeyFile: keyFile, Problem: ServerCertPathAbsent, Keys: missing}
	}

	// A pair that does not load, or whose key does not match its leaf, is
	// not one this server wrote; the operator path reports the load error.
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil || len(pair.Certificate) == 0 {
		return serverCertOperator, nil
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return serverCertOperator, nil
	}
	chains, err := chainsToCA(ca, leaf)
	if err != nil {
		return 0, err
	}
	if !chains {
		return serverCertOperator, nil
	}
	stored, err := ca.CertStore().GetCertificate(cn)
	if err != nil {
		if xkmssdk.IsNotFound(err) {
			return serverCertOperator, nil
		}
		return 0, api.NewOperationFailedError("load stored server TLS certificate", err)
	}
	if stored == nil || !bytes.Equal(stored.Raw, leaf.Raw) {
		return serverCertOperator, nil
	}
	return serverCertSelfIssued, nil
}

// unsetCertPaths names the cert or key path left unset when the other is set.
func unsetCertPaths(certFile, keyFile string) []string {
	var unset []string
	if certFile == "" {
		unset = append(unset, tlsCertFileKey)
	}
	if keyFile == "" {
		unset = append(unset, tlsKeyFileKey)
	}
	return unset
}

// absentCertPaths names each configured path with no file behind it.
func absentCertPaths(certFile, keyFile string) ([]string, error) {
	var absent []string
	for _, p := range []struct{ key, path string }{{tlsCertFileKey, certFile}, {tlsKeyFileKey, keyFile}} {
		_, err := os.Stat(p.path)
		if errors.Is(err, os.ErrNotExist) {
			absent = append(absent, p.key)
			continue
		}
		if err != nil {
			return nil, api.NewDataDirOperationError("server.initializeCA", "stat configured TLS file", p.path, err)
		}
	}
	return absent, nil
}

// chainsToCA reports whether leaf verifies against ca's bundle.
func chainsToCA(ca xkmssdk.TrustedCA, leaf *x509.Certificate) (bool, error) {
	bundle, err := ca.CABundle()
	if err != nil {
		return false, api.NewOperationFailedError("get CA bundle", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundle) {
		return false, nil
	}
	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	return err == nil, nil
}
