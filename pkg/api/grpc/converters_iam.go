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
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// Tenant Conversions
// =============================================================================

// convertProtoToTenant converts a proto Tenant to a domain Tenant.
func convertProtoToTenant(pb *iampb.Tenant) *iam.Tenant {
	if pb == nil {
		return nil
	}

	tenant := &iam.Tenant{
		ID:   pb.Id,
		Name: pb.Name,
	}

	if pb.Settings != nil {
		tenant.Settings = iam.TenantSettings{
			MaxUsers:      int(pb.Settings.MaxUsers),
			MFARequired:   pb.Settings.MfaRequired,
			NamedNetworks: make(map[string][]string),
		}

		// Convert named networks
		for name, cidr := range pb.Settings.NamedNetworks {
			tenant.Settings.NamedNetworks[name] = []string{cidr}
		}
	}

	if pb.CreatedAt != nil {
		tenant.Created = pb.CreatedAt.AsTime()
	}

	if pb.UpdatedAt != nil {
		tenant.Updated = pb.UpdatedAt.AsTime()
	}

	return tenant
}

// convertTenantToProto converts a domain Tenant to a proto Tenant.
func convertTenantToProto(t *iam.Tenant) *iampb.Tenant {
	if t == nil {
		return nil
	}

	pb := &iampb.Tenant{
		Id:   t.ID,
		Name: t.Name,
	}

	// Convert settings
	settings := &iampb.TenantSettings{
		MaxUsers:      int32(t.Settings.MaxUsers), //nolint:gosec // G115: MaxUsers is a reasonable config value
		MfaRequired:   t.Settings.MFARequired,     //nolint:staticcheck // SA1019: the legacy field is mirrored on the wire for older clients.
		NamedNetworks: make(map[string]string),
	}

	// Convert named networks (take first CIDR from slice)
	for name, cidrs := range t.Settings.NamedNetworks {
		if len(cidrs) > 0 {
			settings.NamedNetworks[name] = cidrs[0]
		}
	}

	pb.Settings = settings

	// Convert timestamps
	pb.CreatedAt = timestamppb.New(t.Created)
	pb.UpdatedAt = timestamppb.New(t.Updated)

	return pb
}

// engineQuotasFromProto converts the tenant-create engine quota message to the
// engine's own Quotas struct. A nil message yields nil, which is what tells the
// tenant manager to leave the engine's configured defaults alone — an empty
// struct would instead write zeros and be read as "use the system default" for
// every limit, so the distinction is load-bearing.
func engineQuotasFromProto(pb *iampb.TenantEngineQuotas) *qrdbsdk.Quotas {
	if pb == nil {
		return nil
	}
	return &qrdbsdk.Quotas{
		MaxDatabases:          pb.GetMaxDatabases(),
		MaxRaftGroups:         pb.GetMaxRaftGroups(),
		MaxReplicasPerGroup:   pb.GetMaxReplicasPerGroup(),
		MaxTotalReplicas:      pb.GetMaxTotalReplicas(),
		MaxCollections:        pb.GetMaxCollections(),
		MaxKeys:               pb.GetMaxKeys(),
		MaxStorageBytes:       pb.GetMaxStorageBytes(),
		MaxDatabaseSizeBytes:  pb.GetMaxDatabaseSizeBytes(),
		MaxUsers:              pb.GetMaxUsers(),
		MaxRoles:              pb.GetMaxRoles(),
		MaxCertificates:       pb.GetMaxCertificates(),
		MaxBackups:            pb.GetMaxBackups(),
		MaxVectorDims:         pb.GetMaxVectorDims(),
		MaxTSMetrics:          pb.GetMaxTsMetrics(),
		MaxDedicatedWALGroups: pb.GetMaxDedicatedWalGroups(),
		SystemBarrierQuota:    pb.GetSystemBarrierQuota(),
	}
}

// engineEntitlementsFromProto converts the tenant-create engine entitlement
// message to the engine's own Entitlements struct. A nil message yields nil so
// the engine keeps its configured defaults rather than being handed an
// all-denied entitlement set.
func engineEntitlementsFromProto(pb *iampb.TenantEngineEntitlements) *qrdbsdk.Entitlements {
	if pb == nil {
		return nil
	}
	return &qrdbsdk.Entitlements{
		AllowedIsolationModes:     pb.GetAllowedIsolationModes(),
		AllowDedicatedRaft:        pb.GetAllowDedicatedRaft(),
		AllowVolumeStorage:        pb.GetAllowVolumeStorage(),
		AllowSystemManagedBarrier: pb.GetAllowSystemManagedBarrier(),
		AllowDedicatedWAL:         pb.GetAllowDedicatedWal(),
		AllowedBarrierModes:       pb.GetAllowedBarrierModes(),
	}
}

// =============================================================================
// User Conversions
// =============================================================================

// convertProtoToUser converts a proto User to a domain User.
func convertProtoToUser(pb *iampb.User) *iam.User {
	if pb == nil {
		return nil
	}

	user := &iam.User{
		ID:          pb.Id,
		TenantID:    pb.TenantId,
		Email:       pb.Email,
		Name:        pb.Name,
		Roles:       pb.Roles,
		Groups:      pb.Groups,
		MFAVerified: pb.MfaVerified,
	}

	// Convert attributes (proto uses string->string, domain uses string->interface{})
	if len(pb.Attributes) > 0 {
		user.Attributes = make(map[string]interface{}, len(pb.Attributes))
		for k, v := range pb.Attributes {
			user.Attributes[k] = v
		}
	}

	// Convert timestamps
	if pb.CreatedAt != nil {
		user.Created = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		user.Updated = pb.UpdatedAt.AsTime()
	}
	if pb.LastAuth != nil {
		lastAuth := pb.LastAuth.AsTime()
		user.LastAuth = &lastAuth
	}
	if pb.MfaTimestamp != nil {
		mfaTime := pb.MfaTimestamp.AsTime()
		user.MFATimestamp = &mfaTime
	}

	return user
}

