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

package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// IAM Transport Configuration
// =============================================================================

// iamTransportsToSkip defines which transports to skip for IAM operations.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
// QUIC does not have IAM service implementations.
var iamTransportsToSkip = []Transport{TransportQUIC}

// =============================================================================
// IAM Service Availability Probe
// =============================================================================

// RequireIAMAvailable probes the server to verify that the IAM service
// is enabled. If the server returns "not enabled", "unknown service", or similar
// errors, it validates the error response is correct and returns false.
// Returns true if the IAM service is available, false otherwise.
// Tests should return early when this method returns false.
func (r *TestRunner) RequireIAMAvailable() bool {
	r.t.Helper()

	probeCmd := CommandDef{
		Name:           "IAM service availability probe",
		Args:           []string{"iam", "tenant", "list"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}

	for _, skip := range probeCmd.SkipTransports {
		if skip == r.transport {
			return true
		}
	}

	stdout, stderr, err := r.Run(probeCmd)
	if err != nil && outputIndicatesUnavailable(stdout, stderr) {
		combined := strings.TrimSpace(stdout + stderr)
		r.t.Logf("[%s] IAM service not enabled on server (validated correct error response: %s)",
			r.transport, combined)
		return false
	}
	return true
}

// =============================================================================
// IAM Tenant Commands
// =============================================================================

// IAMTenantCreateCommand returns a command to create an IAM tenant.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMTenantCreateCommand(tenantID, name string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam tenant create %s", tenantID),
		Args:           []string{"iam", "tenant", "create", tenantID, "--name", name, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{tenantID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMTenantCreateWithOptionsCommand returns a command to create an IAM tenant
// with additional options including max users and MFA requirement.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMTenantCreateWithOptionsCommand(tenantID, name string, maxUsers int, mfaRequired bool) CommandDef {
	args := []string{"iam", "tenant", "create", tenantID, "--name", name, "--yes"}
	if maxUsers > 0 {
		args = append(args, "--max-users", fmt.Sprintf("%d", maxUsers))
	}
	if mfaRequired {
		args = append(args, "--mfa-required")
	}
	return CommandDef{
		Name:           fmt.Sprintf("iam tenant create %s with options", tenantID),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{tenantID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMTenantGetCommand returns a command to get an IAM tenant.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMTenantGetCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam tenant get %s", tenantID),
		Args:           []string{"iam", "tenant", "get", tenantID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{tenantID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMTenantGetJSONCommand returns a command to get an IAM tenant in JSON format.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMTenantGetJSONCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam tenant get %s json", tenantID),
		Args:           []string{"iam", "tenant", "get", tenantID, "--output", "json"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{tenantID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMTenantUpdateCommand returns a command to update an IAM tenant name.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMTenantUpdateCommand(tenantID, name string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam tenant update %s", tenantID),
		Args:           []string{"iam", "tenant", "update", tenantID, "--name", name},
		ExpectSuccess:  true,
		ExpectOutput:   []string{tenantID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMTenantDeleteCommand returns a command to delete an IAM tenant.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMTenantDeleteCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam tenant delete %s", tenantID),
		Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{tenantID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMTenantListCommand returns a command to list IAM tenants.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMTenantListCommand() CommandDef {
	return CommandDef{
		Name:           "iam tenant list",
		Args:           []string{"iam", "tenant", "list"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMTenantListJSONCommand returns a command to list IAM tenants in JSON format.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMTenantListJSONCommand() CommandDef {
	return CommandDef{
		Name:           "iam tenant list json",
		Args:           []string{"iam", "tenant", "list", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM User Commands
// =============================================================================

// IAMUserCreateCommand returns a command to create an IAM user within a tenant.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
// QuicBaaS is passwordless: the new user is created in the pending state and
// activates via the FIDO2/PIV invite flow.
func IAMUserCreateCommand(tenantID, email, name string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam user create %s", email),
		Args:           []string{"iam", "user", "create", email, "--tenant", tenantID, "--name", name},
		ExpectSuccess:  true,
		ExpectOutput:   []string{email},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMUserDeleteCommand returns a command to delete an IAM user within a tenant.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMUserDeleteCommand(tenantID, userID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam user delete %s", userID),
		Args:           []string{"iam", "user", "delete", userID, "--tenant", tenantID, "--yes"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM Group Commands
// =============================================================================

// IAMGroupCreateCommand returns a command to create an IAM group within a tenant.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGroupCreateCommand(tenantID, groupID, name string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam group create %s", groupID),
		Args:           []string{"iam", "group", "create", groupID, "--tenant", tenantID, "--name", name},
		ExpectSuccess:  true,
		ExpectOutput:   []string{groupID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGroupCreateWithDescCommand returns a command to create an IAM group with a description.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGroupCreateWithDescCommand(tenantID, groupID, name, desc string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam group create %s with desc", groupID),
		Args:           []string{"iam", "group", "create", groupID, "--tenant", tenantID, "--name", name, "--description", desc},
		ExpectSuccess:  true,
		ExpectOutput:   []string{groupID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGroupGetCommand returns a command to get an IAM group.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGroupGetCommand(tenantID, groupID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam group get %s", groupID),
		Args:           []string{"iam", "group", "get", groupID, "--tenant", tenantID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{groupID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGroupUpdateCommand returns a command to update an IAM group name.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGroupUpdateCommand(tenantID, groupID, name string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam group update %s", groupID),
		Args:           []string{"iam", "group", "update", groupID, "--tenant", tenantID, "--name", name},
		ExpectSuccess:  true,
		ExpectOutput:   []string{groupID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGroupDeleteCommand returns a command to delete an IAM group.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGroupDeleteCommand(tenantID, groupID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam group delete %s", groupID),
		Args:           []string{"iam", "group", "delete", groupID, "--tenant", tenantID, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{groupID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGroupListCommand returns a command to list IAM groups within a tenant.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGroupListCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam group list tenant=%s", tenantID),
		Args:           []string{"iam", "group", "list", "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGroupAddMemberCommand returns a command to add a member to an IAM group.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGroupAddMemberCommand(tenantID, groupID, userID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam group add-member %s to %s", userID, groupID),
		Args:           []string{"iam", "group", "add-member", groupID, "--tenant", tenantID, "--user", userID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{userID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGroupRemoveMemberCommand returns a command to remove a member from an IAM group.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGroupRemoveMemberCommand(tenantID, groupID, userID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam group remove-member %s from %s", userID, groupID),
		Args:           []string{"iam", "group", "remove-member", groupID, "--tenant", tenantID, "--user", userID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{userID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGroupMembersCommand returns a command to list members of an IAM group.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGroupMembersCommand(tenantID, groupID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam group members %s", groupID),
		Args:           []string{"iam", "group", "members", groupID, "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM App Commands
// =============================================================================

// IAMAppRegisterCommand returns a command to register an IAM application.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAppRegisterCommand(tenantID, appName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam app register %s", appName),
		Args:           []string{"iam", "app", "register", "--name", appName, "--tenant", tenantID, "--redirect-uri", "http://localhost:8080/callback"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{appName},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAppRegisterWithRedirectCommand returns a command to register an IAM application
// with a redirect URI.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAppRegisterWithRedirectCommand(tenantID, appName, redirectURI string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam app register %s with redirect", appName),
		Args:           []string{"iam", "app", "register", "--name", appName, "--tenant", tenantID, "--redirect-uri", redirectURI},
		ExpectSuccess:  true,
		ExpectOutput:   []string{appName},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAppGetCommand returns a command to get an IAM application by client ID.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAppGetCommand(tenantID, clientID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam app get %s", clientID),
		Args:           []string{"iam", "app", "get", clientID, "--tenant", tenantID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{clientID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAppUpdateCommand returns a command to update an IAM application name.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAppUpdateCommand(tenantID, clientID, newName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam app update %s", clientID),
		Args:           []string{"iam", "app", "update", clientID, "--tenant", tenantID, "--name", newName},
		ExpectSuccess:  true,
		ExpectOutput:   []string{clientID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAppDeleteCommand returns a command to delete an IAM application.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAppDeleteCommand(tenantID, clientID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam app delete %s", clientID),
		Args:           []string{"iam", "app", "delete", clientID, "--tenant", tenantID, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{clientID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAppListCommand returns a command to list IAM applications within a tenant.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAppListCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam app list tenant=%s", tenantID),
		Args:           []string{"iam", "app", "list", "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAppRotateSecretCommand returns a command to rotate an IAM application's client secret.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAppRotateSecretCommand(tenantID, clientID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam app rotate-secret %s", clientID),
		Args:           []string{"iam", "app", "rotate-secret", clientID, "--tenant", tenantID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{clientID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM Policy Commands
// =============================================================================

// IAMPolicyApplyCommand returns a command to apply an IAM policy from a file.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMPolicyApplyCommand(policyFile string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam policy apply %s", policyFile),
		Args:           []string{"iam", "policy", "apply", "-f", policyFile},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMPolicyGetCommand returns a command to get an IAM policy by name.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMPolicyGetCommand(tenantID, policyName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam policy get %s", policyName),
		Args:           []string{"iam", "policy", "get", policyName, "--tenant", tenantID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{policyName},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMPolicyDeleteCommand returns a command to delete an IAM policy.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMPolicyDeleteCommand(tenantID, policyName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam policy delete %s", policyName),
		Args:           []string{"iam", "policy", "delete", policyName, "--tenant", tenantID, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{policyName},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMPolicyListCommand returns a command to list IAM policies within a tenant.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMPolicyListCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam policy list tenant=%s", tenantID),
		Args:           []string{"iam", "policy", "list", "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMPolicyValidateCommand returns a command to validate an IAM policy file.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMPolicyValidateCommand(policyFile string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam policy validate %s", policyFile),
		Args:           []string{"iam", "policy", "validate", "-f", policyFile},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMPolicyTestCommand returns a command to test an IAM policy evaluation.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMPolicyTestCommand(tenantID, subject, action, resource string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam policy test %s %s %s", subject, action, resource),
		Args:           []string{"iam", "policy", "test", "--tenant", tenantID, "--subject", subject, "--action", action, "--resource", resource},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM Session Commands
// =============================================================================

// IAMSessionListCommand returns a command to list IAM sessions for the admin user.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMSessionListCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam session list tenant=%s", tenantID),
		Args:           []string{"iam", "session", "list", "--tenant", tenantID, "--user", "admin"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMSessionGetCommand returns a command to get an IAM session by ID.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMSessionGetCommand(tenantID, sessionID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam session get %s", sessionID),
		Args:           []string{"iam", "session", "get", sessionID, "--tenant", tenantID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{sessionID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMSessionRevokeCommand returns a command to revoke an IAM session.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMSessionRevokeCommand(tenantID, sessionID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam session revoke %s", sessionID),
		Args:           []string{"iam", "session", "revoke", sessionID, "--tenant", tenantID, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{sessionID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMSessionRevokeAllCommand returns a command to revoke all IAM sessions for the admin user.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMSessionRevokeAllCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam session revoke-all tenant=%s", tenantID),
		Args:           []string{"iam", "session", "revoke-all", "--tenant", tenantID, "--yes", "--user", "admin"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM Audit Commands
// =============================================================================

// IAMAuditQueryCommand returns a command to query IAM audit events.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAuditQueryCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam audit query tenant=%s", tenantID),
		Args:           []string{"iam", "audit", "query", "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAuditQueryWithFiltersCommand returns a command to query IAM audit events with filters.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAuditQueryWithFiltersCommand(tenantID, eventType, category, outcome string) CommandDef {
	args := []string{"iam", "audit", "query", "--tenant", tenantID}
	if eventType != "" {
		args = append(args, "--event-type", eventType)
	}
	if category != "" {
		args = append(args, "--category", category)
	}
	if outcome != "" {
		args = append(args, "--outcome", outcome)
	}
	return CommandDef{
		Name:           fmt.Sprintf("iam audit query tenant=%s filtered", tenantID),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAuditGetCommand returns a command to get a specific IAM audit event.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAuditGetCommand(tenantID, eventID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam audit get %s", eventID),
		Args:           []string{"iam", "audit", "get", eventID, "--tenant", tenantID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{eventID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM GDPR Commands
// =============================================================================

// IAMGDPRConsentGrantCommand returns a command to grant GDPR consent.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGDPRConsentGrantCommand(tenantID, userID, purpose, version string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam gdpr consent grant %s %s", userID, purpose),
		Args:           []string{"iam", "gdpr", "consent", "grant", "--tenant", tenantID, "--user-id", userID, "--purpose", purpose, "--version", version},
		ExpectSuccess:  true,
		ExpectOutput:   []string{purpose},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGDPRConsentWithdrawCommand returns a command to withdraw GDPR consent.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGDPRConsentWithdrawCommand(tenantID, userID, purpose string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam gdpr consent withdraw %s %s", userID, purpose),
		Args:           []string{"iam", "gdpr", "consent", "withdraw", "--tenant", tenantID, "--user-id", userID, "--purpose", purpose, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{purpose},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGDPRConsentGetCommand returns a command to get GDPR consent status.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGDPRConsentGetCommand(tenantID, userID, purpose string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam gdpr consent get %s %s", userID, purpose),
		Args:           []string{"iam", "gdpr", "consent", "get", "--tenant", tenantID, "--user-id", userID, "--purpose", purpose},
		ExpectSuccess:  true,
		ExpectOutput:   []string{purpose},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGDPRConsentListCommand returns a command to list GDPR consents for a user.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGDPRConsentListCommand(tenantID, userID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam gdpr consent list %s", userID),
		Args:           []string{"iam", "gdpr", "consent", "list", "--tenant", tenantID, "--user-id", userID},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGDPRExportRequestCommand returns a command to request a GDPR data export.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGDPRExportRequestCommand(tenantID, userID, format string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam gdpr export request %s", userID),
		Args:           []string{"iam", "gdpr", "export", "request", "--tenant", tenantID, "--user-id", userID, "--format", format},
		ExpectSuccess:  true,
		ExpectOutput:   []string{userID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGDPRExportStatusCommand returns a command to check GDPR export status.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGDPRExportStatusCommand(tenantID, userID, exportID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam gdpr export status %s", exportID),
		Args:           []string{"iam", "gdpr", "export", "status", "--tenant", tenantID, "--user-id", userID, "--export-id", exportID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{exportID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGDPRDeletionRequestCommand returns a command to request GDPR data deletion.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGDPRDeletionRequestCommand(tenantID, userID, reason string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam gdpr deletion request %s", userID),
		Args:           []string{"iam", "gdpr", "deletion", "request", "--tenant", tenantID, "--user-id", userID, "--reason", reason, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{userID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGDPRDeletionCancelCommand returns a command to cancel a GDPR deletion request.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGDPRDeletionCancelCommand(tenantID, userID, deletionID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam gdpr deletion cancel %s", deletionID),
		Args:           []string{"iam", "gdpr", "deletion", "cancel", "--tenant", tenantID, "--user-id", userID, "--deletion-id", deletionID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{deletionID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMGDPRDeletionStatusCommand returns a command to check GDPR deletion status.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMGDPRDeletionStatusCommand(tenantID, userID, deletionID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam gdpr deletion status %s", deletionID),
		Args:           []string{"iam", "gdpr", "deletion", "status", "--tenant", tenantID, "--user-id", userID, "--deletion-id", deletionID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{deletionID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM MFA Commands
// =============================================================================

// IAMMFAStatusCommand returns a command to check MFA status for a user.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMMFAStatusCommand(tenantID, userID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam mfa status %s", userID),
		Args:           []string{"iam", "mfa", "status", "--tenant", tenantID, "--user", userID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{userID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMMFAEnrollCommand returns a command to enroll a user in MFA.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMMFAEnrollCommand(tenantID, userID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam mfa enroll %s", userID),
		Args:           []string{"iam", "mfa", "enroll", "--tenant", tenantID, "--user", userID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{userID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMMFAVerifyCommand returns a command to verify an MFA enrollment with a TOTP code.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMMFAVerifyCommand(enrollmentID, code string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam mfa verify %s", enrollmentID),
		Args:           []string{"iam", "mfa", "verify", "--enrollment-id", enrollmentID, "--code", code},
		ExpectSuccess:  true,
		ExpectOutput:   []string{enrollmentID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMMFADisableCommand returns a command to disable MFA for a user.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMMFADisableCommand(tenantID, userID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam mfa disable %s", userID),
		Args:           []string{"iam", "mfa", "disable", "--tenant", tenantID, "--user", userID, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{userID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMMFABackupCodesStatusCommand returns a command to check MFA backup codes status.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMMFABackupCodesStatusCommand(tenantID, userID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam mfa backup-codes status %s", userID),
		Args:           []string{"iam", "mfa", "backup-codes", "status", "--tenant", tenantID, "--user", userID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{userID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMMFABackupCodesGenerateCommand returns a command to generate MFA backup codes.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMMFABackupCodesGenerateCommand(tenantID, userID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam mfa backup-codes generate %s", userID),
		Args:           []string{"iam", "mfa", "backup-codes", "generate", "--tenant", tenantID, "--user", userID, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{userID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM Federation Provider Commands
// =============================================================================

// IAMProviderCreateCommand returns a command to create a federation provider
// using a preset configuration (e.g., google, github, microsoft).
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMProviderCreateCommand(tenantID, providerPreset, clientID, clientSecret string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam provider create %s", providerPreset),
		Args:           []string{"iam", "provider", "create", "--tenant", tenantID, "--preset", providerPreset, "--client-id", clientID, "--client-secret", clientSecret},
		ExpectSuccess:  true,
		ExpectOutput:   []string{providerPreset},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMProviderCreateCustomCommand returns a command to create a custom federation provider.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMProviderCreateCustomCommand(tenantID, providerID, name, providerType, clientID, clientSecret, issuerURL string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam provider create custom %s", providerID),
		Args:           []string{"iam", "provider", "create", "--tenant", tenantID, "--provider-id", providerID, "--name", name, "--type", providerType, "--client-id", clientID, "--client-secret", clientSecret, "--issuer-url", issuerURL},
		ExpectSuccess:  true,
		ExpectOutput:   []string{providerID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMProviderGetCommand returns a command to get a federation provider.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMProviderGetCommand(tenantID, providerID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam provider get %s", providerID),
		Args:           []string{"iam", "provider", "get", "--provider-id", providerID, "--tenant", tenantID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{providerID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMProviderUpdateCommand returns a command to update a federation provider name.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMProviderUpdateCommand(tenantID, providerID, name string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam provider update %s", providerID),
		Args:           []string{"iam", "provider", "update", "--provider-id", providerID, "--tenant", tenantID, "--name", name},
		ExpectSuccess:  true,
		ExpectOutput:   []string{providerID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMProviderDeleteCommand returns a command to delete a federation provider.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMProviderDeleteCommand(tenantID, providerID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam provider delete %s", providerID),
		Args:           []string{"iam", "provider", "delete", "--provider-id", providerID, "--tenant", tenantID, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{providerID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMProviderListCommand returns a command to list federation providers within a tenant.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMProviderListCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam provider list tenant=%s", tenantID),
		Args:           []string{"iam", "provider", "list", "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM Federation Identity Commands
// =============================================================================

// IAMIdentityLinkCommand returns a command to link a federated identity to a user.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMIdentityLinkCommand(tenantID, userID, providerID, authCode string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam identity link %s to %s", providerID, userID),
		Args:           []string{"iam", "identity", "link", "--tenant", tenantID, "--user-id", userID, "--provider-id", providerID, "--auth-code", authCode},
		ExpectSuccess:  true,
		ExpectOutput:   []string{providerID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMIdentityUnlinkCommand returns a command to unlink a federated identity from a user.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMIdentityUnlinkCommand(tenantID, userID, providerID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam identity unlink %s from %s", providerID, userID),
		Args:           []string{"iam", "identity", "unlink", "--tenant", tenantID, "--user-id", userID, "--provider-id", providerID, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{providerID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMIdentityListCommand returns a command to list federated identities for a user.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMIdentityListCommand(tenantID, userID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam identity list %s", userID),
		Args:           []string{"iam", "identity", "list", "--tenant", tenantID, "--user-id", userID},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM Admin Assignment Commands
// =============================================================================

// IAMAdminAssignmentCreateCommand returns a command to create an admin role assignment.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAdminAssignmentCreateCommand(userID, role, grantedBy string, tenantIDs []string) CommandDef {
	args := []string{"iam", "admin", "assignment", "create", "--user", userID, "--role", role, "--granted-by", grantedBy}
	if len(tenantIDs) > 0 {
		args = append(args, "--tenant-ids", strings.Join(tenantIDs, ","))
	}
	return CommandDef{
		Name:           fmt.Sprintf("iam admin assignment create %s %s", userID, role),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{userID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAdminAssignmentGetCommand returns a command to get an admin role assignment.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAdminAssignmentGetCommand(assignmentID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam admin assignment get %s", assignmentID),
		Args:           []string{"iam", "admin", "assignment", "get", "--assignment-id", assignmentID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{assignmentID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAdminAssignmentUpdateCommand returns a command to update an admin role assignment.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAdminAssignmentUpdateCommand(assignmentID, role string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam admin assignment update %s", assignmentID),
		Args:           []string{"iam", "admin", "assignment", "update", "--assignment-id", assignmentID, "--role", role},
		ExpectSuccess:  true,
		ExpectOutput:   []string{assignmentID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAdminAssignmentDeleteCommand returns a command to delete an admin role assignment.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAdminAssignmentDeleteCommand(assignmentID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam admin assignment delete %s", assignmentID),
		Args:           []string{"iam", "admin", "assignment", "delete", "--assignment-id", assignmentID, "--yes"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{assignmentID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAdminAssignmentListCommand returns a command to list all admin role assignments.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAdminAssignmentListCommand() CommandDef {
	return CommandDef{
		Name:           "iam admin assignment list",
		Args:           []string{"iam", "admin", "assignment", "list"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM Admin Check Permission Commands
// =============================================================================

// IAMAdminCheckPermissionCommand returns a command to check if a user has a permission.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAdminCheckPermissionCommand(userID, permission string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam admin check-permission %s %s", userID, permission),
		Args:           []string{"iam", "admin", "check-permission", "--user-id", userID, "--permission", permission},
		ExpectSuccess:  true,
		ExpectOutput:   []string{userID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAdminCheckPermissionWithTenantCommand returns a command to check if a user
// has a permission within a specific tenant scope.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAdminCheckPermissionWithTenantCommand(userID, permission, tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam admin check-permission %s %s tenant=%s", userID, permission, tenantID),
		Args:           []string{"iam", "admin", "check-permission", "--user-id", userID, "--permission", permission, "--tenant-id", tenantID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{userID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM Admin Impersonate Commands
// =============================================================================

// IAMAdminImpersonateStartCommand returns a command to start an admin impersonation session.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAdminImpersonateStartCommand(adminUserID, targetUserID, tenantID, reason string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam admin impersonate start %s as %s", adminUserID, targetUserID),
		Args:           []string{"iam", "admin", "impersonate", "start", "--admin-user-id", adminUserID, "--target-user-id", targetUserID, "--tenant-id", tenantID, "--reason", reason},
		ExpectSuccess:  true,
		ExpectOutput:   []string{targetUserID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAdminImpersonateEndCommand returns a command to end an admin impersonation session.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAdminImpersonateEndCommand(sessionID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam admin impersonate end %s", sessionID),
		Args:           []string{"iam", "admin", "impersonate", "end", "--session-id", sessionID},
		ExpectSuccess:  true,
		ExpectOutput:   []string{sessionID},
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMAdminImpersonateListCommand returns a command to list admin impersonation sessions.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMAdminImpersonateListCommand(adminUserID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam admin impersonate list %s", adminUserID),
		Args:           []string{"iam", "admin", "impersonate", "list", "--admin-user-id", adminUserID},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM PEP Commands
// =============================================================================

// IAMPEPCheckCommand returns a command to perform a Policy Enforcement Point check.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMPEPCheckCommand(tenantID, subject, action, resource string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam pep check %s %s %s", subject, action, resource),
		Args:           []string{"iam", "pep", "check", "--tenant", tenantID, "--subject", subject, "--action", action, "--resource", resource},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// IAMPEPCheckBatchCommand returns a command to perform a batch PEP check from a file.
// IAM operations are supported via gRPC, REST, Unix, and MCP transports.
func IAMPEPCheckBatchCommand(tenantID, batchFile string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("iam pep check-batch %s", batchFile),
		Args:           []string{"iam", "pep", "check-batch", "--tenant", tenantID, "-f", batchFile},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
}

// =============================================================================
// IAM Test Helpers
// =============================================================================

// parseJSONField extracts a string field from JSON output. If the output
// contains multiple JSON objects or non-JSON content, it attempts to find
// and parse the first valid JSON object.
func parseJSONField(t *testing.T, output, field string) string {
	t.Helper()

	// Try to find JSON object boundaries in the output.
	start := strings.Index(output, "{")
	if start < 0 {
		t.Logf("parseJSONField: no JSON object found in output: %s", output)
		return ""
	}

	// Find the matching closing brace.
	depth := 0
	end := -1
	for i := start; i < len(output); i++ {
		switch output[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i + 1
			}
		}
		if end >= 0 {
			break
		}
	}

	if end < 0 {
		t.Logf("parseJSONField: no matching closing brace in output: %s", output)
		return ""
	}

	jsonStr := output[start:end]
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Logf("parseJSONField: JSON parse error: %v, input: %s", err, jsonStr)
		return ""
	}

	val, ok := parsed[field]
	if !ok {
		t.Logf("parseJSONField: field %q not found in JSON", field)
		return ""
	}

	str, ok := val.(string)
	if !ok {
		return fmt.Sprintf("%v", val)
	}
	return str
}
