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

package certfixture

import "errors"

// Sentinels for the conditions a caller can act on. Everything else is
// reported as one of the typed errors below, which carry the failing operation
// and the path or identity involved.
var (
	// ErrNoDir is returned when no CA directory is supplied. The fixture CA is
	// deliberately persistent: the material is issued by one program and
	// extended by the suites that run against it, so an in-memory CA would
	// leave the suite unable to issue anything that chains to the cluster's
	// trust anchor.
	ErrNoDir = errors.New("certfixture: CA directory is required")

	// ErrNoCommonName is returned when a fixture names no subject common name.
	ErrNoCommonName = errors.New("certfixture: certificate common name is required")

	// ErrNoFileStem is returned when a fixture names no output file stem.
	ErrNoFileStem = errors.New("certfixture: fixture file stem is required")

	// ErrNoTrustDomain is returned when a tenant-scoped client fixture is
	// requested from an authority with no SPIFFE trust domain. The certificate
	// would carry no tenant SAN, and login derives the tenant from that SAN, so
	// issuing one would produce a credential that cannot log in.
	ErrNoTrustDomain = errors.New("certfixture: tenant-scoped client requires a SPIFFE trust domain")

	// ErrNoPrivateKey is returned when the CA answers an issuance without the
	// requester's private key. It cannot happen on the adapter's issuance path
	// (the key is generated outside the keystore precisely so the requester can
	// hold it) and is checked because a fixture without a key is unusable.
	ErrNoPrivateKey = errors.New("certfixture: issuance returned no private key")

	// ErrNoTopologyNodes is returned when a topology declares no nodes.
	ErrNoTopologyNodes = errors.New("certfixture: topology declares no nodes")

	// ErrNotASigner is returned when the decoded private key cannot sign, which
	// would make the credential useless for TLS or for a PIV challenge.
	ErrNotASigner = errors.New("certfixture: private key is not a crypto.Signer")
)

// IssueError reports a failed certificate issuance, naming the identity that
// could not be issued.
type IssueError struct {
	// CommonName is the subject the issuance was for.
	CommonName string

	// Cause is the underlying failure.
	Cause error
}

func (e *IssueError) Error() string {
	return "certfixture: issue " + e.CommonName + ": " + e.Cause.Error()
}

func (e *IssueError) Unwrap() error { return e.Cause }

// AuthorityError reports a failure building or opening the fixture CA.
type AuthorityError struct {
	// Op names the step that failed ("open ca", "ca service", "ca certificate").
	Op string

	// Dir is the CA directory involved, if any.
	Dir string

	// Cause is the underlying failure.
	Cause error
}

func (e *AuthorityError) Error() string {
	if e.Dir == "" {
		return "certfixture: " + e.Op + ": " + e.Cause.Error()
	}
	return "certfixture: " + e.Op + " (" + e.Dir + "): " + e.Cause.Error()
}

func (e *AuthorityError) Unwrap() error { return e.Cause }

// FileError reports a failure reading or writing a fixture file.
type FileError struct {
	// Op names the step that failed ("write", "read", "create directory").
	Op string

	// Path is the file or directory involved.
	Path string

	// Cause is the underlying failure.
	Cause error
}

func (e *FileError) Error() string {
	return "certfixture: " + e.Op + " " + e.Path + ": " + e.Cause.Error()
}

func (e *FileError) Unwrap() error { return e.Cause }

// DecodeError reports material the CA produced that could not be decoded. It
// stands for a real divergence between what this package expects and what the
// CA emits, so it is reported rather than papered over.
type DecodeError struct {
	// What names the material ("certificate", "private key").
	What string

	// Cause is the underlying failure, or nil when the PEM was simply absent.
	Cause error
}

func (e *DecodeError) Error() string {
	if e.Cause == nil {
		return "certfixture: decode " + e.What + ": no PEM block"
	}
	return "certfixture: decode " + e.What + ": " + e.Cause.Error()
}

func (e *DecodeError) Unwrap() error { return e.Cause }

// SPIFFENameCollisionError reports two fixtures of one topology whose common
// names derive the same SPIFFE ID, spiffe://<TenantID>/<Kind>/<Name>. Each
// principal needs its own ID, so the topology is refused before anything is
// issued.
type SPIFFENameCollisionError struct {
	// TenantID is the trust domain both IDs share.
	TenantID string

	// Kind is the SPIFFE kind both IDs share ("user" or "svc").
	Kind string

	// Name is the SPIFFEName both common names derive.
	Name string

	// First and Second are the two common names, in topology order.
	First, Second string
}

func (e *SPIFFENameCollisionError) Error() string {
	return "certfixture: " + e.First + " and " + e.Second +
		" both derive spiffe://" + e.TenantID + "/" + e.Kind + "/" + e.Name
}
