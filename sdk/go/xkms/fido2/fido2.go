// Package fido2 re-exports go-xkms's FIDO2 device handling so
// downstream callers (LUKS unlock, WebAuthn flows) talk to FIDO2
// through the QuicBaaS SDK rather than reaching into go-xkms
// directly.
package fido2

import xkmsfido2 "github.com/jeremyhahn/go-xkms/sdk/go/fido2"

type (
	AuthenticationConfig = xkmsfido2.AuthenticationConfig
	Config               = xkmsfido2.Config
	Device               = xkmsfido2.Device
	EnrollmentConfig     = xkmsfido2.EnrollmentConfig
	EnrollmentResult     = xkmsfido2.EnrollmentResult
	FIDO2Handler         = xkmsfido2.FIDO2Handler
	Handler              = xkmsfido2.Handler
	HIDDevice            = xkmsfido2.HIDDevice
	HIDDeviceEnumerator  = xkmsfido2.HIDDeviceEnumerator
	RelyingParty         = xkmsfido2.RelyingParty

	WebAuthnAttestation         = xkmsfido2.WebAuthnAttestation
	WebAuthnRegistrationOptions = xkmsfido2.WebAuthnRegistrationOptions
)

var (
	DefaultAuthenticationConfig = xkmsfido2.DefaultAuthenticationConfig
	DefaultConfig               = xkmsfido2.DefaultConfig
	DefaultEnrollmentConfig     = xkmsfido2.DefaultEnrollmentConfig
	NewAuthenticator            = xkmsfido2.NewAuthenticator
	NewCTAPHIDDevice            = xkmsfido2.NewCTAPHIDDevice
	NewDefaultEnumerator        = xkmsfido2.NewDefaultEnumerator
	NewHandler                  = xkmsfido2.NewHandler

	ParseWebAuthnRegistrationOptions = xkmsfido2.ParseWebAuthnRegistrationOptions
)

// User identifies a FIDO2 relying-party user.
type User = xkmsfido2.User