// convertUserToProto converts a domain User to a proto User.
func convertUserToProto(u *iam.User) *iampb.User {
	if u == nil {
		return nil
	}

	pb := &iampb.User{
		Id:          u.ID,
		TenantId:    u.TenantID,
		Email:       u.Email,
		Name:        u.Name,
		Roles:       u.Roles,
		Groups:      u.Groups,
		MfaVerified: u.MFAVerified, //nolint:staticcheck // SA1019: the legacy field is still mirrored across the wire and the storage adapter for older clients.
	}

	// Convert attributes
	if len(u.Attributes) > 0 {
		pb.Attributes = make(map[string]string, len(u.Attributes))
		for k, v := range u.Attributes {
			// Convert interface{} to string
			if str, ok := v.(string); ok {
				pb.Attributes[k] = str
			}
		}
	}

	// Convert timestamps
	pb.CreatedAt = timestamppb.New(u.Created)
	pb.UpdatedAt = timestamppb.New(u.Updated)

	if u.LastAuth != nil {
		pb.LastAuth = timestamppb.New(*u.LastAuth)
	}

	if u.MFATimestamp != nil {
		pb.MfaTimestamp = timestamppb.New(*u.MFATimestamp)
	}

	return pb
}

// =============================================================================
// Group Conversions
// =============================================================================

// convertProtoToGroup converts a proto Group to a domain Group.
func convertProtoToGroup(pb *iampb.Group) *iam.Group {
	if pb == nil {
		return nil
	}

	group := &iam.Group{
		ID:          pb.Id,
		TenantID:    pb.TenantId,
		Name:        pb.Name,
		Description: pb.Description,
		Members:     pb.Members,
	}

	if pb.CreatedAt != nil {
		group.Created = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		group.Updated = pb.UpdatedAt.AsTime()
	}

	return group
}

// convertGroupToProto converts a domain Group to a proto Group.
func convertGroupToProto(g *iam.Group) *iampb.Group {
	if g == nil {
		return nil
	}

	return &iampb.Group{
		Id:          g.ID,
		TenantId:    g.TenantID,
		Name:        g.Name,
		Description: g.Description,
		Members:     g.Members,
		CreatedAt:   timestamppb.New(g.Created),
		UpdatedAt:   timestamppb.New(g.Updated),
	}
}

// =============================================================================
// Role Conversions
// =============================================================================

// convertProtoToRole converts a proto Role to a domain Role.
func convertProtoToRole(pb *iampb.Role) *iam.Role {
	if pb == nil {
		return nil
	}

	role := &iam.Role{
		ID:          pb.Id,
		TenantID:    pb.TenantId,
		Name:        pb.Name,
		Description: pb.Description,
		Permissions: pb.Permissions,
	}

	if pb.CreatedAt != nil {
		role.Created = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		role.Updated = pb.UpdatedAt.AsTime()
	}

	return role
}

// convertRoleToProto converts a domain Role to a proto Role.
func convertRoleToProto(r *iam.Role) *iampb.Role {
	if r == nil {
		return nil
	}

	return &iampb.Role{
		Id:          r.ID,
		TenantId:    r.TenantID,
		Name:        r.Name,
		Description: r.Description,
		Permissions: r.Permissions,
		CreatedAt:   timestamppb.New(r.Created),
		UpdatedAt:   timestamppb.New(r.Updated),
	}
}

// =============================================================================
// Application Conversions
// =============================================================================

// convertProtoToApplication converts a proto Application to a domain Application.
func convertProtoToApplication(pb *iampb.Application) *iam.Application {
	if pb == nil {
		return nil
	}

	app := &iam.Application{
		ClientID:       pb.ClientId,
		ClientSecret:   pb.ClientSecret,
		TenantID:       pb.TenantId,
		Name:           pb.Name,
		RedirectURIs:   pb.RedirectUris,
		ResourcePrefix: pb.ResourcePrefix,
		GrantTypes:     pb.GrantTypes,
		ResponseTypes:  pb.ResponseTypes,
	}

	if pb.CreatedAt != nil {
		app.Created = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		app.Updated = pb.UpdatedAt.AsTime()
	}

	return app
}

// convertApplicationToProto converts a domain Application to a proto Application.
func convertApplicationToProto(a *iam.Application) *iampb.Application {
	if a == nil {
		return nil
	}

	return &iampb.Application{
		ClientId:       a.ClientID,
		ClientSecret:   a.ClientSecret,
		TenantId:       a.TenantID,
		Name:           a.Name,
		RedirectUris:   a.RedirectURIs,
		ResourcePrefix: a.ResourcePrefix,
		GrantTypes:     a.GrantTypes,
		ResponseTypes:  a.ResponseTypes,
		CreatedAt:      timestamppb.New(a.Created),
		UpdatedAt:      timestamppb.New(a.Updated),
	}
}

// =============================================================================
// Policy Conversions
// =============================================================================

// convertProtoToPolicy converts a proto Policy to a domain Policy.
func convertProtoToPolicy(pb *iampb.Policy) *iam.Policy {
	if pb == nil {
		return nil
	}

	policy := &iam.Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: iam.PolicyMetadata{
			Name:        pb.Name,
			Tenant:      pb.TenantId,
			Description: pb.Description,
			Labels:      pb.Labels,
			Priority:    int(pb.Priority),
		},
	}

	if pb.CreatedAt != nil {
		policy.Metadata.Created = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		policy.Metadata.Updated = pb.UpdatedAt.AsTime()
	}

	// Note: spec_yaml needs to be parsed separately if needed
	// For now, we just store the metadata

	return policy
}

// convertPolicyToProto converts a domain Policy to a proto Policy.
func convertPolicyToProto(p *iam.Policy) *iampb.Policy {
	if p == nil {
		return nil
	}

	pb := &iampb.Policy{
		Name:        p.Metadata.Name,
		TenantId:    p.Metadata.Tenant,
		Description: p.Metadata.Description,
		Labels:      p.Metadata.Labels,
		Priority:    int32(p.Metadata.Priority), //nolint:gosec // G115: Priority is a small config value
	}

	pb.CreatedAt = timestamppb.New(p.Metadata.Created)
	pb.UpdatedAt = timestamppb.New(p.Metadata.Updated)

	// Serialize the full policy to YAML for the spec_yaml field.
	specYAML, err := yaml.Marshal(p)
	if err == nil {
		pb.SpecYaml = specYAML
	}

	return pb
}

// =============================================================================
// Subject Conversions (for PEP)
// =============================================================================

