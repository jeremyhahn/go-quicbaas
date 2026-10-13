# Parameter Store — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/parameter-store/`.

Fine-grained access control for the Parameter Store using RBAC and ABAC policies.

## Overview

The Parameter Store integrates with QuicBaaS's IAM system to provide:
- **RBAC (Role-Based Access Control)** - Predefined roles for common access patterns
- **ABAC (Attribute-Based Access Control)** - Fine-grained policies based on attributes

## RBAC Roles

### Built-in Roles

| Role | Permissions | Use Case |
|------|-------------|----------|
| `parameters-reader` | Read parameters | Read-only access for applications |
| `parameters-writer` | Read, write parameters | Application configuration management |
| `parameters-admin` | Read, write, delete, admin | Full administrative access |

### Assigning Roles

```bash
# Assign role to user
quicbaas iam role assign user-123 parameters-reader

# List user roles
quicbaas iam user get user-123
```

### Role Permissions

**parameters-reader:**
- `parameter:read` - Read parameter values
- `parameter:list` - List parameters

**parameters-writer:**
- `parameter:read` - Read parameter values
- `parameter:list` - List parameters
- `parameter:write` - Create and update parameters

**parameters-admin:**
- `parameter:read` - Read parameter values
- `parameter:list` - List parameters
- `parameter:write` - Create and update parameters
- `parameter:delete` - Delete parameters
- `parameter:admin` - Administrative operations

## ABAC Policies

ABAC policies enable fine-grained access control based on:
- Parameter path
- Parameter tags
- User attributes
- Context (time, IP, etc.)

### Policy Structure

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: policy-name
  tenant: tenant-id        # Optional: tenant-specific policy
  priority: 100            # Higher priority = evaluated first
spec:
  subjects:
    authenticated: true    # Require authentication
    roles: [role-list]     # Required roles
    groups: [group-list]   # Required groups
  variables:
    var_name: "expression" # Reusable expressions
  rules:
    - name: rule-name
      effect: allow|deny
      actions: [action-list]
      resources: [resource-patterns]
      when: [conditions]   # Required conditions
      unless: [exceptions] # Exception conditions
```

### Example Policies

#### 1. Path-Based Access Control

Restrict access to specific path hierarchies:

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: path-based-access
spec:
  subjects:
    authenticated: true
  rules:
    # Allow users to access their own namespace
    - name: user-namespace-access
      effect: allow
      actions: [parameter:read, parameter:write, parameter:delete]
      resources:
        - parameters:/users/${subject.id}/**

    # Allow reading shared parameters
    - name: shared-read-access
      effect: allow
      actions: [parameter:read, parameter:list]
      resources:
        - parameters:/shared/**
```

#### 2. Environment Isolation

Restrict production access to authorized users:

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: environment-isolation
  priority: 100
