# Security → IAM

## Purpose

The IAM (Identity and Access Management) view manages users, roles, and policies across tenants. Use this view to create users, assign roles to users, manage role definitions and permissions, and configure multi-tenant access control. This is the central hub for authentication and authorization in QuicBaaS.

## Layout

The page is structured as a tabbed interface:

**Tabs** — "Users", "Roles", "Policies", "Tenants"

**Tab: Users**

```
Tenant:      [default ▼]  (if multi-tenant enabled)
Search:      [________]

Create User: [+ New User]

Users:

User ID              Email                  Roles                 Status    Actions
─────────────────────────────────────────────────────────────────────────────────
user_alice           alice@example.com      admin, data-manager   ✓ Active  [⋮]
user_bob             bob@example.com        viewer                ✓ Active  [⋮]
user_carol           carol@example.com      —                     ⚠ Pending [⋮]

[Edit] [Assign Roles] [Reset Password] [Delete]  (per user)
```

- **User ID** — unique identifier
- **Email** — user email address
- **Roles** — assigned role list
- **Status** — Active (green), Pending (yellow), Disabled (gray)
- Three-dot menu for actions

**Tab: Roles**

```
Search: [________]

Create Role: [+ New Role]

Roles:

Role Name            Description                  Users   Permissions      Actions
────────────────────────────────────────────────────────────────────────────────────
admin                Full system access           2       All              [⋮]
data-manager         Manage data resources        5       KMS, Secrets, KV [⋮]
viewer               Read-only access            12       View all         [⋮]

[Edit] [Duplicate] [Delete]  (per role)
```

- **Role Name** — e.g., `admin`, `data-manager`, `viewer`
- **Description** — what the role is for
- **Users** — count of users with this role
- **Permissions** — list of granted scopes

**Tab: Policies**

```
Policy Type: [RBAC ▼]  (or ABAC)
Tenant:      [default ▼]

Policies:

Policy Name          Type   Target          Condition     Actions
──────────────────────────────────────────────────────────────────
secret-access        RBAC   Secrets API     role=dev      [⋮]
kms-encrypt-only     RBAC   KMS Keys        action=encrypt [⋮]
time-based-access    ABAC   All APIs       hour < 18:00  [⋮]

[+ New Policy] [Edit] [Delete]  (per policy)
```

- **Policy Name** — identifier
- **Type** — RBAC (role-based) or ABAC (attribute-based)
- **Target** — which resource or API the policy applies to
- **Condition** — constraint (role, attribute, time, etc.)

**Tab: Tenants** (if multi-tenant enabled)

```
Tenants:

Tenant ID     Name        Users   Roles    Created            Actions
───────────────────────────────────────────────────────────────────────
default       Default     15      3        2026-01-01T00:00Z  [⋮]
acme-corp     ACME Corp   42      8        2026-02-15T10:00Z  [⋮]
startup-xyz   Startup XYZ  5      2        2026-04-01T14:00Z  [⋮]

[+ New Tenant] [Edit] [Delete]  (per tenant)
```

- **Tenant ID** — unique identifier
- **Name** — display name
- **Users** — count of users in tenant
- **Roles** — count of roles available in tenant
- **Created** — ISO 8601 timestamp

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Create User | Plus | Opens a form to register a new user (pending status) | `POST /api/v1/tenants/{tenant}/users` |
| Enroll Authenticator | Key | Generates invite link for user to enroll passkey, security key, or smartcard | `POST /api/v1/tenants/{tenant}/users/{userID}/enroll` |
| Assign Roles | Lock | Opens a role selector; updates user's roles | `POST /api/v1/tenants/{tenant}/users/{userID}/roles` |
| Reset Authenticator | Refresh | Generates a new invite link if user lost their device | `POST /api/v1/tenants/{tenant}/users/{userID}/reset-authenticator` |
| Delete User | Trash | Removes a user from the tenant; shows confirm modal | `DELETE /api/v1/tenants/{tenant}/users/{userID}` |
| Create Role | Plus | Opens a form to define a new role with permissions | `POST /api/v1/tenants/{tenant}/roles` |
| Assign Permissions to Role | Edit | Opens a checklist of permissions; assigns to the role | `PUT /api/v1/tenants/{tenant}/roles/{roleID}` |
| Duplicate Role | Copy | Creates a copy of a role with a new name | `POST /api/v1/tenants/{tenant}/roles` (with copied permissions) |
| Delete Role | Trash | Removes a role; shows warning if users are assigned | `DELETE /api/v1/tenants/{tenant}/roles/{roleID}` |
| Create Policy | Plus | Opens a form for RBAC or ABAC policy creation | `POST /api/v1/tenants/{tenant}/policies` |
| Edit Policy | Edit | Modifies policy rules and conditions | `PUT /api/v1/tenants/{tenant}/policies/{policyID}` |
| Delete Policy | Trash | Removes a policy | `DELETE /api/v1/tenants/{tenant}/policies/{policyID}` |

## Walkthrough

### Create a user and activate authenticator

1. Open the IAM view (`#/security/iam`).
2. Click the **Users** tab.
3. Click **+ New User** button.
4. A form modal appears:

