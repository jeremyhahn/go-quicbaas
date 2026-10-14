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

//go:build integration
// +build integration

package iam_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"gopkg.in/yaml.v3"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	quictransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/quic"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// Protocol represents the communication protocol type.
type Protocol string

const (
	// ProtocolEmbedded represents direct in-process SDK client
	ProtocolEmbedded Protocol = "Embedded"

	// ProtocolREST represents HTTP REST API
	ProtocolREST Protocol = "REST"

	// ProtocolGRPC represents gRPC protocol
	ProtocolGRPC Protocol = "gRPC"

	// ProtocolQUIC represents QUIC protocol
	ProtocolQUIC Protocol = "QUIC"

	// ProtocolUnixSocket represents gRPC over Unix domain socket
	ProtocolUnixSocket Protocol = "UnixSocket"
)

// ProtocolClient provides a unified interface for different protocol clients.
type ProtocolClient interface {
	// Tenant operations
	CreateTenant(ctx context.Context, id, name string, settings map[string]interface{}) (*iam.Tenant, error)
	GetTenant(ctx context.Context, id string) (*iam.Tenant, error)
	UpdateTenant(ctx context.Context, id, name string, settings map[string]interface{}) (*iam.Tenant, error)
	DeleteTenant(ctx context.Context, id string) error
	ListTenants(ctx context.Context) ([]iam.Tenant, error)

	// User operations
	CreateUser(ctx context.Context, tenantID, email, name string) (*iam.User, error)
	GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error)
	UpdateUser(ctx context.Context, tenantID, userID, name string) (*iam.User, error)
	DeleteUser(ctx context.Context, tenantID, userID string) error
	ListUsers(ctx context.Context, tenantID string) ([]iam.User, error)

	// Role operations
	CreateRole(ctx context.Context, tenantID, id, name, description string, permissions []string) (*iam.Role, error)
	GetRole(ctx context.Context, tenantID, roleID string) (*iam.Role, error)
	UpdateRole(ctx context.Context, tenantID, roleID, name, description string, permissions []string) (*iam.Role, error)
	DeleteRole(ctx context.Context, tenantID, roleID string) error
	ListRoles(ctx context.Context, tenantID string) ([]iam.Role, error)

	// Policy operations
	CreatePolicy(ctx context.Context, tenantID, name, description string) (*iam.Policy, error)
	GetPolicy(ctx context.Context, tenantID, policyName string) (*iam.Policy, error)
	DeletePolicy(ctx context.Context, tenantID, policyName string) error
	ListPolicies(ctx context.Context, tenantID string) ([]iam.Policy, error)

	// MFA operations
	GetMFAStatus(ctx context.Context, tenantID, userID string) (bool, string, error)
	BeginMFAEnrollment(ctx context.Context, tenantID, userID, method string) (string, string, error)
	CancelMFAEnrollment(ctx context.Context, tenantID, userID, enrollmentID string) error

	// Close the client connection
	Close() error

	// Protocol returns the protocol type
	Protocol() Protocol
}

// RESTClient implements ProtocolClient for REST API.
type RESTClient struct {
	cluster  *testCluster
	protocol Protocol
}

// NewRESTClient creates a new REST protocol client.
func NewRESTClient(cluster *testCluster) *RESTClient {
	return &RESTClient{
		cluster:  cluster,
		protocol: ProtocolREST,
	}
}

func (c *RESTClient) Protocol() Protocol { return c.protocol }
func (c *RESTClient) Close() error       { return nil }

func (c *RESTClient) CreateTenant(ctx context.Context, id, name string, settings map[string]interface{}) (*iam.Tenant, error) {
	req := map[string]interface{}{
		"id":   id,
		"name": name,
	}
	if settings != nil {
		req["settings"] = settings
	}

	var tenant iam.Tenant
	err := c.cluster.postJSON("/api/v1/tenants", req, &tenant)
	return &tenant, err
}

func (c *RESTClient) GetTenant(ctx context.Context, id string) (*iam.Tenant, error) {
	var tenant iam.Tenant
	err := c.cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s", id), &tenant)
	return &tenant, err
}

func (c *RESTClient) UpdateTenant(ctx context.Context, id, name string, settings map[string]interface{}) (*iam.Tenant, error) {
	req := map[string]interface{}{
		"name": name,
	}
	if settings != nil {
		req["settings"] = settings
	}

	var tenant iam.Tenant
	err := c.cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s", id), req, &tenant)
	return &tenant, err
}

func (c *RESTClient) DeleteTenant(ctx context.Context, id string) error {
	return c.cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", id))
}

func (c *RESTClient) ListTenants(ctx context.Context) ([]iam.Tenant, error) {
	var resp struct {
		Tenants []iam.Tenant `json:"tenants"`
	}
	err := c.cluster.getJSON("/api/v1/tenants", &resp)
	return resp.Tenants, err
}

func (c *RESTClient) CreateUser(ctx context.Context, tenantID, email, name string) (*iam.User, error) {
	req := map[string]interface{}{
		"email":    email,
		"username": fmt.Sprintf("user_%d", time.Now().UnixNano()%1000000),
		"name":     name,
	}

	var user iam.User
	err := c.cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), req, &user)
	return &user, err
}

func (c *RESTClient) GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error) {
	var user iam.User
	err := c.cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s", tenantID, userID), &user)
	return &user, err
}

func (c *RESTClient) UpdateUser(ctx context.Context, tenantID, userID, name string) (*iam.User, error) {
	req := map[string]interface{}{
		"name": name,
	}

	var user iam.User
	err := c.cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s", tenantID, userID), req, &user)
	return &user, err
}

func (c *RESTClient) DeleteUser(ctx context.Context, tenantID, userID string) error {
	return c.cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s", tenantID, userID))
}

func (c *RESTClient) ListUsers(ctx context.Context, tenantID string) ([]iam.User, error) {
	var resp struct {
		Users []iam.User `json:"users"`
	}
	err := c.cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), &resp)
	return resp.Users, err
}

func (c *RESTClient) CreateRole(ctx context.Context, tenantID, id, name, description string, permissions []string) (*iam.Role, error) {
	req := map[string]interface{}{
		"id":          id,
		"name":        name,
		"description": description,
		"permissions": permissions,
	}

	var role iam.Role
	err := c.cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/roles", tenantID), req, &role)
	return &role, err
}

func (c *RESTClient) GetRole(ctx context.Context, tenantID, roleID string) (*iam.Role, error) {
	var role iam.Role
	err := c.cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/roles/%s", tenantID, roleID), &role)
	return &role, err
}

func (c *RESTClient) UpdateRole(ctx context.Context, tenantID, roleID, name, description string, permissions []string) (*iam.Role, error) {
	req := map[string]interface{}{
		"name":        name,
		"description": description,
		"permissions": permissions,
	}

	var role iam.Role
	err := c.cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s/roles/%s", tenantID, roleID), req, &role)
	return &role, err
}

func (c *RESTClient) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	return c.cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/roles/%s", tenantID, roleID))
}

func (c *RESTClient) ListRoles(ctx context.Context, tenantID string) ([]iam.Role, error) {
	var resp struct {
		Roles []iam.Role `json:"roles"`
	}
	err := c.cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/roles", tenantID), &resp)
	return resp.Roles, err
}

func (c *RESTClient) CreatePolicy(ctx context.Context, tenantID, name, description string) (*iam.Policy, error) {
	req := &iam.Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: iam.PolicyMetadata{
			Name:        name,
			Tenant:      tenantID,
			Description: description,
		},
		Spec: iam.PolicySpec{
			Subjects: iam.SubjectSelector{
				Authenticated: true,
			},
			Rules: []iam.Rule{
				{
					Name:      "default-rule",
					Effect:    iam.EffectAllow,
					Actions:   []string{"read"},
					Resources: []string{"*"},
				},
			},
		},
	}

	var policy iam.Policy
	err := c.cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), req, &policy)
	return &policy, err
}

