// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

// Package transport re-exports go-xkms's ACME client transport surface
// (Transport, Stream, address parsing, TLS helpers) so go-trusted-platform
// can name the types that appear in the TrustedCA Client interface without
// importing go-xkms directly.
package transport

import xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/acme/transport"

// Type aliases
type (
	Transport     = xkmstransport.Transport
	Config        = xkmstransport.Config
	Factory       = xkmstransport.Factory
	AddressType   = xkmstransport.AddressType
	ParsedAddress = xkmstransport.ParsedAddress
	TLSConfig     = xkmstransport.TLSConfig
)

// AddressType constants
const (
	AddressTypeTCP          = xkmstransport.AddressTypeTCP
	AddressTypeUnix         = xkmstransport.AddressTypeUnix
	AddressTypeUnixAbstract = xkmstransport.AddressTypeUnixAbstract
	AddressTypeHTTP         = xkmstransport.AddressTypeHTTP
	AddressTypeHTTPS        = xkmstransport.AddressTypeHTTPS
	AddressTypeEmbedded     = xkmstransport.AddressTypeEmbedded
)

// Re-exported function values
var (
	ParseAddress           = xkmstransport.ParseAddress
	LoadTLSConfig          = xkmstransport.LoadTLSConfig
	LoadTLSConfigFromFiles = xkmstransport.LoadTLSConfigFromFiles
	ValidateTLSConfig      = xkmstransport.ValidateTLSConfig
	CloneTLSConfig         = xkmstransport.CloneTLSConfig
)
