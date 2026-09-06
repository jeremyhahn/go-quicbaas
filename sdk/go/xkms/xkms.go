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
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// ObjectStorage re-exports — the canonical object/blob storage interface and
// supporting types travel through the alias chain:
//
//	go-objstore/pkg/common.Storage
//	  → go-xkms/sdk/go.ObjectStorage
//	    → go-quicbaas/sdk/go/xkms.ObjectStorage   (here)
//
// go-trusted-platform (and any other quicbaas consumer) imports only this
// package; it never needs go-objstore or go-xkms in its own go.mod for
// object-storage work.
type (
	// ObjectStorage is the canonical object/blob storage interface.
	ObjectStorage = xkmssdk.ObjectStorage

	// ObjectMetadata holds object-level metadata re-exported through the alias chain.
	ObjectMetadata = xkmssdk.ObjectMetadata

	// ObjectListOptions configures paginated object listing.
	ObjectListOptions = xkmssdk.ObjectListOptions

	// ObjectInfo holds key and metadata for a single listed object.
	ObjectInfo = xkmssdk.ObjectInfo

	// ObjectListResult is the paginated result returned by ObjectStorage.ListWithOptions.
	ObjectListResult = xkmssdk.ObjectListResult

	// ObjectArchiver is the destination interface used by ObjectStorage.Archive.
	ObjectArchiver = xkmssdk.ObjectArchiver

	// ObjectLifecyclePolicy configures retention and expiry rules on a storage backend.
	ObjectLifecyclePolicy = xkmssdk.ObjectLifecyclePolicy
)

// Object-storage constructors re-exported from go-xkms (which in turn
// re-exports them from go-objstore). Use these instead of calling
// go-objstore or go-xkms constructors directly.
var (
	// NewObjectStorageLocal constructs a filesystem-backed ObjectStorage
	// implementation rooted at the given directory.
	NewObjectStorageLocal = xkmssdk.NewObjectStorageLocal

	// NewObjectStorageMemory constructs an in-memory ObjectStorage
	// implementation suitable for testing and embedded use.
	NewObjectStorageMemory = xkmssdk.NewObjectStorageMemory
)

// IsRSAPSS reports whether the supplied x509.SignatureAlgorithm
// uses RSA-PSS padding. xkms's SDK doesn't re-export this from
// pkg/types so we promote it here directly — quote-verification
// code uses it to pick between rsa.VerifyPSS and
// rsa.VerifyPKCS1v15.
var IsRSAPSS = xkmssdk.IsRSAPSS

// ---------------------------------------------------------
// Type aliases
// ---------------------------------------------------------

