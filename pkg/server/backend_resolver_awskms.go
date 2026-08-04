// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

//go:build awskms

package server

import (
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the AWS KMS backend factory. It is compiled only under
// the `awskms` build tag (aws-sdk-go-v2). When the tag is absent the factory is
// unregistered and the resolver returns BackendNotCompiledError. The plaintext
// key never leaves AWS KMS; nothing is reimplemented here.

// buildAWSKMSServiceBackend constructs the AWS KMS ServiceBackend from
// cfg.Seal.AWSKMS. keyStore is the caller's persistence seam: although the
// plaintext key never leaves AWS KMS, the backend's key-metadata store is backed
// by keyStore so per-key references survive restarts.
func buildAWSKMSServiceBackend(cfg *config.Config, spec BackendSpec, keyStore xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
	aws := cfg.Seal.AWSKMS
	backend, err := xkmssdk.NewAWSKMSBackend(&xkmssdk.AWSKMSBackendConfig{
		Region:          aws.Region,
		KeyID:           aws.KeyID,
		AccessKeyID:     aws.AccessKeyID,
		SecretAccessKey: aws.SecretAccessKey,
		SessionToken:    aws.SessionToken,
		Endpoint:        aws.Endpoint,
		KeyStorage:      keyStore,
		CertStorage:     xkmssdk.NewStorageMemory(),
	})
	if err != nil {
		return nil, &BackendConstructError{Backend: config.BackendTypeAWSKMS, Cause: err}
	}
	return wrapServiceBackend(config.BackendTypeAWSKMS, backend)
}

// init registers the AWS KMS factory. Reached only under -tags awskms.
func init() {
	registerBackendFactory(config.BackendTypeAWSKMS, buildAWSKMSServiceBackend)
}