// convertProtoToSubject converts a proto Subject to a domain Subject.
func convertProtoToSubject(pb *iampb.Subject) *iam.Subject {
	if pb == nil {
		return nil
	}

	subject := &iam.Subject{
		ID:          pb.Id,
		TenantID:    pb.TenantId,
		Roles:       pb.Roles,
		Groups:      pb.Groups,
		MFAVerified: pb.MfaVerified,
	}

	// Convert attributes
	if len(pb.Attributes) > 0 {
		subject.Attributes = make(map[string]interface{}, len(pb.Attributes))
		for k, v := range pb.Attributes {
			subject.Attributes[k] = v
		}
	}

	// Convert MFA timestamp
	if pb.MfaTimestamp != nil {
		mfaTime := pb.MfaTimestamp.AsTime()
		subject.MFATimestamp = &mfaTime
	}

	return subject
}

// convertSubjectToProto converts a domain Subject to a proto Subject.
func convertSubjectToProto(s *iam.Subject) *iampb.Subject {
	if s == nil {
		return nil
	}

	pb := &iampb.Subject{
		Id:          s.ID,
		TenantId:    s.TenantID,
		Roles:       s.Roles,
		Groups:      s.Groups,
		MfaVerified: s.MFAVerified,
	}

	// Convert attributes
	if len(s.Attributes) > 0 {
		pb.Attributes = make(map[string]string, len(s.Attributes))
		for k, v := range s.Attributes {
			if str, ok := v.(string); ok {
				pb.Attributes[k] = str
			}
		}
	}

	// Convert MFA timestamp
	if s.MFATimestamp != nil {
		pb.MfaTimestamp = timestamppb.New(*s.MFATimestamp)
	}

	return pb
}

// =============================================================================
// Admin Role Conversions
// =============================================================================

// convertProtoAdminRole converts a proto AdminRole to a domain AdminRole.
func convertProtoAdminRole(pb iampb.AdminRole) iam.AdminRole {
	switch pb {
	case iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN:
		return iam.AdminRoleSuperAdmin
	case iampb.AdminRole_ADMIN_ROLE_TENANT_ADMIN:
		return iam.AdminRoleTenantAdmin
	case iampb.AdminRole_ADMIN_ROLE_USER_ADMIN:
		return iam.AdminRoleUserAdmin
	case iampb.AdminRole_ADMIN_ROLE_GROUP_ADMIN:
		return iam.AdminRoleGroupAdmin
	case iampb.AdminRole_ADMIN_ROLE_POLICY_ADMIN:
		return iam.AdminRolePolicyAdmin
	case iampb.AdminRole_ADMIN_ROLE_AUDIT_ADMIN:
		return iam.AdminRoleAuditAdmin
	case iampb.AdminRole_ADMIN_ROLE_READ_ONLY:
		return iam.AdminRoleReadOnly
	default:
		return ""
	}
}

// convertAdminRoleToProto converts a domain AdminRole to a proto AdminRole.
func convertAdminRoleToProto(role iam.AdminRole) iampb.AdminRole {
	switch role {
	case iam.AdminRoleSuperAdmin:
		return iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN
	case iam.AdminRoleTenantAdmin:
		return iampb.AdminRole_ADMIN_ROLE_TENANT_ADMIN
	case iam.AdminRoleUserAdmin:
		return iampb.AdminRole_ADMIN_ROLE_USER_ADMIN
	case iam.AdminRoleGroupAdmin:
		return iampb.AdminRole_ADMIN_ROLE_GROUP_ADMIN
	case iam.AdminRolePolicyAdmin:
		return iampb.AdminRole_ADMIN_ROLE_POLICY_ADMIN
	case iam.AdminRoleAuditAdmin:
		return iampb.AdminRole_ADMIN_ROLE_AUDIT_ADMIN
	case iam.AdminRoleReadOnly:
		return iampb.AdminRole_ADMIN_ROLE_READ_ONLY
	default:
		return iampb.AdminRole_ADMIN_ROLE_UNSPECIFIED
	}
}

// =============================================================================
// Admin Assignment Conversions
// =============================================================================

// convertAdminAssignmentToProto converts a domain AdminAssignment to a proto AdminAssignment.
func convertAdminAssignmentToProto(a *iam.AdminAssignment) *iampb.AdminAssignment {
	if a == nil {
		return nil
	}

	pb := &iampb.AdminAssignment{
		Id:        a.ID,
		UserId:    a.UserID,
		AdminRole: convertAdminRoleToProto(a.AdminRole),
		TenantIds: a.TenantIDs,
		GrantedBy: a.GrantedBy,
		GrantedAt: timestamppb.New(a.GrantedAt),
		Active:    a.Active,
		Metadata:  a.Metadata,
	}

	// Convert permissions
	if len(a.Permissions) > 0 {
		pb.Permissions = make([]string, len(a.Permissions))
		for i, p := range a.Permissions {
			pb.Permissions[i] = string(p)
		}
	}

	// Convert denied permissions
	if len(a.DeniedPermissions) > 0 {
		pb.DeniedPermissions = make([]string, len(a.DeniedPermissions))
		for i, p := range a.DeniedPermissions {
			pb.DeniedPermissions[i] = string(p)
		}
	}

	// Convert resource scopes
	if len(a.ResourceScopes) > 0 {
		pb.ResourceScopes = make([]*iampb.ResourceScope, len(a.ResourceScopes))
		for i, rs := range a.ResourceScopes {
			pb.ResourceScopes[i] = &iampb.ResourceScope{
				ResourceType:    rs.ResourceType,
				ResourcePattern: rs.ResourcePattern,
			}
		}
	}

	// Convert optional timestamps
	if a.ExpiresAt != nil {
		pb.ExpiresAt = timestamppb.New(*a.ExpiresAt)
	}
	if a.LastUsedAt != nil {
		pb.LastUsedAt = timestamppb.New(*a.LastUsedAt)
	}

	return pb
}

