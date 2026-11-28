// Package internal re-exports the go-xkms backend infrastructure
// surface (Backend, PIV operations, error sentinels).
//
// Imported by trusted-platform's PIV backend adapter.
package backend

import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

type Backend = xkmssdk.ServiceBackend

// PIV manager wiring: PIVKeyGenerator and PIVManagerConfig describe how a
// backend provisions PIV slots, and SetPIVBackendResolver installs the
// resolver used to locate the backend serving a given device.
// PIVCertificateStorage holds the certificate installed in each slot.
//
// ImportPIVKey installs an externally held private key in a slot; a
// PIVKeyGenerator that can accept one also implements PIVKeyImporter.
// An imported key's attestation asserts residency, not exclusivity; see go-xkms ImportPIVKey.
type (
	ImportPIVKeyRequest   = xkmssdk.ImportPIVKeyRequest
	ImportPIVKeyResponse  = xkmssdk.ImportPIVKeyResponse
	PIVCertificateStorage = xkmssdk.PIVCertificateStorage
	PIVKeyGenerator       = xkmssdk.PIVKeyGenerator
	PIVKeyImporter        = xkmssdk.PIVKeyImporter
	PIVManagerConfig      = xkmssdk.PIVManagerConfig
)

var (
	GeneratePIVKey        = xkmssdk.GeneratePIVKey
	GetPIVCertificate     = xkmssdk.GetPIVCertificate
	ImportPIVKey          = xkmssdk.ImportPIVKey
	InitializePIV         = xkmssdk.InitializePIV
	ListPIVSlots          = xkmssdk.ListPIVSlots
	ResetPIV              = xkmssdk.XKMSResetPIV
	SetPIVBackendResolver = xkmssdk.SetPIVBackendResolver
	StorePIVCertificate   = xkmssdk.StorePIVCertificate

	ErrPIVImportNotSupported    = xkmssdk.ErrPIVImportNotSupported
	ErrPIVInvalidSlot           = xkmssdk.ServiceErrPIVInvalidSlot
	ErrPIVNoAttestationMaterial = xkmssdk.ErrPIVNoAttestationMaterial
	ErrPIVNotInitialized        = xkmssdk.ServiceErrPIVNotInitialized

	// Key sentinels the go-xkms key backends return: ErrKeyNotFound for a
	// lookup of a key that is not stored, ErrKeyAlreadyExists for a
	// generate whose key ID is already taken.
	ErrKeyAlreadyExists = xkmssdk.ErrBackendKeyAlreadyExists
	ErrKeyNotFound      = xkmssdk.ErrBackendKeyNotFound
)