type (
	AKProfile                 = xkmssdk.AKProfile
	BackendType               = xkmssdk.BackendType
	CanSealResponse           = xkmssdk.CanSealResponse
	Capabilities              = xkmssdk.Capabilities
	CertificateStorageAdapter = xkmssdk.CertificateStorageAdapter
	CertifyResult             = xkmssdk.CertifyResult
	CertStore                 = xkmssdk.CertStore
	CertStoreConfig           = xkmssdk.CertStoreConfig
	ECCAttributes             = xkmssdk.ECCAttributes
	ECCConfig                 = xkmssdk.ECCConfig
	EKConfig                  = xkmssdk.EKConfig
	FSExtension               = xkmssdk.FSExtension
	GetKeyResponse            = xkmssdk.GetKeyResponse
	IAKConfig                 = xkmssdk.IAKConfig
	IDevIDConfig              = xkmssdk.IDevIDConfig
	LDevIDConfig              = xkmssdk.LDevIDConfig
	LAKConfig                 = xkmssdk.LAKConfig
	InstallOptions            = xkmssdk.InstallOptions
	KeyAlgorithmString        = xkmssdk.KeyAlgorithmString
	KeyAttributes             = xkmssdk.KeyAttributes
	KeyConfig                 = xkmssdk.KeyConfig
	KeyInfo                   = xkmssdk.KeyInfo
	KeyProvider               = xkmssdk.KeyProvider
	KeyRSAConfig              = xkmssdk.KeyRSAConfig
	NVIndexInfo               = xkmssdk.NVIndexInfo
	Password                  = xkmssdk.Password
	PasswordCacheConfig       = xkmssdk.PasswordCacheConfig
	PCR                       = xkmssdk.PCR
	PCRBank                   = xkmssdk.PCRBank
	PINBackend                = xkmssdk.PINBackend
	PIVAttestRequest          = xkmssdk.PIVAttestRequest
	PIVCertFormat             = xkmssdk.PIVCertFormat
	PIVCertificateStorage     = xkmssdk.PIVCertificateStorage
	PIVResetDeviceRequest     = xkmssdk.PIVResetDeviceRequest
	PIVSignRequest            = xkmssdk.PIVSignRequest
	PIVSlot                   = xkmssdk.PIVSlot
	PIVSlotInfo               = xkmssdk.PIVSlotInfo
	PIVStorageType            = xkmssdk.PIVStorageType
	PKCS8BackendConfig        = xkmssdk.PKCS8BackendConfig
	PlatformKeyStore          = xkmssdk.PlatformKeyStore
	PlatformKeyStorer         = xkmssdk.PlatformKeyStorer
	PlatformPassword          = xkmssdk.PlatformPassword
	PlatformSRKConfig         = xkmssdk.PlatformSRKConfig
	ProdCaData                = xkmssdk.ProdCaData
	PropertiesFixed           = xkmssdk.PropertiesFixed
	Quote                     = xkmssdk.Quote
	RSAAttributes             = xkmssdk.RSAAttributes
	RSAConfig                 = xkmssdk.RSAConfig
	SealedData                = xkmssdk.SealedData
	SealOptions               = xkmssdk.SealOptions
	ServiceBackend            = xkmssdk.ServiceBackend
	ServiceBackendConfig      = xkmssdk.ServiceBackendConfig
	ServiceConfig             = xkmssdk.ServiceConfig
	SignatureAlgorithmName    = xkmssdk.SignatureAlgorithmName
	SignerOpts                = xkmssdk.SignerOpts
	SoftwareBackend           = xkmssdk.SoftwareBackend
	SoftwareBackendConfig     = xkmssdk.SoftwareBackendConfig
	SRKConfig                 = xkmssdk.SRKConfig
	StorageBackend            = xkmssdk.StorageBackend
	StoreType                 = xkmssdk.StoreType
	SymmetricAlgorithm        = xkmssdk.SymmetricAlgorithm
	SymmetricEncrypter        = xkmssdk.SymmetricEncrypter
	SymmetricKey              = xkmssdk.SymmetricKey
	TCG_CSR_IDEVID            = xkmssdk.TCG_CSR_IDEVID
	TPM2BackendExternalConfig = xkmssdk.TPM2BackendExternalConfig
	TPMAttributes             = xkmssdk.TPMAttributes
	TPMCertificateStorer      = xkmssdk.TPMCertificateStorer
	TPMConfig                 = xkmssdk.TPMConfig
	TPMEvent                  = xkmssdk.TPMEvent
	TPMHandle                 = xkmssdk.TPMHandle
	TPMKeyBackend             = xkmssdk.TPMKeyBackend
	TPMParams                 = xkmssdk.TPMParams
	TPMSealPolicy             = xkmssdk.TPMSealPolicy
	TPMSignerOpts             = xkmssdk.TPMSignerOpts
	TrustedCA                 = xkmssdk.TrustedCA
	TrustedPlatformModule     = xkmssdk.TrustedPlatformModule
	UNPACKED_TCG_CSR_IDEVID   = xkmssdk.UNPACKED_TCG_CSR_IDEVID
	UnsealOptions             = xkmssdk.UnsealOptions
	UnsealRequest             = xkmssdk.UnsealRequest
	UnsealResponse            = xkmssdk.UnsealResponse
)

// ---------------------------------------------------------
// Constants
// ---------------------------------------------------------