// convertProtoToAdminAssignment converts a proto AdminAssignment to a domain AdminAssignment.
func convertProtoToAdminAssignment(pb *iampb.AdminAssignment) *iam.AdminAssignment {
	if pb == nil {
		return nil
	}

	a := &iam.AdminAssignment{
		ID:        pb.Id,
		UserID:    pb.UserId,
		AdminRole: convertProtoAdminRole(pb.AdminRole),
		TenantIDs: pb.TenantIds,
		GrantedBy: pb.GrantedBy,
		Active:    pb.Active,
		Metadata:  pb.Metadata,
	}

	// Convert permissions
	if len(pb.Permissions) > 0 {
		a.Permissions = make([]iam.AdminPermission, len(pb.Permissions))
		for i, p := range pb.Permissions {
			a.Permissions[i] = iam.AdminPermission(p)
		}
	}

	// Convert denied permissions
	if len(pb.DeniedPermissions) > 0 {
		a.DeniedPermissions = make([]iam.AdminPermission, len(pb.DeniedPermissions))
		for i, p := range pb.DeniedPermissions {
			a.DeniedPermissions[i] = iam.AdminPermission(p)
		}
	}

	// Convert resource scopes
	if len(pb.ResourceScopes) > 0 {
		a.ResourceScopes = make([]iam.ResourceScope, len(pb.ResourceScopes))
		for i, rs := range pb.ResourceScopes {
			a.ResourceScopes[i] = iam.ResourceScope{
				ResourceType:    rs.ResourceType,
				ResourcePattern: rs.ResourcePattern,
			}
		}
	}

	// Convert timestamps
	if pb.GrantedAt != nil {
		a.GrantedAt = pb.GrantedAt.AsTime()
	}
	if pb.ExpiresAt != nil {
		t := pb.ExpiresAt.AsTime()
		a.ExpiresAt = &t
	}
	if pb.LastUsedAt != nil {
		t := pb.LastUsedAt.AsTime()
		a.LastUsedAt = &t
	}

	return a
}

// =============================================================================
// Impersonation Session Conversions
// =============================================================================

// convertImpersonationSessionToProto converts a domain ImpersonationSession to a proto ImpersonationSession.
func convertImpersonationSessionToProto(s *iam.ImpersonationSession) *iampb.ImpersonationSession {
	if s == nil {
		return nil
	}

	pb := &iampb.ImpersonationSession{
		Id:           s.ID,
		AdminUserId:  s.AdminUserID,
		TargetUserId: s.TargetUserID,
		TenantId:     s.TenantID,
		Reason:       s.Reason,
		StartedAt:    timestamppb.New(s.StartedAt),
		ExpiresAt:    timestamppb.New(s.ExpiresAt),
		AuditLogId:   s.AuditLogID,
	}

	if s.EndedAt != nil {
		pb.EndedAt = timestamppb.New(*s.EndedAt)
	}

	return pb
}

// convertProtoToImpersonationSession converts a proto ImpersonationSession to a domain ImpersonationSession.
func convertProtoToImpersonationSession(pb *iampb.ImpersonationSession) *iam.ImpersonationSession {
	if pb == nil {
		return nil
	}

	s := &iam.ImpersonationSession{
		ID:           pb.Id,
		AdminUserID:  pb.AdminUserId,
		TargetUserID: pb.TargetUserId,
		TenantID:     pb.TenantId,
		Reason:       pb.Reason,
		AuditLogID:   pb.AuditLogId,
	}

	if pb.StartedAt != nil {
		s.StartedAt = pb.StartedAt.AsTime()
	}
	if pb.ExpiresAt != nil {
		s.ExpiresAt = pb.ExpiresAt.AsTime()
	}
	if pb.EndedAt != nil {
		t := pb.EndedAt.AsTime()
		s.EndedAt = &t
	}

	return s
}

// =============================================================================
// GDPR Consent Conversions
// =============================================================================

// convertProtoConsentPurpose converts a proto ConsentPurpose to a domain ConsentPurpose.
func convertProtoConsentPurpose(pb iampb.ConsentPurpose) iam.ConsentPurpose {
	switch pb {
	case iampb.ConsentPurpose_CONSENT_PURPOSE_MARKETING:
		return iam.ConsentPurposeMarketing
	case iampb.ConsentPurpose_CONSENT_PURPOSE_ANALYTICS:
		return iam.ConsentPurposeAnalytics
	case iampb.ConsentPurpose_CONSENT_PURPOSE_PERSONALIZATION:
		return iam.ConsentPurposePersonalization
	case iampb.ConsentPurpose_CONSENT_PURPOSE_THIRD_PARTY:
		return iam.ConsentPurposeThirdParty
	case iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING:
		return iam.ConsentPurposeDataProcessing
	case iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_RETENTION:
		return iam.ConsentPurposeDataRetention
	case iampb.ConsentPurpose_CONSENT_PURPOSE_FUNCTIONAL:
		return iam.ConsentPurposeFunctional
	default:
		return ""
	}
}

// convertConsentPurposeToProto converts a domain ConsentPurpose to a proto ConsentPurpose.
func convertConsentPurposeToProto(purpose iam.ConsentPurpose) iampb.ConsentPurpose {
	switch purpose {
	case iam.ConsentPurposeMarketing:
		return iampb.ConsentPurpose_CONSENT_PURPOSE_MARKETING
	case iam.ConsentPurposeAnalytics:
		return iampb.ConsentPurpose_CONSENT_PURPOSE_ANALYTICS
	case iam.ConsentPurposePersonalization:
		return iampb.ConsentPurpose_CONSENT_PURPOSE_PERSONALIZATION
	case iam.ConsentPurposeThirdParty:
		return iampb.ConsentPurpose_CONSENT_PURPOSE_THIRD_PARTY
	case iam.ConsentPurposeDataProcessing:
		return iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING
	case iam.ConsentPurposeDataRetention:
		return iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_RETENTION
	case iam.ConsentPurposeFunctional:
		return iampb.ConsentPurpose_CONSENT_PURPOSE_FUNCTIONAL
	default:
		return iampb.ConsentPurpose_CONSENT_PURPOSE_UNSPECIFIED
	}
}

// convertProtoConsentStatus converts a proto ConsentStatus to a domain ConsentStatus.
func convertProtoConsentStatus(pb iampb.ConsentStatus) iam.ConsentStatus {
	switch pb {
	case iampb.ConsentStatus_CONSENT_STATUS_GRANTED:
		return iam.ConsentStatusGranted
	case iampb.ConsentStatus_CONSENT_STATUS_DENIED:
		return iam.ConsentStatusDenied
	case iampb.ConsentStatus_CONSENT_STATUS_WITHDRAWN:
		return iam.ConsentStatusWithdrawn
	case iampb.ConsentStatus_CONSENT_STATUS_PENDING:
		return iam.ConsentStatusPending
	default:
		return ""
	}
}

