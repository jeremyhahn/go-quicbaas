// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

// Package acme re-exports the go-xkms ACME (RFC 8555) SDK
// surface so downstream consumers (go-trusted-platform's CA
// adapter, ceremony service, ACME storage backends) talk to
// ACME through go-quicbaas rather than reaching into go-xkms
// directly.
//
// Coverage covers ACME entities (Account, Order, Authorization,
// Challenge, Certificate, EAB, Nonce, Tenant), client types and
// configuration, the cross-sign config, and the standard
// challenge verifiers (DNS-01, HTTP-01, the platform-specific
// Endorse-01 and Enroll-01 ceremonies).

package acme

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	acmeclient "github.com/jeremyhahn/go-xkms/pkg/acme/client"
	acmeentities "github.com/jeremyhahn/go-xkms/pkg/acme/entities"
	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"
)

// Type aliases
type (
	AccountClient             = xkmsacme.AccountClient
	ACMEAccountEntity         = xkmsacme.ACMEAccountEntity
	ACMEAuthorizationEntity   = xkmsacme.ACMEAuthorizationEntity
	ACMECertificateEntity     = xkmsacme.ACMECertificateEntity
	ACMECertificateRequest    = xkmsacme.ACMECertificateRequest
	ACMEChallengeEntity       = xkmsacme.ACMEChallengeEntity
	ACMEChallengeVerifierFunc = xkmsacme.ACMEChallengeVerifierFunc
	ACMEClient                = xkmsacme.ACMEClient
	ACMEClientConfig          = xkmsacme.ACMEClientConfig
	// ACMEClientYAMLConfig is the `acme.client` YAML section referenced by
	// ACMEConfig.Client. It is distinct from ACMEClientConfig, which is the
	// runtime/transport configuration handed to NewACMEClient.
	ACMEClientYAMLConfig          = acmeclient.ACMEClientConfig
	ACMEClientParams              = xkmsacme.ACMEClientParams
	ACMEConfig                    = xkmsacme.ACMEConfig
	ACMECrossSignConfig           = xkmsacme.ACMECrossSignConfig
	ACMEEnrollmentConfig          = xkmsacme.ACMEEnrollmentConfig
	ACMEExternalAccountBinding    = xkmsacme.ACMEExternalAccountBinding
	ACMEKeyChangeRequest          = xkmsacme.ACMEKeyChangeRequest
	ACMENewAccountRequest         = xkmsacme.ACMENewAccountRequest
	ACMENonceEntity               = xkmsacme.ACMENonceEntity
	ACMEOrderEntity               = xkmsacme.ACMEOrderEntity
	ACMEOrderFinalizeHook         = xkmsacme.ACMEOrderFinalizeHook
	ACMEOrderFinalizeHookContext  = xkmsacme.ACMEOrderFinalizeHookContext
	ACMERateLimiter               = xkmsacme.ACMERateLimiter
	ACMERestServiceParams         = xkmsacme.ACMERestServiceParams
	ACMERestServicer              = xkmsacme.ACMERestServicer
	ACMEStorageFactory            = xkmsacme.ACMEStorageFactory
	ACMETCGEnrollmentData         = xkmsacme.ACMETCGEnrollmentData
	ACMEOrderPreSignHook          = xkmsacme.ACMEOrderPreSignHook
	ACMEOrderPreSignHookContext   = xkmsacme.ACMEOrderPreSignHookContext
	ACMEOrderPreSignHookRegistry  = xkmsacme.ACMEOrderPreSignHookRegistry
	ACMEErrPreSignHookFailed      = xkmsacme.ACMEErrPreSignHookFailed
	ACMETenant                    = xkmsacme.ACMETenant
	ACMETenantAwareFactory        = xkmsacme.ACMETenantAwareFactory
	AdminClient                   = xkmsacme.AdminClient
	AuthorizationClient           = xkmsacme.AuthorizationClient
	CACertificateAuthority        = xkmsacme.CACertificateAuthority
	CACertificateRequest          = xkmsacme.CACertificateRequest
	CAConfig                      = xkmsacme.CAConfig
	CAIdentity                    = xkmsacme.CAIdentity
	CAKMSClient                   = xkmsacme.CAKMSClient
	CAKMSGenerateKeyRequest       = xkmsacme.CAKMSGenerateKeyRequest
	CAKMSGenerateKeyResponse      = xkmsacme.CAKMSGenerateKeyResponse
	CAKMSKeyAlgorithm             = xkmsacme.CAKMSKeyAlgorithm
	CAKMSKeyInfo                  = xkmsacme.CAKMSKeyInfo
	CAKMSKeyUsage                 = xkmsacme.CAKMSKeyUsage
	CAKMSListKeysResponse         = xkmsacme.CAKMSListKeysResponse
	CAKMSRotateKeyResponse        = xkmsacme.CAKMSRotateKeyResponse
	CASubject                     = xkmsacme.CASubject
	CASubjectAlternativeNames     = xkmsacme.CASubjectAlternativeNames
	CertificateClient             = xkmsacme.CertificateClient
	ChallengeClient               = xkmsacme.ChallengeClient
	Client                        = xkmsacme.Client
	DAOCondition                  = xkmsacme.DAOCondition
	DAOConditionFailedError       = xkmsacme.DAOConditionFailedError
	DAODeserializationFailedError = xkmsacme.DAODeserializationFailedError
	DAOEntity                     = xkmsacme.DAOEntity
	DAOInvalidEntityIDError       = xkmsacme.DAOInvalidEntityIDError
	DAONotFoundError              = xkmsacme.DAONotFoundError
	DAOPageQuery                  = xkmsacme.DAOPageQuery
	DAOSerializationFailedError   = xkmsacme.DAOSerializationFailedError
	DirectoryClient               = xkmsacme.DirectoryClient
	NonceClient                   = xkmsacme.NonceClient
	Option                        = xkmsacme.Option
	OrderClient                   = xkmsacme.OrderClient
	TLSConfig                     = xkmsacme.TLSConfig
)

