// Package internal re-exports the go-xkms backend infrastructure
// surface (Backend, PIV operations, error sentinels).
//
// Imported by trusted-platform's PIV backend adapter.
package backend

import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

type Backend = xkmssdk.ServiceBackend

var (
	GeneratePIVKey       = xkmssdk.GeneratePIVKey
	GetPIVCertificate    = xkmssdk.GetPIVCertificate
	InitializePIV        = xkmssdk.InitializePIV
	ListPIVSlots         = xkmssdk.ListPIVSlots
	StorePIVCertificate  = xkmssdk.StorePIVCertificate
	ErrPIVInvalidSlot    = xkmssdk.ServiceErrPIVInvalidSlot
	ErrPIVNotInitialized = xkmssdk.ServiceErrPIVNotInitialized
)