// convertConsentStatusToProto converts a domain ConsentStatus to a proto ConsentStatus.
func convertConsentStatusToProto(status iam.ConsentStatus) iampb.ConsentStatus {
	switch status {
	case iam.ConsentStatusGranted:
		return iampb.ConsentStatus_CONSENT_STATUS_GRANTED
	case iam.ConsentStatusDenied:
		return iampb.ConsentStatus_CONSENT_STATUS_DENIED
	case iam.ConsentStatusWithdrawn:
		return iampb.ConsentStatus_CONSENT_STATUS_WITHDRAWN
	case iam.ConsentStatusPending:
		return iampb.ConsentStatus_CONSENT_STATUS_PENDING
	default:
		return iampb.ConsentStatus_CONSENT_STATUS_UNSPECIFIED
	}
}

// convertConsentToProto converts a domain Consent to a proto Consent.
func convertConsentToProto(c *iam.Consent) *iampb.Consent {
	if c == nil {
		return nil
	}

	pb := &iampb.Consent{
		Id:        c.ID,
		UserId:    c.UserID,
		TenantId:  c.TenantID,
		Purpose:   convertConsentPurposeToProto(c.Purpose),
		Status:    convertConsentStatusToProto(c.Status),
		Version:   c.Version,
		IpAddress: c.IPAddress,
		UserAgent: c.UserAgent,
		Metadata:  c.Metadata,
		CreatedAt: timestamppb.New(c.Created),
		UpdatedAt: timestamppb.New(c.Updated),
	}

	if c.GrantedAt != nil {
		pb.GrantedAt = timestamppb.New(*c.GrantedAt)
	}
	if c.WithdrawnAt != nil {
		pb.WithdrawnAt = timestamppb.New(*c.WithdrawnAt)
	}
	if c.ExpiresAt != nil {
		pb.ExpiresAt = timestamppb.New(*c.ExpiresAt)
	}

	return pb
}

// convertProtoToConsent converts a proto Consent to a domain Consent.
func convertProtoToConsent(pb *iampb.Consent) *iam.Consent {
	if pb == nil {
		return nil
	}

	c := &iam.Consent{
		ID:        pb.Id,
		UserID:    pb.UserId,
		TenantID:  pb.TenantId,
		Purpose:   convertProtoConsentPurpose(pb.Purpose),
		Status:    convertProtoConsentStatus(pb.Status),
		Version:   pb.Version,
		IPAddress: pb.IpAddress,
		UserAgent: pb.UserAgent,
		Metadata:  pb.Metadata,
	}

	if pb.CreatedAt != nil {
		c.Created = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		c.Updated = pb.UpdatedAt.AsTime()
	}
	if pb.GrantedAt != nil {
		t := pb.GrantedAt.AsTime()
		c.GrantedAt = &t
	}
	if pb.WithdrawnAt != nil {
		t := pb.WithdrawnAt.AsTime()
		c.WithdrawnAt = &t
	}
	if pb.ExpiresAt != nil {
		t := pb.ExpiresAt.AsTime()
		c.ExpiresAt = &t
	}

	return c
}

// =============================================================================
// GDPR Data Export Conversions
// =============================================================================

// convertProtoDataExportFormat converts a proto DataExportFormat to a domain DataExportFormat.
func convertProtoDataExportFormat(pb iampb.DataExportFormat) iam.DataExportFormat {
	switch pb {
	case iampb.DataExportFormat_DATA_EXPORT_FORMAT_JSON:
		return iam.DataExportFormatJSON
	case iampb.DataExportFormat_DATA_EXPORT_FORMAT_CSV:
		return iam.DataExportFormatCSV
	case iampb.DataExportFormat_DATA_EXPORT_FORMAT_XML:
		return iam.DataExportFormatXML
	default:
		return iam.DataExportFormatJSON
	}
}

// convertDataExportFormatToProto converts a domain DataExportFormat to a proto DataExportFormat.
func convertDataExportFormatToProto(format iam.DataExportFormat) iampb.DataExportFormat {
	switch format {
	case iam.DataExportFormatJSON:
		return iampb.DataExportFormat_DATA_EXPORT_FORMAT_JSON
	case iam.DataExportFormatCSV:
		return iampb.DataExportFormat_DATA_EXPORT_FORMAT_CSV
	case iam.DataExportFormatXML:
		return iampb.DataExportFormat_DATA_EXPORT_FORMAT_XML
	default:
		return iampb.DataExportFormat_DATA_EXPORT_FORMAT_UNSPECIFIED
	}
}

// convertProtoDataExportStatus converts a proto DataExportStatus to a domain DataExportStatus.
func convertProtoDataExportStatus(pb iampb.DataExportStatus) iam.DataExportStatus {
	switch pb {
	case iampb.DataExportStatus_DATA_EXPORT_STATUS_PENDING:
		return iam.DataExportStatusPending
	case iampb.DataExportStatus_DATA_EXPORT_STATUS_PROCESSING:
		return iam.DataExportStatusProcessing
	case iampb.DataExportStatus_DATA_EXPORT_STATUS_COMPLETED:
		return iam.DataExportStatusCompleted
	case iampb.DataExportStatus_DATA_EXPORT_STATUS_FAILED:
		return iam.DataExportStatusFailed
	case iampb.DataExportStatus_DATA_EXPORT_STATUS_EXPIRED:
		return iam.DataExportStatusExpired
	default:
		return ""
	}
}

// convertDataExportStatusToProto converts a domain DataExportStatus to a proto DataExportStatus.
func convertDataExportStatusToProto(status iam.DataExportStatus) iampb.DataExportStatus {
	switch status {
	case iam.DataExportStatusPending:
		return iampb.DataExportStatus_DATA_EXPORT_STATUS_PENDING
	case iam.DataExportStatusProcessing:
		return iampb.DataExportStatus_DATA_EXPORT_STATUS_PROCESSING
	case iam.DataExportStatusCompleted:
		return iampb.DataExportStatus_DATA_EXPORT_STATUS_COMPLETED
	case iam.DataExportStatusFailed:
		return iampb.DataExportStatus_DATA_EXPORT_STATUS_FAILED
	case iam.DataExportStatusExpired:
		return iampb.DataExportStatus_DATA_EXPORT_STATUS_EXPIRED
	default:
		return iampb.DataExportStatus_DATA_EXPORT_STATUS_UNSPECIFIED
	}
}

