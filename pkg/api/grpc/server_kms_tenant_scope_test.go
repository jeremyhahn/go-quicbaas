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
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/kmstenant"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// kmsTenantServers are the two gRPC servers that serve KMS verbs.
type kmsTenantServers struct {
	kms *KMSServer
	dek *DEKServer
}

// kmsTenantVerb is one KMS verb called on gRPC with a key name or DEK path.
type kmsTenantVerb struct {
	name string
	id   string
	call func(s kmsTenantServers, ctx context.Context, id string) error
}

func kmsTenantVerbs() []kmsTenantVerb {
	key, signing, dek := kmstenant.KeyName, kmstenant.SigningKeyName, kmstenant.DEKPath
	return []kmsTenantVerb{
		{"GetKey", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.GetKey(ctx, &pb.GetKeyRequest{KeyId: id})
			return err
		}},
		{"GetPublicKey", signing, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.GetPublicKey(ctx, &pb.GetPublicKeyRequest{KeyId: id})
			return err
		}},
		{"RotateKey", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.RotateKey(ctx, &pb.RotateKeyRequest{KeyId: id})
			return err
		}},
		{"RevokeKey", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.RevokeKey(ctx, &pb.RevokeKeyRequest{KeyId: id})
			return err
		}},
		{"DeleteKey", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.DeleteKey(ctx, &pb.DeleteKeyRequest{KeyId: id, Force: true})
			return err
		}},
		{"GetKeyMetadata", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.GetKeyMetadata(ctx, &pb.GetKeyMetadataRequest{KeyId: id})
			return err
		}},
		{"UpdateKeyMetadata", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.UpdateKeyMetadata(ctx, &pb.UpdateKeyMetadataRequest{KeyId: id, Tags: map[string]string{"env": "prod"}})
			return err
		}},
		{"UpdateKeyStatus", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.UpdateKeyStatus(ctx, &pb.UpdateKeyStatusRequest{KeyId: id, Status: pb.KeyStatus_KEY_STATUS_DISABLED})
			return err
		}},
		{"ListKeyVersions", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.ListKeyVersions(ctx, &pb.ListKeyVersionsRequest{KeyId: id})
			return err
		}},
		{"SetKeyPolicy", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.SetKeyPolicy(ctx, &pb.SetKeyPolicyRequest{KeyId: id, Policy: &pb.KeyPolicy{
				AllowedOperations: []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_ENCRYPT},
			}})
			return err
		}},
		{"GetKeyPolicy", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.GetKeyPolicy(ctx, &pb.GetKeyPolicyRequest{KeyId: id})
			return err
		}},
		{"DeleteKeyPolicy", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.DeleteKeyPolicy(ctx, &pb.DeleteKeyPolicyRequest{KeyId: id})
			return err
		}},
		{"SetKeyRotationPolicy", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.SetKeyRotationPolicy(ctx, &pb.SetKeyRotationPolicyRequest{KeyId: id, Policy: &pb.RotationPolicy{
				Enabled: true, AutoRotate: true, IntervalSeconds: 3600,
			}})
			return err
		}},
		{"GetKeyRotationPolicy", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.GetKeyRotationPolicy(ctx, &pb.GetKeyRotationPolicyRequest{KeyId: id})
			return err
		}},
		{"DeleteKeyRotationPolicy", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.DeleteKeyRotationPolicy(ctx, &pb.DeleteKeyRotationPolicyRequest{KeyId: id})
			return err
		}},
		{"SetKeySafety", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.SetKeySafety(ctx, &pb.SetKeySafetyRequest{KeyId: id, MaxOperations: 1000})
			return err
		}},
		{"ClearKeySafety", key, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.kms.ClearKeySafety(ctx, &pb.ClearKeySafetyRequest{KeyId: id})
			return err
		}},
		{"GetDEK", dek, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.dek.GetDEK(ctx, &pb.GetDEKRequest{Path: id})
			return err
		}},
		{"RotateDEK", dek, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.dek.RotateDEK(ctx, &pb.RotateDEKRequest{Path: id})
			return err
		}},
		{"DeleteDEK", dek, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.dek.DeleteDEK(ctx, &pb.DeleteDEKRequest{Path: id, Force: true})
			return err
		}},
		{"GetDEKStatus", dek, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.dek.GetDEKStatus(ctx, &pb.GetDEKStatusRequest{Path: id})
			return err
		}},
		{"SetDefaultDEK", dek, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.dek.SetDefaultDEK(ctx, &pb.SetDefaultDEKRequest{Path: id})
			return err
		}},
		{"SetDEKSafety", dek, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.dek.SetDEKSafety(ctx, &pb.SetDEKSafetyRequest{Path: id, MaxOperations: 1000})
			return err
		}},
		{"ClearDEKSafety", dek, func(s kmsTenantServers, ctx context.Context, id string) error {
			_, err := s.dek.ClearDEKSafety(ctx, &pb.ClearDEKSafetyRequest{Path: id})
			return err
		}},
	}
}

