// Package transport re-exports go-xkms's API transport types.
package transport

import xkmstr "github.com/jeremyhahn/go-xkms/pkg/api/transport"

type (
	GeneratePIVKeyRequest      = xkmstr.GeneratePIVKeyRequest
	GetPIVCertificateRequest   = xkmstr.GetPIVCertificateRequest
	ListPIVSlotsRequest        = xkmstr.ListPIVSlotsRequest
	PIVSlotStatus              = xkmstr.PIVSlotStatus
	StorePIVCertificateRequest = xkmstr.StorePIVCertificateRequest
)
