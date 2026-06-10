// Package transport re-exports go-xkms's API transport types.
package transport

import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

type (
	GeneratePIVKeyRequest      = xkmssdk.GeneratePIVKeyRequest
	GetPIVCertificateRequest   = xkmssdk.GetPIVCertificateRequest
	ListPIVSlotsRequest        = xkmssdk.ListPIVSlotsRequest
	PIVSlotStatus              = xkmssdk.PIVSlotStatus
	StorePIVCertificateRequest = xkmssdk.StorePIVCertificateRequest
)