const (
	BackendTypeTPM2            = xkmssdk.BackendTypeTPM2
	DEFAULT_PASSWORD           = xkmssdk.DEFAULT_PASSWORD
	KeyTypeEncryption          = xkmssdk.KeyTypeEncryption
	KeyTypeHMAC                = xkmssdk.KeyTypeHMAC
	KeyTypeIDevID              = xkmssdk.KeyTypeIDevID
	KeyTypeSigning             = xkmssdk.KeyTypeSigning
	KeyTypeStorage             = xkmssdk.KeyTypeStorage
	KeyTypeTLS                 = xkmssdk.KeyTypeTLS
	PIVFormatPEM               = xkmssdk.PIVFormatPEM
	PIVSlotAuthentication      = xkmssdk.PIVSlotAuthentication
	StoreAWSKMS                = xkmssdk.StoreAWSKMS
	StoreAzureKV               = xkmssdk.StoreAzureKV
	StoreGCPKMS                = xkmssdk.StoreGCPKMS
	StorePKCS11                = xkmssdk.StorePKCS11
	StoreSoftware              = xkmssdk.StoreSoftware
	StoreTPM2                  = xkmssdk.StoreTPM2
	StoreUnknown               = xkmssdk.StoreUnknown
	StoreVault                 = xkmssdk.StoreVault
	SymmetricAES128GCM         = xkmssdk.SymmetricAES128GCM
	SymmetricAES192GCM         = xkmssdk.SymmetricAES192GCM
	SymmetricAES256GCM         = xkmssdk.SymmetricAES256GCM
	SymmetricChaCha20Poly1305  = xkmssdk.SymmetricChaCha20Poly1305
	SymmetricXChaCha20Poly1305 = xkmssdk.SymmetricXChaCha20Poly1305
	TPMHTPersistent            = xkmssdk.TPMHTPersistent
	TPMRHOwner                 = xkmssdk.TPMRHOwner
)

// ---------------------------------------------------------
// Variables (function values, sentinel errors)
// ---------------------------------------------------------

var (
	AttestPIV                         = xkmssdk.AttestPIV
	CertToString                      = xkmssdk.CertToString
	DebugKeyAttributes                = xkmssdk.DebugKeyAttributes
	DecodePCRs                        = xkmssdk.DecodePCRs
	DefaultTPMConfig                  = xkmssdk.DefaultTPMConfig
	DiscoverPlatformAttributes        = xkmssdk.DiscoverPlatformAttributes
	EncodePubKeyPEM                   = xkmssdk.EncodePubKeyPEM
	ErrCertNotFound                   = xkmssdk.ErrCertNotFound
	ErrInvalidKeyAlgorithm            = xkmssdk.ErrInvalidKeyAlgorithm
	ErrInvalidKeyStore                = xkmssdk.ErrInvalidKeyStore
	ErrPlatformSRKAlreadyEnrolled     = xkmssdk.ErrPlatformSRKAlreadyEnrolled
	ErrPlatformSRKEnrollEmptyPIN      = xkmssdk.ErrPlatformSRKEnrollEmptyPIN
	ErrTPMEndorsementCertNotFound     = xkmssdk.ErrTPMEndorsementCertNotFound
	ErrTPMInvalidActivationCredential = xkmssdk.ErrTPMInvalidActivationCredential
	ErrTPMNotInitialized              = xkmssdk.ErrTPMNotInitialized
	ErrTPMStoreCertNotFound           = xkmssdk.ErrTPMStoreCertNotFound
	IDevIDAttributesFromConfig        = xkmssdk.IDevIDAttributesFromConfig
	LDevIDAttributesFromConfig        = xkmssdk.LDevIDAttributesFromConfig
	KeyAttributesFromConfig           = xkmssdk.KeyAttributesFromConfig
	MinimalInstallOptions             = xkmssdk.MinimalInstallOptions
	NewCertStore                      = xkmssdk.NewCertStore
	NewFileBackend                    = xkmssdk.NewFileBackend
	NewFileStorageBackend             = xkmssdk.NewFileStorageBackend
	NewJWTSigningMethodSigner         = xkmssdk.NewJWTSigningMethodSigner
	NewPassword                       = xkmssdk.NewPassword
	NewPasswordFromString             = xkmssdk.NewPasswordFromString
	NewPKCS8Backend                   = xkmssdk.NewPKCS8Backend
	NewPlatformPassword               = xkmssdk.NewPlatformPassword
	NewPlatformKeyStore               = xkmssdk.NewPlatformKeyStore
	NewServiceBackend                 = xkmssdk.NewServiceBackend
	NewSoftwareBackend                = xkmssdk.NewSoftwareBackend
	NewStorageCertAdapter             = xkmssdk.NewStorageCertAdapter
	NewStorageMemory                  = xkmssdk.NewStorageMemory
	NewTPM2                           = xkmssdk.NewTPM2
	NewTPM2BackendWithTPM             = xkmssdk.NewTPM2BackendWithTPM
	OpenUnixSocketTransport           = xkmssdk.OpenUnixSocketTransport
	PackIDevIDCSR                     = xkmssdk.PackIDevIDCSR
	PackProdCaData                    = xkmssdk.PackProdCaData
	ParseCurve                        = xkmssdk.ParseCurve
	ParseHash                         = xkmssdk.ParseHash
	ParseKeyAlgorithm                 = xkmssdk.ParseKeyAlgorithm
	ParseSignatureAlgorithm           = xkmssdk.ParseSignatureAlgorithm
	ParseStoreType                    = xkmssdk.ParseStoreType
	ResetPIV                          = xkmssdk.ResetPIV
	RSASRKTemplate                    = xkmssdk.RSASRKTemplate
	ServiceClose                      = xkmssdk.ServiceClose
	ServiceDeleteKey                  = xkmssdk.ServiceDeleteKey
	ServiceDeleteKeyByID              = xkmssdk.ServiceDeleteKeyByID
	ServiceGenerateKey                = xkmssdk.ServiceGenerateKey
	ServiceGenerateKeyWithBackend     = xkmssdk.ServiceGenerateKeyWithBackend
	ServiceGetBackend                 = xkmssdk.ServiceGetBackend
	ServiceInitialize                 = xkmssdk.ServiceInitialize
	ServiceIsInitialized              = xkmssdk.ServiceIsInitialized
	ServiceKey                        = xkmssdk.ServiceKey
	ServiceKeyByID                    = xkmssdk.ServiceKeyByID
	ServiceReset                      = xkmssdk.ServiceReset
	ServiceSaveCertificate            = xkmssdk.ServiceSaveCertificate
	ServiceSignerByID                 = xkmssdk.ServiceSignerByID
	SignPIV                           = xkmssdk.SignPIV
	UnpackProdCaData                  = xkmssdk.UnpackProdCaData
)