func (c *RESTClient) GetPolicy(ctx context.Context, tenantID, policyName string) (*iam.Policy, error) {
	var policy iam.Policy
	err := c.cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/policies/%s", tenantID, policyName), &policy)
	return &policy, err
}

func (c *RESTClient) DeletePolicy(ctx context.Context, tenantID, policyName string) error {
	return c.cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/policies/%s", tenantID, policyName))
}

func (c *RESTClient) ListPolicies(ctx context.Context, tenantID string) ([]iam.Policy, error) {
	var resp struct {
		Policies []iam.Policy `json:"policies"`
	}
	err := c.cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), &resp)
	return resp.Policies, err
}

func (c *RESTClient) GetMFAStatus(ctx context.Context, tenantID, userID string) (bool, string, error) {
	var status MFAStatusResponse
	err := c.cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa", tenantID, userID), &status)
	return status.Enabled, status.Method, err
}

func (c *RESTClient) BeginMFAEnrollment(ctx context.Context, tenantID, userID, method string) (string, string, error) {
	req := map[string]interface{}{
		"method": method,
	}

	var enrollment MFAEnrollmentResponse
	err := c.cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa/enroll", tenantID, userID), req, &enrollment)
	return enrollment.EnrollmentID, enrollment.Secret, err
}

func (c *RESTClient) CancelMFAEnrollment(ctx context.Context, tenantID, userID, enrollmentID string) error {
	return c.cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa/enroll/%s", tenantID, userID, enrollmentID))
}

// GRPCClient implements ProtocolClient for gRPC.
type GRPCClient struct {
	conn      *grpc.ClientConn
	iamClient iampb.IAMServiceClient
	mfaClient iampb.MFAServiceClient
	protocol  Protocol
}

// NewGRPCClient creates a new gRPC protocol client.
func NewGRPCClient(endpoint string) (*GRPCClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "load integration TLS config", Cause: err}
	}
	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to connect to gRPC endpoint", Cause: err}
	}

	return &GRPCClient{
		conn:      conn,
		iamClient: iampb.NewIAMServiceClient(conn),
		mfaClient: iampb.NewMFAServiceClient(conn),
		protocol:  ProtocolGRPC,
	}, nil
}

func (c *GRPCClient) Protocol() Protocol { return c.protocol }

func (c *GRPCClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *GRPCClient) CreateTenant(ctx context.Context, id, name string, settings map[string]interface{}) (*iam.Tenant, error) {
	req := &iampb.CreateTenantRequest{
		Id:   id,
		Name: name,
	}

	resp, err := c.iamClient.CreateTenant(ctx, req)
	if err != nil {
		return nil, err
	}

	return convertProtoToTenant(resp), nil
}

func (c *GRPCClient) GetTenant(ctx context.Context, id string) (*iam.Tenant, error) {
	req := &iampb.GetTenantRequest{
		Id: id,
	}

	resp, err := c.iamClient.GetTenant(ctx, req)
	if err != nil {
		return nil, err
	}

	return convertProtoToTenant(resp), nil
}

func (c *GRPCClient) UpdateTenant(ctx context.Context, id, name string, settings map[string]interface{}) (*iam.Tenant, error) {
	req := &iampb.UpdateTenantRequest{
		Id:   id,
		Name: name,
	}

	resp, err := c.iamClient.UpdateTenant(ctx, req)
	if err != nil {
		return nil, err
	}

	return convertProtoToTenant(resp), nil
}

func (c *GRPCClient) DeleteTenant(ctx context.Context, id string) error {
	req := &iampb.DeleteTenantRequest{
		Id: id,
	}

	_, err := c.iamClient.DeleteTenant(ctx, req)
	return err
}

func (c *GRPCClient) ListTenants(ctx context.Context) ([]iam.Tenant, error) {
	req := &iampb.ListTenantsRequest{}

	resp, err := c.iamClient.ListTenants(ctx, req)
	if err != nil {
		return nil, err
	}

	tenants := make([]iam.Tenant, len(resp.Tenants))
	for i, t := range resp.Tenants {
		tenants[i] = *convertProtoToTenant(t)
	}

	return tenants, nil
}

func (c *GRPCClient) CreateUser(ctx context.Context, tenantID, email, name string) (*iam.User, error) {
	req := &iampb.CreateUserRequest{
		TenantId: tenantID,
		Email:    email,
		Name:     name,
	}

	resp, err := c.iamClient.CreateUser(ctx, req)
	if err != nil {
		return nil, err
	}

	return convertProtoToUser(resp)
}

func (c *GRPCClient) GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error) {
	req := &iampb.GetUserRequest{
		TenantId: tenantID,
		UserId:   userID,
	}

	resp, err := c.iamClient.GetUser(ctx, req)
	if err != nil {
		return nil, err
	}

	return convertProtoToUser(resp)
}

func (c *GRPCClient) UpdateUser(ctx context.Context, tenantID, userID, name string) (*iam.User, error) {
	req := &iampb.UpdateUserRequest{
		TenantId: tenantID,
		UserId:   userID,
		Name:     name,
	}

	resp, err := c.iamClient.UpdateUser(ctx, req)
	if err != nil {
		return nil, err
	}

	return convertProtoToUser(resp)
}

func (c *GRPCClient) DeleteUser(ctx context.Context, tenantID, userID string) error {
	req := &iampb.DeleteUserRequest{
		TenantId: tenantID,
		UserId:   userID,
	}

	_, err := c.iamClient.DeleteUser(ctx, req)
	return err
}

func (c *GRPCClient) ListUsers(ctx context.Context, tenantID string) ([]iam.User, error) {
	req := &iampb.ListUsersRequest{
		TenantId: tenantID,
	}

	resp, err := c.iamClient.ListUsers(ctx, req)
	if err != nil {
		return nil, err
	}

	users := make([]iam.User, len(resp.Users))
	for i, u := range resp.Users {
		user, err := convertProtoToUser(u)
		if err != nil {
			return nil, err
		}
		users[i] = *user
	}

	return users, nil
}

func (c *GRPCClient) CreateRole(ctx context.Context, tenantID, id, name, description string, permissions []string) (*iam.Role, error) {
	req := &iampb.CreateRoleRequest{
		TenantId:    tenantID,
		Id:          id,
		Name:        name,
		Description: description,
		Permissions: permissions,
	}

	resp, err := c.iamClient.CreateRole(ctx, req)
	if err != nil {
		return nil, err
	}

	return convertProtoToRole(resp), nil
}

func (c *GRPCClient) GetRole(ctx context.Context, tenantID, roleID string) (*iam.Role, error) {
	req := &iampb.GetRoleRequest{
		TenantId: tenantID,
		RoleId:   roleID,
	}

	resp, err := c.iamClient.GetRole(ctx, req)
	if err != nil {
		return nil, err
	}

	return convertProtoToRole(resp), nil
}

func (c *GRPCClient) UpdateRole(ctx context.Context, tenantID, roleID, name, description string, permissions []string) (*iam.Role, error) {
	req := &iampb.UpdateRoleRequest{
		TenantId:    tenantID,
		RoleId:      roleID,
		Name:        name,
		Description: description,
		Permissions: permissions,
	}

	resp, err := c.iamClient.UpdateRole(ctx, req)
	if err != nil {
		return nil, err
	}

	return convertProtoToRole(resp), nil
}

func (c *GRPCClient) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	req := &iampb.DeleteRoleRequest{
		TenantId: tenantID,
		RoleId:   roleID,
	}

	_, err := c.iamClient.DeleteRole(ctx, req)
	return err
}

func (c *GRPCClient) ListRoles(ctx context.Context, tenantID string) ([]iam.Role, error) {
	req := &iampb.ListRolesRequest{
		TenantId: tenantID,
	}

	resp, err := c.iamClient.ListRoles(ctx, req)
	if err != nil {
		return nil, err
	}

	roles := make([]iam.Role, len(resp.Roles))
	for i, r := range resp.Roles {
		roles[i] = *convertProtoToRole(r)
	}

	return roles, nil
}

