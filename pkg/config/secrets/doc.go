// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Package secrets provides a framework for resolving secret references
// in configuration files.
//
// Configuration secrets can be stored in various backends and referenced
// using URI schemes. This package provides the interfaces and implementations
// for resolving these references to their actual values.
//
// # Supported URI Schemes
//
// The following URI schemes are supported:
//
//   - kms://path/to/secret - Local QuicBaaS KMS secrets store
//   - remote-kms://host:port/path - Remote QuicBaaS instance
//
// Dispatch is by URI scheme only. A value carrying no registered scheme is
// not a secret reference: it is used verbatim (the literal fallback owned by
// go-qrdb, which also honours an explicit literal:// prefix).
//
// # Basic Usage
//
// Create a resolver registry and register the resolvers you need:
//
//	registry := secrets.NewResolverRegistry()
//	registry.Register(secrets.NewLocalKMSResolver(kmsClient))
//	registry.Register(secrets.NewRemoteKMSResolver(remoteClient))
//
// Use the registry to resolve individual secret references:
//
//	value, err := registry.Resolve(ctx, "kms://config/database/password")
//	if err != nil {
//	    // handle error
//	}
//	fmt.Println(value.String()) // resolved plaintext value
//
// # Processing Configuration Structs
//
// Use the ConfigProcessor to walk a configuration struct and resolve
// all secret references found in string fields:
//
//	type Config struct {
//	    Database struct {
//	        Password string `yaml:"password"`
//	    } `yaml:"database"`
//	}
//
//	processor := secrets.NewConfigProcessor(registry)
//	result, err := processor.Process(ctx, &config)
//	if err != nil {
//	    // handle error
//	}
//
//	// config.Database.Password now contains the resolved value
//	fmt.Printf("Resolved %d secrets\n", result.Resolved)
//
// # Generate-On-First-Use
//
// A config field whose secret has not been provisioned yet is expressed with
// the ensure form of the kms:// scheme, which mints the secret on first use
// and persists it, so every later boot resolves the SAME value:
//
//	kms://ca/root/keys/0/password?ensure=true&len=32&enc=base64
//
// Query parameters:
//
//   - ensure - boolean; when true the secret is generated and stored if absent
//   - len    - positive integer entropy length; bytes for base64/hex/raw,
//     digits for numeric (default 32)
//   - enc    - output encoding: base64, hex, numeric, or raw (default base64)
//
// The query string is not part of the secret identity; only the path is.
// LocalKMSResolver delegates this to SecretsService.EnsureSecret through the
// SecretsEnsurer interface, so the value is durable rather than regenerated
// per boot. A SecretsClient that does not implement SecretsEnsurer reports a
// typed ResolutionFailedError instead of degrading to a plain read.
//
// # Error Handling
//
// All errors in this package use typed error types that wrap the standard
// QBError type. Use the Is* helper functions to check error types:
//
//	value, err := registry.Resolve(ctx, uri)
//	if secrets.IsSecretNotFound(err) {
//	    // secret doesn't exist in the backend
//	}
//	if secrets.IsRemoteConnectionFailed(err) {
//	    // couldn't connect to remote KMS
//	}
//
// # Thread Safety
//
// The ResolverRegistry is thread-safe and can be used concurrently from
// multiple goroutines. Individual resolver implementations are also
// thread-safe.
//
// # Implementing Custom Resolvers
//
// Implement the SecretResolver interface to add support for additional
// secret backends:
//
//	type MyResolver struct {}
//
//	func (r *MyResolver) Scheme() string {
//	    return "myscheme"
//	}
//
//	func (r *MyResolver) CanResolve(raw string) bool {
//	    return strings.HasPrefix(raw, "myscheme://")
//	}
//
//	func (r *MyResolver) Resolve(ctx context.Context, raw string) (*SecretValue, error) {
//	    // extract path from URI and fetch secret; pass secret bytes directly
//	    return NewSecretValue(raw, fetchedBytes, StrategyLiteral), nil
//	}
package secrets
