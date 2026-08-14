// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration && awskms

// awskms_test.go exercises the AWS KMS barrier sealing strategy end-to-end
// against a Floci KMS emulator. It is compiled only when the daemon
// under test was built with the `awskms` tag (so the seal factory is present)
// AND the suite is built with `integration`.
//
// Setup creates a symmetric KMS customer master key in Floci via the
// AWS SDK, then boots a daemon whose seal.strategy=awskms points at the same
// Floci endpoint, and drives the init -> seal -> unseal round-trip.
package seal

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

// flociEndpointEnv names the env var carrying the Floci endpoint.
// The integration compose sets AWS_ENDPOINT_URL=http://floci:4566.
const flociEndpointEnv = "AWS_ENDPOINT_URL"

// awsRegionEnv / awsCredsEnv name the Floci-dummy AWS credentials env.
const (
	awsRegionEnv = "AWS_DEFAULT_REGION"
	awsAKIDEnv   = "AWS_ACCESS_KEY_ID"
	awsSecretEnv = "AWS_SECRET_ACCESS_KEY"
)

// resolveFloci returns (endpoint, region, accessKeyID, secretAccessKey)
// for the Floci KMS emulator, falling back to the compose defaults when
// the env is partially set.
func resolveFloci() (endpoint, region, akid, secret string) {
	endpoint = os.Getenv(flociEndpointEnv)
	if endpoint == "" {
		endpoint = "http://floci:4566"
	}
	region = os.Getenv(awsRegionEnv)
	if region == "" {
		region = "us-east-1"
	}
	akid = os.Getenv(awsAKIDEnv)
	if akid == "" {
		akid = "test"
	}
	secret = os.Getenv(awsSecretEnv)
	if secret == "" {
		secret = "test"
	}
	return endpoint, region, akid, secret
}

// createKMSKey provisions a symmetric AES-256 customer master key in
// Floci and returns its key ID. The key models the operator-provisioned
// CMK the awskms seal strategy wraps the barrier root key under.
func createKMSKey(t *testing.T, endpoint, region, akid, secret string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(akid, secret, ""),
		),
	)
	if err != nil {
		t.Fatalf("load AWS config for Floci: %v", err)
	}

	client := kms.NewFromConfig(cfg, func(o *kms.Options) {
		o.BaseEndpoint = &endpoint
	})

	out, err := client.CreateKey(ctx, &kms.CreateKeyInput{
		KeySpec:  kmstypes.KeySpecSymmetricDefault,
		KeyUsage: kmstypes.KeyUsageTypeEncryptDecrypt,
	})
	if err != nil {
		t.Fatalf("Floci KMS CreateKey failed (endpoint=%s): %v", endpoint, err)
	}
	if out.KeyMetadata == nil || out.KeyMetadata.KeyId == nil {
		t.Fatal("Floci KMS CreateKey returned no key metadata")
	}
	return *out.KeyMetadata.KeyId
}

// awsKMSConfigYAML renders the daemon seal config for the awskms strategy.
func awsKMSConfigYAML(endpoint, region, keyID, akid, secret string) string {
	return fmt.Sprintf(`seal:
  enabled: true
  strategy: awskms
  awskms:
    region: %q
    key_id: %q
    access_key_id: %q
    secret_access_key: %q
    endpoint: %q
`, region, keyID, akid, secret, endpoint)
}

// TestAWSKMSSealRoundTrip boots a daemon configured with the awskms strategy
// pointed at Floci KMS and asserts the init -> seal -> unseal round-trip
// recovers the barrier root key.
func TestAWSKMSSealRoundTrip(t *testing.T) {
	endpoint, region, akid, secret := resolveFloci()
	keyID := createKMSKey(t, endpoint, region, akid, secret)
	t.Logf("provisioned Floci KMS key: %s", keyID)

	cfgYAML := awsKMSConfigYAML(endpoint, region, keyID, akid, secret)
	runCloudSealRoundTrip(t, "awskms", cfgYAML)
}