func (c *GRPCClient) CreatePolicy(ctx context.Context, tenantID, name, description string) (*iam.Policy, error) {
	// Create policy structure for gRPC
	policy := &iam.Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: iam.PolicyMetadata{
			Name:        name,
			Tenant:      tenantID,
			Description: description,
		},
		Spec: iam.PolicySpec{
			Subjects: iam.SubjectSelector{
				Authenticated: true,
			},
			Rules: []iam.Rule{
				{
					Name:      "default-rule",
					Effect:    iam.EffectAllow,
					Actions:   []string{"read"},
					Resources: []string{"*"},
				},
			},
		},
	}

	// Convert to YAML for gRPC
	specYAML, err := yaml.Marshal(policy.Spec)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to marshal policy spec", Cause: err}
	}

	req := &iampb.CreatePolicyRequest{
		TenantId:    tenantID,
		Name:        name,
		Description: description,
		SpecYaml:    specYAML,
	}

	resp, err := c.iamClient.CreatePolicy(ctx, req)
	if err != nil {
		return nil, err
	}

	return convertProtoToPolicy(resp, tenantID), nil
}

func (c *GRPCClient) GetPolicy(ctx context.Context, tenantID, policyName string) (*iam.Policy, error) {
	req := &iampb.GetPolicyRequest{
		TenantId:   tenantID,
		PolicyName: policyName,
	}

	resp, err := c.iamClient.GetPolicy(ctx, req)
	if err != nil {
		return nil, err
	}

	return convertProtoToPolicy(resp, tenantID), nil
}

func (c *GRPCClient) DeletePolicy(ctx context.Context, tenantID, policyName string) error {
	req := &iampb.DeletePolicyRequest{
		TenantId:   tenantID,
		PolicyName: policyName,
	}

	_, err := c.iamClient.DeletePolicy(ctx, req)
	return err
}

func (c *GRPCClient) ListPolicies(ctx context.Context, tenantID string) ([]iam.Policy, error) {
	req := &iampb.ListPoliciesRequest{
		TenantId: tenantID,
	}

	resp, err := c.iamClient.ListPolicies(ctx, req)
	if err != nil {
		return nil, err
	}

	policies := make([]iam.Policy, len(resp.Policies))
	for i, p := range resp.Policies {
		policies[i] = *convertProtoToPolicy(p, tenantID)
	}

	return policies, nil
}

func (c *GRPCClient) GetMFAStatus(ctx context.Context, tenantID, userID string) (bool, string, error) {
	req := &iampb.GetMFAStatusRequest{
		TenantId: tenantID,
		UserId:   userID,
	}

	resp, err := c.mfaClient.GetStatus(ctx, req)
	if err != nil {
		return false, "", err
	}

	methodStr := ""
	if resp.Method == iampb.MFAMethod_MFA_METHOD_TOTP {
		methodStr = "totp"
	} else if resp.Method == iampb.MFAMethod_MFA_METHOD_WEBAUTHN {
		methodStr = "webauthn"
	}

	return resp.Enabled, methodStr, nil
}

func (c *GRPCClient) BeginMFAEnrollment(ctx context.Context, tenantID, userID, method string) (string, string, error) {
	mfaMethod := iampb.MFAMethod_MFA_METHOD_TOTP
	if method == "webauthn" {
		mfaMethod = iampb.MFAMethod_MFA_METHOD_WEBAUTHN
	}

	req := &iampb.BeginMFAEnrollmentRequest{
		TenantId: tenantID,
		UserId:   userID,
		Method:   mfaMethod,
	}

	resp, err := c.mfaClient.BeginEnrollment(ctx, req)
	if err != nil {
		return "", "", err
	}

	return resp.EnrollmentId, resp.Secret, nil
}

func (c *GRPCClient) CancelMFAEnrollment(ctx context.Context, tenantID, userID, enrollmentID string) error {
	req := &iampb.CancelMFAEnrollmentRequest{
		EnrollmentId: enrollmentID,
	}

	_, err := c.mfaClient.CancelEnrollment(ctx, req)
	return err
}

// Converter functions for protobuf to domain types
func convertProtoToTenant(pb *iampb.Tenant) *iam.Tenant {
	t := &iam.Tenant{
		ID:   pb.Id,
		Name: pb.Name,
	}

	if pb.CreatedAt != nil {
		t.Created = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		t.Updated = pb.UpdatedAt.AsTime()
	}

	return t
}

func convertProtoToUser(pb *iampb.User) (*iam.User, error) {
	id, err := strconv.ParseUint(pb.Id, 10, 64)
	if err != nil {
		return nil, err
	}
	u := &iam.User{
		ID:       id,
		TenantID: pb.TenantId,
		Email:    pb.Email,
		Name:     pb.Name,
		Roles:    pb.Roles,
	}

	if pb.CreatedAt != nil {
		u.Created = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		u.Updated = pb.UpdatedAt.AsTime()
	}
	if pb.LastAuth != nil {
		t := pb.LastAuth.AsTime()
		u.LastAuth = &t
	}

	return u, nil
}

func convertProtoToRole(pb *iampb.Role) *iam.Role {
	r := &iam.Role{
		ID:          pb.Id,
		TenantID:    pb.TenantId,
		Name:        pb.Name,
		Description: pb.Description,
		Permissions: pb.Permissions,
	}

	if pb.CreatedAt != nil {
		r.Created = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		r.Updated = pb.UpdatedAt.AsTime()
	}

	return r
}

func convertProtoToPolicy(pb *iampb.Policy, tenantID string) *iam.Policy {
	p := &iam.Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: iam.PolicyMetadata{
			Name:        pb.Name,
			Tenant:      tenantID,
			Description: pb.Description,
		},
	}

	if pb.CreatedAt != nil {
		p.Metadata.Created = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		p.Metadata.Updated = pb.UpdatedAt.AsTime()
	}

	// Parse spec YAML if provided
	if len(pb.SpecYaml) > 0 {
		var spec iam.PolicySpec
		if err := yaml.Unmarshal(pb.SpecYaml, &spec); err == nil {
			p.Spec = spec
		}
	}

	return p
}

// =============================================================================
// Unix Socket Protocol Client (gRPC over Unix Domain Socket)
// =============================================================================

// UnixSocketClient implements ProtocolClient for gRPC over Unix domain socket.
type UnixSocketClient struct {
	*GRPCClient // Embed GRPCClient for all method implementations
}

// NewUnixSocketClient creates a new Unix socket protocol client.
func NewUnixSocketClient(socketPath string) (*UnixSocketClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "load integration TLS config", Cause: err}
	}
	conn, err := grpc.DialContext(ctx, "unix://"+socketPath,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to connect to Unix socket", Cause: err}
	}

	return &UnixSocketClient{
		GRPCClient: &GRPCClient{
			conn:      conn,
			iamClient: iampb.NewIAMServiceClient(conn),
			mfaClient: iampb.NewMFAServiceClient(conn),
			protocol:  ProtocolUnixSocket,
		},
	}, nil
}

func (c *UnixSocketClient) Protocol() Protocol { return ProtocolUnixSocket }

// =============================================================================
// QUIC Protocol Client - Uses SDK QUIC transport for IAM operations
// =============================================================================

// QUICClient implements ProtocolClient for QUIC protocol.
type QUICClient struct {
	transport *quictransport.Transport
	endpoint  string
	protocol  Protocol
}

// NewQUICClient creates a new QUIC protocol client.
func NewQUICClient(endpoint string) (*QUICClient, error) {
	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "load integration TLS config", Cause: err}
	}
	tlsCfg.NextProtos = []string{"quicbaas-quic"}
	cfg := &transport.Config{
		Address:         endpoint,
		Timeout:         30 * time.Second,
		TLSConfig:       tlsCfg,
		MaxRetries:      3,
		RetryBackoff:    100 * time.Millisecond,
		MaxRetryBackoff: 5 * time.Second,
	}

	t, err := quictransport.New(cfg)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to create QUIC transport", Cause: err}
	}

	return &QUICClient{
		transport: t,
		endpoint:  endpoint,
		protocol:  ProtocolQUIC,
	}, nil
}

