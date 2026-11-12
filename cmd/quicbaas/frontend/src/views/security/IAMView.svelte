<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  IAMView renders a tabbed operator console for Identity and Access Management.

  Tabs:
    Users     — user table for a tenant: create / edit / disable / delete / assign role / GDPR consents.
    Tenants   — tenant list: create / edit / delete.
    Roles     — role list per tenant: create / edit / delete.
    Policies  — ABAC policy list per tenant: create / edit / delete.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import { tenantOptionsOf, withRequiredTenants, type TenantOption } from '../../lib/tenantOptions.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import type {
    IAMUser,
    IAMTenant,
    IAMRole,
    IAMPolicy,
    CreateUserRequest,
    UpdateUserRequest,
    UpdateTenantRequest,
    CreateRoleRequest,
    UpdateRoleRequest,
    CreatePolicyRequest,
    UpdatePolicyRequest,
    ConsentPurpose,
    UserConsent,
    IAMSession,
    MFAStatusResponse,
    DataExportRequest,
    DeletionRequest,
    WebAuthnCredentialSummary,
    ProvisionTenantRequest,
    ProvisionTenantResponse,
    TenantAdminEnrollment,
  } from '@quicbaas/sdk';
  import { ShieldCheck, UserPlus, Pencil, Trash2, UserCheck, ToggleLeft, RefreshCw, FileCheck, MonitorSmartphone, KeyRound, Database, Fingerprint } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import { routerStore, routeIAMTab } from '../../stores/router.svelte.ts';
  import { formatDateTime } from '../../lib/time.ts';

  // ---------------------------------------------------------------------------
  // Tab state
  // ---------------------------------------------------------------------------

  type Tab = 'users' | 'tenants' | 'roles' | 'policies';

  // Honour a tab deep-link from the router (e.g. #/iam/roles). When the
  // route carries no tab param, default to 'users'.
  function tabFromRoute(): Tab {
    const param = routerStore.params['tab'] ?? '';
    if (param === 'tenants' || param === 'roles' || param === 'policies') {
      return param;
    }
    return 'users';
  }

  let activeTab: Tab = $state(tabFromRoute());

  // Keep activeTab in sync when the user navigates via the browser back/forward
  // buttons or pastes a deep link while the view is already mounted.
  $effect(() => {
    const tab = tabFromRoute();
    if (tab !== activeTab) {
      activeTab = tab;
    }
  });

  // Push the selected tab into the URL so the link is shareable.
  function selectTab(tab: Tab): void {
    activeTab = tab;
    routeIAMTab(tab);
  }

  // ---------------------------------------------------------------------------
  // Active tenant selector (used by users/roles/policies tabs)
  // ---------------------------------------------------------------------------

  // Starts on the session's own tenant: an operator administers the tenant
  // they signed in to, and a hardcoded tenant sent a tenant-scoped operator
  // to one they cannot read (#312).
  let selectedTenantID = $state(authStore.tenantID);

  // The listing only ever ADDS options, for principals permitted to enumerate
  // tenants (see lib/tenantOptions.ts). It is fetched silently: a refusal
  // must not raise an error on the Users/Roles/Policies tabs.
  let discoveredTenants = $state<TenantOption[]>([]);

  const tenantOptions = $derived(
    withRequiredTenants(discoveredTenants, [selectedTenantID, authStore.tenantID]),
  );

  $effect(() => {
    // Read synchronously so the listing follows the signed-in principal.
    const forSession = authStore.sessionKey;
    discoveredTenants = [];
    void (async () => {
      try {
        const client = await getSDKClient();
        const resp = await client.iam.listTenants();
        if (forSession !== authStore.sessionKey) return;
        discoveredTenants = tenantOptionsOf(resp.tenants);
      } catch {
        // Not authorized to enumerate tenants (or the call failed); the
        // session's own tenant stays in the list either way.
      }
    })();
  });

  // ---------------------------------------------------------------------------
  // Data
  // ---------------------------------------------------------------------------

  let users: IAMUser[] = $state([]);
  let tenants: IAMTenant[] = $state([]);
  let roles: IAMRole[] = $state([]);
  let policies: IAMPolicy[] = $state([]);

  // One column declaration per tab. Each tab is its own table with its own
  // page and sort in the URL, so switching tabs never carries one tab's page
  // number onto another's rows.
  const USER_COLUMNS: Column<IAMUser>[] = [
    { key: 'username', label: 'Username', sortable: true, searchable: true },
    { key: 'email', label: 'Email', sortable: true, searchable: true },
    { key: 'lifecycle', label: 'Status', sortable: true, value: (u) => u.lifecycle ?? 'unknown' },
    { key: 'roles', label: 'Roles', value: (u) => (u.roles ?? []).join(' ') },
    { key: 'created_at', label: 'Created', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

  const TENANT_COLUMNS: Column<IAMTenant>[] = [
    { key: 'id', label: 'ID', sortable: true, searchable: true },
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'description', label: 'Description', sortable: true, searchable: true },
    { key: 'created_at', label: 'Created', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

  const IAM_ROLE_COLUMNS: Column<IAMRole>[] = [
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'description', label: 'Description', sortable: true, searchable: true },
    { key: 'permissions', label: 'Permissions' },
    { key: 'created_at', label: 'Created', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

  const POLICY_COLUMNS: Column<IAMPolicy>[] = [
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'description', label: 'Description', sortable: true, searchable: true },
    { key: 'created_at', label: 'Created', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];
  let loading = $state(false);
  let error: string | null = $state(null);

  // ---------------------------------------------------------------------------
  // Invite User dialog state
  // ---------------------------------------------------------------------------

  let showCreateUser = $state(false);
  let newUsername = $state('');
  let newEmail = $state('');
  let newDisplayName = $state('');
  let newAuthMethod: 'webauthn' | 'piv' = $state('webauthn');
  let newInviteExpiryHours = $state(24);
  let newSelectedRoles: string[] = $state([]);
  let createUserError: string | null = $state(null);
  let creating = $state(false);

  // ---------------------------------------------------------------------------
  // Invite modal state (one-time-show after user creation)
  // ---------------------------------------------------------------------------

  let inviteToken = $state('');
  let inviteURL = $state('');
  let inviteExpiresAt = $state('');
  let showInviteModal = $state(false);
  let inviteCopied = $state(false);

  // ---------------------------------------------------------------------------
  // Edit User dialog state
  // ---------------------------------------------------------------------------

  let userToEdit: IAMUser | null = $state(null);
  let editEmail = $state('');
  // The checkbox reads "active"; the lifecycle is only sent when it is toggled,
  // so saving an email change never moves a pending or suspended account.
  let editActive = $state(false);
  let editActiveInitial = false;
  let editUserError: string | null = $state(null);
  let editingUser = $state(false);

  // ---------------------------------------------------------------------------
  // Assign Role dialog state
  // ---------------------------------------------------------------------------

  let userForRole: IAMUser | null = $state(null);
  // assignRoleID holds the role UUID selected from the dropdown.
  // The store keys on role ID (UUID), not role name, so this must be an ID.
  let assignRoleID = $state('');
  let assignRoleError: string | null = $state(null);
  let assigningRole = $state(false);
  // The role UUIDs the user holds, as shown in the dialog; a revoke removes
  // its role from this list without mutating the users-table row.
  let userRoleIDs: string[] = $state([]);
  // The role UUID whose revoke is in flight, if any.
  let revokingRoleID: string | null = $state(null);

  // ---------------------------------------------------------------------------
  // Consents dialog state
  // ---------------------------------------------------------------------------

  const consentPurposes: ConsentPurpose[] = [
    'marketing',
    'analytics',
    'personalization',
    'third_party',
    'data_processing',
    'data_retention',
    'functional',
  ];

  let userForConsents: IAMUser | null = $state(null);
  let userConsents: UserConsent[] = $state([]);
  let consentsLoading = $state(false);
  let consentsError: string | null = $state(null);
  let grantPurpose: ConsentPurpose | '' = $state('');
  let grantVersion = $state('');
  let grantingConsent = $state(false);
  // The purpose whose withdraw is in flight, if any.
  let withdrawingPurpose: ConsentPurpose | null = $state(null);

  // ---------------------------------------------------------------------------
  // Data requests (GDPR export / deletion) dialog state
  // ---------------------------------------------------------------------------

  let userForDataRequests: IAMUser | null = $state(null);
  let dataRequestsError: string | null = $state(null);
  let exportIDInput = $state('');
  let exportRequest: DataExportRequest | null = $state(null);
  let exportBusy = $state(false);
  let deletionReason = $state('');
  let deletionIDInput = $state('');
  let deletionRequest: DeletionRequest | null = $state(null);
  let deletionBusy = $state(false);
  // The request-deletion confirmation is open.
  let confirmRequestDeletion = $state(false);
  // The server has no list route: the ids created this session, per user, newest last.
  const createdExportIDs = new Map<string, string[]>();
  const createdDeletionIDs = new Map<string, string[]>();

  // ---------------------------------------------------------------------------
  // Sessions dialog state
  // ---------------------------------------------------------------------------

  let userForSessions: IAMUser | null = $state(null);
  let userSessions: IAMSession[] = $state([]);
  let sessionsLoading = $state(false);
  let sessionsError: string | null = $state(null);
  // The revoke-all confirmation is open.
  let confirmRevokeSessions = $state(false);
  let revokingSessions = $state(false);

  // ---------------------------------------------------------------------------
  // MFA dialog state
  // ---------------------------------------------------------------------------

  let userForMFA: IAMUser | null = $state(null);
  let mfaStatus: MFAStatusResponse | null = $state(null);
  let mfaLoading = $state(false);
  let mfaError: string | null = $state(null);
  // Set while the Disable MFA confirmation is shown.
  let confirmDisableMFA = $state(false);
  let disablingMFA = $state(false);

  // ---------------------------------------------------------------------------
  // Passkeys (WebAuthn credentials) dialog state
  // ---------------------------------------------------------------------------

  let userForPasskeys: IAMUser | null = $state(null);
  let passkeys: WebAuthnCredentialSummary[] | null = $state(null);
  let passkeysLoading = $state(false);
  let passkeysError: string | null = $state(null);
  // Set while the Remove confirmation for one credential is shown.
  let confirmRemovePasskey: WebAuthnCredentialSummary | null = $state(null);
  // Set while the Remove all confirmation is shown.
  let confirmRemoveAllPasskeys = $state(false);
  let removingPasskeys = $state(false);

  // ---------------------------------------------------------------------------
  // Create Tenant dialog state
  // ---------------------------------------------------------------------------

  let showCreateTenant = $state(false);
  let newTenantName = $state('');
  let newTenantDesc = $state('');
  let createTenantError: string | null = $state(null);
  let creatingTenant = $state(false);

  // ---------------------------------------------------------------------------
  // Provision Tenant dialog state
  // ---------------------------------------------------------------------------

  let showProvisionTenant = $state(false);
  let provTenantID = $state('');
  let provTenantName = $state('');
  let provAdminUsername = $state('');
  let provEnrollment: TenantAdminEnrollment = $state('invite');
  let provCsrPem = $state('');
  let provInviteExpiry = $state('');
  let provisionTenantError: string | null = $state(null);
  let provisioningTenant = $state(false);
  // The provisioning result; holds the one-time invite token or certificate
  // until the result dialog closes.
  let provisionResult: ProvisionTenantResponse | null = $state(null);

  // ---------------------------------------------------------------------------
  // Edit Tenant dialog state
  // ---------------------------------------------------------------------------

  let tenantToEdit: IAMTenant | null = $state(null);
  let editTenantName = $state('');
  let editTenantDesc = $state('');
  let editTenantError: string | null = $state(null);
  let editingTenant = $state(false);

  // ---------------------------------------------------------------------------
  // Delete tenant confirm
  // ---------------------------------------------------------------------------

  let tenantToDelete: IAMTenant | null = $state(null);
  let deletingTenant = $state(false);

  // ---------------------------------------------------------------------------
  // Create Role dialog state
  // ---------------------------------------------------------------------------

  let showCreateRole = $state(false);
  let newRoleName = $state('');
  let newRoleDesc = $state('');
  let newRolePerms = $state('');
  let createRoleError: string | null = $state(null);
  let creatingRole = $state(false);

  // ---------------------------------------------------------------------------
  // Edit Role dialog state
  // ---------------------------------------------------------------------------

  let roleToEdit: IAMRole | null = $state(null);
  let editRoleDesc = $state('');
  let editRolePerms = $state('');
  let editRoleError: string | null = $state(null);
  let editingRole = $state(false);

  // ---------------------------------------------------------------------------
  // Delete role confirm
  // ---------------------------------------------------------------------------

  let roleToDelete: IAMRole | null = $state(null);
  let deletingRole = $state(false);

  // ---------------------------------------------------------------------------
  // Reconcile roles state
  // ---------------------------------------------------------------------------

  let reconcilingRoles = $state(false);

  // ---------------------------------------------------------------------------
  // Create Policy dialog state
  // ---------------------------------------------------------------------------

  let showCreatePolicy = $state(false);
  let newPolicyName = $state('');
  let newPolicyDesc = $state('');
  let newPolicyYAML = $state('');
  let createPolicyError: string | null = $state(null);
  let creatingPolicy = $state(false);

  // ---------------------------------------------------------------------------
  // Edit Policy dialog state
  // ---------------------------------------------------------------------------

  let policyToEdit: IAMPolicy | null = $state(null);
  let editPolicyDesc = $state('');
  let editPolicyYAML = $state('');
  let editPolicyError: string | null = $state(null);
  let editingPolicy = $state(false);

  // ---------------------------------------------------------------------------
  // Delete policy confirm
  // ---------------------------------------------------------------------------

  let policyToDelete: IAMPolicy | null = $state(null);
  let deletingPolicy = $state(false);

  // ---------------------------------------------------------------------------
  // Delete user confirmation
  // ---------------------------------------------------------------------------

  let userToDelete: IAMUser | null = $state(null);
  let deleting = $state(false);

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  async function loadUsers(): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listUsers(selectedTenantID);
      const seen = new Set<string>();
      users = resp.users.filter((u) => {
        if (seen.has(u.id)) return false;
        seen.add(u.id);
        return true;
      });
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load users';
    } finally {
      loading = false;
    }
  }

  /** uniqueTenants drops repeated ids from a tenant listing, keeping the first. */
  function uniqueTenants<T extends { id: string }>(list: T[]): T[] {
    const seen = new Set<string>();
    return list.filter((t) => {
      if (seen.has(t.id)) return false;
      seen.add(t.id);
      return true;
    });
  }

  async function loadTenants(): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listTenants();
      tenants = uniqueTenants(resp.tenants);
      // Keep the selector in step with tenants created or deleted here.
      discoveredTenants = tenantOptionsOf(tenants);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load tenants';
    } finally {
      loading = false;
    }
  }

  async function loadRoles(): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listRoles(selectedTenantID);
      const seenR = new Set<string>();
      roles = resp.roles.filter((r) => {
        if (seenR.has(r.id)) return false;
        seenR.add(r.id);
        return true;
      });
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load roles';
    } finally {
      loading = false;
    }
  }

  async function loadPolicies(): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listPolicies(selectedTenantID);
      const seenP = new Set<string>();
      policies = resp.policies.filter((p) => {
        if (seenP.has(p.name)) return false;
        seenP.add(p.name);
        return true;
      });
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load policies';
    } finally {
      loading = false;
    }
  }

  function loadTab(tab: Tab): void {
    if (tab === 'users') {
      void loadUsers();
      // Load roles in parallel so role UUIDs in user.roles can be resolved
      // to human-readable names in the users table without a separate trip.
      void loadRoles();
    } else if (tab === 'tenants') void loadTenants();
    else if (tab === 'roles') void loadRoles();
    else if (tab === 'policies') void loadPolicies();
  }

  $effect(() => {
    void activeTab;
    void selectedTenantID;
    loadTab(activeTab);
  });

  // ---------------------------------------------------------------------------
  // Create user
  // ---------------------------------------------------------------------------

  async function submitCreateUser(): Promise<void> {
    createUserError = null;
    if (!newUsername.trim() || !newEmail.trim()) {
      createUserError = 'Username and email are required';
      return;
    }
    creating = true;
    try {
      const client = await getSDKClient();
      const req: CreateUserRequest = {
        tenant_id: selectedTenantID,
        username: newUsername.trim(),
        email: newEmail.trim(),
        display_name: newDisplayName.trim() || undefined,
        auth_methods: [newAuthMethod],
        roles: newSelectedRoles.length > 0 ? newSelectedRoles : undefined,
        invite_expiry_hours: newInviteExpiryHours,
      };
      const resp = await client.iam.createUser(selectedTenantID, req);
      showCreateUser = false;
      newUsername = '';
      newEmail = '';
      newDisplayName = '';
      newAuthMethod = 'webauthn';
      newInviteExpiryHours = 24;
      newSelectedRoles = [];
      inviteToken = resp.invite_token;
      inviteURL = resp.invite_url;
      inviteExpiresAt = resp.expires_at;
      inviteCopied = false;
      showInviteModal = true;
      void loadUsers();
    } catch (e) {
      createUserError = e instanceof Error ? e.message : 'Failed to create user';
    } finally {
      creating = false;
    }
  }

  function copyInviteURL(): void {
    void navigator.clipboard.writeText(inviteURL).then(() => {
      inviteCopied = true;
      setTimeout(() => { inviteCopied = false; }, 2000);
    });
  }

  // ---------------------------------------------------------------------------
  // Edit user
  // ---------------------------------------------------------------------------

  function openEditUser(user: IAMUser): void {
    userToEdit = user;
    editEmail = user.email;
    editActive = user.lifecycle === 'active';
    editActiveInitial = editActive;
    editUserError = null;
  }

  async function submitEditUser(): Promise<void> {
    if (!userToEdit) return;
    editUserError = null;
    editingUser = true;
    try {
      const client = await getSDKClient();
      const req: UpdateUserRequest = {
        email: editEmail.trim() || undefined,
        lifecycle: editActive === editActiveInitial ? undefined : editActive ? 'active' : 'disabled',
      };
      await client.iam.updateUser(selectedTenantID, userToEdit.id, req);
      showToast({ message: `User "${userToEdit.username}" updated`, type: 'success' });
      userToEdit = null;
      void loadUsers();
    } catch (e) {
      editUserError = e instanceof Error ? e.message : 'Failed to update user';
    } finally {
      editingUser = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Assign role to user
  // ---------------------------------------------------------------------------

  function openAssignRole(user: IAMUser): void {
    userForRole = user;
    userRoleIDs = [...(user.roles ?? [])];
    revokingRoleID = null;
    assignRoleID = '';
    assignRoleError = null;
    // Populate the roles dropdown if it hasn't been loaded yet (the roles
    // data is normally fetched only when the Roles tab is active).
    if (roles.length === 0) {
      void loadRoles();
    }
  }

  async function submitAssignRole(): Promise<void> {
    if (!userForRole) return;
    assignRoleError = null;
    if (!assignRoleID) {
      assignRoleError = 'A role is required';
      return;
    }
    assigningRole = true;
    try {
      const client = await getSDKClient();
      // Pass the role UUID — the handler and store both key on role ID, not name.
      await client.iam.assignRoleToUser(selectedTenantID, userForRole.id, assignRoleID);
      const roleName = roles.find((r) => r.id === assignRoleID)?.name ?? assignRoleID;
      showToast({ message: `Role "${roleName}" assigned to "${userForRole.username}"`, type: 'success' });
      userForRole = null;
      void loadUsers();
    } catch (e) {
      assignRoleError = e instanceof Error ? e.message : 'Failed to assign role';
    } finally {
      assigningRole = false;
    }
  }

  /** The role's name, resolved as the Users table does; the UUID if unknown. */
  function roleNameOf(roleID: string): string {
    return roles.find((r) => r.id === roleID)?.name ?? roleID;
  }

  async function revokeRole(roleID: string): Promise<void> {
    if (!userForRole) return;
    const user = userForRole;
    assignRoleError = null;
    revokingRoleID = roleID;
    try {
      const client = await getSDKClient();
      // The server's {role} path parameter is the role ID (UUID), not its name.
      await client.iam.revokeRoleFromUser(selectedTenantID, user.id, roleID);
      showToast({ message: `Role "${roleNameOf(roleID)}" revoked from "${user.username}"`, type: 'success' });
      userRoleIDs = userRoleIDs.filter((id) => id !== roleID);
      void loadUsers();
    } catch (e) {
      assignRoleError = e instanceof Error ? e.message : 'Failed to revoke role';
    } finally {
      revokingRoleID = null;
    }
  }

  async function loadUserConsents(user: IAMUser): Promise<void> {
    consentsLoading = true;
    try {
      const client = await getSDKClient();
      const list = await client.iam.listUserConsents(selectedTenantID, user.id);
      // Drop a response that lands after the dialog closed or moved to another user.
      if (userForConsents?.id === user.id) {
        userConsents = list ?? [];
      }
    } catch (e) {
      if (userForConsents?.id === user.id) {
        consentsError = e instanceof Error ? e.message : 'Failed to load consents';
      }
    } finally {
      consentsLoading = false;
    }
  }

  function openConsents(user: IAMUser): void {
    userForConsents = user;
    userConsents = [];
    consentsError = null;
    grantPurpose = '';
    grantVersion = '';
    withdrawingPurpose = null;
    void loadUserConsents(user);
  }

  function closeConsents(): void {
    userForConsents = null;
    consentsError = null;
  }

  function rememberID(store: Map<string, string[]>, userID: string, id: string): void {
    const ids = store.get(userID) ?? [];
    if (!ids.includes(id)) ids.push(id);
    store.set(userID, ids);
  }

  function newestID(store: Map<string, string[]>, userID: string): string {
    return store.get(userID)?.at(-1) ?? '';
  }

  function openDataRequests(user: IAMUser): void {
    userForDataRequests = user;
    dataRequestsError = null;
    exportRequest = null;
    deletionRequest = null;
    exportBusy = false;
    deletionBusy = false;
    deletionReason = '';
    confirmRequestDeletion = false;
    exportIDInput = newestID(createdExportIDs, user.id);
    deletionIDInput = newestID(createdDeletionIDs, user.id);
  }

  function closeDataRequests(): void {
    userForDataRequests = null;
    dataRequestsError = null;
    confirmRequestDeletion = false;
  }

  function dataRequestErr(e: unknown, fallback: string): string {
    return e instanceof Error ? e.message : fallback;
  }

  async function submitRequestExport(): Promise<void> {
    if (!userForDataRequests) return;
    const user = userForDataRequests;
    dataRequestsError = null;
    exportBusy = true;
    try {
      const client = await getSDKClient();
      const req = await client.iam.requestDataExport(selectedTenantID, user.id);
      rememberID(createdExportIDs, user.id, req.id);
      if (userForDataRequests?.id === user.id) {
        exportRequest = req;
        exportIDInput = req.id;
      }
      showToast({ message: `Data export requested for "${user.username}"`, type: 'success' });
    } catch (e) {
      if (userForDataRequests?.id === user.id) dataRequestsError = dataRequestErr(e, 'Failed to request export');
    } finally {
      exportBusy = false;
    }
  }

  async function checkExportStatus(): Promise<void> {
    if (!userForDataRequests) return;
    const user = userForDataRequests;
    const id = exportIDInput.trim();
    if (!id) return;
    dataRequestsError = null;
    exportBusy = true;
    try {
      const client = await getSDKClient();
      const req = await client.iam.getExportStatus(selectedTenantID, user.id, id);
      if (userForDataRequests?.id === user.id) exportRequest = req;
    } catch (e) {
      if (userForDataRequests?.id === user.id) dataRequestsError = dataRequestErr(e, 'Failed to check export status');
    } finally {
      exportBusy = false;
    }
  }

  async function downloadDataExport(): Promise<void> {
    if (!userForDataRequests || !exportRequest) return;
    const user = userForDataRequests;
    const id = exportRequest.id;
    dataRequestsError = null;
    exportBusy = true;
    try {
      const client = await getSDKClient();
      const data = await client.iam.downloadExport(selectedTenantID, user.id, id);
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `export-${id}.json`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (e) {
      if (userForDataRequests?.id === user.id) dataRequestsError = dataRequestErr(e, 'Failed to download export');
    } finally {
      exportBusy = false;
    }
  }

  async function submitRequestDeletion(): Promise<void> {
    if (!userForDataRequests) return;
    const user = userForDataRequests;
    dataRequestsError = null;
    deletionBusy = true;
    try {
      const client = await getSDKClient();
      const reason = deletionReason.trim();
      const req = await client.iam.requestDeletion(selectedTenantID, user.id, reason ? { reason } : undefined);
      rememberID(createdDeletionIDs, user.id, req.id);
      if (userForDataRequests?.id === user.id) {
        deletionRequest = req;
        deletionIDInput = req.id;
        deletionReason = '';
      }
      showToast({ message: `Data deletion requested for "${user.username}"`, type: 'success' });
    } catch (e) {
      if (userForDataRequests?.id === user.id) dataRequestsError = dataRequestErr(e, 'Failed to request deletion');
    } finally {
      confirmRequestDeletion = false;
      deletionBusy = false;
    }
  }

  async function checkDeletionStatus(): Promise<void> {
    if (!userForDataRequests) return;
    const user = userForDataRequests;
    const id = deletionIDInput.trim();
    if (!id) return;
    dataRequestsError = null;
    deletionBusy = true;
    try {
      const client = await getSDKClient();
      const req = await client.iam.getDeletionStatus(selectedTenantID, user.id, id);
      if (userForDataRequests?.id === user.id) deletionRequest = req;
    } catch (e) {
      if (userForDataRequests?.id === user.id) dataRequestsError = dataRequestErr(e, 'Failed to check deletion status');
    } finally {
      deletionBusy = false;
    }
  }

  async function submitCancelDeletion(): Promise<void> {
    if (!userForDataRequests || !deletionRequest) return;
    const user = userForDataRequests;
    const id = deletionRequest.id;
    dataRequestsError = null;
    deletionBusy = true;
    try {
      const client = await getSDKClient();
      const req = await client.iam.cancelDeletion(selectedTenantID, user.id, id);
      if (userForDataRequests?.id === user.id) deletionRequest = req;
      showToast({ message: `Data deletion cancelled for "${user.username}"`, type: 'success' });
    } catch (e) {
      if (userForDataRequests?.id === user.id) dataRequestsError = dataRequestErr(e, 'Failed to cancel deletion');
    } finally {
      deletionBusy = false;
    }
  }

  async function loadUserMFA(user: IAMUser): Promise<void> {
    mfaLoading = true;
    try {
      const client = await getSDKClient();
      const status = await client.iam.getMFAStatus(selectedTenantID, user.id);
      // Drop a response that lands after the dialog closed or moved to another user.
      if (userForMFA?.id === user.id) {
        mfaStatus = status;
      }
    } catch (e) {
      if (userForMFA?.id === user.id) {
        mfaError = e instanceof Error ? e.message : 'Failed to load MFA status';
      }
    } finally {
      mfaLoading = false;
    }
  }

  function openMFA(user: IAMUser): void {
    userForMFA = user;
    mfaStatus = null;
    mfaError = null;
    confirmDisableMFA = false;
    disablingMFA = false;
    void loadUserMFA(user);
  }

  function closeMFA(): void {
    userForMFA = null;
    mfaError = null;
    confirmDisableMFA = false;
  }

  async function submitDisableMFA(): Promise<void> {
    if (!userForMFA) return;
    const user = userForMFA;
    mfaError = null;
    disablingMFA = true;
    try {
      const client = await getSDKClient();
      await client.iam.disableMFA(selectedTenantID, user.id);
      showToast({ message: `MFA disabled for "${user.username}"`, type: 'success' });
      confirmDisableMFA = false;
      if (userForMFA?.id === user.id) {
        await loadUserMFA(user);
      }
    } catch (e) {
      confirmDisableMFA = false;
      if (userForMFA?.id === user.id) {
        mfaError = e instanceof Error ? e.message : 'Failed to disable MFA';
      }
    } finally {
      disablingMFA = false;
    }
  }

  async function loadUserPasskeys(user: IAMUser): Promise<void> {
    passkeysLoading = true;
    try {
      const client = await getSDKClient();
      const result = await client.iam.listWebAuthnCredentials(selectedTenantID, user.id);
      // Drop a response that lands after the dialog closed or moved to another user.
      if (userForPasskeys?.id === user.id) {
        passkeys = result.credentials ?? [];
      }
    } catch (e) {
      if (userForPasskeys?.id === user.id) {
        passkeysError = e instanceof Error ? e.message : 'Failed to load passkeys';
      }
    } finally {
      passkeysLoading = false;
    }
  }

  function openPasskeys(user: IAMUser): void {
    userForPasskeys = user;
    passkeys = null;
    passkeysError = null;
    confirmRemovePasskey = null;
    confirmRemoveAllPasskeys = false;
    removingPasskeys = false;
    void loadUserPasskeys(user);
  }

  function closePasskeys(): void {
    userForPasskeys = null;
    passkeysError = null;
    confirmRemovePasskey = null;
    confirmRemoveAllPasskeys = false;
  }

  /** shortCredentialID keeps the head of a long credential id for the table. */
  function shortCredentialID(id: string): string {
    return id.length > 16 ? `${id.slice(0, 12)}…` : id;
  }

  async function submitRemovePasskey(): Promise<void> {
    if (!userForPasskeys || !confirmRemovePasskey) return;
    const user = userForPasskeys;
    const credential = confirmRemovePasskey;
    passkeysError = null;
    removingPasskeys = true;
    try {
      const client = await getSDKClient();
      await client.iam.deleteWebAuthnCredential(selectedTenantID, user.id, credential.id);
      showToast({ message: `Passkey removed from "${user.username}"`, type: 'success' });
      confirmRemovePasskey = null;
      if (userForPasskeys?.id === user.id) {
        await loadUserPasskeys(user);
      }
    } catch (e) {
      confirmRemovePasskey = null;
      if (userForPasskeys?.id === user.id) {
        passkeysError = e instanceof Error ? e.message : 'Failed to remove passkey';
      }
    } finally {
      removingPasskeys = false;
    }
  }

  async function submitRemoveAllPasskeys(): Promise<void> {
    if (!userForPasskeys) return;
    const user = userForPasskeys;
    passkeysError = null;
    removingPasskeys = true;
    try {
      const client = await getSDKClient();
      const result = await client.iam.deleteAllWebAuthnCredentials(selectedTenantID, user.id);
      showToast({ message: `${result.deleted} passkey(s) removed from "${user.username}"`, type: 'success' });
      confirmRemoveAllPasskeys = false;
      if (userForPasskeys?.id === user.id) {
        await loadUserPasskeys(user);
      }
    } catch (e) {
      confirmRemoveAllPasskeys = false;
      if (userForPasskeys?.id === user.id) {
        passkeysError = e instanceof Error ? e.message : 'Failed to remove passkeys';
      }
    } finally {
      removingPasskeys = false;
    }
  }

  async function submitGrantConsent(): Promise<void> {
    if (!userForConsents) return;
    const user = userForConsents;
    consentsError = null;
    const version = grantVersion.trim();
    if (!grantPurpose || !version) {
      consentsError = 'A purpose and a version are required';
      return;
    }
    const purpose = grantPurpose;
    grantingConsent = true;
    try {
      const client = await getSDKClient();
      await client.iam.grantConsent(selectedTenantID, user.id, { purpose, version });
      showToast({ message: `Consent "${purpose}" granted for "${user.username}"`, type: 'success' });
      grantPurpose = '';
      grantVersion = '';
      await loadUserConsents(user);
    } catch (e) {
      consentsError = e instanceof Error ? e.message : 'Failed to grant consent';
    } finally {
      grantingConsent = false;
    }
  }

  async function withdrawUserConsent(purpose: ConsentPurpose): Promise<void> {
    if (!userForConsents) return;
    const user = userForConsents;
    consentsError = null;
    withdrawingPurpose = purpose;
    try {
      const client = await getSDKClient();
      await client.iam.withdrawConsent(selectedTenantID, user.id, purpose);
      showToast({ message: `Consent "${purpose}" withdrawn for "${user.username}"`, type: 'success' });
      await loadUserConsents(user);
    } catch (e) {
      consentsError = e instanceof Error ? e.message : 'Failed to withdraw consent';
    } finally {
      withdrawingPurpose = null;
    }
  }

  async function loadUserSessions(user: IAMUser): Promise<void> {
    sessionsLoading = true;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listUserSessions(selectedTenantID, user.id);
      // Drop a response that lands after the dialog closed or moved to another user.
      if (userForSessions?.id === user.id) {
        userSessions = resp?.sessions ?? [];
      }
    } catch (e) {
      if (userForSessions?.id === user.id) {
        sessionsError = e instanceof Error ? e.message : 'Failed to load sessions';
      }
    } finally {
      sessionsLoading = false;
    }
  }

  function openSessions(user: IAMUser): void {
    userForSessions = user;
    userSessions = [];
    sessionsError = null;
    confirmRevokeSessions = false;
    revokingSessions = false;
    void loadUserSessions(user);
  }

  function closeSessions(): void {
    userForSessions = null;
    sessionsError = null;
    confirmRevokeSessions = false;
  }

  async function confirmRevokeAllSessions(): Promise<void> {
    if (!userForSessions) return;
    const user = userForSessions;
    sessionsError = null;
    revokingSessions = true;
    try {
      const client = await getSDKClient();
      await client.iam.revokeAllUserSessions(selectedTenantID, user.id);
      showToast({ message: `All sessions of "${user.username}" revoked`, type: 'success' });
      confirmRevokeSessions = false;
      await loadUserSessions(user);
    } catch (e) {
      confirmRevokeSessions = false;
      sessionsError = e instanceof Error ? e.message : 'Failed to revoke sessions';
    } finally {
      revokingSessions = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Create tenant
  // ---------------------------------------------------------------------------

  async function submitCreateTenant(): Promise<void> {
    createTenantError = null;
    if (!newTenantName.trim()) {
      createTenantError = 'Tenant name is required';
      return;
    }
    creatingTenant = true;
    try {
      const client = await getSDKClient();
      await client.iam.createTenant({ name: newTenantName.trim(), description: newTenantDesc.trim() });
      showToast({ message: `Tenant "${newTenantName}" created`, type: 'success' });
      showCreateTenant = false;
      newTenantName = '';
      newTenantDesc = '';
      void loadTenants();
    } catch (e) {
      createTenantError = e instanceof Error ? e.message : 'Failed to create tenant';
    } finally {
      creatingTenant = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Provision tenant (tenant + first admin by invite or CSR)
  // ---------------------------------------------------------------------------

  function openProvisionTenant(): void {
    provTenantID = '';
    provTenantName = '';
    provAdminUsername = '';
    provEnrollment = 'invite';
    provCsrPem = '';
    provInviteExpiry = '';
    provisionTenantError = null;
    showProvisionTenant = true;
  }

  function closeProvisionTenant(): void {
    showProvisionTenant = false;
    provisionTenantError = null;
    provCsrPem = '';
  }

  async function submitProvisionTenant(): Promise<void> {
    provisionTenantError = null;
    const tenantID = provTenantID.trim();
    if (!tenantID) {
      provisionTenantError = 'Tenant ID is required';
      return;
    }
    const req: ProvisionTenantRequest = { tenant_id: tenantID, admin_enrollment: provEnrollment };
    const name = provTenantName.trim();
    if (name) req.name = name;
    const adminUsername = provAdminUsername.trim();
    if (adminUsername) req.admin_username = adminUsername;
    if (provEnrollment === 'cert') {
      const csr = provCsrPem.trim();
      if (!csr) {
        provisionTenantError = 'Admin CSR PEM is required for certificate enrollment';
        return;
      }
      req.admin_csr_pem = csr;
    } else {
      const expiry = String(provInviteExpiry).trim();
      if (expiry) {
        if (!/^\d+$/.test(expiry) || Number(expiry) <= 0) {
          provisionTenantError = 'Invite expiry must be a positive whole number of hours';
          return;
        }
        req.invite_expiry_hours = Number(expiry);
      }
    }
    provisioningTenant = true;
    try {
      const client = await getSDKClient();
      provisionResult = await client.iam.provisionTenant(req);
      showToast({ message: `Tenant "${tenantID}" provisioned`, type: 'success' });
      closeProvisionTenant();
    } catch (e) {
      provisionTenantError = e instanceof Error ? e.message : 'Failed to provision tenant';
    } finally {
      provisioningTenant = false;
    }
  }

  function closeProvisionResult(): void {
    provisionResult = null;
    void loadTenants();
  }

  async function copyProvisionSecret(value: string | undefined, what: string): Promise<void> {
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      showToast({ message: `${what} copied`, type: 'success' });
    } catch {
      showToast({ message: `Could not copy the ${what.toLowerCase()}`, type: 'error' });
    }
  }

  // ---------------------------------------------------------------------------
  // Edit tenant
  // ---------------------------------------------------------------------------

  function openEditTenant(t: IAMTenant): void {
    tenantToEdit = t;
    editTenantName = t.name;
    editTenantDesc = t.description ?? '';
    editTenantError = null;
  }

  async function submitEditTenant(): Promise<void> {
    if (!tenantToEdit) return;
    editTenantError = null;
    editingTenant = true;
    try {
      const client = await getSDKClient();
      const req: UpdateTenantRequest = {
        name: editTenantName.trim() || undefined,
        description: editTenantDesc.trim() || undefined,
      };
      await client.iam.updateTenant(tenantToEdit.id, req);
      showToast({ message: `Tenant "${tenantToEdit.name}" updated`, type: 'success' });
      tenantToEdit = null;
      void loadTenants();
    } catch (e) {
      editTenantError = e instanceof Error ? e.message : 'Failed to update tenant';
    } finally {
      editingTenant = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete tenant
  // ---------------------------------------------------------------------------

  async function confirmDeleteTenant(): Promise<void> {
    if (!tenantToDelete) return;
    deletingTenant = true;
    try {
      const client = await getSDKClient();
      await client.iam.deleteTenant(tenantToDelete.id);
      showToast({ message: `Tenant "${tenantToDelete.name}" deleted`, type: 'success' });
      tenantToDelete = null;
      void loadTenants();
    } catch (e) {
      showToast({ message: e instanceof Error ? e.message : 'Delete failed', type: 'error' });
    } finally {
      deletingTenant = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Create role
  // ---------------------------------------------------------------------------

  async function submitCreateRole(): Promise<void> {
    createRoleError = null;
    if (!newRoleName.trim()) {
      createRoleError = 'Role name is required';
      return;
    }
    creatingRole = true;
    try {
      const client = await getSDKClient();
      const req: CreateRoleRequest = {
        name: newRoleName.trim(),
        description: newRoleDesc.trim() || undefined,
        permissions: newRolePerms.trim()
          ? newRolePerms.split(',').map((p) => p.trim()).filter(Boolean)
          : [],
      };
      await client.iam.createRole(selectedTenantID, req);
      showToast({ message: `Role "${newRoleName}" created`, type: 'success' });
      showCreateRole = false;
      newRoleName = '';
      newRoleDesc = '';
      newRolePerms = '';
      void loadRoles();
    } catch (e) {
      createRoleError = e instanceof Error ? e.message : 'Failed to create role';
    } finally {
      creatingRole = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Edit role
  // ---------------------------------------------------------------------------

  function openEditRole(role: IAMRole): void {
    roleToEdit = role;
    editRoleDesc = role.description ?? '';
    editRolePerms = (role.permissions ?? []).join(', ');
    editRoleError = null;
  }

  async function submitEditRole(): Promise<void> {
    if (!roleToEdit) return;
    editRoleError = null;
    editingRole = true;
    try {
      const client = await getSDKClient();
      const req: UpdateRoleRequest = {
        description: editRoleDesc.trim() || undefined,
        permissions: editRolePerms.trim()
          ? editRolePerms.split(',').map((p) => p.trim()).filter(Boolean)
          : [],
      };
      await client.iam.updateRole(selectedTenantID, roleToEdit.id, req);
      showToast({ message: `Role "${roleToEdit.name}" updated`, type: 'success' });
      roleToEdit = null;
      void loadRoles();
    } catch (e) {
      editRoleError = e instanceof Error ? e.message : 'Failed to update role';
    } finally {
      editingRole = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete role
  // ---------------------------------------------------------------------------

  async function confirmDeleteRole(): Promise<void> {
    if (!roleToDelete) return;
    deletingRole = true;
    try {
      const client = await getSDKClient();
      await client.iam.deleteRole(selectedTenantID, roleToDelete.id);
      showToast({ message: `Role "${roleToDelete.name}" deleted`, type: 'success' });
      roleToDelete = null;
      void loadRoles();
    } catch (e) {
      showToast({ message: e instanceof Error ? e.message : 'Delete failed', type: 'error' });
    } finally {
      deletingRole = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Reconcile roles
  //
  // Built-in roles are seeded once when a tenant is created, so a permission
  // rename in a later server build leaves every existing tenant carrying the
  // old verbs. Reconcile converges this tenant's built-in roles onto the
  // definitions the running build carries; custom roles are left alone.
  // ---------------------------------------------------------------------------

  async function submitReconcileRoles(): Promise<void> {
    reconcilingRoles = true;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.reconcileRoles(selectedTenantID);
      const changedCount = resp.roles?.length ?? 0;
      showToast({
        message: resp.changed
          ? `Reconciled ${changedCount} role${changedCount === 1 ? '' : 's'} in "${selectedTenantID}"`
          : `Roles in "${selectedTenantID}" are already up to date`,
        type: 'success',
      });
      void loadRoles();
    } catch (e) {
      showToast({ message: e instanceof Error ? e.message : 'Reconcile failed', type: 'error' });
    } finally {
      reconcilingRoles = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Create policy
  // ---------------------------------------------------------------------------

  async function submitCreatePolicy(): Promise<void> {
    createPolicyError = null;
    if (!newPolicyName.trim()) {
      createPolicyError = 'Policy name is required';
      return;
    }
    creatingPolicy = true;
    try {
      const client = await getSDKClient();
      const req: CreatePolicyRequest = {
        name: newPolicyName.trim(),
        description: newPolicyDesc.trim() || undefined,
        spec_yaml: newPolicyYAML.trim() || undefined,
      };
      await client.iam.createPolicy(selectedTenantID, req);
      showToast({ message: `Policy "${newPolicyName}" created`, type: 'success' });
      showCreatePolicy = false;
      newPolicyName = '';
      newPolicyDesc = '';
      newPolicyYAML = '';
      void loadPolicies();
    } catch (e) {
      createPolicyError = e instanceof Error ? e.message : 'Failed to create policy';
    } finally {
      creatingPolicy = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Edit policy
  // ---------------------------------------------------------------------------

  function openEditPolicy(policy: IAMPolicy): void {
    policyToEdit = policy;
    editPolicyDesc = policy.description ?? '';
    editPolicyYAML = policy.spec_yaml ?? '';
    editPolicyError = null;
  }

  async function submitEditPolicy(): Promise<void> {
    if (!policyToEdit) return;
    editPolicyError = null;
    editingPolicy = true;
    try {
      const client = await getSDKClient();
      const req: UpdatePolicyRequest = {
        description: editPolicyDesc.trim() || undefined,
        spec_yaml: editPolicyYAML.trim() || undefined,
      };
      await client.iam.updatePolicy(selectedTenantID, policyToEdit.name, req);
      showToast({ message: `Policy "${policyToEdit.name}" updated`, type: 'success' });
      policyToEdit = null;
      void loadPolicies();
    } catch (e) {
      editPolicyError = e instanceof Error ? e.message : 'Failed to update policy';
    } finally {
      editingPolicy = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete policy
  // ---------------------------------------------------------------------------

  async function confirmDeletePolicy(): Promise<void> {
    if (!policyToDelete) return;
    deletingPolicy = true;
    try {
      const client = await getSDKClient();
      await client.iam.deletePolicy(selectedTenantID, policyToDelete.name);
      showToast({ message: `Policy "${policyToDelete.name}" deleted`, type: 'success' });
      policyToDelete = null;
      void loadPolicies();
    } catch (e) {
      showToast({ message: e instanceof Error ? e.message : 'Delete failed', type: 'error' });
    } finally {
      deletingPolicy = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete user
  // ---------------------------------------------------------------------------

  async function confirmDeleteUser(): Promise<void> {
    if (!userToDelete) return;
    deleting = true;
    try {
      const client = await getSDKClient();
      await client.iam.deleteUser(selectedTenantID, userToDelete.id);
      showToast({ message: `User "${userToDelete.username}" deleted`, type: 'success' });
      userToDelete = null;
      void loadUsers();
    } catch (e) {
      showToast({ message: e instanceof Error ? e.message : 'Delete failed', type: 'error' });
    } finally {
      deleting = false;
    }
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="IAM" subtitle="Identity, Access, and Policy Management" icon={ShieldCheck}>
    {#if activeTab === 'users'}
      <button
        class="btn btn-primary btn-sm"
        onclick={() => { showCreateUser = true; }}
        aria-label="Invite user"
      >
        + Invite User
      </button>
    {:else if activeTab === 'tenants'}
      <button
        class="btn btn-primary btn-sm"
        onclick={() => { showCreateTenant = true; }}
        aria-label="Create tenant"
      >
        + Create Tenant
      </button>
      <button
        class="btn btn-ghost btn-sm"
        onclick={openProvisionTenant}
        aria-label="Provision tenant"
      >
        + Provision Tenant
      </button>
    {:else if activeTab === 'roles'}
      <button
        class="btn btn-ghost btn-sm"
        onclick={() => { void submitReconcileRoles(); }}
        disabled={reconcilingRoles}
        aria-label="Reconcile roles"
        title="Converge this tenant's built-in roles onto the role definitions this server build carries — adds the verbs it now grants, removes the ones it no longer defines. Custom roles are untouched."
      >
        {#if reconcilingRoles}
          <span class="loading loading-spinner loading-xs"></span>
        {:else}
          <RefreshCw class="h-3 w-3" />
        {/if}
        Reconcile Roles
      </button>
      <button
        class="btn btn-primary btn-sm"
        onclick={() => { showCreateRole = true; }}
        aria-label="Create role"
      >
        + Create Role
      </button>
    {:else if activeTab === 'policies'}
      <button
        class="btn btn-primary btn-sm"
        onclick={() => { showCreatePolicy = true; }}
        aria-label="Create policy"
      >
        + Create Policy
      </button>
    {/if}
  </PageHeader>

  <!-- Tenant selector (used by users / roles / policies tabs) -->
  {#if activeTab !== 'tenants'}
    <div class="flex items-center gap-2">
      <label class="text-sm text-base-content/60" for="iam-tenant-select">Tenant</label>
      <select
        id="iam-tenant-select"
        class="select select-bordered select-sm font-mono"
        bind:value={selectedTenantID}
      >
        {#each tenantOptions as t (t.id)}
          <option value={t.id}>{t.name}</option>
        {/each}
      </select>
    </div>
  {/if}

  <!-- Tab bar -->
  <div role="tablist" class="tabs tabs-border">
    {#each (['users', 'tenants', 'roles', 'policies'] as const) as tab}
      <button
        role="tab"
        class="tab"
        class:tab-active={activeTab === tab}
        onclick={() => { selectTab(tab); }}
      >
        {tab.charAt(0).toUpperCase() + tab.slice(1)}
      </button>
    {/each}
  </div>

  <!-- Error -->
  {#if error}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{error}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { loadTab(activeTab); }}>Retry</button>
    </div>
  {/if}

  <!-- Loading -->
  {#if loading}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md"></span>
    </div>
  {:else}

    <!-- Users tab -->
    {#if activeTab === 'users'}
      <DataTable
        columns={USER_COLUMNS}
        rows={users}
        rowKey={(user: IAMUser) => user.id}
        urlKey="users"
        ariaLabel="Users"
        searchPlaceholder="Search users..."
        searchLabel="Search users"
        noMatchText="No users match the search."
      >
        {#snippet empty()}
          <EmptyState
            icon={UserPlus}
            title="No users in tenant {selectedTenantID} — + Invite User is how you start."
            description="Users authenticate to the platform and can be assigned roles and policies."
            actionLabel="+ Invite User"
            onaction={() => { showCreateUser = true; }}
          />
        {/snippet}

        {#snippet row(user: IAMUser)}
          <tr class="hover">
            <td class="font-mono text-xs">{user.username}</td>
            <td class="text-sm">{user.email}</td>
            <td>
              <span
                class="badge badge-xs badge-soft"
                class:badge-success={user.lifecycle === 'active'}
                class:badge-info={user.lifecycle === 'pending'}
                class:badge-warning={user.lifecycle !== 'active' && user.lifecycle !== 'pending'}
              >
                {user.lifecycle ?? 'unknown'}
              </span>
            </td>
            <td>
              <div class="flex flex-wrap gap-1">
                {#each (user.roles ?? []) as role}
                  <span class="badge badge-xs badge-ghost">{roles.find((r) => r.id === role)?.name ?? role}</span>
                {/each}
              </div>
            </td>
            <td class="text-xs text-base-content/50">{formatDateTime(user.created_at)}</td>
            <td>
              <div class="flex gap-1">
                <button
                  class="btn btn-ghost btn-xs"
                  title={`Edit user ${user.username}`}
                  aria-label={`Edit user ${user.username}`}
                  onclick={() => openEditUser(user)}
                >
                  <Pencil class="h-3 w-3" />
                </button>
                <button
                  class="btn btn-ghost btn-xs"
                  title={`Assign role to ${user.username}`}
                  aria-label={`Assign role to ${user.username}`}
                  onclick={() => openAssignRole(user)}
                >
                  <UserCheck class="h-3 w-3" />
                </button>
                <button
                  class="btn btn-ghost btn-xs"
                  title={`Consents of ${user.username}`}
                  aria-label={`Consents of ${user.username}`}
                  onclick={() => openConsents(user)}
                >
                  <FileCheck class="h-3 w-3" />
                </button>
                <button
                  class="btn btn-ghost btn-xs"
                  title={`Data requests for ${user.username}`}
                  aria-label={`Data requests for ${user.username}`}
                  onclick={() => openDataRequests(user)}
                >
                  <Database class="h-3 w-3" />
                </button>
                <button
                  class="btn btn-ghost btn-xs"
                  title="Sessions"
                  aria-label={`Sessions of ${user.username}`}
                  onclick={() => openSessions(user)}
                >
                  <MonitorSmartphone class="h-3 w-3" />
                </button>
                <button
                  class="btn btn-ghost btn-xs"
                  title="MFA"
                  aria-label={`MFA of ${user.username}`}
                  onclick={() => openMFA(user)}
                >
                  <KeyRound class="h-3 w-3" />
                </button>
                <button
                  class="btn btn-ghost btn-xs"
                  title="Passkeys"
                  aria-label={`Passkeys of ${user.username}`}
                  onclick={() => openPasskeys(user)}
                >
                  <Fingerprint class="h-3 w-3" />
                </button>
                <button
                  class="btn btn-ghost btn-xs text-error"
                  title={`Delete user ${user.username}`}
                  aria-label={`Delete user ${user.username}`}
                  onclick={() => { userToDelete = user; }}
                >
                  <Trash2 class="h-3 w-3" />
                </button>
              </div>
            </td>
          </tr>
        {/snippet}
      </DataTable>

    <!-- Tenants tab -->
    {:else if activeTab === 'tenants'}
      <DataTable
        columns={TENANT_COLUMNS}
        rows={tenants}
        rowKey={(tenant: IAMTenant) => tenant.id}
        urlKey="tenants"
        ariaLabel="Tenants"
        searchPlaceholder="Search tenants..."
        searchLabel="Search tenants"
        noMatchText="No tenants match the search."
      >
        {#snippet empty()}
          <div class="text-center py-10 text-base-content/30 text-sm">No tenants found</div>
        {/snippet}

        {#snippet row(tenant: IAMTenant)}
          <tr class="hover">
            <td class="font-mono text-xs">{tenant.id}</td>
            <td class="font-medium text-sm">{tenant.name}</td>
            <td class="text-sm text-base-content/60">{tenant.description || '—'}</td>
            <td class="text-xs text-base-content/50">{formatDateTime(tenant.created_at)}</td>
            <td>
              <div class="flex gap-1">
                <button
                  class="btn btn-ghost btn-xs"
                  title={`Edit tenant ${tenant.name}`}
                  aria-label={`Edit tenant ${tenant.name}`}
                  onclick={() => openEditTenant(tenant)}
                >
                  <Pencil class="h-3 w-3" />
                </button>
                <button
                  class="btn btn-ghost btn-xs text-error"
                  title={`Delete tenant ${tenant.name}`}
                  aria-label={`Delete tenant ${tenant.name}`}
                  onclick={() => { tenantToDelete = tenant; }}
                >
                  <Trash2 class="h-3 w-3" />
                </button>
              </div>
            </td>
          </tr>
        {/snippet}
      </DataTable>

    <!-- Roles tab -->
    {:else if activeTab === 'roles'}
      <DataTable
        columns={IAM_ROLE_COLUMNS}
        rows={roles}
        rowKey={(role: IAMRole) => role.id}
        urlKey="iamroles"
        ariaLabel="Roles"
        searchPlaceholder="Search roles..."
        searchLabel="Search roles"
        noMatchText="No roles match the search."
      >
        {#snippet empty()}
          <div class="text-center py-10 text-base-content/30 text-sm">No roles defined in tenant "{selectedTenantID}"</div>
        {/snippet}

        {#snippet row(role: IAMRole)}
          <tr class="hover">
            <td class="font-mono text-xs font-medium">{role.name}</td>
            <td class="text-sm text-base-content/60">{role.description || '—'}</td>
            <td>
              <div class="flex flex-wrap gap-1">
                {#each (role.permissions ?? []).slice(0, 4) as perm}
                  <span class="badge badge-xs badge-ghost">{perm}</span>
                {/each}
                {#if (role.permissions ?? []).length > 4}
                  <span class="badge badge-xs badge-ghost">+{(role.permissions ?? []).length - 4}</span>
                {/if}
                <!-- Drift: permissions this role still carries that the running
                     server build no longer defines. Reconcile Roles repairs it. -->
                {#each role.retired_permissions ?? [] as retired}
                  <span
                    class="badge badge-xs badge-warning badge-outline"
                    title={`Retired: this server build no longer defines "${retired}". Run Reconcile Roles to converge this role onto the current definitions.`}
                  >{retired}</span>
                {/each}
              </div>
            </td>
            <td class="text-xs text-base-content/50">{formatDateTime(role.created_at)}</td>
            <td>
              <div class="flex gap-1">
                <button
                  class="btn btn-ghost btn-xs"
                  title={`Edit role ${role.name}`}
                  aria-label={`Edit role ${role.name}`}
                  onclick={() => openEditRole(role)}
                >
                  <Pencil class="h-3 w-3" />
                </button>
                <button
                  class="btn btn-ghost btn-xs text-error"
                  title={`Delete role ${role.name}`}
                  aria-label={`Delete role ${role.name}`}
                  onclick={() => { roleToDelete = role; }}
                >
                  <Trash2 class="h-3 w-3" />
                </button>
              </div>
            </td>
          </tr>
        {/snippet}
      </DataTable>

    <!-- Policies tab -->
    {:else if activeTab === 'policies'}
      <DataTable
        columns={POLICY_COLUMNS}
        rows={policies}
        rowKey={(policy: IAMPolicy) => policy.name}
        urlKey="policies"
        ariaLabel="Policies"
        searchPlaceholder="Search policies..."
        searchLabel="Search policies"
        noMatchText="No policies match the search."
      >
        {#snippet empty()}
          <div class="text-center py-10 text-base-content/30 text-sm">No policies defined in tenant "{selectedTenantID}"</div>
        {/snippet}

        {#snippet row(policy: IAMPolicy)}
          <tr class="hover">
            <td class="font-mono text-xs font-medium">{policy.name}</td>
            <td class="text-sm text-base-content/60">{policy.description || '—'}</td>
            <td class="text-xs text-base-content/50">{formatDateTime(policy.created_at)}</td>
            <td>
              <div class="flex gap-1">
                <button
                  class="btn btn-ghost btn-xs"
                  title={`Edit policy ${policy.name}`}
                  aria-label={`Edit policy ${policy.name}`}
                  onclick={() => openEditPolicy(policy)}
                >
                  <Pencil class="h-3 w-3" />
                </button>
                <button
                  class="btn btn-ghost btn-xs text-error"
                  title={`Delete policy ${policy.name}`}
                  aria-label={`Delete policy ${policy.name}`}
                  onclick={() => { policyToDelete = policy; }}
                >
                  <Trash2 class="h-3 w-3" />
                </button>
              </div>
            </td>
          </tr>
        {/snippet}
      </DataTable>
    {/if}

  {/if}
</div>

<!-- =========================================================================
     CREATE USER DIALOG
     ========================================================================= -->
{#if showCreateUser}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Invite user dialog">
    <div class="modal-box max-w-md">
      <h3 class="text-base font-semibold mb-4">Invite User</h3>
      {#if createUserError}
        <div class="alert alert-error text-sm mb-3" role="alert">{createUserError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="create-username">Username</label>
          <input id="create-username" class="input input-bordered input-sm" type="text" placeholder="alice" bind:value={newUsername} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="create-email">Email</label>
          <input id="create-email" class="input input-bordered input-sm" type="email" placeholder="alice@example.com" bind:value={newEmail} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="create-display-name">Display Name <span class="text-xs opacity-60">(optional)</span></label>
          <input id="create-display-name" class="input input-bordered input-sm" type="text" placeholder="Alice Smith" bind:value={newDisplayName} />
        </div>
        <div class="form-control">
          <span class="label label-text mb-1">Authentication Method</span>
          <div class="flex gap-3">
            <label class="flex items-center gap-2 cursor-pointer border rounded-lg px-3 py-2 flex-1 {newAuthMethod === 'webauthn' ? 'border-primary bg-primary/10' : 'border-base-300'}">
              <input type="radio" class="radio radio-primary radio-sm" name="auth-method" value="webauthn" bind:group={newAuthMethod} />
              <span class="text-sm font-medium">FIDO2 / WebAuthn</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer border rounded-lg px-3 py-2 flex-1 {newAuthMethod === 'piv' ? 'border-primary bg-primary/10' : 'border-base-300'}">
              <input type="radio" class="radio radio-primary radio-sm" name="auth-method" value="piv" bind:group={newAuthMethod} />
              <span class="text-sm font-medium">PIV Certificate</span>
            </label>
          </div>
        </div>
        <div class="form-control">
          <label class="label label-text" for="invite-expiry">Invite Expires In</label>
          <select id="invite-expiry" class="select select-bordered select-sm" bind:value={newInviteExpiryHours}>
            <option value={1}>1 hour</option>
            <option value={8}>8 hours</option>
            <option value={24}>24 hours</option>
            <option value={72}>3 days</option>
            <option value={168}>7 days</option>
          </select>
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showCreateUser = false; createUserError = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={creating} onclick={submitCreateUser}>{creating ? 'Sending Invite…' : 'Send Invite'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showCreateUser = false; }} onkeydown={() => { showCreateUser = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     INVITE URL MODAL (one-time display)
     ========================================================================= -->
{#if showInviteModal}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Invite sent dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-2">Invite Sent</h3>
      <p class="text-sm opacity-70 mb-4">
        Share this invite link with the user. It expires on {new Date(inviteExpiresAt).toLocaleString()} and can only be used once.
        <strong class="block mt-1 text-warning">This URL will not be shown again.</strong>
      </p>
      <div class="bg-base-200 rounded-lg p-3 font-mono text-xs break-all mb-4 select-all">{inviteURL}</div>
      <div class="flex gap-2">
        <button class="btn btn-primary btn-sm flex-1" onclick={copyInviteURL}>
          {inviteCopied ? 'Copied!' : 'Copy Invite URL'}
        </button>
        <button class="btn btn-ghost btn-sm" onclick={() => { showInviteModal = false; }}>Close</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showInviteModal = false; }} onkeydown={() => { showInviteModal = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     EDIT USER DIALOG
     ========================================================================= -->
{#if userToEdit}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Edit user dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-4">Edit User <span class="font-mono text-primary">{userToEdit.username}</span></h3>
      {#if editUserError}
        <div class="alert alert-error text-sm mb-3" role="alert">{editUserError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="edit-email">Email</label>
          <input id="edit-email" class="input input-bordered input-sm" type="email" bind:value={editEmail} />
        </div>
        <label class="flex items-center gap-3 cursor-pointer">
          <input type="checkbox" class="checkbox checkbox-sm" bind:checked={editActive} />
          <span class="label-text">Account active</span>
        </label>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { userToEdit = null; editUserError = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={editingUser} onclick={submitEditUser}>{editingUser ? 'Saving…' : 'Save'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { userToEdit = null; }} onkeydown={() => { userToEdit = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     ASSIGN ROLE DIALOG
     ========================================================================= -->
{#if userForRole}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Assign role dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-4">Roles of <span class="font-mono text-primary">{userForRole.username}</span></h3>
      {#if assignRoleError}
        <div class="alert alert-error text-sm mb-3" role="alert">{assignRoleError}</div>
      {/if}
      {#if userRoleIDs.length === 0}
        <p class="text-sm text-base-content/50 mb-3">No roles assigned</p>
      {:else}
        <ul class="mb-3 flex flex-col gap-1" aria-label="Current roles">
          {#each userRoleIDs as roleID (roleID)}
            <li class="flex items-center justify-between gap-2">
              <span class="badge badge-sm badge-ghost">{roleNameOf(roleID)}</span>
              <button
                class="btn btn-ghost btn-xs text-error"
                aria-label={`Revoke role ${roleNameOf(roleID)} from ${userForRole.username}`}
                disabled={revokingRoleID === roleID}
                onclick={() => revokeRole(roleID)}
              >{revokingRoleID === roleID ? 'Revoking…' : 'Revoke'}</button>
            </li>
          {/each}
        </ul>
      {/if}
      <div class="form-control">
        <label class="label label-text" for="assign-role-select">Role</label>
        {#if loading || (roles.length === 0 && !error)}
          <div class="flex items-center gap-2 py-2">
            <span class="loading loading-spinner loading-xs"></span>
            <span class="text-sm text-base-content/50">Loading roles…</span>
          </div>
        {:else if roles.length === 0}
          <p class="text-sm text-base-content/50 py-2">No roles found in tenant <span class="font-mono">{selectedTenantID}</span>. Create a role first.</p>
        {:else}
          <select id="assign-role-select" class="select select-bordered select-sm" bind:value={assignRoleID}>
            <option value="">— select a role —</option>
            {#each roles as r (r.id)}
              <option value={r.id}>{r.name}</option>
            {/each}
          </select>
        {/if}
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { userForRole = null; assignRoleError = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={assigningRole || !assignRoleID} onclick={submitAssignRole}>{assigningRole ? 'Assigning…' : 'Assign'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { userForRole = null; }} onkeydown={() => { userForRole = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     CONSENTS DIALOG
     ========================================================================= -->
{#if userForConsents}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Consents dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Consents of <span class="font-mono text-primary">{userForConsents.username}</span></h3>
      {#if consentsError}
        <div class="alert alert-error text-sm mb-3" role="alert">{consentsError}</div>
      {/if}
      {#if consentsLoading && userConsents.length === 0}
        <div class="flex items-center gap-2 py-2 mb-3">
          <span class="loading loading-spinner loading-xs"></span>
          <span class="text-sm text-base-content/50">Loading consents…</span>
        </div>
      {:else if userConsents.length === 0}
        <p class="text-sm text-base-content/50 mb-3">No consents recorded</p>
      {:else}
        <table class="table table-xs mb-3" aria-label="User consents">
          <thead>
            <tr><th>Purpose</th><th>Status</th><th>Version</th><th>Granted / withdrawn</th><th><span class="sr-only">Actions</span></th></tr>
          </thead>
          <tbody>
            {#each userConsents as c (c.purpose)}
              <tr>
                <td class="font-mono">{c.purpose}</td>
                <td><span class="badge badge-xs {c.status === 'granted' ? 'badge-success' : 'badge-ghost'}">{c.status}</span></td>
                <td class="font-mono">{c.version}</td>
                <td class="text-xs text-base-content/50">{c.status === 'withdrawn' && c.withdrawn_at ? formatDateTime(c.withdrawn_at) : c.granted_at ? formatDateTime(c.granted_at) : '—'}</td>
                <td>
                  {#if c.status === 'granted'}
                    <button
                      class="btn btn-ghost btn-xs text-error"
                      aria-label={`Withdraw consent ${c.purpose} of ${userForConsents.username}`}
                      disabled={withdrawingPurpose === c.purpose}
                      onclick={() => withdrawUserConsent(c.purpose)}
                    >{withdrawingPurpose === c.purpose ? 'Withdrawing…' : 'Withdraw'}</button>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
      <div class="grid grid-cols-2 gap-2">
        <div class="form-control">
          <label class="label label-text" for="grant-consent-purpose">Purpose</label>
          <select id="grant-consent-purpose" class="select select-bordered select-sm" bind:value={grantPurpose}>
            <option value="">— select a purpose —</option>
            {#each consentPurposes as p (p)}
              <option value={p}>{p}</option>
            {/each}
          </select>
        </div>
        <div class="form-control">
          <label class="label label-text" for="grant-consent-version">Version</label>
          <input id="grant-consent-version" class="input input-bordered input-sm" type="text" placeholder="e.g. 2026-01" bind:value={grantVersion} />
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeConsents}>Close</button>
        <button class="btn btn-primary btn-sm" disabled={grantingConsent || !grantPurpose || !grantVersion.trim()} onclick={submitGrantConsent}>{grantingConsent ? 'Granting…' : 'Grant'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeConsents} onkeydown={closeConsents}></div>
  </div>
{/if}

<!-- =========================================================================
     DATA REQUESTS DIALOG (GDPR export / deletion)
     ========================================================================= -->
{#if userForDataRequests}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Data requests dialog">
    <div class="modal-box max-w-xl">
      <h3 class="text-base font-semibold mb-4">Data requests of <span class="font-mono text-primary">{userForDataRequests.username}</span></h3>
      {#if dataRequestsError}
        <div class="alert alert-error text-sm mb-3" role="alert">{dataRequestsError}</div>
      {/if}

      <section aria-label="Data export" class="mb-4">
        <h4 class="text-sm font-semibold mb-2">Export</h4>
        <div class="flex items-end gap-2 mb-2">
          <button class="btn btn-primary btn-sm" disabled={exportBusy} onclick={submitRequestExport}>Request export</button>
          <div class="form-control flex-1">
            <label class="label label-text" for="data-export-id">Export ID</label>
            <input id="data-export-id" class="input input-bordered input-sm font-mono" type="text" bind:value={exportIDInput} />
          </div>
          <button class="btn btn-ghost btn-sm" aria-label="Check export status" disabled={exportBusy || !exportIDInput.trim()} onclick={checkExportStatus}>Check status</button>
        </div>
        {#if exportRequest}
          <table class="table table-xs mb-2" aria-label="Data export request">
            <tbody>
              <tr><th>ID</th><td class="font-mono">{exportRequest.id}</td></tr>
              <tr><th>Status</th><td>{exportRequest.status}</td></tr>
              <tr><th>Requested</th><td>{exportRequest.requested_at ? formatDateTime(exportRequest.requested_at) : '—'}</td></tr>
              <tr><th>Completed</th><td>{exportRequest.completed_at ? formatDateTime(exportRequest.completed_at) : '—'}</td></tr>
              <tr><th>Expires</th><td>{exportRequest.expires_at ? formatDateTime(exportRequest.expires_at) : '—'}</td></tr>
              {#if exportRequest.error_message}
                <tr><th>Error</th><td class="text-error">{exportRequest.error_message}</td></tr>
              {/if}
            </tbody>
          </table>
          {#if exportRequest.status === 'completed'}
            <button class="btn btn-ghost btn-sm" disabled={exportBusy} onclick={downloadDataExport}>Download</button>
          {/if}
        {/if}
      </section>

      <section aria-label="Data deletion">
        <h4 class="text-sm font-semibold mb-2">Deletion</h4>
        <div class="flex items-end gap-2 mb-2">
          <div class="form-control flex-1">
            <label class="label label-text" for="data-deletion-reason">Reason</label>
            <input id="data-deletion-reason" class="input input-bordered input-sm" type="text" bind:value={deletionReason} />
          </div>
          <button class="btn btn-error btn-sm" disabled={deletionBusy} onclick={() => { confirmRequestDeletion = true; }}>Request deletion</button>
        </div>
        <div class="flex items-end gap-2 mb-2">
          <div class="form-control flex-1">
            <label class="label label-text" for="data-deletion-id">Deletion ID</label>
            <input id="data-deletion-id" class="input input-bordered input-sm font-mono" type="text" bind:value={deletionIDInput} />
          </div>
          <button class="btn btn-ghost btn-sm" aria-label="Check deletion status" disabled={deletionBusy || !deletionIDInput.trim()} onclick={checkDeletionStatus}>Check status</button>
        </div>
        {#if deletionRequest}
          <table class="table table-xs mb-2" aria-label="Data deletion request">
            <tbody>
              <tr><th>ID</th><td class="font-mono">{deletionRequest.id}</td></tr>
              <tr><th>Status</th><td>{deletionRequest.status}</td></tr>
              <tr><th>Reason</th><td>{deletionRequest.reason || '—'}</td></tr>
              <tr><th>Requested</th><td>{deletionRequest.requested_at ? formatDateTime(deletionRequest.requested_at) : '—'}</td></tr>
              <tr><th>Grace period ends</th><td>{deletionRequest.grace_period_ends_at ? formatDateTime(deletionRequest.grace_period_ends_at) : '—'}</td></tr>
              <tr><th>Cancelled</th><td>{deletionRequest.cancelled_at ? formatDateTime(deletionRequest.cancelled_at) : '—'}</td></tr>
              {#if deletionRequest.error_message}
                <tr><th>Error</th><td class="text-error">{deletionRequest.error_message}</td></tr>
              {/if}
            </tbody>
          </table>
          {#if deletionRequest.status === 'pending'}
            <button class="btn btn-ghost btn-sm text-error" disabled={deletionBusy} onclick={submitCancelDeletion}>Cancel deletion</button>
          {/if}
        {/if}
      </section>

      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeDataRequests}>Close</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeDataRequests} onkeydown={closeDataRequests}></div>
  </div>
  {#if confirmRequestDeletion}
    <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Request deletion confirmation">
      <div class="modal-box max-w-sm">
        <h3 class="text-base font-semibold mb-2">Request Data Deletion?</h3>
        <p class="text-sm text-base-content/70">
          The data of <span class="font-mono font-semibold">{userForDataRequests.username}</span> will be deleted after the grace period, unless the request is cancelled before it ends.
        </p>
        <div class="modal-action">
          <button class="btn btn-ghost btn-sm" onclick={() => { confirmRequestDeletion = false; }}>Cancel</button>
          <button class="btn btn-error btn-sm" disabled={deletionBusy} onclick={submitRequestDeletion}>{deletionBusy ? 'Requesting…' : 'Request'}</button>
        </div>
      </div>
      <div class="modal-backdrop" role="none" onclick={() => { confirmRequestDeletion = false; }} onkeydown={() => { confirmRequestDeletion = false; }}></div>
    </div>
  {/if}
{/if}

<!-- =========================================================================
     SESSIONS DIALOG
     ========================================================================= -->
{#if userForSessions}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Sessions dialog">
    <div class="modal-box max-w-3xl">
      <h3 class="text-base font-semibold mb-4">Sessions of <span class="font-mono text-primary">{userForSessions.username}</span></h3>
      {#if sessionsError}
        <div class="alert alert-error text-sm mb-3" role="alert">{sessionsError}</div>
      {/if}
      {#if sessionsLoading && userSessions.length === 0}
        <div class="flex items-center gap-2 py-2 mb-3">
          <span class="loading loading-spinner loading-xs"></span>
          <span class="text-sm text-base-content/50">Loading sessions…</span>
        </div>
      {:else if userSessions.length === 0}
        <p class="text-sm text-base-content/50 mb-3">No active sessions</p>
      {:else}
        <div class="overflow-x-auto mb-3">
          <table class="table table-xs" aria-label="User sessions">
            <thead>
              <tr><th>Created</th><th>Last active</th><th>Expires</th><th>IP address</th><th>User agent</th><th>MFA</th><th>Device</th></tr>
            </thead>
            <tbody>
              {#each userSessions as s (s.id)}
                <tr>
                  <td class="text-xs">{formatDateTime(s.created)}</td>
                  <td class="text-xs">{formatDateTime(s.last_active)}</td>
                  <td class="text-xs">{formatDateTime(s.expires_at)}</td>
                  <td class="font-mono text-xs">{s.ip_address || '—'}</td>
                  <td class="text-xs max-w-xs truncate" title={s.user_agent}>{s.user_agent || '—'}</td>
                  <td><span class="badge badge-xs {s.mfa_verified ? 'badge-success' : 'badge-ghost'}">{s.mfa_verified ? 'verified' : 'no'}</span></td>
                  <td class="text-xs">{s.device_info?.name || '—'}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeSessions}>Close</button>
        <button class="btn btn-error btn-sm" disabled={revokingSessions || userSessions.length === 0} onclick={() => { confirmRevokeSessions = true; }}>Revoke all sessions</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeSessions} onkeydown={closeSessions}></div>
  </div>
  {#if confirmRevokeSessions}
    <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Revoke sessions confirmation">
      <div class="modal-box max-w-sm">
        <h3 class="text-base font-semibold mb-2">Revoke All Sessions?</h3>
        <p class="text-sm text-base-content/70">
          Sign <span class="font-mono font-semibold">{userForSessions.username}</span> out of every session? They must sign in again.
        </p>
        <div class="modal-action">
          <button class="btn btn-ghost btn-sm" onclick={() => { confirmRevokeSessions = false; }}>Cancel</button>
          <button class="btn btn-error btn-sm" disabled={revokingSessions} onclick={confirmRevokeAllSessions}>{revokingSessions ? 'Revoking…' : 'Revoke'}</button>
        </div>
      </div>
      <div class="modal-backdrop" role="none" onclick={() => { confirmRevokeSessions = false; }} onkeydown={() => { confirmRevokeSessions = false; }}></div>
    </div>
  {/if}
{/if}

<!-- =========================================================================
     MFA DIALOG
     ========================================================================= -->
{#if userForMFA}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="MFA dialog">
    <div class="modal-box max-w-md">
      <h3 class="text-base font-semibold mb-4">MFA of <span class="font-mono text-primary">{userForMFA.username}</span></h3>
      {#if mfaError}
        <div class="alert alert-error text-sm mb-3" role="alert">{mfaError}</div>
      {/if}
      {#if mfaLoading && !mfaStatus}
        <div class="flex items-center gap-2 py-2 mb-3">
          <span class="loading loading-spinner loading-xs"></span>
          <span class="text-sm text-base-content/50">Loading MFA status…</span>
        </div>
      {:else if mfaStatus}
        <table class="table table-xs mb-3" aria-label="MFA status">
          <tbody>
            <tr>
              <th>Status</th>
              <td><span class="badge badge-xs {mfaStatus.enabled ? 'badge-success' : 'badge-ghost'}">{mfaStatus.enabled ? 'Enabled' : 'Disabled'}</span></td>
            </tr>
            {#if mfaStatus.method}
              <tr><th>Method</th><td class="font-mono">{mfaStatus.method}</td></tr>
            {/if}
            {#if mfaStatus.enrolled_at}
              <tr><th>Enrolled</th><td class="text-xs">{formatDateTime(mfaStatus.enrolled_at)}</td></tr>
            {/if}
            {#if mfaStatus.last_verified}
              <tr><th>Last verified</th><td class="text-xs">{formatDateTime(mfaStatus.last_verified)}</td></tr>
            {/if}
            {#if mfaStatus.backup_codes_remaining !== undefined || mfaStatus.backup_codes_total !== undefined}
              <tr>
                <th>Backup codes</th>
                <td class="font-mono">{mfaStatus.backup_codes_remaining ?? '—'}{mfaStatus.backup_codes_total !== undefined ? ` of ${mfaStatus.backup_codes_total}` : ''} remaining</td>
              </tr>
            {/if}
          </tbody>
        </table>
      {/if}
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeMFA}>Close</button>
        {#if mfaStatus?.enabled}
          <button class="btn btn-error btn-sm" disabled={disablingMFA} onclick={() => { confirmDisableMFA = true; }}>Disable MFA</button>
        {/if}
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeMFA} onkeydown={closeMFA}></div>
  </div>
{/if}

{#if userForMFA && confirmDisableMFA}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Disable MFA confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Disable MFA?</h3>
      <p class="text-sm text-base-content/70">
        Disable MFA for <span class="font-mono font-semibold">{userForMFA.username}</span>? The user must enroll again to restore it.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { confirmDisableMFA = false; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={disablingMFA} onclick={submitDisableMFA}>{disablingMFA ? 'Disabling…' : 'Disable'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { confirmDisableMFA = false; }} onkeydown={() => { confirmDisableMFA = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     PASSKEYS DIALOG
     ========================================================================= -->
{#if userForPasskeys}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Passkeys dialog">
    <div class="modal-box max-w-3xl">
      <h3 class="text-base font-semibold mb-4">Passkeys of <span class="font-mono text-primary">{userForPasskeys.username}</span></h3>
      {#if passkeysError}
        <div class="alert alert-error text-sm mb-3" role="alert">{passkeysError}</div>
      {/if}
      {#if passkeysLoading && !passkeys}
        <div class="flex items-center gap-2 py-2 mb-3">
          <span class="loading loading-spinner loading-xs"></span>
          <span class="text-sm text-base-content/50">Loading passkeys…</span>
        </div>
      {:else if passkeys && passkeys.length === 0}
        <p class="text-sm text-base-content/50 mb-3">No passkeys registered.</p>
      {:else if passkeys}
        <div class="overflow-x-auto mb-3">
          <table class="table table-xs" aria-label="Passkeys">
            <thead>
              <tr>
                <th>ID</th>
                <th>AAGUID</th>
                <th>Attestation</th>
                <th>Transports</th>
                <th>Sign count</th>
                <th>Backup eligible</th>
                <th>Backup state</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {#each passkeys as credential (credential.id)}
                <tr>
                  <td class="font-mono" title={credential.id}>
                    {shortCredentialID(credential.id)}
                    {#if credential.clone_warning}
                      <span class="badge badge-xs badge-warning ml-1" title="The authenticator's sign count went backwards: it may be cloned">Clone warning</span>
                    {/if}
                  </td>
                  <td class="font-mono">{credential.aaguid || '—'}</td>
                  <td>{credential.attestation_type || '—'}</td>
                  <td>{credential.transports && credential.transports.length > 0 ? credential.transports.join(', ') : '—'}</td>
                  <td class="font-mono">{credential.sign_count}</td>
                  <td>{credential.backup_eligible ? 'Yes' : 'No'}</td>
                  <td>{credential.backup_state ? 'Yes' : 'No'}</td>
                  <td>
                    <button
                      class="btn btn-ghost btn-xs text-error"
                      aria-label={`Remove passkey ${credential.id}`}
                      disabled={removingPasskeys}
                      onclick={() => { confirmRemovePasskey = credential; }}
                    >Remove</button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closePasskeys}>Close</button>
        {#if passkeys && passkeys.length > 0}
          <button class="btn btn-error btn-sm" disabled={removingPasskeys} onclick={() => { confirmRemoveAllPasskeys = true; }}>Remove all</button>
        {/if}
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closePasskeys} onkeydown={closePasskeys}></div>
  </div>
{/if}

{#if userForPasskeys && confirmRemovePasskey}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Remove passkey confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Remove passkey?</h3>
      <p class="text-sm text-base-content/70">
        Remove passkey <span class="font-mono font-semibold" title={confirmRemovePasskey.id}>{shortCredentialID(confirmRemovePasskey.id)}</span> from <span class="font-mono font-semibold">{userForPasskeys.username}</span>? The user can no longer sign in with it.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { confirmRemovePasskey = null; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={removingPasskeys} onclick={submitRemovePasskey}>{removingPasskeys ? 'Removing…' : 'Remove'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { confirmRemovePasskey = null; }} onkeydown={() => { confirmRemovePasskey = null; }}></div>
  </div>
{/if}

{#if userForPasskeys && confirmRemoveAllPasskeys}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Remove all passkeys confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Remove all passkeys?</h3>
      <p class="text-sm text-base-content/70">
        Remove every passkey of <span class="font-mono font-semibold">{userForPasskeys.username}</span>? The user must register again to sign in with a passkey.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { confirmRemoveAllPasskeys = false; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={removingPasskeys} onclick={submitRemoveAllPasskeys}>{removingPasskeys ? 'Removing…' : 'Remove all'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { confirmRemoveAllPasskeys = false; }} onkeydown={() => { confirmRemoveAllPasskeys = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     CREATE TENANT DIALOG
     ========================================================================= -->
{#if showCreateTenant}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Create tenant dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-4">Create Tenant</h3>
      {#if createTenantError}
        <div class="alert alert-error text-sm mb-3" role="alert">{createTenantError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="create-tenant-name">Name</label>
          <input id="create-tenant-name" class="input input-bordered input-sm" type="text" placeholder="acme-corp" bind:value={newTenantName} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="create-tenant-desc">Description</label>
          <input id="create-tenant-desc" class="input input-bordered input-sm" type="text" placeholder="Optional description" bind:value={newTenantDesc} />
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showCreateTenant = false; createTenantError = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={creatingTenant} onclick={submitCreateTenant}>{creatingTenant ? 'Creating…' : 'Create'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showCreateTenant = false; }} onkeydown={() => { showCreateTenant = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     PROVISION TENANT DIALOG
     ========================================================================= -->
{#if showProvisionTenant}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Provision tenant dialog">
    <div class="modal-box max-w-md">
      <h3 class="text-base font-semibold mb-4">Provision Tenant</h3>
      {#if provisionTenantError}
        <div class="alert alert-error text-sm mb-3" role="alert">{provisionTenantError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="prov-tenant-id">Tenant ID</label>
          <input id="prov-tenant-id" class="input input-bordered input-sm" type="text" placeholder="acme" bind:value={provTenantID} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="prov-tenant-name">Name</label>
          <input id="prov-tenant-name" class="input input-bordered input-sm" type="text" placeholder="Optional display name" bind:value={provTenantName} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="prov-admin-username">Admin username</label>
          <input id="prov-admin-username" class="input input-bordered input-sm" type="text" placeholder="Optional; the server picks a default" bind:value={provAdminUsername} />
        </div>
        <fieldset class="form-control">
          <legend class="label label-text">Admin enrollment</legend>
          <label class="label cursor-pointer justify-start gap-2">
            <input type="radio" class="radio radio-sm" name="prov-enrollment" value="invite" bind:group={provEnrollment} />
            <span class="label-text">Invite</span>
          </label>
          <label class="label cursor-pointer justify-start gap-2">
            <input type="radio" class="radio radio-sm" name="prov-enrollment" value="cert" bind:group={provEnrollment} />
            <span class="label-text">Certificate (CSR)</span>
          </label>
        </fieldset>
        {#if provEnrollment === 'cert'}
          <div class="form-control">
            <label class="label label-text" for="prov-csr-pem">Admin CSR PEM</label>
            <textarea id="prov-csr-pem" class="textarea textarea-bordered textarea-sm font-mono text-xs" rows="6" placeholder="-----BEGIN CERTIFICATE REQUEST-----" bind:value={provCsrPem}></textarea>
          </div>
        {:else}
          <div class="form-control">
            <label class="label label-text" for="prov-invite-expiry">Invite expiry (hours)</label>
            <input id="prov-invite-expiry" class="input input-bordered input-sm" type="text" inputmode="numeric" placeholder="Optional; server default" bind:value={provInviteExpiry} />
          </div>
        {/if}
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeProvisionTenant}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={provisioningTenant} onclick={submitProvisionTenant}>{provisioningTenant ? 'Provisioning…' : 'Provision'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeProvisionTenant} onkeydown={closeProvisionTenant}></div>
  </div>
{/if}

<!-- =========================================================================
     PROVISION TENANT RESULT DIALOG
     ========================================================================= -->
{#if provisionResult}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Tenant provisioned dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Tenant Provisioned</h3>
      <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
        <dt class="font-medium">Tenant ID</dt><dd class="font-mono">{provisionResult.tenant_id}</dd>
        <dt class="font-medium">Admin username</dt><dd class="font-mono">{provisionResult.admin_username}</dd>
        <dt class="font-medium">Admin user ID</dt><dd class="font-mono">{provisionResult.admin_user_id}</dd>
        <dt class="font-medium">Admin role ID</dt><dd class="font-mono">{provisionResult.admin_role_id}</dd>
        <dt class="font-medium">Pending</dt><dd>{provisionResult.admin_pending ? 'Yes' : 'No'}</dd>
      </dl>
      {#if provisionResult.invite_token}
        <div class="mt-4 space-y-2">
          <div class="alert alert-warning text-sm">This invite token is shown once. Copy it now and send it to the admin.</div>
          <label class="label label-text" for="prov-invite-token">Invite token</label>
          <textarea id="prov-invite-token" class="textarea textarea-bordered textarea-sm font-mono text-xs w-full" rows="3" readonly value={provisionResult.invite_token}></textarea>
          <button class="btn btn-ghost btn-xs" aria-label="Copy invite token" onclick={() => copyProvisionSecret(provisionResult?.invite_token, 'Invite token')}>Copy</button>
          {#if provisionResult.invite_expires_at}
            <p class="text-xs">Expires {formatDateTime(provisionResult.invite_expires_at)}</p>
          {/if}
        </div>
      {/if}
      {#if provisionResult.admin_cert_pem}
        <div class="mt-4 space-y-2">
          <label class="label label-text" for="prov-admin-cert">Admin certificate</label>
          <textarea id="prov-admin-cert" class="textarea textarea-bordered textarea-sm font-mono text-xs w-full" rows="6" readonly value={provisionResult.admin_cert_pem}></textarea>
          <button class="btn btn-ghost btn-xs" aria-label="Copy certificate" onclick={() => copyProvisionSecret(provisionResult?.admin_cert_pem, 'Certificate')}>Copy</button>
          {#if provisionResult.admin_cert_thumbprint}
            <p class="text-xs">Thumbprint <span class="font-mono">{provisionResult.admin_cert_thumbprint}</span></p>
          {/if}
        </div>
      {/if}
      <div class="modal-action">
        <button class="btn btn-primary btn-sm" onclick={closeProvisionResult}>Done</button>
      </div>
    </div>
  </div>
{/if}

<!-- =========================================================================
     EDIT TENANT DIALOG
     ========================================================================= -->
{#if tenantToEdit}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Edit tenant dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-4">Edit Tenant <span class="font-mono text-primary">{tenantToEdit.name}</span></h3>
      {#if editTenantError}
        <div class="alert alert-error text-sm mb-3" role="alert">{editTenantError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="edit-tenant-name">Name</label>
          <input id="edit-tenant-name" class="input input-bordered input-sm" type="text" bind:value={editTenantName} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="edit-tenant-desc">Description</label>
          <input id="edit-tenant-desc" class="input input-bordered input-sm" type="text" bind:value={editTenantDesc} />
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { tenantToEdit = null; editTenantError = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={editingTenant} onclick={submitEditTenant}>{editingTenant ? 'Saving…' : 'Save'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { tenantToEdit = null; }} onkeydown={() => { tenantToEdit = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     DELETE TENANT CONFIRM DIALOG
     ========================================================================= -->
{#if tenantToDelete}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete tenant confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Tenant?</h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-mono font-semibold">{tenantToDelete.name}</span>? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { tenantToDelete = null; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={deletingTenant} onclick={confirmDeleteTenant}>{deletingTenant ? 'Deleting…' : 'Delete'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { tenantToDelete = null; }} onkeydown={() => { tenantToDelete = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     CREATE ROLE DIALOG
     ========================================================================= -->
{#if showCreateRole}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Create role dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-4">Create Role</h3>
      {#if createRoleError}
        <div class="alert alert-error text-sm mb-3" role="alert">{createRoleError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="create-role-name">Name</label>
          <input id="create-role-name" class="input input-bordered input-sm font-mono" type="text" placeholder="editor" bind:value={newRoleName} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="create-role-desc">Description</label>
          <input id="create-role-desc" class="input input-bordered input-sm" type="text" placeholder="Optional" bind:value={newRoleDesc} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="create-role-perms">Permissions (comma-separated)</label>
          <input id="create-role-perms" class="input input-bordered input-sm font-mono text-xs" type="text" placeholder="read:*, write:objects" bind:value={newRolePerms} />
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showCreateRole = false; createRoleError = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={creatingRole} onclick={submitCreateRole}>{creatingRole ? 'Creating…' : 'Create'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showCreateRole = false; }} onkeydown={() => { showCreateRole = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     EDIT ROLE DIALOG
     ========================================================================= -->
{#if roleToEdit}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Edit role dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-4">Edit Role <span class="font-mono text-primary">{roleToEdit.name}</span></h3>
      {#if editRoleError}
        <div class="alert alert-error text-sm mb-3" role="alert">{editRoleError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="edit-role-desc">Description</label>
          <input id="edit-role-desc" class="input input-bordered input-sm" type="text" bind:value={editRoleDesc} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="edit-role-perms">Permissions (comma-separated)</label>
          <input id="edit-role-perms" class="input input-bordered input-sm font-mono text-xs" type="text" bind:value={editRolePerms} />
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { roleToEdit = null; editRoleError = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={editingRole} onclick={submitEditRole}>{editingRole ? 'Saving…' : 'Save'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { roleToEdit = null; }} onkeydown={() => { roleToEdit = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     DELETE ROLE CONFIRM DIALOG
     ========================================================================= -->
{#if roleToDelete}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete role confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Role?</h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-mono font-semibold">{roleToDelete.name}</span>? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { roleToDelete = null; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={deletingRole} onclick={confirmDeleteRole}>{deletingRole ? 'Deleting…' : 'Delete'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { roleToDelete = null; }} onkeydown={() => { roleToDelete = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     CREATE POLICY DIALOG
     ========================================================================= -->
{#if showCreatePolicy}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Create policy dialog">
    <div class="modal-box max-w-md">
      <h3 class="text-base font-semibold mb-4">Create Policy</h3>
      {#if createPolicyError}
        <div class="alert alert-error text-sm mb-3" role="alert">{createPolicyError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="create-policy-name">Name</label>
          <input id="create-policy-name" class="input input-bordered input-sm font-mono" type="text" placeholder="allow-read-objects" bind:value={newPolicyName} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="create-policy-desc">Description</label>
          <input id="create-policy-desc" class="input input-bordered input-sm" type="text" placeholder="Optional" bind:value={newPolicyDesc} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="create-policy-yaml">Policy YAML</label>
          <textarea id="create-policy-yaml" class="textarea textarea-bordered font-mono text-xs" rows="6" placeholder="version: 1&#10;rules:&#10;  - ..." bind:value={newPolicyYAML}></textarea>
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showCreatePolicy = false; createPolicyError = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={creatingPolicy} onclick={submitCreatePolicy}>{creatingPolicy ? 'Creating…' : 'Create'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showCreatePolicy = false; }} onkeydown={() => { showCreatePolicy = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     EDIT POLICY DIALOG
     ========================================================================= -->
{#if policyToEdit}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Edit policy dialog">
    <div class="modal-box max-w-md">
      <h3 class="text-base font-semibold mb-4">Edit Policy <span class="font-mono text-primary">{policyToEdit.name}</span></h3>
      {#if editPolicyError}
        <div class="alert alert-error text-sm mb-3" role="alert">{editPolicyError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="edit-policy-desc">Description</label>
          <input id="edit-policy-desc" class="input input-bordered input-sm" type="text" bind:value={editPolicyDesc} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="edit-policy-yaml">Policy YAML</label>
          <textarea id="edit-policy-yaml" class="textarea textarea-bordered font-mono text-xs" rows="8" bind:value={editPolicyYAML}></textarea>
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { policyToEdit = null; editPolicyError = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={editingPolicy} onclick={submitEditPolicy}>{editingPolicy ? 'Saving…' : 'Save'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { policyToEdit = null; }} onkeydown={() => { policyToEdit = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     DELETE POLICY CONFIRM DIALOG
     ========================================================================= -->
{#if policyToDelete}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete policy confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Policy?</h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-mono font-semibold">{policyToDelete.name}</span>? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { policyToDelete = null; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={deletingPolicy} onclick={confirmDeletePolicy}>{deletingPolicy ? 'Deleting…' : 'Delete'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { policyToDelete = null; }} onkeydown={() => { policyToDelete = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     DELETE USER CONFIRM DIALOG
     ========================================================================= -->
{#if userToDelete}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete user confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete User?</h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-mono font-semibold">{userToDelete.username}</span>? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { userToDelete = null; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={deleting} onclick={confirmDeleteUser}>{deleting ? 'Deleting…' : 'Delete'}</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { userToDelete = null; }} onkeydown={() => { userToDelete = null; }}></div>
  </div>
{/if}
