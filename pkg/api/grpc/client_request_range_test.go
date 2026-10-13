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

package grpc

import (
	"context"
	"math"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestClient_RequestOutOfRange proves that a secrets, crypto or scheduler
// request value too large for its int32 wire field is rejected as invalid
// input before the client touches the connection, instead of being sent as a
// wrapped integer.
func TestClient_RequestOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1
	ctx := context.Background()
	client := &Client{}

	tests := []struct {
		name  string
		field string
		call  func() error
	}{
		{
			name:  "ListSecrets limit",
			field: "limit",
			call: func() error {
				_, err := client.ListSecrets(ctx, &service.ListSecretsRequest{Limit: tooBig})
				return err
			},
		},
		{
			name:  "ListSecretVersions limit",
			field: "limit",
			call: func() error {
				_, err := client.ListSecretVersions(ctx, &service.ListSecretVersionsRequest{SecretID: "s1", Limit: tooBig})
				return err
			},
		},
		{
			name:  "GetSecretsByPrefix limit",
			field: "limit",
			call: func() error {
				_, err := client.GetSecretsByPrefix(ctx, &service.GetSecretsByPrefixRequest{Prefix: "app/", Limit: tooBig})
				return err
			},
		},
		{
			name:  "EnsureSecret length",
			field: "length",
			call: func() error {
				_, err := client.EnsureSecret(ctx, &service.EnsureSecretRequest{SecretID: "s1", Length: tooBig})
				return err
			},
		},
		{
			name:  "KMSGenerateRandom num bytes",
			field: "num_bytes",
			call: func() error {
				_, err := client.KMSGenerateRandom(ctx, &service.GenerateRandomRequest{NumBytes: tooBig})
				return err
			},
		},
		{
			name:  "SchedulerListJobs limit",
			field: "limit",
			call: func() error {
				_, err := client.SchedulerListJobs(ctx, &service.ListSchedulerJobsRequest{Limit: tooBig})
				return err
			},
		},
		{
			name:  "SchedulerGetJobHistory limit",
			field: "limit",
			call: func() error {
				_, err := client.SchedulerGetJobHistory(ctx, &service.GetSchedulerJobHistoryRequest{JobID: 1, Limit: tooBig})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireClientRangeError(t, tt.call(), tt.field)
		})
	}
}