func (c *QUICClient) Protocol() Protocol { return c.protocol }

func (c *QUICClient) Close() error {
	if c.transport != nil {
		return c.transport.Close()
	}
	return nil
}

func (c *QUICClient) CreateTenant(ctx context.Context, id, name string, settings map[string]interface{}) (*iam.Tenant, error) {
	// Validate required fields
	if id == "" {
		return nil, &qberrors.MessageError{Message: "tenant ID is required"}
	}
	if name == "" {
		return nil, &qberrors.MessageError{Message: "tenant name is required"}
	}

	req := map[string]interface{}{
		"id":   id,
		"name": name,
	}
	if settings != nil {
		req["settings"] = settings
	}

	resp, err := c.transport.Request(ctx, "IAM.Tenant", "Create", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToTenant(resp)
}

func (c *QUICClient) GetTenant(ctx context.Context, id string) (*iam.Tenant, error) {
	req := map[string]interface{}{
		"id": id,
	}

	resp, err := c.transport.Request(ctx, "IAM.Tenant", "Get", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToTenant(resp)
}

func (c *QUICClient) UpdateTenant(ctx context.Context, id, name string, settings map[string]interface{}) (*iam.Tenant, error) {
	req := map[string]interface{}{
		"id":   id,
		"name": name,
	}
	if settings != nil {
		req["settings"] = settings
	}

	resp, err := c.transport.Request(ctx, "IAM.Tenant", "Update", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToTenant(resp)
}

func (c *QUICClient) DeleteTenant(ctx context.Context, id string) error {
	req := map[string]interface{}{
		"id": id,
	}

	_, err := c.transport.Request(ctx, "IAM.Tenant", "Delete", req)
	return err
}

func (c *QUICClient) ListTenants(ctx context.Context) ([]iam.Tenant, error) {
	resp, err := c.transport.Request(ctx, "IAM.Tenant", "List", map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToTenantList(resp)
}

func (c *QUICClient) CreateUser(ctx context.Context, tenantID, email, name string) (*iam.User, error) {
	req := map[string]interface{}{
		"tenant_id": tenantID,
		"email":     email,
		"name":      name,
		"username":  fmt.Sprintf("user_%d", time.Now().UnixNano()%1000000),
	}

	resp, err := c.transport.Request(ctx, "IAM.User", "Create", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToUser(resp)
}

func (c *QUICClient) GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error) {
	req := map[string]interface{}{
		"tenant_id": tenantID,
		"user_id":   userID,
	}

	resp, err := c.transport.Request(ctx, "IAM.User", "Get", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToUser(resp)
}

func (c *QUICClient) UpdateUser(ctx context.Context, tenantID, userID, name string) (*iam.User, error) {
	req := map[string]interface{}{
		"tenant_id": tenantID,
		"user_id":   userID,
		"name":      name,
	}

	resp, err := c.transport.Request(ctx, "IAM.User", "Update", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToUser(resp)
}

func (c *QUICClient) DeleteUser(ctx context.Context, tenantID, userID string) error {
	req := map[string]interface{}{
		"tenant_id": tenantID,
		"user_id":   userID,
	}

	_, err := c.transport.Request(ctx, "IAM.User", "Delete", req)
	return err
}

func (c *QUICClient) ListUsers(ctx context.Context, tenantID string) ([]iam.User, error) {
	req := map[string]interface{}{
		"tenant_id": tenantID,
	}

	resp, err := c.transport.Request(ctx, "IAM.User", "List", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToUserList(resp)
}

func (c *QUICClient) CreateRole(ctx context.Context, tenantID, id, name, description string, permissions []string) (*iam.Role, error) {
	req := map[string]interface{}{
		"tenant_id":   tenantID,
		"id":          id,
		"name":        name,
		"description": description,
		"permissions": permissions,
	}

	resp, err := c.transport.Request(ctx, "IAM.Role", "Create", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToRole(resp)
}

func (c *QUICClient) GetRole(ctx context.Context, tenantID, roleID string) (*iam.Role, error) {
	req := map[string]interface{}{
		"tenant_id": tenantID,
		"role_id":   roleID,
	}

	resp, err := c.transport.Request(ctx, "IAM.Role", "Get", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToRole(resp)
}

func (c *QUICClient) UpdateRole(ctx context.Context, tenantID, roleID, name, description string, permissions []string) (*iam.Role, error) {
	req := map[string]interface{}{
		"tenant_id":   tenantID,
		"role_id":     roleID,
		"name":        name,
		"description": description,
		"permissions": permissions,
	}

	resp, err := c.transport.Request(ctx, "IAM.Role", "Update", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToRole(resp)
}

func (c *QUICClient) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	req := map[string]interface{}{
		"tenant_id": tenantID,
		"role_id":   roleID,
	}

	_, err := c.transport.Request(ctx, "IAM.Role", "Delete", req)
	return err
}

func (c *QUICClient) ListRoles(ctx context.Context, tenantID string) ([]iam.Role, error) {
	req := map[string]interface{}{
		"tenant_id": tenantID,
	}

	resp, err := c.transport.Request(ctx, "IAM.Role", "List", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToRoleList(resp)
}

func (c *QUICClient) CreatePolicy(ctx context.Context, tenantID, name, description string) (*iam.Policy, error) {
	// Build a full policy structure matching what server expects (camelCase JSON keys)
	policySpec := map[string]interface{}{
		"apiVersion": "iam.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":        name,
			"tenant":      tenantID,
			"description": description,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "default-rule",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"*"},
				},
			},
		},
	}

	req := map[string]interface{}{
		"tenant_id": tenantID,
		"policy":    policySpec,
	}

	resp, err := c.transport.Request(ctx, "IAM.Policy", "Create", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToPolicy(resp, tenantID)
}

func (c *QUICClient) GetPolicy(ctx context.Context, tenantID, policyName string) (*iam.Policy, error) {
	req := map[string]interface{}{
		"tenant_id":   tenantID,
		"policy_name": policyName,
	}

	resp, err := c.transport.Request(ctx, "IAM.Policy", "Get", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToPolicy(resp, tenantID)
}

func (c *QUICClient) DeletePolicy(ctx context.Context, tenantID, policyName string) error {
	req := map[string]interface{}{
		"tenant_id":   tenantID,
		"policy_name": policyName,
	}

	_, err := c.transport.Request(ctx, "IAM.Policy", "Delete", req)
	return err
}

func (c *QUICClient) ListPolicies(ctx context.Context, tenantID string) ([]iam.Policy, error) {
	req := map[string]interface{}{
		"tenant_id": tenantID,
	}

	resp, err := c.transport.Request(ctx, "IAM.Policy", "List", req)
	if err != nil {
		return nil, err
	}

	return convertQUICResponseToPolicyList(resp, tenantID)
}

func (c *QUICClient) GetMFAStatus(ctx context.Context, tenantID, userID string) (bool, string, error) {
	req := map[string]interface{}{
		"tenant_id": tenantID,
		"user_id":   userID,
	}

	resp, err := c.transport.Request(ctx, "IAM.MFA", "GetStatus", req)
	if err != nil {
		return false, "", err
	}

	data, ok := resp.(map[string]interface{})
	if !ok {
		return false, "", &qberrors.MessageError{Message: "invalid response type"}
	}

	enabled, _ := data["enabled"].(bool)
	method, _ := data["method"].(string)
	return enabled, method, nil
}

func (c *QUICClient) BeginMFAEnrollment(ctx context.Context, tenantID, userID, method string) (string, string, error) {
	req := map[string]interface{}{
		"tenant_id": tenantID,
		"user_id":   userID,
		"method":    method,
	}

	resp, err := c.transport.Request(ctx, "IAM.MFA", "BeginEnrollment", req)
	if err != nil {
		return "", "", err
	}

	data, ok := resp.(map[string]interface{})
	if !ok {
		return "", "", &qberrors.MessageError{Message: "invalid response type"}
	}

	enrollmentID, _ := data["enrollment_id"].(string)
	secret, _ := data["secret"].(string)
	return enrollmentID, secret, nil
}

func (c *QUICClient) CancelMFAEnrollment(ctx context.Context, tenantID, userID, enrollmentID string) error {
	req := map[string]interface{}{
		"tenant_id":     tenantID,
		"user_id":       userID,
		"enrollment_id": enrollmentID,
	}

	_, err := c.transport.Request(ctx, "IAM.MFA", "CancelEnrollment", req)
	return err
}

// =============================================================================
// Embedded Protocol Client
// =============================================================================

// EmbeddedClient implements ProtocolClient for embedded (in-process) access.
// Note: Embedded protocol is skipped in integration tests because it requires
// starting a local server, which is tested separately in unit tests.
type EmbeddedClient struct {
	protocol Protocol
}

// NewEmbeddedClient creates a new embedded protocol client.
func NewEmbeddedClient() (*EmbeddedClient, error) {
	return &EmbeddedClient{
		protocol: ProtocolEmbedded,
	}, nil
}

func (c *EmbeddedClient) Protocol() Protocol { return c.protocol }
func (c *EmbeddedClient) Close() error       { return nil }

// Embedded IAM operations - placeholder implementations
// These would use direct SDK client calls in a full implementation
func (c *EmbeddedClient) CreateTenant(ctx context.Context, id, name string, settings map[string]interface{}) (*iam.Tenant, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) GetTenant(ctx context.Context, id string) (*iam.Tenant, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) UpdateTenant(ctx context.Context, id, name string, settings map[string]interface{}) (*iam.Tenant, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) DeleteTenant(ctx context.Context, id string) error {
	return &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) ListTenants(ctx context.Context) ([]iam.Tenant, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) CreateUser(ctx context.Context, tenantID, email, name string) (*iam.User, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) UpdateUser(ctx context.Context, tenantID, userID, name string) (*iam.User, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) DeleteUser(ctx context.Context, tenantID, userID string) error {
	return &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) ListUsers(ctx context.Context, tenantID string) ([]iam.User, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) CreateRole(ctx context.Context, tenantID, id, name, description string, permissions []string) (*iam.Role, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) GetRole(ctx context.Context, tenantID, roleID string) (*iam.Role, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) UpdateRole(ctx context.Context, tenantID, roleID, name, description string, permissions []string) (*iam.Role, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	return &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) ListRoles(ctx context.Context, tenantID string) ([]iam.Role, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) CreatePolicy(ctx context.Context, tenantID, name, description string) (*iam.Policy, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) GetPolicy(ctx context.Context, tenantID, policyName string) (*iam.Policy, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) DeletePolicy(ctx context.Context, tenantID, policyName string) error {
	return &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) ListPolicies(ctx context.Context, tenantID string) ([]iam.Policy, error) {
	return nil, &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) GetMFAStatus(ctx context.Context, tenantID, userID string) (bool, string, error) {
	return false, "", &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) BeginMFAEnrollment(ctx context.Context, tenantID, userID, method string) (string, string, error) {
	return "", "", &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}
func (c *EmbeddedClient) CancelMFAEnrollment(ctx context.Context, tenantID, userID, enrollmentID string) error {
	return &qberrors.MessageError{Message: "embedded IAM requires local server - skipped in integration tests"}
}

// =============================================================================
// Helper functions for converting map responses to domain types
// =============================================================================

func mapToTenant(data map[string]interface{}) (*iam.Tenant, error) {
	// Check for nested tenant field (server response format: {tenant: {...}, timestamp: "..."})
	if tenantData, ok := data["tenant"].(map[string]interface{}); ok {
		data = tenantData
	}

	tenant := &iam.Tenant{}
	if id, ok := data["id"].(string); ok {
		tenant.ID = id
	}
	if name, ok := data["name"].(string); ok {
		tenant.Name = name
	}
	// Parse timestamps - handle both "created"/"updated" and "created_at"/"updated_at" field names
	if created, ok := data["created"].(string); ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			tenant.Created = t
		}
	} else if created, ok := data["created_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			tenant.Created = t
		}
	}
	if updated, ok := data["updated"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updated); err == nil {
			tenant.Updated = t
		}
	} else if updated, ok := data["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updated); err == nil {
			tenant.Updated = t
		}
	}
	return tenant, nil
}

func mapToTenantList(data map[string]interface{}) ([]iam.Tenant, error) {
	tenantsRaw, ok := data["tenants"].([]interface{})
	if !ok {
		return []iam.Tenant{}, nil
	}
	tenants := make([]iam.Tenant, 0, len(tenantsRaw))
	for _, t := range tenantsRaw {
		if tm, ok := t.(map[string]interface{}); ok {
			tenant, err := mapToTenant(tm)
			if err != nil {
				continue
			}
			tenants = append(tenants, *tenant)
		}
	}
	return tenants, nil
}

func mapToUser(data map[string]interface{}) (*iam.User, error) {
	// Check for nested user field (server response format: {user: {...}, timestamp: "..."})
	if userData, ok := data["user"].(map[string]interface{}); ok {
		data = userData
	}

	user := &iam.User{}
	if id, ok := data["id"].(string); ok {
		parsed, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			return nil, err
		}
		user.ID = parsed
	}
	if tenantID, ok := data["tenant_id"].(string); ok {
		user.TenantID = tenantID
	}
	if email, ok := data["email"].(string); ok {
		user.Email = email
	}
	// Handle name - SDK QUIC uses "display_name", server uses "name"
	if name, ok := data["name"].(string); ok {
		user.Name = name
	} else if name, ok := data["display_name"].(string); ok {
		user.Name = name
	}
	// Parse timestamps - handle both "created"/"updated" and "created_at"/"updated_at" field names
	if created, ok := data["created"].(string); ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			user.Created = t
		}
	} else if created, ok := data["created_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			user.Created = t
		}
	}
	if updated, ok := data["updated"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updated); err == nil {
			user.Updated = t
		}
	} else if updated, ok := data["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updated); err == nil {
			user.Updated = t
		}
	}
	return user, nil
}

