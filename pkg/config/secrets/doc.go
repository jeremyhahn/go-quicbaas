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
//   - auto, 123456, changeme, placeholder - Auto-generate secrets
//
// # Basic Usage
//
// Create a resolver registry and register the resolvers you need:
//
//	registry := secrets.NewResolverRegistry()
//	registry.Register(secrets.NewLocalKMSResolver(kmsClient))
//	registry.Register(secrets.NewRemoteKMSResolver(remoteClient))
//	registry.Register(secrets.NewAutoGenResolver())
//
// Use the registry to resolve individual secret references:
//
//	value, err := registry.Resolve(ctx, "kms://config/database/password")
//	if err != nil {
//	    // handle error
//	}
//	fmt.Println(value.Value) // resolved plaintext value
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
//	for _, resolved := range result.Resolved {
//	    fmt.Printf("Resolved %s using %s\n", resolved.FieldPath, resolved.Strategy)
//	}
//
// # Auto-Generation
//
// The AutoGenResolver generates high-entropy secrets when configuration
// values match certain placeholders. This is useful for initial setup
// when secrets haven't been provisioned yet:
//
//   - "auto" - triggers auto-generation
//   - "123456" - common placeholder
//   - "changeme" - common placeholder
//   - "CHANGEME" - common placeholder
//   - "placeholder" - explicit placeholder
//
// Generated secrets are 32 bytes (256 bits) of cryptographically random
// data, base64-encoded using URL-safe encoding.
//
// # Error Handling
//
// All errors in this package use typed error types that wrap the standard
// DragonError type. Use the Is* helper functions to check error types:
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
//	    // extract path from URI and fetch secret
//	    return &SecretValue{
//	        Raw:      raw,
//	        Value:    fetchedValue,
//	        Strategy: StrategyLiteral, // or define your own
//	    }, nil
//	}
package secrets