```
Create User

Tenant:      [default ▼]  (read-only if single-tenant)
Email:       [alice@example.com______________]
Name:        [Alice Smith___________]

Authenticator: [Auto-generate invite link]  (checkbox)

               [Cancel] [Create]
```

5. Enter **Email**: `alice@example.com`.
6. Enter **Name**: `Alice Smith`.
7. Check **Auto-generate invite link** to create an enrollment invitation.
8. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "User created: alice@example.com (pending activation)"
✓ If invite was generated: "Invite link: https://quicbaas.example.com/auth/invite?token=..."
✓ User appears in the Users list with status "Pending" (yellow)
✓ Admin should send the invite URL to alice@example.com
```

9. Alice receives the invite, visits the URL, and enrolls her first passkey or security key.
10. Once she activates, her status changes to "Active" (green).
11. Find the activated user in the Users table (alice@example.com).
12. Click the three-dot menu and select **Assign Roles**.
12. A multi-select modal appears:

```
Assign Roles to alice@example.com

Available Roles:

[ ] admin           (Full system access)
[✓] data-manager    (Manage data resources)
[ ] viewer          (Read-only access)

             [Cancel] [Save]
```

13. Check **admin** and **data-manager** to assign both roles to Alice.
14. Click **Save**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Roles assigned to alice"
✓ Users table updates: alice now shows "admin, data-manager" in the Roles column
```

### Create a custom role

15. Click the **Roles** tab.
16. Click **+ New Role** button.
17. A form modal appears:

```
Create Role

Role Name:    [kms-operators_______________]
Description:  [Users who manage encryption keys]
Tenant:       [default ▼]

Permissions:

[ ] agents:register       (Register agents)
[✓] kms:generatekey       (Generate KMS keys)
[✓] kms:rotatekey         (Rotate keys)
[✓] kms:deletekey         (Delete keys)
[ ] kms:decrypt           (Decrypt data)
[ ] secrets:read          (Read secrets)
[ ] secrets:write         (Write secrets)

             [Cancel] [Create]
```

18. Enter **Role Name**: `kms-operators`.
19. Enter **Description**: `Users who manage encryption keys`.
20. Check the KMS-related permissions:
    - `kms:generatekey`
    - `kms:rotatekey`
    - `kms:deletekey`
21. Leave `kms:decrypt` unchecked (separation of duties — operators create/rotate but don't decrypt).
22. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Role created: kms-operators"
✓ Role appears in the Roles list with 3 permissions
```

### Assign a role to a user

23. Go back to the **Users** tab.
24. Create another user (e.g., `kms-admin@example.com`).
25. After creation, click the three-dot menu and select **Assign Roles**.
26. Check **kms-operators** (your newly created role).
27. Click **Save**.

**Expected outcome:**

```
✓ Success toast: "Roles assigned"
✓ User now has the `kms-operators` role with its 3 permissions
```

### Create a policy (RBAC example)

28. Click the **Policies** tab.
29. Click **+ New Policy** button.
30. A form appears:

```
Create Policy

Policy Name:  [secret-restricted__________]
Type:         [RBAC ▼]  (or ABAC)
Tenant:       [default ▼]

RBAC Configuration:

Target:       [Secrets API ▼]  (or KMS, Data, etc.)
Role:         [data-manager ▼]
Action:       [✓ Read] [✓ Write] [✗ Delete]

Effect:       [Allow ▼]  (or Deny)

              [Cancel] [Create]
```

31. Enter **Policy Name**: `secret-restricted`.
32. **Type** is RBAC.
33. **Target**: `Secrets API`.
34. **Role**: `data-manager`.
35. **Action**: Check Read and Write, leave Delete unchecked.
36. **Effect**: Allow.
37. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Policy created: secret-restricted"
✓ Policy appears in the Policies list
```

38. Any user with the `data-manager` role can now read and write secrets, but not delete them.

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Create User form shows validation error | Email is invalid or already exists | Use a valid email address; check if the user already exists |
| Assign Roles button is disabled | User is in a different tenant | Switch tenants in the Tenant dropdown; then find the user |
| Role has no permissions assigned | New roles start with no permissions | Click Assign Roles or edit the role to add permissions |
| Delete Role fails with "Users assigned" | Role is still in use by users | Reassign those users to a different role first |
| Reset Password does not send email | Email service is not configured | Check daemon config; set up email integration |
| Policy does not take effect | Policy syntax is invalid or target does not exist | Verify policy type (RBAC vs ABAC); check target resource name |
| Users cannot log in after role change | Role change revokes their permissions | Reassign appropriate role; or ask them to log out and back in |

## See also

- [CLI: quicbaas iam](../cli/iam.md) — manage IAM via CLI
- [iam/README.md](../iam/README.md) — IAM architecture and configuration
- [iam/rbac.md](../iam/rbac.md) — RBAC reference
- [iam/abac.md](../iam/abac.md) — ABAC policy syntax
- [security/authentication.md](../security/authentication.md) — authentication mechanisms
- [security/authorization.md](../security/authorization.md) — authorization policies