func mapToUserList(data map[string]interface{}) ([]iam.User, error) {
	usersRaw, ok := data["users"].([]interface{})
	if !ok {
		return []iam.User{}, nil
	}
	users := make([]iam.User, 0, len(usersRaw))
	for _, u := range usersRaw {
		if um, ok := u.(map[string]interface{}); ok {
			user, err := mapToUser(um)
			if err != nil {
				continue
			}
			users = append(users, *user)
		}
	}
	return users, nil
}

func mapToRole(data map[string]interface{}) (*iam.Role, error) {
	// Check for nested role field (server response format: {role: {...}, timestamp: "..."})
	if roleData, ok := data["role"].(map[string]interface{}); ok {
		data = roleData
	}

	role := &iam.Role{}
	if id, ok := data["id"].(string); ok {
		role.ID = id
	}
	if tenantID, ok := data["tenant_id"].(string); ok {
		role.TenantID = tenantID
	}
	if name, ok := data["name"].(string); ok {
		role.Name = name
	}
	if desc, ok := data["description"].(string); ok {
		role.Description = desc
	}
	if perms, ok := data["permissions"].([]interface{}); ok {
		for _, p := range perms {
			if ps, ok := p.(string); ok {
				role.Permissions = append(role.Permissions, ps)
			}
		}
	}
	// Parse timestamps - handle both "created"/"updated" and "created_at"/"updated_at" field names
	if created, ok := data["created"].(string); ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			role.Created = t
		}
	} else if created, ok := data["created_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			role.Created = t
		}
	}
	if updated, ok := data["updated"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updated); err == nil {
			role.Updated = t
		}
	} else if updated, ok := data["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updated); err == nil {
			role.Updated = t
		}
	}
	return role, nil
}

