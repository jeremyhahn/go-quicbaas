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

package sshstore

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	xkmsscope "github.com/jeremyhahn/go-xkms/pkg/scope"
	sshscope "github.com/jeremyhahn/go-xkms/pkg/ssh/scope"
	gossh "golang.org/x/crypto/ssh"
)

// The legacy file store kept each SSH record as a small JSON document. Its
// format is fixed: the files on disk were written once and are only ever
// read here, so the envelopes below copy that format field for field rather
// than importing it from go-xkms, which no longer reads it.

// legacyKeyEnvelope is a legacy key record. MaterialPEM holds the private
// key's OpenSSH PEM as a plain JSON string; it is empty for a key whose
// private half lives in a crypto backend.
type legacyKeyEnvelope struct {
	Version      int       `json:"v"`
	Scope        string    `json:"scope"`
	Owner        string    `json:"owner"`
	Name         string    `json:"name"`
	Algorithm    string    `json:"algo"`
	Fingerprint  string    `json:"fp"`
	CreatedAt    time.Time `json:"created"`
	Comment      string    `json:"comment,omitempty"`
	PublicKey    string    `json:"pub"`
	MaterialPEM  string    `json:"pem_b64,omitempty"`
	BackendID    string    `json:"backend_id,omitempty"`
	BackendKeyID string    `json:"backend_key_id,omitempty"`
}

// legacyPinEnvelope is a legacy host-pin record. PublicKey is the base64
// (standard) SSH wire form of the host key.
type legacyPinEnvelope struct {
	Version     int       `json:"v"`
	Scope       string    `json:"scope"`
	Owner       string    `json:"owner"`
	Host        string    `json:"host"`
	Fingerprint string    `json:"fp"`
	Algorithm   string    `json:"algo,omitempty"`
	AddedAt     time.Time `json:"added"`
	Source      string    `json:"source,omitempty"`
	PublicKey   string    `json:"pub,omitempty"`
}

// legacyAuthorizedEnvelope is a legacy authorized-keys record: the
// authorized_keys line verbatim, with the slot it was filed under.
type legacyAuthorizedEnvelope struct {
	Version int       `json:"v"`
	Scope   string    `json:"scope"`
	Owner   string    `json:"owner"`
	Name    string    `json:"name"`
	AddedAt time.Time `json:"added"`
	Line    string    `json:"line"`
}

// legacyKey is a decoded legacy key record. PEM is the private key's
// plaintext PEM; the caller wipes it once the key is written.
type legacyKey struct {
	Scope        sshscope.Scope
	Owner        string
	Name         string
	Algorithm    string
	Fingerprint  string
	CreatedAt    time.Time
	Comment      string
	PublicKey    gossh.PublicKey
	PEM          []byte
	BackendID    string
	BackendKeyID string
}

// legacyPin is a decoded legacy host pin.
type legacyPin struct {
	Scope       sshscope.Scope
	Owner       string
	Host        string
	Fingerprint string
	Algorithm   string
	AddedAt     time.Time
	Source      string
	PublicKey   []byte
}

// legacyAuthorized is a decoded legacy authorized-keys record.
type legacyAuthorized struct {
	Scope   sshscope.Scope
	Owner   string
	Name    string
	AddedAt time.Time
	Line    string
}

// decodeLegacyKey decodes a legacy key record and checks that its public key
// parses and, for a software key, that its PEM parses as a private key.
func decodeLegacyKey(data []byte) (*legacyKey, error) {
	var env legacyKeyEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	sc, err := xkmsscope.Parse(env.Scope)
	if err != nil {
		return nil, err
	}
	pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(env.PublicKey))
	if err != nil {
		return nil, err
	}
	key := &legacyKey{
		Scope:        sc,
		Owner:        env.Owner,
		Name:         env.Name,
		Algorithm:    env.Algorithm,
		Fingerprint:  env.Fingerprint,
		CreatedAt:    env.CreatedAt,
		Comment:      env.Comment,
		PublicKey:    pub,
		BackendID:    env.BackendID,
		BackendKeyID: env.BackendKeyID,
	}
	if env.BackendID != "" {
		return key, nil
	}
	if env.MaterialPEM == "" {
		return nil, errors.New("software key record has no private key material")
	}
	key.PEM = []byte(env.MaterialPEM)
	if _, err := gossh.ParseRawPrivateKey(key.PEM); err != nil {
		clear(key.PEM)
		return nil, err
	}
	return key, nil
}

// decodeLegacyPin decodes a legacy host-pin record. A pin written before
// pins carried the host public key cannot be migrated and is rejected.
func decodeLegacyPin(data []byte) (*legacyPin, error) {
	var env legacyPinEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	sc, err := xkmsscope.Parse(env.Scope)
	if err != nil {
		return nil, err
	}
	if env.PublicKey == "" {
		return nil, errors.New("host pin record has no public key")
	}
	pub, err := base64.StdEncoding.DecodeString(env.PublicKey)
	if err != nil {
		return nil, err
	}
	if _, err := gossh.ParsePublicKey(pub); err != nil {
		return nil, err
	}
	return &legacyPin{
		Scope:       sc,
		Owner:       env.Owner,
		Host:        env.Host,
		Fingerprint: env.Fingerprint,
		Algorithm:   env.Algorithm,
		AddedAt:     env.AddedAt,
		Source:      env.Source,
		PublicKey:   pub,
	}, nil
}

// decodeLegacyAuthorized decodes a legacy authorized-keys record and checks
// that its line carries a parseable public key.
func decodeLegacyAuthorized(data []byte) (*legacyAuthorized, error) {
	var env legacyAuthorizedEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	sc, err := xkmsscope.Parse(env.Scope)
	if err != nil {
		return nil, err
	}
	if _, _, _, _, err := gossh.ParseAuthorizedKey([]byte(env.Line)); err != nil {
		return nil, err
	}
	return &legacyAuthorized{
		Scope:   sc,
		Owner:   env.Owner,
		Name:    env.Name,
		AddedAt: env.AddedAt,
		Line:    env.Line,
	}, nil
}