// convertDataExportRequestToProto converts a domain DataExportRequest to a proto DataExportRequest.
func convertDataExportRequestToProto(r *iam.DataExportRequest) *iampb.DataExportRequest {
	if r == nil {
		return nil
	}

	pb := &iampb.DataExportRequest{
		Id:                r.ID,
		UserId:            r.UserID,
		TenantId:          r.TenantID,
		RequestedBy:       r.RequestedBy,
		Format:            convertDataExportFormatToProto(r.Format),
		Status:            convertDataExportStatusToProto(r.Status),
		IncludeCategories: r.IncludeCategories,
		ExcludeCategories: r.ExcludeCategories,
		RequestedAt:       timestamppb.New(r.RequestedAt),
		DownloadUrl:       r.DownloadURL,
		FileSizeBytes:     r.FileSizeBytes,
		ErrorMessage:      r.ErrorMessage,
		Metadata:          r.Metadata,
	}

	if r.StartedAt != nil {
		pb.StartedAt = timestamppb.New(*r.StartedAt)
	}
	if r.CompletedAt != nil {
		pb.CompletedAt = timestamppb.New(*r.CompletedAt)
	}
	if r.ExpiresAt != nil {
		pb.ExpiresAt = timestamppb.New(*r.ExpiresAt)
	}

	return pb
}

// convertProtoToDataExportRequest converts a proto DataExportRequest to a domain DataExportRequest.
func convertProtoToDataExportRequest(pb *iampb.DataExportRequest) *iam.DataExportRequest {
	if pb == nil {
		return nil
	}

	r := &iam.DataExportRequest{
		ID:                pb.Id,
		UserID:            pb.UserId,
		TenantID:          pb.TenantId,
		RequestedBy:       pb.RequestedBy,
		Format:            convertProtoDataExportFormat(pb.Format),
		Status:            convertProtoDataExportStatus(pb.Status),
		IncludeCategories: pb.IncludeCategories,
		ExcludeCategories: pb.ExcludeCategories,
		DownloadURL:       pb.DownloadUrl,
		FileSizeBytes:     pb.FileSizeBytes,
		ErrorMessage:      pb.ErrorMessage,
		Metadata:          pb.Metadata,
	}

	if pb.RequestedAt != nil {
		r.RequestedAt = pb.RequestedAt.AsTime()
	}
	if pb.StartedAt != nil {
		t := pb.StartedAt.AsTime()
		r.StartedAt = &t
	}
	if pb.CompletedAt != nil {
		t := pb.CompletedAt.AsTime()
		r.CompletedAt = &t
	}
	if pb.ExpiresAt != nil {
		t := pb.ExpiresAt.AsTime()
		r.ExpiresAt = &t
	}

	return r
}

// =============================================================================
// GDPR User Data Export Conversions
// =============================================================================

// convertUserDataExportToProto converts a domain UserDataExport to a proto UserDataExport.
func convertUserDataExportToProto(e *iam.UserDataExport) *iampb.UserDataExport {
	if e == nil {
		return nil
	}

	pb := &iampb.UserDataExport{
		ExportMetadata: convertExportMetadataToProto(&e.ExportMetadata),
	}

	if e.Profile != nil {
		pb.Profile = convertUserProfileExportToProto(e.Profile)
	}

	if len(e.Sessions) > 0 {
		pb.Sessions = make([]*iampb.SessionExport, len(e.Sessions))
		for i, s := range e.Sessions {
			pb.Sessions[i] = convertSessionExportToProto(s)
		}
	}

	if len(e.Consents) > 0 {
		pb.Consents = make([]*iampb.ConsentExport, len(e.Consents))
		for i, c := range e.Consents {
			pb.Consents[i] = convertConsentExportToProto(c)
		}
	}

	if len(e.Groups) > 0 {
		pb.Groups = make([]*iampb.GroupMembershipExport, len(e.Groups))
		for i, g := range e.Groups {
			pb.Groups[i] = convertGroupMembershipExportToProto(g)
		}
	}

	if len(e.Roles) > 0 {
		pb.Roles = make([]*iampb.RoleAssignmentExport, len(e.Roles))
		for i, r := range e.Roles {
			pb.Roles[i] = convertRoleAssignmentExportToProto(r)
		}
	}

	if len(e.AuditLogs) > 0 {
		pb.AuditLogs = make([]*iampb.AuditLogExport, len(e.AuditLogs))
		for i, a := range e.AuditLogs {
			pb.AuditLogs[i] = convertAuditLogExportToProto(a)
		}
	}

	if len(e.Credentials) > 0 {
		pb.Credentials = make([]*iampb.CredentialExport, len(e.Credentials))
		for i, c := range e.Credentials {
			pb.Credentials[i] = convertCredentialExportToProto(c)
		}
	}

	if e.MFAInfo != nil {
		pb.MfaInfo = convertMFAInfoExportToProto(e.MFAInfo)
	}

	return pb
}

// convertExportMetadataToProto converts a domain ExportMetadata to a proto ExportMetadata.
func convertExportMetadataToProto(m *iam.ExportMetadata) *iampb.ExportMetadata {
	if m == nil {
		return nil
	}

	return &iampb.ExportMetadata{
		ExportId:      m.ExportID,
		UserId:        m.UserID,
		TenantId:      m.TenantID,
		ExportedAt:    timestamppb.New(m.ExportedAt),
		Format:        convertDataExportFormatToProto(m.Format),
		Categories:    m.Categories,
		SchemaVersion: m.SchemaVersion,
	}
}