func mapToRoleList(data map[string]interface{}) ([]iam.Role, error) {
	rolesRaw, ok := data["roles"].([]interface{})
	if !ok {
		return []iam.Role{}, nil
	}
	roles := make([]iam.Role, 0, len(rolesRaw))
	for _, r := range rolesRaw {
		if rm, ok := r.(map[string]interface{}); ok {
			role, err := mapToRole(rm)
			if err != nil {
				continue
			}
			roles = append(roles, *role)
		}
	}
	return roles, nil
}

func mapToPolicy(data map[string]interface{}, tenantID string) (*iam.Policy, error) {
	// Check for nested policy field (server response format: {policy: {...}, timestamp: "..."})
	if policyData, ok := data["policy"].(map[string]interface{}); ok {
		data = policyData
	}

	policy := &iam.Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: iam.PolicyMetadata{
			Tenant: tenantID,
		},
	}

	// Check for apiVersion
	if apiVersion, ok := data["apiVersion"].(string); ok {
		policy.APIVersion = apiVersion
	}
	// Check for kind
	if kind, ok := data["kind"].(string); ok {
		policy.Kind = kind
	}

	// Handle both flat name and nested metadata.name
	if name, ok := data["name"].(string); ok {
		policy.Metadata.Name = name
	}
	if metadata, ok := data["metadata"].(map[string]interface{}); ok {
		if name, ok := metadata["name"].(string); ok {
			policy.Metadata.Name = name
		}
		if desc, ok := metadata["description"].(string); ok {
			policy.Metadata.Description = desc
		}
		if tenant, ok := metadata["tenant"].(string); ok {
			policy.Metadata.Tenant = tenant
		}
	}
	if desc, ok := data["description"].(string); ok {
		policy.Metadata.Description = desc
	}
	return policy, nil
}

func mapToPolicyList(data map[string]interface{}, tenantID string) ([]iam.Policy, error) {
	policiesRaw, ok := data["policies"].([]interface{})
	if !ok {
		return []iam.Policy{}, nil
	}
	policies := make([]iam.Policy, 0, len(policiesRaw))
	for _, p := range policiesRaw {
		if pm, ok := p.(map[string]interface{}); ok {
			policy, err := mapToPolicy(pm, tenantID)
			if err != nil {
				continue
			}
			policies = append(policies, *policy)
		}
	}
	return policies, nil
}

// =============================================================================
// QUIC response converter functions
// =============================================================================

func convertQUICResponseToTenant(resp interface{}) (*iam.Tenant, error) {
	data, ok := resp.(map[string]interface{})
	if !ok {
		return nil, &qberrors.MessageError{Message: "invalid response type"}
	}
	return mapToTenant(data)
}

func convertQUICResponseToTenantList(resp interface{}) ([]iam.Tenant, error) {
	data, ok := resp.(map[string]interface{})
	if !ok {
		return nil, &qberrors.MessageError{Message: "invalid response type"}
	}
	return mapToTenantList(data)
}

func convertQUICResponseToUser(resp interface{}) (*iam.User, error) {
	data, ok := resp.(map[string]interface{})
	if !ok {
		return nil, &qberrors.MessageError{Message: "invalid response type"}
	}
	return mapToUser(data)
}

func convertQUICResponseToUserList(resp interface{}) ([]iam.User, error) {
	data, ok := resp.(map[string]interface{})
	if !ok {
		return nil, &qberrors.MessageError{Message: "invalid response type"}
	}
	return mapToUserList(data)
}

func convertQUICResponseToRole(resp interface{}) (*iam.Role, error) {
	data, ok := resp.(map[string]interface{})
	if !ok {
		return nil, &qberrors.MessageError{Message: "invalid response type"}
	}
	return mapToRole(data)
}

func convertQUICResponseToRoleList(resp interface{}) ([]iam.Role, error) {
	data, ok := resp.(map[string]interface{})
	if !ok {
		return nil, &qberrors.MessageError{Message: "invalid response type"}
	}
	return mapToRoleList(data)
}

func convertQUICResponseToPolicy(resp interface{}, tenantID string) (*iam.Policy, error) {
	data, ok := resp.(map[string]interface{})
	if !ok {
		return nil, &qberrors.MessageError{Message: "invalid response type"}
	}
	return mapToPolicy(data, tenantID)
}

func convertQUICResponseToPolicyList(resp interface{}, tenantID string) ([]iam.Policy, error) {
	data, ok := resp.(map[string]interface{})
	if !ok {
		return nil, &qberrors.MessageError{Message: "invalid response type"}
	}
	return mapToPolicyList(data, tenantID)
}

// requireProtocolClient creates protocol-specific clients and requires them to be available.
// If a protocol is not available, the test is skipped with a clear explanation.
func requireProtocolClient(t *testing.T, protocol Protocol, cluster *testCluster) ProtocolClient {
	t.Helper()

	switch protocol {
	case ProtocolEmbedded:
		// Use embedded service from TestMain if available
		_, err := helpers.GetEmbeddedService(t)
		if err != nil {
			require.NoError(t, err, "Embedded service must be available")
			return nil
		}
		client, clientErr := NewEmbeddedClient()
		require.NoError(t, clientErr, "failed to create embedded client")
		return client

	case ProtocolREST:
		// REST is always available via the test cluster
		return NewRESTClient(cluster)

	case ProtocolGRPC:
		// Try to create gRPC client - fail if not available
		grpcClient, err := NewGRPCClient(cluster.grpcEndpoint)
		require.NoError(t, err, "gRPC protocol must be available - ensure the test cluster is properly configured with gRPC endpoint")
		return grpcClient

	case ProtocolQUIC:
		// QUIC client - uses SDK QUIC transport
		require.NotEmpty(t, cluster.quicEndpoint, "QUIC endpoint must be configured for test cluster")
		quicClient, err := NewQUICClient(cluster.quicEndpoint)
		require.NoError(t, err, "QUIC protocol must be available - ensure the test cluster is properly configured with QUIC endpoint")
		return quicClient

	case ProtocolUnixSocket:
		// Unix socket client - gRPC over Unix domain socket
		require.NotEmpty(t, cluster.unixSocket, "Unix socket must be configured for test cluster")
		unixClient, err := NewUnixSocketClient(cluster.unixSocket)
		require.NoError(t, err, "Unix socket protocol must be available - ensure the test cluster is properly configured with Unix socket")
		return unixClient

	default:
		require.Fail(t, "Unknown protocol: %s", protocol)
		return nil
	}
}

// allProtocols returns the list of all supported protocols to test.
// All 5 protocols are included for comprehensive multi-protocol testing:
// - Embedded: Direct in-process SDK access (skipped in integration tests)
// - REST: HTTP REST API
// - gRPC: Protocol Buffers over HTTP/2
// - QUIC: UDP-based transport with SDK QUIC transport
// - UnixSocket: gRPC over Unix domain socket
//
// NOTE: no MCP protocol here. quicbaas retired its bespoke JSON-RPC MCP
// listener in favor of go-qrdb's unified multi-protocol server; the unified
// server's MCP surface has no finalized docker endpoint convention yet, so
// this suite does not exercise it (deferred).
func allProtocols() []Protocol {
	protocols := []Protocol{
		ProtocolREST,
		ProtocolGRPC,
		ProtocolQUIC,
		ProtocolUnixSocket,
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, ProtocolEmbedded)
	}
	return protocols
}

// =============================================================================
// Multi-Protocol Test Cases
// =============================================================================

