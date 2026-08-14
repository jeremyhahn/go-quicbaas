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

// dynamicsecrets_handler.go routes the quicbaas.DynamicSecrets service
// (Security().DynamicSecrets()) over the generated DynamicSecretsService gRPC
// proto.
//
// The SDK dynamic secrets client speaks plain Go structs that do not implement
// proto.Message, so the generic conn.Invoke path cannot marshal them. This
// handler converts each request into its proto counterpart and decodes the
// reply back into the leaf sdk/go/client/dynamicsecrets response types the
// client type-asserts on — the same contract the REST handler implements.

package grpc

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// dynamicSecretsServicePrefix is the gRPC-style service identifier the SDK
// dynamic secrets surface routes through. It matches the const used by the
// REST / QUIC / MCP / embedded transports.
const dynamicSecretsServicePrefix = "quicbaas.DynamicSecrets"

// isDynamicSecretsService reports whether svc targets the
// quicbaas.DynamicSecrets surface.
func isDynamicSecretsService(svc string) bool {
	return strings.HasPrefix(svc, dynamicSecretsServicePrefix)
}

// dynamicSecretsHandler converts between the leaf client/dynamicsecrets
// structs and the generated DynamicSecretsService proto types.
type dynamicSecretsHandler struct {
	conn *grpc.ClientConn
}

// newDynamicSecretsHandler creates a handler bound to conn.
func newDynamicSecretsHandler(conn *grpc.ClientConn) *dynamicSecretsHandler {
	return &dynamicSecretsHandler{conn: conn}
}

// dynamicSecretsMethod is the signature every DynamicSecrets verb implements.
type dynamicSecretsMethod func(*dynamicSecretsHandler, context.Context, pb.DynamicSecretsServiceClient, interface{}) (interface{}, error)

// dynamicSecretsMethods maps SDK method names to their handler funcs for O(1)
// dispatch. The eight entries mirror the eight RPCs on DynamicSecretsService.
var dynamicSecretsMethods = map[string]dynamicSecretsMethod{
	"CreateRole":         (*dynamicSecretsHandler).createRole,
	"GetRole":            (*dynamicSecretsHandler).getRole,
	"ListRoles":          (*dynamicSecretsHandler).listRoles,
	"DeleteRole":         (*dynamicSecretsHandler).deleteRole,
	"GenerateCredential": (*dynamicSecretsHandler).generateCredential,
	"RenewLease":         (*dynamicSecretsHandler).renewLease,
	"RevokeLease":        (*dynamicSecretsHandler).revokeLease,
	"ListLeases":         (*dynamicSecretsHandler).listLeases,
}

// handleRequest dispatches a DynamicSecrets method to its gRPC handler.
func (h *dynamicSecretsHandler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	fn, ok := dynamicSecretsMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("grpc.dynamicsecrets."+method,
			"method not exposed by DynamicSecretsService gRPC proto")
	}
	return fn(h, ctx, pb.NewDynamicSecretsServiceClient(h.conn), req)
}

func (h *dynamicSecretsHandler) createRole(ctx context.Context, client pb.DynamicSecretsServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*dsclient.CreateRoleRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.dynamicsecrets.CreateRole",
			"invalid request type, expected *dynamicsecrets.CreateRoleRequest")
	}

	pbResp, err := client.CreateRole(ctx, &pb.DynSecretsCreateRoleRequest{
		Name:          r.Name,
		GeneratorType: r.GeneratorType,
		Config:        r.Config,
		DefaultTtl:    durationpb.New(r.DefaultTTL),
		MaxTtl:        durationpb.New(r.MaxTTL),
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.dynamicsecrets.CreateRole")
	}

	return &dsclient.CreateRoleResponse{
		Role:      dsRoleFromProto(pbResp.GetRole()),
		CreatedAt: dsTimeFromProto(pbResp.GetTimestamp()),
	}, nil
}