// ---------------------------------------------------------
// Additional re-exports added for trusted-platform's
// internal/xkms adapter and DNS keystore
// ---------------------------------------------------------

type (
	AddCustodianMemberRequest          = xkmssdk.AddCustodianMemberRequest
	AddCustodianMemberResponse         = xkmssdk.AddCustodianMemberResponse
	AttestKeyRequest                   = xkmssdk.AttestKeyRequest
	AttestKeyResponse                  = xkmssdk.AttestKeyResponse
	BackendConfig                      = xkmssdk.BackendConfig
	BackendInfo                        = xkmssdk.BackendInfo
	BarrierEnableEncryptionRequest     = xkmssdk.BarrierEnableEncryptionRequest
	BarrierEnableEncryptionResponse    = xkmssdk.BarrierEnableEncryptionResponse
	BarrierGenerateRecoveryKeysRequest = xkmssdk.BarrierGenerateRecoveryKeysRequest
	BarrierGenerateRootTokenRequest    = xkmssdk.BarrierGenerateRootTokenRequest
	BarrierHasRecoveryKeysResponse     = xkmssdk.BarrierHasRecoveryKeysResponse
	BarrierInitializeRequest           = xkmssdk.BarrierInitializeRequest
	BarrierInitializeShamirRequest     = xkmssdk.BarrierInitializeShamirRequest
	BarrierInitializeShamirResponse    = xkmssdk.BarrierInitializeShamirResponse
	BarrierRecoverWithKeysRequest      = xkmssdk.BarrierRecoverWithKeysRequest
	BarrierRecoveryKeysResponse        = xkmssdk.BarrierRecoveryKeysResponse
	BarrierRekeyRequest                = xkmssdk.BarrierRekeyRequest
	BarrierRekeyResponse               = xkmssdk.BarrierRekeyResponse
	BarrierRootTokenResponse           = xkmssdk.BarrierRootTokenResponse
	BarrierStatusResponse              = xkmssdk.BarrierStatusResponse
	BarrierUnsealRequest               = xkmssdk.BarrierUnsealRequest
	BarrierUnsealShareRequest          = xkmssdk.BarrierUnsealShareRequest
	BarrierUnsealShareResponse         = xkmssdk.BarrierUnsealShareResponse
	BarrierUnsealSharesRequest         = xkmssdk.BarrierUnsealSharesRequest
	BeginAuthenticationRequest         = xkmssdk.BeginAuthenticationRequest
	BeginAuthenticationResponse        = xkmssdk.BeginAuthenticationResponse
	BeginRegistrationRequest           = xkmssdk.BeginRegistrationRequest
	BeginRegistrationResponse          = xkmssdk.BeginRegistrationResponse
	ChangeSOPINRequest                 = xkmssdk.ChangeSOPINRequest
	ChangeUserPINRequest               = xkmssdk.ChangeUserPINRequest
	ClaimCertBeginRequest              = xkmssdk.ClaimCertBeginRequest
	ClaimCertBeginResponse             = xkmssdk.ClaimCertBeginResponse
	ClaimCertCompleteRequest           = xkmssdk.ClaimCertCompleteRequest
	ClaimCertCompleteResponse          = xkmssdk.ClaimCertCompleteResponse
	ClaimShareBeginRequest             = xkmssdk.ClaimShareBeginRequest
	ClaimShareBeginResponse            = xkmssdk.ClaimShareBeginResponse
	ClaimShareCompleteRequest          = xkmssdk.ClaimShareCompleteRequest
	ClaimShareCompleteResponse         = xkmssdk.ClaimShareCompleteResponse
	Client                             = xkmssdk.Client
	CopyKeyRequest                     = xkmssdk.CopyKeyRequest
	CopyKeyResponse                    = xkmssdk.CopyKeyResponse
	CreateCustodianGroupRequest        = xkmssdk.CreateCustodianGroupRequest
	CreateCustodianGroupResponse       = xkmssdk.CreateCustodianGroupResponse
	CreateTenantRequest                = xkmssdk.CreateTenantRequest
	CreateTenantResponse               = xkmssdk.CreateTenantResponse
	CreateUserRequest                  = xkmssdk.CreateUserRequest
	CredentialStrategyResponse         = xkmssdk.CredentialStrategyResponse
	CredentialSubmitRequest            = xkmssdk.CredentialSubmitRequest
	CredentialSubmitResponse           = xkmssdk.CredentialSubmitResponse
	DecryptRequest                     = xkmssdk.DecryptRequest
	DecryptResponse                    = xkmssdk.DecryptResponse
	DeleteKeyResponse                  = xkmssdk.DeleteKeyResponse
	DeletePIVCertificateRequest        = xkmssdk.DeletePIVCertificateRequest
	DeriveKeyECDHRequest               = xkmssdk.DeriveKeyECDHRequest
	DeriveKeyECDHResponse              = xkmssdk.DeriveKeyECDHResponse
	DeriveKeyRequest                   = xkmssdk.DeriveKeyRequest
	DeriveKeyResponse                  = xkmssdk.DeriveKeyResponse
	DistributeSharesRequest            = xkmssdk.DistributeSharesRequest
	DistributeSharesResponse           = xkmssdk.DistributeSharesResponse
	EncryptAsymRequest                 = xkmssdk.EncryptAsymRequest
	EncryptAsymResponse                = xkmssdk.EncryptAsymResponse
	EncryptRequest                     = xkmssdk.EncryptRequest
	EncryptResponse                    = xkmssdk.EncryptResponse
	EnrollDeviceRequest                = xkmssdk.EnrollDeviceRequest
	EnrollDeviceResponse               = xkmssdk.EnrollDeviceResponse
	ExportKeyMaterialRequest           = xkmssdk.ExportKeyMaterialRequest
	ExportKeyMaterialResponse          = xkmssdk.ExportKeyMaterialResponse
	ExportKeyRequest                   = xkmssdk.ExportKeyRequest
	ExportKeyResponse                  = xkmssdk.ExportKeyResponse
	FinishAuthenticationRequest        = xkmssdk.FinishAuthenticationRequest
	FinishAuthenticationResponse       = xkmssdk.FinishAuthenticationResponse
	FinishRegistrationRequest          = xkmssdk.FinishRegistrationRequest
	FinishRegistrationResponse         = xkmssdk.FinishRegistrationResponse
	GenerateCRLRequest                 = xkmssdk.GenerateCRLRequest
	GenerateCRLResponse                = xkmssdk.GenerateCRLResponse
	GenerateKeyRequest                 = xkmssdk.GenerateKeyRequest
	GenerateKeyResponse                = xkmssdk.GenerateKeyResponse
	GeneratePIVCSRRequest              = xkmssdk.GeneratePIVCSRRequest
	GeneratePIVCSRResponse             = xkmssdk.GeneratePIVCSRResponse
	GeneratePIVKeyRequest              = xkmssdk.GeneratePIVKeyRequest
	GeneratePIVKeyResponse             = xkmssdk.GeneratePIVKeyResponse
	GetCABundleRequest                 = xkmssdk.GetCABundleRequest
	GetCABundleResponse                = xkmssdk.GetCABundleResponse
	GetCACertificateRequest            = xkmssdk.GetCACertificateRequest
	GetCACertificateResponse           = xkmssdk.GetCACertificateResponse
	GetCertificateChainResponse        = xkmssdk.GetCertificateChainResponse
	GetCertificateResponse             = xkmssdk.GetCertificateResponse
	GetCustodianGroupResponse          = xkmssdk.GetCustodianGroupResponse
	GetImportParametersRequest         = xkmssdk.GetImportParametersRequest
	GetImportParametersResponse        = xkmssdk.GetImportParametersResponse
	GetPIVCertificateRequest           = xkmssdk.GetPIVCertificateRequest
	GetPIVCertificateResponse          = xkmssdk.GetPIVCertificateResponse
	GetTenantResponse                  = xkmssdk.GetTenantResponse
	GetTLSCertificateResponse          = xkmssdk.GetTLSCertificateResponse
	GetUserResponse                    = xkmssdk.GetUserResponse
	HealthResponse                     = xkmssdk.HealthResponse
	ImportKeyRequest                   = xkmssdk.ImportKeyRequest
	ImportKeyResponse                  = xkmssdk.ImportKeyResponse
	InitStatusResponse                 = xkmssdk.InitStatusResponse
	IsRevokedRequest                   = xkmssdk.IsRevokedRequest
	IsRevokedResponse                  = xkmssdk.IsRevokedResponse
	IssueAKCertificateRequest          = xkmssdk.IssueAKCertificateRequest
	IssueAKCertificateResponse         = xkmssdk.IssueAKCertificateResponse
	IssueCertificateRequest            = xkmssdk.IssueCertificateRequest
	IssueCertificateResponse           = xkmssdk.IssueCertificateResponse
	IssueEKCertificateRequest          = xkmssdk.IssueEKCertificateRequest
	IssueEKCertificateResponse         = xkmssdk.IssueEKCertificateResponse
	ListBackendsResponse               = xkmssdk.ListBackendsResponse
	ListCertificatesResponse           = xkmssdk.ListCertificatesResponse
	ListCustodianGroupsResponse        = xkmssdk.ListCustodianGroupsResponse
	ListKeysResponse                   = xkmssdk.ListKeysResponse
	ListOption                         = xkmssdk.ListOption
	ListPIVSlotsRequest                = xkmssdk.ListPIVSlotsRequest
	ListPIVSlotsResponse               = xkmssdk.ListPIVSlotsResponse
	ListSharesResponse                 = xkmssdk.ListSharesResponse
	ListTenantsResponse                = xkmssdk.ListTenantsResponse
	ListUserCredentialsResponse        = xkmssdk.ListUserCredentialsResponse
	ListUsersResponse                  = xkmssdk.ListUsersResponse
	LockoutStatusResponse              = xkmssdk.LockoutStatusResponse
	PasswordAddRequest                 = xkmssdk.PasswordAddRequest
	PasswordAddResponse                = xkmssdk.PasswordAddResponse
	PasswordDeleteRequest              = xkmssdk.PasswordDeleteRequest
	PasswordGenerateRequest            = xkmssdk.PasswordGenerateRequest
	PasswordGenerateResponse           = xkmssdk.PasswordGenerateResponse
	PasswordGetRequest                 = xkmssdk.PasswordGetRequest
	PasswordGetResponse                = xkmssdk.PasswordGetResponse
	PasswordListRequest                = xkmssdk.PasswordListRequest
	PasswordListResponse               = xkmssdk.PasswordListResponse
	PasswordStoreSetAccessModeRequest  = xkmssdk.PasswordStoreSetAccessModeRequest
	PasswordStoreStatusResponse        = xkmssdk.PasswordStoreStatusResponse
	PasswordStoreUnlockRequest         = xkmssdk.PasswordStoreUnlockRequest
	PasswordUpdateRequest              = xkmssdk.PasswordUpdateRequest
	PolicyCreateRequest                = xkmssdk.PolicyCreateRequest
	PolicyCreateResponse               = xkmssdk.PolicyCreateResponse
	PolicyDeleteRequest                = xkmssdk.PolicyDeleteRequest
	PolicyExportRequest                = xkmssdk.PolicyExportRequest
	PolicyExportResponse               = xkmssdk.PolicyExportResponse
	PolicyGetRequest                   = xkmssdk.PolicyGetRequest
	PolicyGetResponse                  = xkmssdk.PolicyGetResponse
	PolicyListResponse                 = xkmssdk.PolicyListResponse
	PolicyRefreshRequest               = xkmssdk.PolicyRefreshRequest
	PolicyVerifyRequest                = xkmssdk.PolicyVerifyRequest
	PolicyVerifyResponse               = xkmssdk.PolicyVerifyResponse
	Protocol                           = xkmssdk.Protocol
	RemoveCustodianMemberRequest       = xkmssdk.RemoveCustodianMemberRequest
	ResetLockoutRequest                = xkmssdk.ResetLockoutRequest
	RevokeCertificateRequest           = xkmssdk.RevokeCertificateRequest
	RevokeCertificateResponse          = xkmssdk.RevokeCertificateResponse
	RotateKeyRequest                   = xkmssdk.RotateKeyRequest
	RotateKeyResponse                  = xkmssdk.RotateKeyResponse
	SaveCertificateChainRequest        = xkmssdk.SaveCertificateChainRequest
	SaveCertificateRequest             = xkmssdk.SaveCertificateRequest
	SealRequest                        = xkmssdk.SealRequest
	SealResponse                       = xkmssdk.SealResponse
	SealStoreDeleteRequest             = xkmssdk.SealStoreDeleteRequest
	SealStoreGetRequest                = xkmssdk.SealStoreGetRequest
	SealStoreGetResponse               = xkmssdk.SealStoreGetResponse
	SealStoreListResponse              = xkmssdk.SealStoreListResponse
	SealStorePutRequest                = xkmssdk.SealStorePutRequest
	SealStoreResealRequest             = xkmssdk.SealStoreResealRequest
	SealStoreStatusResponse            = xkmssdk.SealStoreStatusResponse
	SetSOPINRequest                    = xkmssdk.SetSOPINRequest
	SetUserPINRequest                  = xkmssdk.SetUserPINRequest
	ShareCollectionStatus              = xkmssdk.ShareCollectionStatus
	SignCSRInitRequest                 = xkmssdk.SignCSRInitRequest
	SignCSRInitResponse                = xkmssdk.SignCSRInitResponse
	SignCSRRequest                     = xkmssdk.SignCSRRequest
	SignCSRResponse                    = xkmssdk.SignCSRResponse
	SignRequest                        = xkmssdk.SignRequest
	SignResponse                       = xkmssdk.SignResponse
	SignTCGCSRRequest                  = xkmssdk.SignTCGCSRRequest
	SignTCGCSRResponse                 = xkmssdk.SignTCGCSRResponse
	StorePIVCertificateRequest         = xkmssdk.StorePIVCertificateRequest
	SubmitShareRequest                 = xkmssdk.SubmitShareRequest
	SubmitShareResponse                = xkmssdk.SubmitShareResponse
	TenantBarrierInitRequest           = xkmssdk.TenantBarrierInitRequest
	TenantBarrierStatusResponse        = xkmssdk.TenantBarrierStatusResponse
	TenantBarrierUnsealRequest         = xkmssdk.TenantBarrierUnsealRequest
	UnwrapKeyByIDRequest               = xkmssdk.UnwrapKeyByIDRequest
	UnwrapKeyByIDResponse              = xkmssdk.UnwrapKeyByIDResponse
	UnwrapKeyRequest                   = xkmssdk.UnwrapKeyRequest
	UnwrapKeyResponse                  = xkmssdk.UnwrapKeyResponse
	UpdateTenantEntitlementsRequest    = xkmssdk.UpdateTenantEntitlementsRequest
	UpdateTenantEntitlementsResponse   = xkmssdk.UpdateTenantEntitlementsResponse
	UpdateTenantQuotasRequest          = xkmssdk.UpdateTenantQuotasRequest
	UpdateTenantQuotasResponse         = xkmssdk.UpdateTenantQuotasResponse
	VerifyRequest                      = xkmssdk.VerifyRequest
	VerifyResponse                     = xkmssdk.VerifyResponse
	VerifySOPINRequest                 = xkmssdk.VerifySOPINRequest
	VerifyUserPINRequest               = xkmssdk.VerifyUserPINRequest
	WrapKeyByIDRequest                 = xkmssdk.WrapKeyByIDRequest
	WrapKeyByIDResponse                = xkmssdk.WrapKeyByIDResponse
	WrapKeyRequest                     = xkmssdk.WrapKeyRequest
	WrapKeyResponse                    = xkmssdk.WrapKeyResponse
	XKMSServicer                       = xkmssdk.XKMSServicer
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

// Attested-peer verification primitive (go-xkms#273): a TPM quote from a
// peer becomes a Verdict — channel-bound to the TLS session, fresh against
// the verifier's clock (never the TPM's), with typed failure classes that
// separate "the claim was absent" from "the claim was false". Pure aliases
// of the go-xkms SDK surface; the platform's remote-attestation path is the
// consumer.
type (
	// PeerAttestationVerifier turns Evidence plus Policy into a Verdict.
	PeerAttestationVerifier = xkmssdk.PeerAttestationVerifier

	// PeerVerifierOption configures NewPeerAttestationVerifier.
	PeerVerifierOption = xkmssdk.PeerVerifierOption

	// PeerEvidence is what a peer presents: its AK certificate chain, the
	// quote, the signature and the PCR values it claims.
	PeerEvidence = xkmssdk.PeerEvidence

	// PeerPolicy is what the relying party requires of the evidence.
	PeerPolicy = xkmssdk.PeerPolicy

	// PeerVerdict is the outcome: pass/fail, the individual Checks, the
	// failure class and the PCRs actually observed.
	PeerVerdict = xkmssdk.PeerVerdict

	// PeerChecks records each individual verification step.
	PeerChecks = xkmssdk.PeerChecks

	// PeerClockInfo carries the quote's TPM clock fields for the record.
	PeerClockInfo = xkmssdk.PeerClockInfo

	// PeerFailureClass distinguishes a claim that was absent from one that
	// was cryptographically false.
	PeerFailureClass = xkmssdk.PeerFailureClass

	// PeerPolicyError, PeerContextError and PeerChannelBindingError are the
	// typed failures a verification can end in.
	PeerPolicyError         = xkmssdk.PeerPolicyError
	PeerContextError        = xkmssdk.PeerContextError
	PeerChannelBindingError = xkmssdk.PeerChannelBindingError
)

const (
	// PeerFailureNone, PeerFailureAbsence and PeerFailureCryptographic are
	// the PeerFailureClass values.
	PeerFailureNone          = xkmssdk.PeerFailureNone
	PeerFailureAbsence       = xkmssdk.PeerFailureAbsence
	PeerFailureCryptographic = xkmssdk.PeerFailureCryptographic

	// PeerChannelBindingExporterLabel is the TLS exporter label the channel
	// binding is derived under; PeerChannelBindingLength is its byte length.
	PeerChannelBindingExporterLabel = xkmssdk.PeerChannelBindingExporterLabel
	PeerChannelBindingLength        = xkmssdk.PeerChannelBindingLength
)

var (
	// NewPeerAttestationVerifier constructs the verifier.
	NewPeerAttestationVerifier = xkmssdk.NewPeerAttestationVerifier

	// WithPeerVerifierClock injects the freshness clock (tests, skewed hosts).
	WithPeerVerifierClock = xkmssdk.WithPeerVerifierClock

	// PeerChannelBinding derives the channel binding from a TLS connection
	// state; PeerQualifyingData combines it with the nonce into the quote's
	// qualifying data.
	PeerChannelBinding = xkmssdk.PeerChannelBinding
	PeerQualifyingData = xkmssdk.PeerQualifyingData
)