spec:
  subjects:
    authenticated: true
  rules:
    # Deny production writes to non-admins
    - name: deny-prod-writes
      effect: deny
      actions: [parameter:write, parameter:delete]
      resources:
        - parameters:/*/prod/**
        - parameters:/*/production/**
      unless:
        - subject.roles contains prod-admin
        - subject.groups contains platform-team

    # Allow production reads for everyone
    - name: allow-prod-reads
      effect: allow
      actions: [parameter:read, parameter:list]
      resources:
        - parameters:/*/prod/**
        - parameters:/*/production/**
```

#### 3. Tag-Based Access Control

Control access based on parameter tags:

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: tag-based-access
spec:
  subjects:
    authenticated: true
  rules:
    # Users can only access parameters matching their environment
    - name: environment-match
      effect: allow
      actions: [parameter:read]
      resources:
        - parameters:**
      when:
        - context.parameter_tags.env == subject.attrs.allowed_env

    # Sensitive parameters require special role
    - name: sensitive-parameters
      effect: deny
      actions: [parameter:read, parameter:write]
      resources:
        - parameters:**
      when:
        - context.parameter_tags.sensitivity == "high"
      unless:
        - subject.roles contains security-admin
```

#### 4. Encryption Policy

Enforce encryption for sensitive paths:

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: encryption-policy
  priority: 200
spec:
  subjects:
    authenticated: true
  rules:
    # Deny unencrypted writes to secrets path
    - name: require-encryption
      effect: deny
      actions: [parameter:write]
      resources:
        - parameters:/*/secrets/**
        - parameters:/*/credentials/**
      when:
        - context.parameter_encrypted == false
```

#### 5. Tenant Isolation

Enforce multi-tenant isolation:

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: tenant-isolation
  priority: 9999
spec:
  subjects:
    authenticated: true
  rules:
    # Deny cross-tenant access
    - name: cross-tenant-deny
      effect: deny
      actions: ["*"]
      resources:
        - parameters:/tenant/**
      unless:
        - resource.path startswith "parameters:/tenant/${subject.tenant}/"
```

#### 6. Time-Based Access

Restrict changes to maintenance windows:

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: maintenance-window
spec:
  subjects:
    groups: [operations]
  rules:
    # Allow production changes only during maintenance window
    - name: maintenance-only
      effect: allow
      actions: [parameter:write, parameter:delete]
      resources:
        - parameters:/production/**
      when:
        - time.hour >= 2
        - time.hour <= 6
        - time.weekday in ["saturday", "sunday"]
```

#### 7. Batch Operation Limits

Restrict batch operation sizes:

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: batch-limits
spec:
  subjects:
    authenticated: true
  rules:
    # Limit batch operations for non-admins
    - name: batch-size-limit
      effect: deny
      actions: [parameter:write, parameter:delete]
      resources:
        - parameters:**
      when:
        - context.parameter_batch_size > 100
      unless:
        - subject.roles contains parameters-admin
```

## Parameter Context

The ABAC provider extracts parameter context for policy evaluation:

| Context Variable | Type | Description |
|-----------------|------|-------------|
| `context.parameter_path` | string | Full parameter path |
| `context.parameter_type` | string | Parameter type (string, number, etc.) |
| `context.parameter_encrypted` | boolean | Whether parameter is encrypted |
| `context.parameter_tags` | map | Parameter tags |
| `context.parameter_owner` | string | Parameter owner user ID |
| `context.parameter_tenant` | string | Parameter tenant ID |
| `context.parameter_operation` | string | Operation (get, set, delete, etc.) |
| `context.parameter_batch_size` | int | Batch operation size |
| `context.parameter_audit_enabled` | boolean | Audit logging enabled |

## Best Practices

### 1. Use Hierarchical Paths

Design paths to support natural ABAC policies:

```
/tenant/{tenant}/env/{env}/service/{service}/key
```

This enables policies like:
- `/tenant/${subject.tenant}/**` - Tenant isolation
- `/tenant/*/env/prod/**` - Environment restrictions

### 2. Tag Consistently

Use consistent tag schemas:

```yaml
tags:
  env: prod|staging|dev
  team: platform|backend|frontend
  sensitivity: low|medium|high
  compliance: pci|hipaa|soc2
```

### 3. Layer Policies

Use priority to layer policies:

| Priority | Purpose |
|----------|---------|
| 9999 | Security boundaries (tenant isolation) |
| 1000 | Compliance requirements |
| 100 | Environment restrictions |
| 10 | Team/service access |
| 1 | Default allow rules |

### 4. Audit Critical Access

Enable audit logging for sensitive parameters:

```bash
curl -X PUT "http://localhost:8080/api/v1/parameters/app/secrets/api-key" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"value": "dmFsdWU=", "kms_key_id": "default", "audit_enabled": true, "tags": {"sensitivity": "high"}}'
```

### 5. Test Policies

Evaluate a request against the active policies:

```bash
quicbaas iam policy test \
  --subject '{"id": "user-123", "roles": ["parameters-writer"]}' \
  --action parameter:write \
  --resource "parameters:/production/database/password"
```

## Troubleshooting

### Access Denied

1. Check user roles:
   ```bash
   quicbaas iam user get user-123
   ```

2. View policy evaluation:
   ```bash
   quicbaas iam policy test \
     --subject '{"id": "user-123", "roles": ["parameters-reader"]}' \
     --action parameter:read \
     --resource "parameters:/app/config"
   ```

3. Check parameter tags match policy conditions

### Policy Not Applied

1. Verify policy priority (higher priority evaluated first)
2. Check tenant scope (global vs tenant-specific)
3. Ensure subject criteria match (roles, groups, authenticated)

## See Also

- README - Overview and quick start
- [Architecture](architecture.md) - Internal design
- IAM Documentation - Identity and access management
- [ABAC Guide](../authorization/architecture.md) - ABAC policy reference
