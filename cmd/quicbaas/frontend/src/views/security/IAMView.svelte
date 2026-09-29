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
    Users     — user table for a tenant: create / edit / disable / delete / assign role.
    Tenants   — tenant list: create / edit / delete.
    Roles     — role list per tenant: create / edit / delete.
    Policies  — ABAC policy list per tenant: create / edit / delete.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
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
  } from '@quicbaas/sdk';
  import { ShieldCheck, UserPlus, Pencil, Trash2, UserCheck, ToggleLeft, RefreshCw } from 'lucide-svelte';
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

  // 'system' is qrdb's canonical control-plane tenant; the legacy 'default'
  // tenant was retired by qrdb's tenant-isolation refactor, so defaulting to it
  // made the Users/Roles/Policies tabs query a non-existent tenant (404).
  let selectedTenantID = $state('system');

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

  // ---------------------------------------------------------------------------
  // Create Tenant dialog state
  // ---------------------------------------------------------------------------

  let showCreateTenant = $state(false);
  let newTenantName = $state('');
  let newTenantDesc = $state('');
  let createTenantError: string | null = $state(null);
  let creatingTenant = $state(false);

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

  async function loadTenants(): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listTenants();
      const seenT = new Set<string>();
      tenants = resp.tenants.filter((t) => {
        if (seenT.has(t.id)) return false;
        seenT.add(t.id);
        return true;
      });
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
        <!-- 'system' is the qrdb canonical control-plane tenant; the retired
             'default' tenant was removed by qrdb's tenant-isolation refactor. -->
        {#if !tenants.some((t) => t.id === 'system')}
          <option value="system">system</option>
        {/if}
        {#each tenants as t (t.id)}
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
      <h3 class="text-base font-semibold mb-4">Assign Role to <span class="font-mono text-primary">{userForRole.username}</span></h3>
      {#if assignRoleError}
        <div class="alert alert-error text-sm mb-3" role="alert">{assignRoleError}</div>
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
