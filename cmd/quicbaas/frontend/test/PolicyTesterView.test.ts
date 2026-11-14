/**
 * Copyright (c) 2026 Jeremy Hahn
 * Copyright (c) 2026 Automate The Things, LLC
 *
 * This file is part of go-quicbaas.
 *
 * go-quicbaas is dual-licensed:
 *
 * 1. GNU Affero General Public License v3.0 (AGPL-3.0)
 * 2. Commercial License
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { IAMPolicyDocument } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockPolicy: IAMPolicyDocument = {
  apiVersion: 'iam.quicbaas.io/v1',
  kind: 'Policy',
  metadata: { name: 'readers', tenant: 'acme' },
  spec: { subjects: { roles: ['reader'] }, rules: [{ name: 'read', effect: 'allow' }] },
};

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const {
  mockListTenants,
  mockGetPolicy,
  mockValidatePolicy,
  mockTestPolicy,
  mockSimulatePolicy,
  mockGetEffectivePermissions,
  mockGetSDKClient,
} = vi.hoisted(() => ({
  mockListTenants: vi.fn(),
  mockGetPolicy: vi.fn(),
  mockValidatePolicy: vi.fn(),
  mockTestPolicy: vi.fn(),
  mockSimulatePolicy: vi.fn(),
  mockGetEffectivePermissions: vi.fn(),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import PolicyTesterView from '../src/views/security/PolicyTesterView.svelte';

async function renderReady(): Promise<void> {
  render(PolicyTesterView);
  await waitFor(() => {
    expect(screen.getByLabelText('Subject tenant ID')).toHaveValue('acme');
  });
}

async function fillCheck(): Promise<void> {
  await fireEvent.input(screen.getByLabelText('Subject ID'), { target: { value: 'user-1' } });
  await fireEvent.input(screen.getByLabelText('Roles'), { target: { value: 'reader, auditor' } });
  await fireEvent.input(screen.getByLabelText('Action'), { target: { value: 'read' } });
  await fireEvent.input(screen.getByLabelText('Resource'), { target: { value: 'kv:orders/*' } });
}

const expectedSubject = {
  id: 'user-1',
  tenant_id: 'acme',
  roles: ['reader', 'auditor'],
  authenticated: true,
  mfa_verified: false,
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('PolicyTesterView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListTenants.mockResolvedValue({ tenants: [{ id: 'acme' }, { id: 'globex' }] });
    mockGetPolicy.mockResolvedValue(mockPolicy);
    mockGetSDKClient.mockResolvedValue({
      iam: {
        listTenants: mockListTenants,
        getPolicy: mockGetPolicy,
        validatePolicy: mockValidatePolicy,
        testPolicy: mockTestPolicy,
        simulatePolicy: mockSimulatePolicy,
        getEffectivePermissions: mockGetEffectivePermissions,
      },
    });
  });

  it('checks the built subject with testPolicy and shows the decision', async () => {
    mockTestPolicy.mockResolvedValue({
      allowed: true,
      matched_policy: 'readers',
      matched_rule: 'read',
      reason: 'rule matched',
    });
    await renderReady();
    await fillCheck();
    await fireEvent.click(screen.getByRole('button', { name: 'Check' }));

    await waitFor(() => {
      expect(screen.getByLabelText('Check result')).toHaveTextContent('Allowed');
    });
    expect(mockTestPolicy).toHaveBeenCalledWith('acme', {
      subject: expectedSubject,
      action: 'read',
      resource: 'kv:orders/*',
    });
    const result = screen.getByLabelText('Check result');
    expect(result).toHaveTextContent('Matched policy: readers');
    expect(result).toHaveTextContent('Matched rule: read');
    expect(result).toHaveTextContent('Reason: rule matched');
  });

  it('loads a policy into the document textarea', async () => {
    await renderReady();
    await fireEvent.input(screen.getByLabelText('Policy name'), { target: { value: 'readers' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Load' }));

    await waitFor(() => {
      expect(screen.getByLabelText('Policy document')).toHaveValue(JSON.stringify(mockPolicy, null, 2));
    });
    expect(mockGetPolicy).toHaveBeenCalledWith('acme', 'readers');
  });

  it('simulates the parsed document and shows the trace steps', async () => {
    mockSimulatePolicy.mockResolvedValue({
      allowed: false,
      reason: 'no rule matched',
      evaluation_steps: [
        { step_number: 1, policy_name: 'readers', rule_name: 'read', action: 'read', result: false, details: 'resource mismatch' },
      ],
    });
    await renderReady();
    await fillCheck();
    await fireEvent.input(screen.getByLabelText('Policy document'), { target: { value: JSON.stringify(mockPolicy) } });
    await fireEvent.click(screen.getByRole('button', { name: 'Simulate' }));

    await waitFor(() => {
      expect(screen.getByLabelText('Simulation result')).toHaveTextContent('Denied');
    });
    expect(mockSimulatePolicy).toHaveBeenCalledWith('acme', {
      policy: mockPolicy,
      check_request: { subject: expectedSubject, action: 'read', resource: 'kv:orders/*' },
      include_trace: true,
    });
    const steps = screen.getByRole('table', { name: 'Evaluation steps' });
    expect(steps).toHaveTextContent('resource mismatch');
    expect(steps).toHaveTextContent('no match');
  });

  it('validates the document and lists the errors', async () => {
    mockValidatePolicy.mockResolvedValue({ valid: false, errors: ['spec.rules[0]: unknown effect'] });
    await renderReady();
    await fireEvent.input(screen.getByLabelText('Policy document'), { target: { value: JSON.stringify(mockPolicy) } });
    await fireEvent.click(screen.getByRole('button', { name: 'Validate' }));

    await waitFor(() => {
      expect(screen.getByLabelText('Validation result')).toHaveTextContent('spec.rules[0]: unknown effect');
    });
    expect(screen.getByLabelText('Validation result')).toHaveTextContent('Invalid');
    expect(mockValidatePolicy).toHaveBeenCalledWith(
      'acme',
      { name: 'readers', description: undefined, spec_yaml: JSON.stringify(mockPolicy) },
      'readers',
    );
  });

  it('shows invalid JSON inline without a call', async () => {
    await renderReady();
    await fillCheck();
    await fireEvent.input(screen.getByLabelText('Policy document'), { target: { value: '{not json' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Simulate' }));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Invalid JSON');
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Validate' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Invalid JSON');
    expect(mockSimulatePolicy).not.toHaveBeenCalled();
    expect(mockValidatePolicy).not.toHaveBeenCalled();
  });

  it('renders the effective permissions', async () => {
    mockGetEffectivePermissions.mockResolvedValue({
      subject: expectedSubject,
      permissions: {
        'kv:orders/*': { allowed: ['read', 'list'], denied: ['delete'] },
      },
      total_policies_evaluated: 3,
    });
    await renderReady();
    await fillCheck();
    await fireEvent.input(screen.getByLabelText('Resource patterns'), { target: { value: 'kv:orders/*\n\n' } });
    await fireEvent.input(screen.getByLabelText('Actions'), { target: { value: 'read, delete' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Explain' }));

    await waitFor(() => {
      expect(screen.getByText('Policies evaluated: 3')).toBeInTheDocument();
    });
    expect(mockGetEffectivePermissions).toHaveBeenCalledWith('acme', {
      subject: expectedSubject,
      resource_patterns: ['kv:orders/*'],
      actions: ['read', 'delete'],
    });
    const table = screen.getByRole('table', { name: 'Permission sets' });
    expect(table).toHaveTextContent('read, list');
    expect(table).toHaveTextContent('delete');
  });

  it('shows an SDK error', async () => {
    mockTestPolicy.mockRejectedValue(new Error('Policy engine unavailable'));
    await renderReady();
    await fillCheck();
    await fireEvent.click(screen.getByRole('button', { name: 'Check' }));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Policy engine unavailable');
    });
  });
});
