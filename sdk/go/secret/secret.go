// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package secret re-exports go-qrdb's config-time secret-reference resolution
// SPI so platform consumers (go-trusted-platform) reach these through the
// quicbaas SDK rather than importing go-qrdb directly — the Layer-5 "quicbaas
// SDK only" boundary. Every symbol below is a type alias / value forwarder, so
// they are the SAME types go-qrdb defines; a value produced by qrdb's resolver
// registry is assignable to these names and vice versa with no conversion.
//
// Keeping these in a subpackage (rather than flattened into the root quicbaas
// SDK package) preserves their natural names without risking collisions with
// other subsystems in the root namespace.
package secret

import qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"

// Resolver and value types.
type (
	// ResolverRegistry dispatches secret resolution to the first registered
	// resolver that recognizes a URI scheme. (Aliases qrdb's ResolverRegistry.)
	ResolverRegistry = qrdbsecret.ResolverRegistry
	// SecretResolver resolves a single secret URI to a SecretValue.
	SecretResolver = qrdbsecret.SecretResolver
	// SecretValue holds resolved plaintext plus resolution metadata; call Clear().
	SecretValue = qrdbsecret.SecretValue
	// ConfigProcessor walks a config struct and resolves credential:// URIs in place.
	ConfigProcessor = qrdbsecret.ConfigProcessor
	// ProcessResult reports what a ConfigProcessor resolved.
	ProcessResult = qrdbsecret.ProcessResult
	// ProcessorOption configures a ConfigProcessor.
	ProcessorOption = qrdbsecret.ProcessorOption
	// ResolvedStrategy identifies how a secret value was resolved.
	ResolvedStrategy = qrdbsecret.ResolvedStrategy
	// CredentialRetrieveFunc retrieves a credential by reference.
	CredentialRetrieveFunc = qrdbsecret.CredentialRetrieveFunc
	// CredentialRetriever retrieves credentials backing a credential:// resolver.
	CredentialRetriever = qrdbsecret.CredentialRetriever
)

// Constructors.
var (
	NewResolverRegistry               = qrdbsecret.NewResolverRegistry
	NewCredentialResolver             = qrdbsecret.NewCredentialResolver
	NewSecretValue                    = qrdbsecret.NewSecretValue
	NewConfigProcessor                = qrdbsecret.NewConfigProcessor
	WithFailClosed                    = qrdbsecret.WithFailClosed
	CredentialResolverFromServicer    = qrdbsecret.CredentialResolverFromServicer
	NewTenantScopedCredentialRetrieve = qrdbsecret.NewTenantScopedCredentialRetrieve
)

// Resolved-strategy constants.
const (
	StrategyLiteral    = qrdbsecret.StrategyLiteral
	StrategyCredential = qrdbsecret.StrategyCredential
	StrategyLocalKMS   = qrdbsecret.StrategyLocalKMS
	StrategyRemoteKMS  = qrdbsecret.StrategyRemoteKMS
	StrategyAutoGen    = qrdbsecret.StrategyAutoGen
)

// Secret error types and sentinels.
type (
	InvalidSecretURIError = qrdbsecret.InvalidSecretURIError
	SecretNotFoundError   = qrdbsecret.SecretNotFoundError
	ResolutionFailedError = qrdbsecret.ResolutionFailedError
	ProcessorInputError   = qrdbsecret.ProcessorInputError
)

var (
	ErrNilContext          = qrdbsecret.ErrNilContext
	ErrUnresolvedSecretURI = qrdbsecret.ErrUnresolvedSecretURI
)