// Constants
const (
	CAKMSAlgorithmECDSAP256 = xkmsacme.CAKMSAlgorithmECDSAP256
	CAKMSAlgorithmECDSAP384 = xkmsacme.CAKMSAlgorithmECDSAP384
	CAKMSAlgorithmED25519   = xkmsacme.CAKMSAlgorithmED25519
	CAKMSAlgorithmRSA2048   = xkmsacme.CAKMSAlgorithmRSA2048
	CAKMSAlgorithmRSA4096   = xkmsacme.CAKMSAlgorithmRSA4096
	CAKMSKeyUsageDecrypt    = xkmsacme.CAKMSKeyUsageDecrypt
	CAKMSKeyUsageEncrypt    = xkmsacme.CAKMSKeyUsageEncrypt
	CAKMSKeyUsageSign       = xkmsacme.CAKMSKeyUsageSign
	CAKMSKeyUsageVerify     = xkmsacme.CAKMSKeyUsageVerify
	DAOConditionValue       = xkmsacme.DAOConditionValue
	DAOOpNotEqual           = xkmsacme.DAOOpNotEqual

	// ACMETenantStatus* mirror the lifecycle states carried by
	// ACMETenant.Status. The go-xkms SDK frontage aliases the Tenant
	// entity but not its status constants, so they are promoted from
	// the canonical entities package here.
	ACMETenantStatusActive    = acmeentities.TenantStatusActive
	ACMETenantStatusSuspended = acmeentities.TenantStatusSuspended
	ACMETenantStatusDeleted   = acmeentities.TenantStatusDeleted
)

// Variables and re-exported function values
var (
	ACMEClientID                     = xkmsacme.ACMEClientID
	ACMECrossSignerFromClient        = xkmsacme.ACMECrossSignerFromClient
	ACMEErrInvalidConfiguration      = xkmsacme.ACMEErrInvalidConfiguration
	CAErrCertificateNotFound         = xkmsacme.CAErrCertificateNotFound
	CAErrInvalidConfig               = xkmsacme.CAErrInvalidConfig
	CAErrNotInitialized              = xkmsacme.CAErrNotInitialized
	ErrDAOStopIteration              = xkmsacme.ErrDAOStopIteration
	ErrNotFound                      = xkmsacme.ErrNotFound
	IsDAONotFound                    = xkmsacme.IsDAONotFound
	NewACMEClient                    = xkmsacme.NewACMEClient
	NewACMEOrderFinalizeHookRegistry = xkmsacme.NewACMEOrderFinalizeHookRegistry
	NewACMEOrderPreSignHookRegistry  = xkmsacme.NewACMEOrderPreSignHookRegistry
	NewACMERateLimiter               = xkmsacme.NewACMERateLimiter
	NewACMERestService               = xkmsacme.NewACMERestService
	NewACMEXKMSTPMAdapter            = xkmsacme.NewACMEXKMSTPMAdapter
	NewClient                        = xkmsacme.NewClient
	WithMCP                          = xkmsacme.WithMCP
	WithQUIC                         = xkmsacme.WithQUIC
	WithREST                         = xkmsacme.WithREST
	WithTimeout                      = xkmsacme.WithTimeout
	WithTLS                          = xkmsacme.WithTLS
	WithUnix                         = xkmsacme.WithUnix
)

// Generic type aliases — declared separately because Go's
// type-block syntax doesn't accept generic re-exports without
// explicit type parameters.

// DAOPageResult holds a page of entities returned by a DAO list operation.
type DAOPageResult[E any] = xkmsacme.DAOPageResult[E]

// DAOPagerFunc is a callback invoked for each page during DAO pagination.
type DAOPagerFunc[E any] = xkmsacme.DAOPagerFunc[E]

// GenericDAO provides typed CRUD operations over a KVStore for a given entity type.
type GenericDAO[E qrdbsdk.Entity] = xkmsacme.GenericDAO[E]

// DAOQueryBuilder provides a fluent query interface for DAO entities.
type DAOQueryBuilder[E qrdbsdk.Entity] = xkmsacme.DAOQueryBuilder[E]
