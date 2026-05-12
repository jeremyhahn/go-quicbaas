// Package internal re-exports the go-xkms backend infrastructure
// surface (Backend, PIV operations, error sentinels).
//
// Imported by trusted-platform's PIV backend adapter.
package backend

import xkms "github.com/jeremyhahn/go-xkms/pkg/xkms"

type Backend = xkms.Backend

var (
	GeneratePIVKey         = xkms.GeneratePIVKey
	GetPIVCertificate      = xkms.GetPIVCertificate
	InitializePIV          = xkms.InitializePIV
	ListPIVSlots           = xkms.ListPIVSlots
	StorePIVCertificate    = xkms.StorePIVCertificate
	ErrPIVInvalidSlot      = xkms.ErrPIVInvalidSlot
	ErrPIVNotInitialized   = xkms.ErrPIVNotInitialized
)