// TestTenantCRUD_AllProtocols tests tenant CRUD operations across all protocols.
func TestTenantCRUD_AllProtocols(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	for _, protocol := range allProtocols() {
		t.Run(string(protocol), func(t *testing.T) {
			client := requireProtocolClient(t, protocol, cluster)
			defer client.Close()

			ctx := context.Background()
			tenantID := fmt.Sprintf("tenant-protocol-%s-%d", strings.ToLower(string(protocol)), time.Now().UnixNano())

			// Create
			t.Run("Create", func(t *testing.T) {
				tenant, err := client.CreateTenant(ctx, tenantID, "Protocol Test Tenant", nil)
				require.NoError(t, err, "Failed to create tenant via %s", protocol)
				assert.Equal(t, tenantID, tenant.ID)
				assert.Equal(t, "Protocol Test Tenant", tenant.Name)
				assert.NotZero(t, tenant.Created)
			})

			// Get
			t.Run("Get", func(t *testing.T) {
				tenant, err := client.GetTenant(ctx, tenantID)
				require.NoError(t, err, "Failed to get tenant via %s", protocol)
				assert.Equal(t, tenantID, tenant.ID)
			})

			// List - with retry for eventual consistency
			t.Run("List", func(t *testing.T) {
				listCtx, listCancel := context.WithTimeout(ctx, 5*time.Second)
				defer listCancel()

				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()

				var found bool
				for !found {
					tenants, err := client.ListTenants(listCtx)
					require.NoError(t, err, "Failed to list tenants via %s", protocol)

					for _, tenant := range tenants {
						if tenant.ID == tenantID {
							found = true
							break
						}
					}
					if found {
						break
					}

					select {
					case <-listCtx.Done():
						t.Fatal("Timed out waiting for tenant to appear in list")
					case <-ticker.C:
					}
				}
				assert.True(t, found, "Created tenant should be in list")
			})

			// Update
			t.Run("Update", func(t *testing.T) {
				tenant, err := client.UpdateTenant(ctx, tenantID, "Updated Protocol Test Tenant", nil)
				require.NoError(t, err, "Failed to update tenant via %s", protocol)
				assert.Equal(t, "Updated Protocol Test Tenant", tenant.Name)
			})

			// Delete
			t.Run("Delete", func(t *testing.T) {
				err := client.DeleteTenant(ctx, tenantID)
				require.NoError(t, err, "Failed to delete tenant via %s", protocol)

				// Verify deletion
				_, err = client.GetTenant(ctx, tenantID)
				assert.Error(t, err, "Deleted tenant should not be found")
			})
		})
	}
}

// TestUserCRUD_AllProtocols tests user CRUD operations across all protocols.
func TestUserCRUD_AllProtocols(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	for _, protocol := range allProtocols() {
		t.Run(string(protocol), func(t *testing.T) {
			client := requireProtocolClient(t, protocol, cluster)
			defer client.Close()

			ctx := context.Background()
			tenantID := fmt.Sprintf("tenant-user-%s-%d", strings.ToLower(string(protocol)), time.Now().UnixNano())

			// Setup: Create tenant
			_, err := client.CreateTenant(ctx, tenantID, "User Test Tenant", nil)
			require.NoError(t, err)
			defer client.DeleteTenant(ctx, tenantID)

			email := fmt.Sprintf("user-%s-%d@test.local", strings.ToLower(string(protocol)), time.Now().UnixNano())

			var userID string

			// Create
			t.Run("Create", func(t *testing.T) {
				user, err := client.CreateUser(ctx, tenantID, email, "Test User")
				require.NoError(t, err, "Failed to create user via %s", protocol)
				assert.Equal(t, email, user.Email)
				assert.Equal(t, "Test User", user.Name)
				assert.NotZero(t, user.ID)
				userID = strconv.FormatUint(user.ID, 10)
			})

			// Get
			t.Run("Get", func(t *testing.T) {
				user, err := client.GetUser(ctx, tenantID, userID)
				require.NoError(t, err, "Failed to get user via %s", protocol)
				assert.Equal(t, userID, strconv.FormatUint(user.ID, 10))
				assert.Equal(t, email, user.Email)
			})

			// List
			t.Run("List", func(t *testing.T) {
				users, err := client.ListUsers(ctx, tenantID)
				require.NoError(t, err, "Failed to list users via %s", protocol)

				found := false
				for _, user := range users {
					if strconv.FormatUint(user.ID, 10) == userID {
						found = true
						break
					}
				}
				assert.True(t, found, "Created user should be in list")
			})

			// Update
			t.Run("Update", func(t *testing.T) {
				user, err := client.UpdateUser(ctx, tenantID, userID, "Updated Test User")
				require.NoError(t, err, "Failed to update user via %s", protocol)
				assert.Equal(t, "Updated Test User", user.Name)
			})

			// Delete
			t.Run("Delete", func(t *testing.T) {
				err := client.DeleteUser(ctx, tenantID, userID)
				require.NoError(t, err, "Failed to delete user via %s", protocol)

				// Verify deletion
				_, err = client.GetUser(ctx, tenantID, userID)
				assert.Error(t, err, "Deleted user should not be found")
			})
		})
	}
}

// TestRoleCRUD_AllProtocols tests role CRUD operations across all protocols.
func TestRoleCRUD_AllProtocols(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	for _, protocol := range allProtocols() {
		t.Run(string(protocol), func(t *testing.T) {
			client := requireProtocolClient(t, protocol, cluster)
			defer client.Close()

			ctx := context.Background()
			tenantID := fmt.Sprintf("tenant-role-%s-%d", strings.ToLower(string(protocol)), time.Now().UnixNano())

			// Setup: Create tenant
			_, err := client.CreateTenant(ctx, tenantID, "Role Test Tenant", nil)
			require.NoError(t, err)
			defer client.DeleteTenant(ctx, tenantID)

			roleID := fmt.Sprintf("role-%s-%d", strings.ToLower(string(protocol)), time.Now().UnixNano())

			// Create
			t.Run("Create", func(t *testing.T) {
				role, err := client.CreateRole(ctx, tenantID, roleID, "Test Role", "Test Description", []string{"kv:get", "kv:put"})
				require.NoError(t, err, "Failed to create role via %s", protocol)
				assert.Equal(t, roleID, role.ID)
				assert.Equal(t, "Test Role", role.Name)
				assert.Equal(t, []string{"kv:get", "kv:put"}, role.Permissions)
			})

			// Get
			t.Run("Get", func(t *testing.T) {
				role, err := client.GetRole(ctx, tenantID, roleID)
				require.NoError(t, err, "Failed to get role via %s", protocol)
				assert.Equal(t, roleID, role.ID)
			})

			// List
			t.Run("List", func(t *testing.T) {
				roles, err := client.ListRoles(ctx, tenantID)
				require.NoError(t, err, "Failed to list roles via %s", protocol)

				found := false
				for _, role := range roles {
					if role.ID == roleID {
						found = true
						break
					}
				}
				assert.True(t, found, "Created role should be in list")
			})

			// Update
			t.Run("Update", func(t *testing.T) {
				role, err := client.UpdateRole(ctx, tenantID, roleID, "Updated Role", "Updated Description", []string{"kv:get", "kv:put", "kv:delete"})
				require.NoError(t, err, "Failed to update role via %s", protocol)
				assert.Equal(t, "Updated Role", role.Name)
				assert.Contains(t, role.Permissions, "kv:delete")
			})

			// Delete
			t.Run("Delete", func(t *testing.T) {
				err := client.DeleteRole(ctx, tenantID, roleID)
				require.NoError(t, err, "Failed to delete role via %s", protocol)

				// Verify deletion
				_, err = client.GetRole(ctx, tenantID, roleID)
				assert.Error(t, err, "Deleted role should not be found")
			})
		})
	}
}