// convertUserProfileExportToProto converts a domain UserProfileExport to a proto UserProfileExport.
func convertUserProfileExportToProto(p *iam.UserProfileExport) *iampb.UserProfileExport {
	if p == nil {
		return nil
	}

	pb := &iampb.UserProfileExport{
		Id:      p.ID,
		Email:   p.Email,
		Name:    p.Name,
		Created: timestamppb.New(p.Created),
		Updated: timestamppb.New(p.Updated),
	}

	if len(p.Attributes) > 0 {
		pb.Attributes = make(map[string]string, len(p.Attributes))
		for k, v := range p.Attributes {
			if str, ok := v.(string); ok {
				pb.Attributes[k] = str
			}
		}
	}

	if p.LastAuth != nil {
		pb.LastAuth = timestamppb.New(*p.LastAuth)
	}

	return pb
}

// convertSessionExportToProto converts a domain SessionExport to a proto SessionExport.
func convertSessionExportToProto(s *iam.SessionExport) *iampb.SessionExport {
	if s == nil {
		return nil
	}

	pb := &iampb.SessionExport{
		Id:         s.ID,
		StartedAt:  timestamppb.New(s.StartedAt),
		ExpiresAt:  timestamppb.New(s.ExpiresAt),
		LastActive: timestamppb.New(s.LastActive),
		IpAddress:  s.IPAddress,
		UserAgent:  s.UserAgent,
		AuthMethod: s.AuthMethod,
	}

	if s.EndedAt != nil {
		pb.EndedAt = timestamppb.New(*s.EndedAt)
	}

	return pb
}

// convertConsentExportToProto converts a domain ConsentExport to a proto ConsentExport.
func convertConsentExportToProto(c *iam.ConsentExport) *iampb.ConsentExport {
	if c == nil {
		return nil
	}

	pb := &iampb.ConsentExport{
		Purpose: convertConsentPurposeToProto(c.Purpose),
		Status:  convertConsentStatusToProto(c.Status),
		Version: c.Version,
	}

	if c.GrantedAt != nil {
		pb.GrantedAt = timestamppb.New(*c.GrantedAt)
	}
	if c.WithdrawnAt != nil {
		pb.WithdrawnAt = timestamppb.New(*c.WithdrawnAt)
	}

	return pb
}

// convertGroupMembershipExportToProto converts a domain GroupMembershipExport to a proto GroupMembershipExport.
func convertGroupMembershipExportToProto(g *iam.GroupMembershipExport) *iampb.GroupMembershipExport {
	if g == nil {
		return nil
	}

	return &iampb.GroupMembershipExport{
		GroupId:   g.GroupID,
		GroupName: g.GroupName,
		JoinedAt:  timestamppb.New(g.JoinedAt),
	}
}

// convertRoleAssignmentExportToProto converts a domain RoleAssignmentExport to a proto RoleAssignmentExport.
func convertRoleAssignmentExportToProto(r *iam.RoleAssignmentExport) *iampb.RoleAssignmentExport {
	if r == nil {
		return nil
	}

	return &iampb.RoleAssignmentExport{
		RoleId:     r.RoleID,
		RoleName:   r.RoleName,
		AssignedAt: timestamppb.New(r.AssignedAt),
	}
}

// convertAuditLogExportToProto converts a domain AuditLogExport to a proto AuditLogExport.
func convertAuditLogExportToProto(a *iam.AuditLogExport) *iampb.AuditLogExport {
	if a == nil {
		return nil
	}

	return &iampb.AuditLogExport{
		Timestamp: timestamppb.New(a.Timestamp),
		Action:    a.Action,
		Resource:  a.Resource,
		IpAddress: a.IPAddress,
		Success:   a.Success,
	}
}

// convertCredentialExportToProto converts a domain CredentialExport to a proto CredentialExport.
func convertCredentialExportToProto(c *iam.CredentialExport) *iampb.CredentialExport {
	if c == nil {
		return nil
	}

	return &iampb.CredentialExport{
		Id:          c.ID,
		Type:        c.Type,
		Name:        c.Name,
		CreatedAt:   timestamppb.New(c.CreatedAt),
		LastUsedAt:  timestamppb.New(c.LastUsedAt),
		Attestation: c.Attestation,
		Transports:  c.Transports,
	}
}

// convertMFAInfoExportToProto converts a domain MFAInfoExport to a proto MFAInfoExport.
func convertMFAInfoExportToProto(m *iam.MFAInfoExport) *iampb.MFAInfoExport {
	if m == nil {
		return nil
	}

	pb := &iampb.MFAInfoExport{
		Enabled:              m.Enabled,
		Method:               m.Method,
		BackupCodesRemaining: int32(m.BackupCodes), //nolint:gosec // G115: Backup codes count is small
	}

	if m.EnrolledAt != nil {
		pb.EnrolledAt = timestamppb.New(*m.EnrolledAt)
	}

	return pb
}

// =============================================================================
// GDPR Data Deletion Conversions
// =============================================================================

// convertProtoDeletionStatus converts a proto DeletionStatus to a domain DeletionStatus.
func convertProtoDeletionStatus(pb iampb.DeletionStatus) iam.DeletionStatus {
	switch pb {
	case iampb.DeletionStatus_DELETION_STATUS_PENDING:
		return iam.DeletionStatusPending
	case iampb.DeletionStatus_DELETION_STATUS_PROCESSING:
		return iam.DeletionStatusProcessing
	case iampb.DeletionStatus_DELETION_STATUS_COMPLETED:
		return iam.DeletionStatusCompleted
	case iampb.DeletionStatus_DELETION_STATUS_FAILED:
		return iam.DeletionStatusFailed
	case iampb.DeletionStatus_DELETION_STATUS_CANCELLED:
		return iam.DeletionStatusCancelled
	default:
		return ""
	}
}

// convertDeletionStatusToProto converts a domain DeletionStatus to a proto DeletionStatus.
func convertDeletionStatusToProto(status iam.DeletionStatus) iampb.DeletionStatus {
	switch status {
	case iam.DeletionStatusPending:
		return iampb.DeletionStatus_DELETION_STATUS_PENDING
	case iam.DeletionStatusProcessing:
		return iampb.DeletionStatus_DELETION_STATUS_PROCESSING
	case iam.DeletionStatusCompleted:
		return iampb.DeletionStatus_DELETION_STATUS_COMPLETED
	case iam.DeletionStatusFailed:
		return iampb.DeletionStatus_DELETION_STATUS_FAILED
	case iam.DeletionStatusCancelled:
		return iampb.DeletionStatus_DELETION_STATUS_CANCELLED
	default:
		return iampb.DeletionStatus_DELETION_STATUS_UNSPECIFIED
	}
}