func (h *dynamicSecretsHandler) getRole(ctx context.Context, client pb.DynamicSecretsServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*dsclient.GetRoleRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.dynamicsecrets.GetRole",
			"invalid request type, expected *dynamicsecrets.GetRoleRequest")
	}

	pbResp, err := client.GetRole(ctx, &pb.DynSecretsGetRoleRequest{Name: r.Name})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.dynamicsecrets.GetRole")
	}

	return &dsclient.GetRoleResponse{Role: dsRoleFromProto(pbResp.GetRole())}, nil
}

func (h *dynamicSecretsHandler) listRoles(ctx context.Context, client pb.DynamicSecretsServiceClient, req interface{}) (interface{}, error) {
	pbReq := &pb.DynSecretsListRolesRequest{}
	if r, ok := req.(*dsclient.ListRolesRequest); ok && r != nil {
		pbReq.Prefix = r.Prefix
	}

	pbResp, err := client.ListRoles(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "grpc.dynamicsecrets.ListRoles")
	}

	roles := make([]*dsclient.Role, 0, len(pbResp.GetRoles()))
	for _, role := range pbResp.GetRoles() {
		roles = append(roles, dsRoleFromProto(role))
	}
	return &dsclient.ListRolesResponse{Roles: roles, Count: int(pbResp.GetCount())}, nil
}

func (h *dynamicSecretsHandler) deleteRole(ctx context.Context, client pb.DynamicSecretsServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*dsclient.DeleteRoleRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.dynamicsecrets.DeleteRole",
			"invalid request type, expected *dynamicsecrets.DeleteRoleRequest")
	}

	pbResp, err := client.DeleteRole(ctx, &pb.DynSecretsDeleteRoleRequest{Name: r.Name})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.dynamicsecrets.DeleteRole")
	}

	return &dsclient.DeleteRoleResponse{DeletedAt: dsTimeFromProto(pbResp.GetTimestamp())}, nil
}

func (h *dynamicSecretsHandler) generateCredential(ctx context.Context, client pb.DynamicSecretsServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*dsclient.GenerateCredentialRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.dynamicsecrets.GenerateCredential",
			"invalid request type, expected *dynamicsecrets.GenerateCredentialRequest")
	}

	pbReq := &pb.GenerateCredentialRequest{RoleName: r.RoleName}
	if r.TTL > 0 {
		pbReq.Ttl = durationpb.New(r.TTL)
	}

	pbResp, err := client.GenerateCredential(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "grpc.dynamicsecrets.GenerateCredential")
	}

	lease := dsLeaseFromProto(pbResp.GetLease())
	return &dsclient.GenerateCredentialResponse{
		Credential: dsCredentialFromProto(pbResp.GetCredential(), pbResp.GetLeaseId(), lease),
		Lease:      lease,
		LeaseID:    pbResp.GetLeaseId(),
	}, nil
}

func (h *dynamicSecretsHandler) renewLease(ctx context.Context, client pb.DynamicSecretsServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*dsclient.RenewLeaseRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.dynamicsecrets.RenewLease",
			"invalid request type, expected *dynamicsecrets.RenewLeaseRequest")
	}

	pbReq := &pb.RenewLeaseRequest{LeaseId: r.LeaseID}
	if r.Increment > 0 {
		pbReq.Increment = durationpb.New(r.Increment)
	}

	pbResp, err := client.RenewLease(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "grpc.dynamicsecrets.RenewLease")
	}

	return &dsclient.RenewLeaseResponse{
		Lease:     dsLeaseFromProto(pbResp.GetLease()),
		RenewedAt: dsTimeFromProto(pbResp.GetTimestamp()),
	}, nil
}

func (h *dynamicSecretsHandler) revokeLease(ctx context.Context, client pb.DynamicSecretsServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*dsclient.RevokeLeaseRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.dynamicsecrets.RevokeLease",
			"invalid request type, expected *dynamicsecrets.RevokeLeaseRequest")
	}

	pbResp, err := client.RevokeLease(ctx, &pb.RevokeLeaseRequest{LeaseId: r.LeaseID})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.dynamicsecrets.RevokeLease")
	}

	return &dsclient.RevokeLeaseResponse{RevokedAt: dsTimeFromProto(pbResp.GetTimestamp())}, nil
}