// TestPolicyCRUD_AllProtocols tests policy CRUD operations across all protocols.
func TestPolicyCRUD_AllProtocols(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	for _, protocol := range allProtocols() {
		t.Run(string(protocol), func(t *testing.T) {
			client := requireProtocolClient(t, protocol, cluster)
			defer client.Close()

			ctx := context.Background()
			tenantID := fmt.Sprintf("tenant-policy-%s-%d", strings.ToLower(string(protocol)), time.Now().UnixNano())

			// Setup: Create tenant
			_, err := client.CreateTenant(ctx, tenantID, "Policy Test Tenant", nil)
			require.NoError(t, err)
			defer client.DeleteTenant(ctx, tenantID)

			policyName := fmt.Sprintf("policy-%s-%d", strings.ToLower(string(protocol)), time.Now().UnixNano())

			// Create
			t.Run("Create", func(t *testing.T) {
				policy, err := client.CreatePolicy(ctx, tenantID, policyName, "Test Policy")
				require.NoError(t, err, "Failed to create policy via %s", protocol)
				assert.Equal(t, policyName, policy.Metadata.Name)
				assert.Equal(t, tenantID, policy.Metadata.Tenant)
			})

			// Get
			t.Run("Get", func(t *testing.T) {
				policy, err := client.GetPolicy(ctx, tenantID, policyName)
				require.NoError(t, err, "Failed to get policy via %s", protocol)
				assert.Equal(t, policyName, policy.Metadata.Name)
			})

			// List
			t.Run("List", func(t *testing.T) {
				policies, err := client.ListPolicies(ctx, tenantID)
				require.NoError(t, err, "Failed to list policies via %s", protocol)

				found := false
				for _, policy := range policies {
					if policy.Metadata.Name == policyName {
						found = true
						break
					}
				}
				assert.True(t, found, "Created policy should be in list")
			})

			// Delete
			t.Run("Delete", func(t *testing.T) {
				err := client.DeletePolicy(ctx, tenantID, policyName)
				require.NoError(t, err, "Failed to delete policy via %s", protocol)

				// Verify deletion
				_, err = client.GetPolicy(ctx, tenantID, policyName)
				assert.Error(t, err, "Deleted policy should not be found")
			})
		})
	}
}

// TestMFAOperations_AllProtocols tests MFA operations across all protocols.
func TestMFAOperations_AllProtocols(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	for _, protocol := range allProtocols() {
		t.Run(string(protocol), func(t *testing.T) {
			client := requireProtocolClient(t, protocol, cluster)
			defer client.Close()

			ctx := context.Background()
			tenantID := fmt.Sprintf("tenant-mfa-%s-%d", strings.ToLower(string(protocol)), time.Now().UnixNano())

			// Setup: Create tenant and user
			_, err := client.CreateTenant(ctx, tenantID, "MFA Test Tenant", nil)
			require.NoError(t, err)
			defer client.DeleteTenant(ctx, tenantID)

			email := fmt.Sprintf("mfa-user-%s-%d@test.local", strings.ToLower(string(protocol)), time.Now().UnixNano())

			user, err := client.CreateUser(ctx, tenantID, email, "MFA Test User")
			require.NoError(t, err)
			userID := strconv.FormatUint(user.ID, 10)

			// Get MFA Status (not enrolled)
			t.Run("StatusNotEnrolled", func(t *testing.T) {
				enabled, method, err := client.GetMFAStatus(ctx, tenantID, userID)
				require.NoError(t, err, "Failed to get MFA status via %s", protocol)
				assert.False(t, enabled, "MFA should not be enabled initially")
				assert.Empty(t, method, "MFA method should be empty")
			})

			// Begin MFA Enrollment
			var enrollmentID string
			t.Run("BeginEnrollment", func(t *testing.T) {
				id, secret, err := client.BeginMFAEnrollment(ctx, tenantID, userID, "totp")
				require.NoError(t, err, "Failed to begin MFA enrollment via %s", protocol)
				assert.NotEmpty(t, id, "Enrollment ID should not be empty")
				assert.NotEmpty(t, secret, "Secret should not be empty")
				enrollmentID = id
			})

			// Cancel MFA Enrollment
			t.Run("CancelEnrollment", func(t *testing.T) {
				err := client.CancelMFAEnrollment(ctx, tenantID, userID, enrollmentID)
				require.NoError(t, err, "Failed to cancel MFA enrollment via %s", protocol)

				// Verify still not enrolled
				enabled, _, err := client.GetMFAStatus(ctx, tenantID, userID)
				require.NoError(t, err)
				assert.False(t, enabled, "MFA should not be enabled after cancellation")
			})
		})
	}
}

// TestProtocolConsistency verifies that the same operation returns consistent results across protocols.
func TestProtocolConsistency(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	ctx := context.Background()
	tenantID := fmt.Sprintf("tenant-consistency-%d", time.Now().UnixNano())

	// Create tenant via REST
	restClient := NewRESTClient(cluster)
	defer restClient.Close()

	tenant, err := restClient.CreateTenant(ctx, tenantID, "Consistency Test", nil)
	require.NoError(t, err)
	defer restClient.DeleteTenant(ctx, tenantID)

	// Verify the same tenant can be read via all protocols
	for _, protocol := range allProtocols() {
		t.Run(string(protocol), func(t *testing.T) {
			client := requireProtocolClient(t, protocol, cluster)
			defer client.Close()

			retrievedTenant, err := client.GetTenant(ctx, tenantID)
			require.NoError(t, err, "Failed to get tenant via %s", protocol)

			assert.Equal(t, tenant.ID, retrievedTenant.ID, "Tenant ID should match across protocols")
			assert.Equal(t, tenant.Name, retrievedTenant.Name, "Tenant name should match across protocols")
		})
	}
}

// TestCrossProtocolOperations verifies operations can be performed across different protocols.
func TestCrossProtocolOperations(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	ctx := context.Background()
	tenantID := fmt.Sprintf("tenant-cross-%d", time.Now().UnixNano())

	protocols := allProtocols()
	require.GreaterOrEqual(t, len(protocols), 2, "Need at least 2 protocols for cross-protocol testing")

	// Create via first protocol
	client1 := requireProtocolClient(t, protocols[0], cluster)
	defer client1.Close()

	_, err := client1.CreateTenant(ctx, tenantID, "Cross Protocol Test", nil)
	require.NoError(t, err, "Failed to create via %s", protocols[0])

	// Read via second protocol
	client2 := requireProtocolClient(t, protocols[1], cluster)
	defer client2.Close()

	tenant, err := client2.GetTenant(ctx, tenantID)
	require.NoError(t, err, "Failed to read via %s", protocols[1])
	assert.Equal(t, tenantID, tenant.ID)

	// Update via first protocol
	_, err = client1.UpdateTenant(ctx, tenantID, "Updated Cross Protocol", nil)
	require.NoError(t, err, "Failed to update via %s", protocols[0])

	// Verify update via second protocol
	tenant, err = client2.GetTenant(ctx, tenantID)
	require.NoError(t, err, "Failed to verify update via %s", protocols[1])
	assert.Equal(t, "Updated Cross Protocol", tenant.Name)

	// Delete via second protocol
	err = client2.DeleteTenant(ctx, tenantID)
	require.NoError(t, err, "Failed to delete via %s", protocols[1])

	// Verify deletion via first protocol
	_, err = client1.GetTenant(ctx, tenantID)
	assert.Error(t, err, "Tenant should be deleted")
}

// TestProtocolErrorHandling verifies error handling is consistent across protocols.
func TestProtocolErrorHandling(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	for _, protocol := range allProtocols() {
		t.Run(string(protocol), func(t *testing.T) {
			client := requireProtocolClient(t, protocol, cluster)
			defer client.Close()

			ctx := context.Background()

			// Test getting non-existent tenant
			t.Run("NonExistentTenant", func(t *testing.T) {
				_, err := client.GetTenant(ctx, "nonexistent-tenant-id")
				assert.Error(t, err, "Should return error for non-existent tenant via %s", protocol)
			})

			// Test invalid tenant creation
			t.Run("InvalidTenant", func(t *testing.T) {
				_, err := client.CreateTenant(ctx, "", "Empty ID", nil)
				assert.Error(t, err, "Should return error for empty tenant ID via %s", protocol)
			})

			// Test operations on non-existent user
			t.Run("NonExistentUser", func(t *testing.T) {
				tenantID := fmt.Sprintf("tenant-error-%s-%d", strings.ToLower(string(protocol)), time.Now().UnixNano())
				_, err := client.CreateTenant(ctx, tenantID, "Error Test", nil)
				require.NoError(t, err)
				defer client.DeleteTenant(ctx, tenantID)

				_, err = client.GetUser(ctx, tenantID, "nonexistent-user-id")
				assert.Error(t, err, "Should return error for non-existent user via %s", protocol)
			})
		})
	}
}
