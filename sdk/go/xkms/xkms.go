// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

// xkms_reexports.go re-exports the go-xkms SDK surface that
// downstream consumers (go-trusted-platform, BMC, gateway, …)
// need. Every entry is a thin alias (`type X = xkmssdk.X`,
// `const X = xkmssdk.X`, or `var X = xkmssdk.X`); values remain
// runtime-compatible with the upstream go-xkms types.
//
// Architectural intent: consumers import cryptographic
// primitives (TPM 2.0, PIV, FIDO2, key attributes, sealing
// policy, certificate stores, ACME workflows) through the
// QuicBaaS SDK rather than reaching into go-xkms's package
// layout directly. QuicBaaS is the unified BaaS SDK; xkms is
// an implementation detail we curate here.
//
// Symbol coverage is driven by what go-trusted-platform
// actually uses (audited via grep + protobuf usage analysis).
// When a new xkms symbol is needed, add the alias here rather
// than importing xkms in downstream code.

package xkms

import (
	xkmstypes "github.com/jeremyhahn/go-xkms/pkg/types"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// IsRSAPSS reports whether the supplied x509.SignatureAlgorithm
// uses RSA-PSS padding. xkms's SDK doesn't re-export this from
// pkg/types so we promote it here directly — quote-verification
// code uses it to pick between rsa.VerifyPSS and
// rsa.VerifyPKCS1v15.
var IsRSAPSS = xkmstypes.IsRSAPSS


// ---------------------------------------------------------
// Type aliases
// ---------------------------------------------------------

type (
	AKProfile = xkmssdk.AKProfile
	BackendType = xkmssdk.BackendType
	CanSealResponse = xkmssdk.CanSealResponse
	Capabilities = xkmssdk.Capabilities
	CertificateStorageAdapter = xkmssdk.CertificateStorageAdapter
	CertifyResult = xkmssdk.CertifyResult
	CertStore = xkmssdk.CertStore
	CertStoreConfig = xkmssdk.CertStoreConfig
	ECCAttributes = xkmssdk.ECCAttributes
	ECCConfig = xkmssdk.ECCConfig
	EKConfig = xkmssdk.EKConfig
	FSExtension = xkmssdk.FSExtension
	GetKeyResponse = xkmssdk.GetKeyResponse
	KeyAlgorithmString = xkmssdk.KeyAlgorithmString
	KeyAttributes = xkmssdk.KeyAttributes
	KeyConfig = xkmssdk.KeyConfig
	KeyInfo = xkmssdk.KeyInfo
	KeyProvider = xkmssdk.KeyProvider
	KeyRSAConfig = xkmssdk.KeyRSAConfig
	NVIndexInfo = xkmssdk.NVIndexInfo
	Password = xkmssdk.Password
	PasswordCacheConfig = xkmssdk.PasswordCacheConfig
	PCR = xkmssdk.PCR
	PCRBank = xkmssdk.PCRBank
	PINBackend = xkmssdk.PINBackend
	PINManager = xkmssdk.PINManager
	PIVAttestRequest = xkmssdk.PIVAttestRequest
	PIVResetDeviceRequest = xkmssdk.PIVResetDeviceRequest
	PIVSignRequest = xkmssdk.PIVSignRequest
	PKCS8BackendConfig = xkmssdk.PKCS8BackendConfig
	PlatformKeyStorer = xkmssdk.PlatformKeyStorer
	PlatformPassword = xkmssdk.PlatformPassword
	PlatformSRKConfig = xkmssdk.PlatformSRKConfig
	ProdCaData = xkmssdk.ProdCaData
	PropertiesFixed = xkmssdk.PropertiesFixed
	Quote = xkmssdk.Quote
	RSAAttributes = xkmssdk.RSAAttributes
	RSAConfig = xkmssdk.RSAConfig
	SealedData = xkmssdk.SealedData
	SealOptions = xkmssdk.SealOptions
	ServiceBackend = xkmssdk.ServiceBackend
	ServiceBackendConfig = xkmssdk.ServiceBackendConfig
	ServiceConfig = xkmssdk.ServiceConfig
	SignatureAlgorithmName = xkmssdk.SignatureAlgorithmName
	SignerOpts = xkmssdk.SignerOpts
	SoftwareBackend = xkmssdk.SoftwareBackend
	SoftwareBackendConfig = xkmssdk.SoftwareBackendConfig
	SRKConfig = xkmssdk.SRKConfig
	StorageBackend = xkmssdk.StorageBackend
	StoreType = xkmssdk.StoreType
	SymmetricAlgorithm = xkmssdk.SymmetricAlgorithm
	SymmetricEncrypter = xkmssdk.SymmetricEncrypter
	SymmetricKey = xkmssdk.SymmetricKey
	TCG_CSR_IDEVID = xkmssdk.TCG_CSR_IDEVID
	TPM2BackendExternalConfig = xkmssdk.TPM2BackendExternalConfig
	TPMAttributes = xkmssdk.TPMAttributes
	TPMCertificateStorer = xkmssdk.TPMCertificateStorer
	TPMConfig = xkmssdk.TPMConfig
	TPMEvent = xkmssdk.TPMEvent
	TPMHandle = xkmssdk.TPMHandle
	TPMKeyBackend = xkmssdk.TPMKeyBackend
	TPMParams = xkmssdk.TPMParams
	TPMSealPolicy = xkmssdk.TPMSealPolicy
	TPMSignerOpts = xkmssdk.TPMSignerOpts
	TrustedCA = xkmssdk.TrustedCA
	TrustedPlatformModule = xkmssdk.TrustedPlatformModule
	UNPACKED_TCG_CSR_IDEVID = xkmssdk.UNPACKED_TCG_CSR_IDEVID
	UnsealOptions = xkmssdk.UnsealOptions
	UnsealRequest = xkmssdk.UnsealRequest
	UnsealResponse = xkmssdk.UnsealResponse
)

// ---------------------------------------------------------
// Constants
// ---------------------------------------------------------

const (
	BackendTypeTPM2 = xkmssdk.BackendTypeTPM2
	DEFAULT_PASSWORD = xkmssdk.DEFAULT_PASSWORD
	KeyTypeEncryption = xkmssdk.KeyTypeEncryption
	KeyTypeHMAC = xkmssdk.KeyTypeHMAC
	KeyTypeIDevID = xkmssdk.KeyTypeIDevID
	KeyTypeSigning = xkmssdk.KeyTypeSigning
	KeyTypeStorage = xkmssdk.KeyTypeStorage
	KeyTypeTLS = xkmssdk.KeyTypeTLS
	StoreAWSKMS = xkmssdk.StoreAWSKMS
	StoreAzureKV = xkmssdk.StoreAzureKV
	StoreGCPKMS = xkmssdk.StoreGCPKMS
	StorePKCS11 = xkmssdk.StorePKCS11
	StoreSoftware = xkmssdk.StoreSoftware
	StoreTPM2 = xkmssdk.StoreTPM2
	StoreUnknown = xkmssdk.StoreUnknown
	StoreVault = xkmssdk.StoreVault
	SymmetricAES128GCM = xkmssdk.SymmetricAES128GCM
	SymmetricAES192GCM = xkmssdk.SymmetricAES192GCM
	SymmetricAES256GCM = xkmssdk.SymmetricAES256GCM
	SymmetricChaCha20Poly1305 = xkmssdk.SymmetricChaCha20Poly1305
	SymmetricXChaCha20Poly1305 = xkmssdk.SymmetricXChaCha20Poly1305
	TPMHTPersistent = xkmssdk.TPMHTPersistent
	TPMRHOwner = xkmssdk.TPMRHOwner
)

// ---------------------------------------------------------
// Variables (function values, sentinel errors)
// ---------------------------------------------------------

var (
	AttestPIV = xkmssdk.AttestPIV
	CertToString = xkmssdk.CertToString
	DebugKeyAttributes = xkmssdk.DebugKeyAttributes
	DecodePCRs = xkmssdk.DecodePCRs
	DefaultTPMConfig = xkmssdk.DefaultTPMConfig
	DiscoverPlatformAttributes = xkmssdk.DiscoverPlatformAttributes
	EncodePubKeyPEM = xkmssdk.EncodePubKeyPEM
	ErrCertNotFound = xkmssdk.ErrCertNotFound
	ErrInvalidKeyAlgorithm = xkmssdk.ErrInvalidKeyAlgorithm
	ErrInvalidKeyStore = xkmssdk.ErrInvalidKeyStore
	ErrTPMEndorsementCertNotFound = xkmssdk.ErrTPMEndorsementCertNotFound
	ErrTPMInvalidActivationCredential = xkmssdk.ErrTPMInvalidActivationCredential
	ErrTPMNotInitialized = xkmssdk.ErrTPMNotInitialized
	ErrTPMStoreCertNotFound = xkmssdk.ErrTPMStoreCertNotFound
	IDevIDAttributesFromConfig = xkmssdk.IDevIDAttributesFromConfig
	KeyAttributesFromConfig = xkmssdk.KeyAttributesFromConfig
	MinimalInstallOptions = xkmssdk.MinimalInstallOptions
	NewCertStore = xkmssdk.NewCertStore
	NewFileBackend = xkmssdk.NewFileBackend
	NewFileStorageBackend = xkmssdk.NewFileStorageBackend
	NewJWTSigningMethodSigner = xkmssdk.NewJWTSigningMethodSigner
	NewPassword = xkmssdk.NewPassword
	NewPasswordFromString = xkmssdk.NewPasswordFromString
	NewPKCS8Backend = xkmssdk.NewPKCS8Backend
	NewPlatformPassword = xkmssdk.NewPlatformPassword
	NewServiceBackend = xkmssdk.NewServiceBackend
	NewSoftwareBackend = xkmssdk.NewSoftwareBackend
	NewStorageCertAdapter = xkmssdk.NewStorageCertAdapter
	NewStorageMemory = xkmssdk.NewStorageMemory
	NewTPM2 = xkmssdk.NewTPM2
	NewTPM2BackendWithTPM = xkmssdk.NewTPM2BackendWithTPM
	OpenUnixSocketTransport = xkmssdk.OpenUnixSocketTransport
	PackIDevIDCSR = xkmssdk.PackIDevIDCSR
	PackProdCaData = xkmssdk.PackProdCaData
	ParseCurve = xkmssdk.ParseCurve
	ParseHash = xkmssdk.ParseHash
	ParseKeyAlgorithm = xkmssdk.ParseKeyAlgorithm
	ParseSignatureAlgorithm = xkmssdk.ParseSignatureAlgorithm
	ParseStoreType = xkmssdk.ParseStoreType
	ResetPIV = xkmssdk.ResetPIV
	RSASRKTemplate = xkmssdk.RSASRKTemplate
	ServiceClose = xkmssdk.ServiceClose
	ServiceDeleteKey = xkmssdk.ServiceDeleteKey
	ServiceDeleteKeyByID = xkmssdk.ServiceDeleteKeyByID
	ServiceGenerateKey = xkmssdk.ServiceGenerateKey
	ServiceGenerateKeyWithBackend = xkmssdk.ServiceGenerateKeyWithBackend
	ServiceGetBackend = xkmssdk.ServiceGetBackend
	ServiceInitialize = xkmssdk.ServiceInitialize
	ServiceIsInitialized = xkmssdk.ServiceIsInitialized
	ServiceKey = xkmssdk.ServiceKey
	ServiceKeyByID = xkmssdk.ServiceKeyByID
	ServiceReset = xkmssdk.ServiceReset
	ServiceSaveCertificate = xkmssdk.ServiceSaveCertificate
	ServiceSignerByID = xkmssdk.ServiceSignerByID
	SignPIV = xkmssdk.SignPIV
	UnpackProdCaData = xkmssdk.UnpackProdCaData
)

// ---------------------------------------------------------
// Additional re-exports added for trusted-platform's
// internal/xkms adapter and DNS keystore
// ---------------------------------------------------------

type (
	BackendConfig                  = xkmssdk.BackendConfig
	BackendInfo                    = xkmssdk.BackendInfo
	BeginAuthenticationRequest     = xkmssdk.BeginAuthenticationRequest
	BeginAuthenticationResponse    = xkmssdk.BeginAuthenticationResponse
	BeginRegistrationRequest       = xkmssdk.BeginRegistrationRequest
	BeginRegistrationResponse      = xkmssdk.BeginRegistrationResponse
	Client                         = xkmssdk.Client
	CopyKeyRequest                 = xkmssdk.CopyKeyRequest
	CopyKeyResponse                = xkmssdk.CopyKeyResponse
	DecryptRequest                 = xkmssdk.DecryptRequest
	DecryptResponse                = xkmssdk.DecryptResponse
	DeleteKeyResponse              = xkmssdk.DeleteKeyResponse
	EncryptAsymRequest             = xkmssdk.EncryptAsymRequest
	EncryptAsymResponse            = xkmssdk.EncryptAsymResponse
	EncryptRequest                 = xkmssdk.EncryptRequest
	EncryptResponse                = xkmssdk.EncryptResponse
	ExportKeyRequest               = xkmssdk.ExportKeyRequest
	ExportKeyResponse              = xkmssdk.ExportKeyResponse
	FinishAuthenticationRequest    = xkmssdk.FinishAuthenticationRequest
	FinishAuthenticationResponse   = xkmssdk.FinishAuthenticationResponse
	FinishRegistrationRequest      = xkmssdk.FinishRegistrationRequest
	FinishRegistrationResponse     = xkmssdk.FinishRegistrationResponse
	GenerateKeyRequest             = xkmssdk.GenerateKeyRequest
	GenerateKeyResponse            = xkmssdk.GenerateKeyResponse
	GetCertificateChainResponse    = xkmssdk.GetCertificateChainResponse
	GetCertificateResponse         = xkmssdk.GetCertificateResponse
	GetImportParametersRequest     = xkmssdk.GetImportParametersRequest
	GetImportParametersResponse    = xkmssdk.GetImportParametersResponse
	GetTLSCertificateResponse      = xkmssdk.GetTLSCertificateResponse
	GetUserResponse                = xkmssdk.GetUserResponse
	HealthResponse                 = xkmssdk.HealthResponse
	ImportKeyRequest               = xkmssdk.ImportKeyRequest
	ImportKeyResponse              = xkmssdk.ImportKeyResponse
	ListBackendsResponse           = xkmssdk.ListBackendsResponse
	ListCertificatesResponse       = xkmssdk.ListCertificatesResponse
	ListKeysResponse               = xkmssdk.ListKeysResponse
	ListUserCredentialsResponse    = xkmssdk.ListUserCredentialsResponse
	ListUsersResponse              = xkmssdk.ListUsersResponse
	Protocol                       = xkmssdk.Protocol
	RotateKeyRequest               = xkmssdk.RotateKeyRequest
	RotateKeyResponse              = xkmssdk.RotateKeyResponse
	SaveCertificateChainRequest    = xkmssdk.SaveCertificateChainRequest
	SaveCertificateRequest         = xkmssdk.SaveCertificateRequest
	SealRequest                    = xkmssdk.SealRequest
	SealResponse                   = xkmssdk.SealResponse
	SignRequest                    = xkmssdk.SignRequest
	SignResponse                   = xkmssdk.SignResponse
	UnwrapKeyRequest               = xkmssdk.UnwrapKeyRequest
	UnwrapKeyResponse              = xkmssdk.UnwrapKeyResponse
	VerifyRequest                  = xkmssdk.VerifyRequest
	VerifyResponse                 = xkmssdk.VerifyResponse
	WrapKeyRequest                 = xkmssdk.WrapKeyRequest
	WrapKeyResponse                = xkmssdk.WrapKeyResponse
	XKMSServicer                   = xkmssdk.XKMSServicer
)

const (
	ProtocolUnix     = xkmssdk.ProtocolUnix
	ProtocolUnixGRPC = xkmssdk.ProtocolUnixGRPC
	ProtocolREST     = xkmssdk.ProtocolREST
	ProtocolGRPC     = xkmssdk.ProtocolGRPC
	ProtocolQUIC     = xkmssdk.ProtocolQUIC
	ProtocolMCP      = xkmssdk.ProtocolMCP
	ProtocolEmbedded = xkmssdk.ProtocolEmbedded
)

var (
	New         = xkmssdk.New
	NewEmbedded = xkmssdk.NewEmbedded
)
