// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

// Package workflow re-exports go-xkms's ACME workflow surface (the
// high-level register / request-certificate / revoke orchestration the
// TrustedCA Client exposes via Workflow()) so go-trusted-platform can use
// and mock it without importing go-xkms directly.
package workflow

import xkmsworkflow "github.com/jeremyhahn/go-xkms/sdk/go/acme/workflow"

// Type aliases
type (
	Workflow             = xkmsworkflow.Workflow
	Client               = xkmsworkflow.Client
	Option               = xkmsworkflow.Option
	RegisterRequest      = xkmsworkflow.RegisterRequest
	RegisterResponse     = xkmsworkflow.RegisterResponse
	CertificateRequest   = xkmsworkflow.CertificateRequest
	CertificateResponse  = xkmsworkflow.CertificateResponse
	RevokeRequest        = xkmsworkflow.RevokeRequest
	ChallengeHandler     = xkmsworkflow.ChallengeHandler
	ChallengeHandlerFunc = xkmsworkflow.ChallengeHandlerFunc
	RegistrationError    = xkmsworkflow.RegistrationError
	AuthorizationError   = xkmsworkflow.AuthorizationError
	ChallengeError       = xkmsworkflow.ChallengeError
	FinalizationError    = xkmsworkflow.FinalizationError
	PollTimeoutError     = xkmsworkflow.PollTimeoutError
	RevocationError      = xkmsworkflow.RevocationError
	NoChallengeError     = xkmsworkflow.NoChallengeError
)

// Re-exported function values
var (
	NewWorkflow            = xkmsworkflow.NewWorkflow
	WithPollTimeout        = xkmsworkflow.WithPollTimeout
	WithPollInterval       = xkmsworkflow.WithPollInterval
	NewRegistrationError   = xkmsworkflow.NewRegistrationError
	WrapRegistrationError  = xkmsworkflow.WrapRegistrationError
	NewAuthorizationError  = xkmsworkflow.NewAuthorizationError
	WrapAuthorizationError = xkmsworkflow.WrapAuthorizationError
	NewChallengeError      = xkmsworkflow.NewChallengeError
	WrapChallengeError     = xkmsworkflow.WrapChallengeError
	NewFinalizationError   = xkmsworkflow.NewFinalizationError
	WrapFinalizationError  = xkmsworkflow.WrapFinalizationError
	NewPollTimeoutError    = xkmsworkflow.NewPollTimeoutError
	NewRevocationError     = xkmsworkflow.NewRevocationError
	WrapRevocationError    = xkmsworkflow.WrapRevocationError
	NewNoChallengeError    = xkmsworkflow.NewNoChallengeError
)