func (h *dynamicSecretsHandler) listLeases(ctx context.Context, client pb.DynamicSecretsServiceClient, req interface{}) (interface{}, error) {
	pbReq := &pb.ListLeasesRequest{}
	if r, ok := req.(*dsclient.ListLeasesRequest); ok && r != nil {
		pbReq.Prefix = r.Prefix
	}

	pbResp, err := client.ListLeases(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "grpc.dynamicsecrets.ListLeases")
	}

	leases := make([]*dsclient.Lease, 0, len(pbResp.GetLeases()))
	for _, l := range pbResp.GetLeases() {
		leases = append(leases, dsLeaseFromProto(l))
	}
	return &dsclient.ListLeasesResponse{Leases: leases, Count: int(pbResp.GetCount())}, nil
}

// =============================================================================
// Conversion helpers
// =============================================================================

// dsRoleFromProto converts a proto Role into the SDK role type.
func dsRoleFromProto(r *pb.Role) *dsclient.Role {
	if r == nil {
		return nil
	}
	return &dsclient.Role{
		Name:          r.GetName(),
		GeneratorType: r.GetGeneratorType(),
		Config:        r.GetConfig(),
		DefaultTTL:    r.GetDefaultTtl().AsDuration(),
		MaxTTL:        r.GetMaxTtl().AsDuration(),
	}
}

// dsLeaseFromProto converts a proto Lease into the SDK lease type.
func dsLeaseFromProto(l *pb.Lease) *dsclient.Lease {
	if l == nil {
		return nil
	}
	return &dsclient.Lease{
		LeaseID:    l.GetLeaseId(),
		SecretPath: l.GetSecretPath(),
		IssuedAt:   dsTimeFromProto(l.GetCreatedAt()),
		ExpiresAt:  dsTimeFromProto(l.GetExpiresAt()),
		TTL:        l.GetTtl().AsDuration(),
		RenewCount: int(l.GetRenewCount()),
		Renewable:  l.GetRenewable(),
		Metadata:   l.GetMetadata(),
	}
}

// dsCredentialFromProto converts a proto Credential into the SDK credential
// type. The proto carries the generator's full data map under `metadata`; the
// lease supplies the expiry, which the proto Credential does not repeat.
func dsCredentialFromProto(c *pb.Credential, leaseID string, lease *dsclient.Lease) *dsclient.Credential {
	if c == nil {
		return nil
	}
	cred := &dsclient.Credential{
		CredentialID: c.GetCredentialId(),
		Data:         dsCredentialData(c),
		LeaseID:      leaseID,
	}
	if lease != nil {
		cred.ExpiresAt = lease.ExpiresAt
	}
	return cred
}

// dsCredentialData flattens a proto Credential into the SDK's key/value data
// map. The generator's own map is authoritative; the typed convenience fields
// are folded in only when the map does not already carry them.
func dsCredentialData(c *pb.Credential) map[string]string {
	data := make(map[string]string, len(c.GetMetadata())+4)
	for k, v := range c.GetMetadata() {
		data[k] = v
	}
	for key, value := range map[string]string{
		"username":          c.GetUsername(),
		"password":          c.GetPassword(),
		"connection_string": c.GetConnectionString(),
		"host":              c.GetHost(),
		"database":          c.GetDatabase(),
	} {
		if value == "" {
			continue
		}
		if _, exists := data[key]; !exists {
			data[key] = value
		}
	}
	if len(data) == 0 {
		return nil
	}
	return data
}

// dsTimeFromProto converts a proto timestamp to a time.Time, mapping nil and
// the Unix epoch to the zero time so callers can use IsZero() to detect an
// absent value.
func dsTimeFromProto(ts *timestamppb.Timestamp) time.Time {
	if ts == nil || (ts.GetSeconds() == 0 && ts.GetNanos() == 0) {
		return time.Time{}
	}
	return ts.AsTime()
}