// TestKMSServer_TenantBGetsNotFoundForTenantAKey pins #1476 on gRPC: the
// tenant the authorization interceptor derives from the caller's token
// reaches the KMS service, so tenant B naming tenant A's key or DEK gets
// the same NotFound a missing id gets and leaves tenant A's state
// unchanged, while tenant A still reaches its own.
func TestKMSServer_TenantBGetsNotFoundForTenantAKey(t *testing.T) {
	cases := []struct {
		name      string
		tenant    string
		missingID bool
		want      codes.Code
	}{
		{name: "tenant A reaches its own", tenant: kmstenant.OwnerTenant, want: codes.OK},
		{name: "tenant B gets not found for tenant A's id", tenant: kmstenant.OtherTenant, want: codes.NotFound},
		{name: "tenant A gets not found for a missing id", tenant: kmstenant.OwnerTenant, missingID: true, want: codes.NotFound},
	}

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	for _, verb := range kmsTenantVerbs() {
		for _, tc := range cases {
			t.Run(verb.name+"/"+tc.name, func(t *testing.T) {
				svc := kmstenant.NewService(t)
				servers := kmsTenantServers{
					kms: NewKMSServerWithService(nil, svc, log),
					dek: NewDEKServer(svc, log),
				}
				ctx := contextWithAuthorizedSubject(context.Background(), &platformtoken.AuthorizedContext{
					Subject: "caller", TenantID: tc.tenant,
				})
				id := verb.id
				if tc.missingID {
					id = kmstenant.MissingID
				}
				before := kmstenant.ReadOwnerState(t, svc)

				err := verb.call(servers, ctx, id)

				require.Equal(t, tc.want, status.Code(err), "error: %v", err)
				if tc.want != codes.OK {
					require.Equal(t, before, kmstenant.ReadOwnerState(t, svc), "a refused call changed tenant A's state")
				}
			})
		}
	}
}

// TestKMSServer_ImportKeyIsTenantScoped proves ImportKey goes through the
// tenant-scoped service (#1992): the importing tenant reads its key back,
// another tenant cannot, and a name in the tenant namespace is refused.
func TestKMSServer_ImportKeyIsTenantScoped(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	svc := kmstenant.NewService(t)
	srv := NewKMSServerWithService(nil, svc, log)
	ctxFor := func(tenant string) context.Context {
		return contextWithAuthorizedSubject(context.Background(), &platformtoken.AuthorizedContext{
			Subject: "caller", TenantID: tenant,
		})
	}
	spec := &pb.KeySpec{Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM}
	owner, other := ctxFor(kmstenant.OwnerTenant), ctxFor(kmstenant.OtherTenant)

	_, err = srv.ImportKey(owner, &pb.ImportKeyRequest{Name: "foo", KeyMaterial: []byte("material"), KeySpec: spec})
	require.NoError(t, err)

	got, err := srv.GetKey(owner, &pb.GetKeyRequest{KeyId: "foo"})
	require.NoError(t, err)
	require.Equal(t, "foo", got.GetKey().GetKeyId())
	listed, err := srv.ListKeys(owner, &pb.ListKeysRequest{})
	require.NoError(t, err)
	var names []string
	for _, k := range listed.GetKeys() {
		names = append(names, k.GetKeyId())
	}
	require.Contains(t, names, "foo")

	_, err = srv.GetKey(other, &pb.GetKeyRequest{KeyId: "foo"})
	require.Equal(t, codes.NotFound, status.Code(err), "error: %v", err)

	crossName := "tenant/" + kmstenant.OtherTenant + "/planted"
	_, err = srv.ImportKey(owner, &pb.ImportKeyRequest{Name: crossName, KeyMaterial: []byte("material"), KeySpec: spec})
	require.Equal(t, codes.InvalidArgument, status.Code(err), "error: %v", err)
	_, err = srv.GetKey(other, &pb.GetKeyRequest{KeyId: "planted"})
	require.Equal(t, codes.NotFound, status.Code(err), "a refused import wrote into the other tenant: %v", err)
}

// TestKMSServer_ImportKeyRefusedWithoutService proves ImportKey never falls
// back to the manager, which would take the caller's name verbatim.
func TestKMSServer_ImportKeyRefusedWithoutService(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	mgr := kmstenant.NewManager()
	srv := NewKMSServer(mgr, log)

	_, err = srv.ImportKey(context.Background(), &pb.ImportKeyRequest{
		Name: "foo", KeyMaterial: []byte("material"),
		KeySpec: &pb.KeySpec{Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err), "error: %v", err)
	require.Contains(t, status.Convert(err).Message(), ErrKMSServiceRequired.Error())
	keys, err := mgr.ListKeys(context.Background(), "")
	require.NoError(t, err)
	require.Empty(t, keys, "a refused import reached the manager")
}
