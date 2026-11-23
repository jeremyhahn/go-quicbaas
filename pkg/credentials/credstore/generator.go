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

// Package credstore generates dynamic secrets into go-qrdb's barrier-backed
// credential store, where `credential://` references resolve them.
package credstore

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
)

// GeneratorType is the role generator_type of this generator.
const GeneratorType = "credential"

// Config keys, defaults and bounds.
const (
	configLength   = "length"
	configEncoding = "encoding"

	encodingBase64 = "base64"
	encodingHex    = "hex"

	defaultLength = 32
	minLength     = 16
	maxLength     = 1024

	idBytes         = 16
	referencePrefix = "dynamic-"
	referenceScheme = "credential://"
)

// Store is the credential store one call writes to: the caller's tenant's
// barrier-backed store, or the system store for a call with no tenant.
type Store interface {
	Put(ctx context.Context, name string, value []byte) error
	Delete(ctx context.Context, name string) error
}

// StoreFor returns the Store for ctx's tenant. It fails closed: a tenant
// with no unsealed barrier is an error, never the system store.
type StoreFor func(ctx context.Context) (Store, error)

// ReferenceName is the credential-store name of a generated credential:
// "dynamic-" + credentialID. A caller resolves it as credential://<name>.
func ReferenceName(credentialID string) string {
	return referencePrefix + credentialID
}

// Generator mints random secrets into the credential store. It holds no
// logger, and nothing it returns carries a secret value except the generated
// credential's Data.
type Generator struct {
	storeFor StoreFor
	now      func() time.Time
}

var (
	_ credentials.CredentialGenerator = (*Generator)(nil)
	_ credentials.MaterialKeeper      = (*Generator)(nil)
)

// NewGenerator returns the generator. A nil storeFor is an error.
func NewGenerator(storeFor StoreFor) (*Generator, error) {
	if storeFor == nil {
		return nil, errors.New("credstore: nil store resolver")
	}
	return &Generator{storeFor: storeFor, now: time.Now}, nil
}

// Type returns GeneratorType.
func (g *Generator) Type() string { return GeneratorType }

// KeepsMaterial reports true: the secret lives in the credential store only.
func (g *Generator) KeepsMaterial() bool { return true }

type genConfig struct {
	length   int
	encoding string
}

func parseConfig(config map[string]string) (genConfig, error) {
	cfg := genConfig{length: defaultLength, encoding: encodingBase64}
	for key, raw := range config {
		switch key {
		case configLength:
			n, err := strconv.Atoi(raw)
			if err != nil {
				return cfg, fmt.Errorf("credstore: config %q must be a decimal number of bytes", configLength)
			}
			if n < minLength || n > maxLength {
				return cfg, fmt.Errorf("credstore: config %q must be between %d and %d", configLength, minLength, maxLength)
			}
			cfg.length = n
		case configEncoding:
			if raw != encodingBase64 && raw != encodingHex {
				return cfg, fmt.Errorf("credstore: config %q must be %q or %q", configEncoding, encodingBase64, encodingHex)
			}
			cfg.encoding = raw
		default:
			return cfg, fmt.Errorf("credstore: unknown config key %q", key)
		}
	}
	return cfg, nil
}

// ValidateConfig accepts only the keys "length" (16 to 1024 bytes, default
// 32) and "encoding" ("base64" or "hex", default "base64").
func (g *Generator) ValidateConfig(config map[string]string) error {
	_, err := parseConfig(config)
	return err
}

// Generate mints a random secret, writes it to the caller's credential store
// under ReferenceName(id), and returns it with its credential:// reference.
func (g *Generator) Generate(ctx context.Context, role *credentials.Role) (*credentials.Credential, error) {
	if role == nil {
		return nil, errors.New("credstore: generate: nil role")
	}
	cfg, err := parseConfig(role.Config)
	if err != nil {
		return nil, err
	}

	idRaw := make([]byte, idBytes)
	if _, err := rand.Read(idRaw); err != nil {
		return nil, fmt.Errorf("credstore: generate: credential id: %w", err)
	}
	id := hex.EncodeToString(idRaw)

	secret := make([]byte, cfg.length)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("credstore: generate %s: random value: %w", id, err)
	}
	var value string
	if cfg.encoding == encodingHex {
		value = hex.EncodeToString(secret)
	} else {
		value = base64.RawURLEncoding.EncodeToString(secret)
	}
	clear(secret)

	store, err := g.storeFor(ctx)
	if err != nil {
		return nil, fmt.Errorf("credstore: generate %s: credential store: %w", id, err)
	}
	if err := store.Put(ctx, ReferenceName(id), []byte(value)); err != nil {
		return nil, fmt.Errorf("credstore: generate %s: store: %w", id, err)
	}

	now := g.now()
	return &credentials.Credential{
		CredentialID: id,
		Type:         GeneratorType,
		Data: map[string]string{
			"value":     value,
			"reference": referenceScheme + ReferenceName(id),
		},
		CreatedAt: now,
		ExpiresAt: now.Add(role.DefaultTTL),
	}, nil
}

// Revoke deletes the credential from the caller's credential store. A
// credential that is already gone is success, so the revoke is idempotent.
func (g *Generator) Revoke(ctx context.Context, cred *credentials.Credential) error {
	if cred == nil || cred.CredentialID == "" {
		return errors.New("credstore: revoke: empty credential id")
	}
	id := cred.CredentialID
	store, err := g.storeFor(ctx)
	if err != nil {
		return fmt.Errorf("credstore: revoke %s: credential store: %w", id, err)
	}
	if err := store.Delete(ctx, ReferenceName(id)); err != nil {
		if errors.Is(err, qrdbsdk.ErrCredentialNotFound) {
			return nil
		}
		return fmt.Errorf("credstore: revoke %s: delete: %w", id, err)
	}
	return nil
}

// Rotate generates a new credential, then revokes the old one. If the revoke
// fails, the new credential is returned together with the error, so the
// caller keeps it and sees the failure.
func (g *Generator) Rotate(ctx context.Context, old *credentials.Credential, role *credentials.Role) (*credentials.Credential, error) {
	next, err := g.Generate(ctx, role)
	if err != nil {
		return nil, err
	}
	if old != nil {
		if err := g.Revoke(ctx, old); err != nil {
			return next, fmt.Errorf("credstore: rotate %s: revoke previous: %w", next.CredentialID, err)
		}
	}
	return next, nil
}
