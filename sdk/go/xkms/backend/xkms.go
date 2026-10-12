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
type (
	PIVCertificateStorage = xkmssdk.PIVCertificateStorage
	PIVKeyGenerator       = xkmssdk.PIVKeyGenerator
	PIVManagerConfig      = xkmssdk.PIVManagerConfig
)

var (
	GeneratePIVKey        = xkmssdk.GeneratePIVKey
	GetPIVCertificate     = xkmssdk.GetPIVCertificate
	InitializePIV         = xkmssdk.InitializePIV
	ListPIVSlots          = xkmssdk.ListPIVSlots
	ResetPIV              = xkmssdk.XKMSResetPIV
	SetPIVBackendResolver = xkmssdk.SetPIVBackendResolver
	StorePIVCertificate   = xkmssdk.StorePIVCertificate

	ErrPIVInvalidSlot           = xkmssdk.ServiceErrPIVInvalidSlot
	ErrPIVNoAttestationMaterial = xkmssdk.ErrPIVNoAttestationMaterial
	ErrPIVNotInitialized        = xkmssdk.ServiceErrPIVNotInitialized
)
