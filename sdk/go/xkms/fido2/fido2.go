// Package fido2 re-exports go-xkms's FIDO2 device handling so
// downstream callers (LUKS unlock, WebAuthn flows) talk to FIDO2
// through the QuicBaaS SDK rather than reaching into go-xkms
// directly. The FIDO2 PIN errors are re-exported too, so a PIN-protected
// unlock never imports go-xkms.
package fido2

import xkmsfido2 "github.com/jeremyhahn/go-xkms/sdk/go/fido2"

type (
	AuthenticationConfig = xkmsfido2.AuthenticationConfig
	AuthenticationResult = xkmsfido2.AuthenticationResult
	Authenticator        = xkmsfido2.Authenticator
	Config               = xkmsfido2.Config
	CTAPHIDDevice        = xkmsfido2.CTAPHIDDevice
	Device               = xkmsfido2.Device
	EnrollmentConfig     = xkmsfido2.EnrollmentConfig
	EnrollmentResult     = xkmsfido2.EnrollmentResult
	FIDO2Handler         = xkmsfido2.FIDO2Handler
	Handler              = xkmsfido2.Handler
	HIDDevice            = xkmsfido2.HIDDevice
	HIDDeviceEnumerator  = xkmsfido2.HIDDeviceEnumerator
	RelyingParty         = xkmsfido2.RelyingParty

	WebAuthnAssertion             = xkmsfido2.WebAuthnAssertion
	WebAuthnAssertionRequest      = xkmsfido2.WebAuthnAssertionRequest
	WebAuthnAttestation           = xkmsfido2.WebAuthnAttestation
	WebAuthnAuthenticationOptions = xkmsfido2.WebAuthnAuthenticationOptions
	WebAuthnRegistrationOptions   = xkmsfido2.WebAuthnRegistrationOptions
)

// PIN and request-authorization errors from a FIDO2 assertion. A caller
// that sets AuthenticationConfig.PIN tells "the key needs a PIN" from "the
// PIN was wrong" from "the key has no PIN set" or "the key is locked" with
// errors.Is against these, without importing go-xkms. ErrInvalidOption
// reports a request the caller built wrongly, most often asking for user
// verification on a key that verifies by PIN.
var (
	ErrPINRequired           = xkmsfido2.ErrPINRequired
	ErrInvalidPIN            = xkmsfido2.ErrInvalidPIN
	ErrPINBlocked            = xkmsfido2.ErrPINBlocked
	ErrPINNotSet             = xkmsfido2.ErrPINNotSet
	ErrPINAuthInvalid        = xkmsfido2.ErrPINAuthInvalid
	ErrPINAuthBlocked        = xkmsfido2.ErrPINAuthBlocked
	ErrPINRetriesUnavailable = xkmsfido2.ErrPINRetriesUnavailable
	ErrInvalidOption         = xkmsfido2.ErrInvalidOption
)

// PINAttemptsError is a wrong PIN annotated with the attempts the
// authenticator still accepts before it locks out. errors.Is still
// matches ErrInvalidPIN through it.
type PINAttemptsError = xkmsfido2.PINAttemptsError

// Errors returned when a nil config is passed to an enrollment or authentication call.
var (
	ErrAuthenticationConfigNil = xkmsfido2.ErrAuthenticationConfigNil
	ErrEnrollmentConfigNil     = xkmsfido2.ErrEnrollmentConfigNil
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