// convertDataDeletionRequestToProto converts a domain DataDeletionRequest to a proto DataDeletionRequest.
func convertDataDeletionRequestToProto(r *iam.DataDeletionRequest) *iampb.DataDeletionRequest {
	if r == nil {
		return nil
	}

	pb := &iampb.DataDeletionRequest{
		Id:                 r.ID,
		UserId:             r.UserID,
		TenantId:           r.TenantID,
		RequestedBy:        r.RequestedBy,
		Reason:             r.Reason,
		Status:             convertDeletionStatusToProto(r.Status),
		DeleteCategories:   r.DeleteCategories,
		PreserveCategories: r.PreserveCategories,
		RequestedAt:        timestamppb.New(r.RequestedAt),
		CancelledBy:        r.CancelledBy,
		ErrorMessage:       r.ErrorMessage,
		Metadata:           r.Metadata,
	}

	if r.GracePeriodEndsAt != nil {
		pb.GracePeriodEndsAt = timestamppb.New(*r.GracePeriodEndsAt)
	}
	if r.StartedAt != nil {
		pb.StartedAt = timestamppb.New(*r.StartedAt)
	}
	if r.CompletedAt != nil {
		pb.CompletedAt = timestamppb.New(*r.CompletedAt)
	}
	if r.CancelledAt != nil {
		pb.CancelledAt = timestamppb.New(*r.CancelledAt)
	}
	if r.DeletedDataSummary != nil {
		pb.DeletedDataSummary = convertDeletionSummaryToProto(r.DeletedDataSummary)
	}

	return pb
}

// convertProtoToDataDeletionRequest converts a proto DataDeletionRequest to a domain DataDeletionRequest.
func convertProtoToDataDeletionRequest(pb *iampb.DataDeletionRequest) *iam.DataDeletionRequest {
	if pb == nil {
		return nil
	}

	r := &iam.DataDeletionRequest{
		ID:                 pb.Id,
		UserID:             pb.UserId,
		TenantID:           pb.TenantId,
		RequestedBy:        pb.RequestedBy,
		Reason:             pb.Reason,
		Status:             convertProtoDeletionStatus(pb.Status),
		DeleteCategories:   pb.DeleteCategories,
		PreserveCategories: pb.PreserveCategories,
		CancelledBy:        pb.CancelledBy,
		ErrorMessage:       pb.ErrorMessage,
		Metadata:           pb.Metadata,
	}

	if pb.RequestedAt != nil {
		r.RequestedAt = pb.RequestedAt.AsTime()
	}
	if pb.GracePeriodEndsAt != nil {
		t := pb.GracePeriodEndsAt.AsTime()
		r.GracePeriodEndsAt = &t
	}
	if pb.StartedAt != nil {
		t := pb.StartedAt.AsTime()
		r.StartedAt = &t
	}
	if pb.CompletedAt != nil {
		t := pb.CompletedAt.AsTime()
		r.CompletedAt = &t
	}
	if pb.CancelledAt != nil {
		t := pb.CancelledAt.AsTime()
		r.CancelledAt = &t
	}
	if pb.DeletedDataSummary != nil {
		r.DeletedDataSummary = convertProtoToDeletionSummary(pb.DeletedDataSummary)
	}

	return r
}

// convertDeletionSummaryToProto converts a domain DeletionSummary to a proto DeletionSummary.
func convertDeletionSummaryToProto(s *iam.DeletionSummary) *iampb.DeletionSummary {
	if s == nil {
		return nil
	}

	pb := &iampb.DeletionSummary{
		ProfileDeleted:          s.ProfileDeleted,
		SessionsDeleted:         int32(s.SessionsDeleted),         //nolint:gosec // G115: Reasonable count
		AuditLogsAnonymized:     int32(s.AuditLogsAnonymized),     //nolint:gosec // G115: Reasonable count
		ConsentsDeleted:         int32(s.ConsentsDeleted),         //nolint:gosec // G115: Reasonable count
		CredentialsDeleted:      int32(s.CredentialsDeleted),      //nolint:gosec // G115: Reasonable count
		GroupMembershipsRemoved: int32(s.GroupMembershipsRemoved), //nolint:gosec // G115: Reasonable count
		RoleAssignmentsRemoved:  int32(s.RoleAssignmentsRemoved),  //nolint:gosec // G115: Reasonable count
		TokensRevoked:           int32(s.TokensRevoked),           //nolint:gosec // G115: Reasonable count
	}

	if len(s.AdditionalData) > 0 {
		pb.AdditionalData = make(map[string]int32, len(s.AdditionalData))
		for k, v := range s.AdditionalData {
			pb.AdditionalData[k] = int32(v) //nolint:gosec // G115: Reasonable count
		}
	}

	return pb
}

// convertProtoToDeletionSummary converts a proto DeletionSummary to a domain DeletionSummary.
func convertProtoToDeletionSummary(pb *iampb.DeletionSummary) *iam.DeletionSummary {
	if pb == nil {
		return nil
	}

	s := &iam.DeletionSummary{
		ProfileDeleted:          pb.ProfileDeleted,
		SessionsDeleted:         int(pb.SessionsDeleted),
		AuditLogsAnonymized:     int(pb.AuditLogsAnonymized),
		ConsentsDeleted:         int(pb.ConsentsDeleted),
		CredentialsDeleted:      int(pb.CredentialsDeleted),
		GroupMembershipsRemoved: int(pb.GroupMembershipsRemoved),
		RoleAssignmentsRemoved:  int(pb.RoleAssignmentsRemoved),
		TokensRevoked:           int(pb.TokensRevoked),
	}

	if len(pb.AdditionalData) > 0 {
		s.AdditionalData = make(map[string]int, len(pb.AdditionalData))
		for k, v := range pb.AdditionalData {
			s.AdditionalData[k] = int(v)
		}
	}

	return s
}

// =============================================================================
// Helper Functions
// =============================================================================

// convertProtoTimestamp safely converts a proto timestamp to time.Time.
func convertProtoTimestamp(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

// convertTimeToProto safely converts a time.Time to proto timestamp.
func convertTimeToProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
