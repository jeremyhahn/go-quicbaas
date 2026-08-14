/**
 * DEKView.test.ts
 *
 * Verifies that DEKView mounts, calls listDEKs on init, renders the DEK
 * list, exposes create / view-details / rotate / set-default / safety /
 * delete / re-encryption affordances that call the corresponding DEKClient
 * methods with the right arguments, confirm-gates destructive and
 * destructive-adjacent actions, and degrades gracefully when a fetch fails.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import type { DEKMetadata } from '@quicbaas/sdk';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import DEKView from '../src/views/security/DEKView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

const mockCreateDEK = vi.fn();
const mockGetDEK = vi.fn();
const mockListDEKs = vi.fn();
const mockRotateDEK = vi.fn();
const mockDeleteDEK = vi.fn();
const mockGetDEKStatus = vi.fn();
const mockSetDefaultDEK = vi.fn();
const mockSetDEKSafety = vi.fn();
const mockClearDEKSafety = vi.fn();
const mockStartReencryption = vi.fn();
const mockGetReencryptionProgress = vi.fn();

function mockClient(): void {
  vi.mocked(getSDKClient).mockResolvedValue({
    dek: {
      createDEK: mockCreateDEK,
      getDEK: mockGetDEK,
      listDEKs: mockListDEKs,
      rotateDEK: mockRotateDEK,
      deleteDEK: mockDeleteDEK,
      getDEKStatus: mockGetDEKStatus,
      setDefaultDEK: mockSetDefaultDEK,
      setDEKSafety: mockSetDEKSafety,
      clearDEKSafety: mockClearDEKSafety,
      startReencryption: mockStartReencryption,
      getReencryptionProgress: mockGetReencryptionProgress,
    },
  } as never);
}

function dek(overrides: Partial<DEKMetadata> = {}): DEKMetadata {
  return {
    path: 'tenant/app/key',
    algorithm: 'AES-256-GCM',
    version: 1,
    status: 'active',
    is_default: false,
    created_at: '2026-01-01T00:00:00Z',
    usage_count: 10,
    bytes_used: 1024,
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mockListDEKs.mockResolvedValue({ deks: [], has_more: false, total_count: 0, timestamp: '' });
  mockCreateDEK.mockResolvedValue({ dek: dek(), timestamp: '' });
  mockGetDEK.mockResolvedValue({ dek: dek(), timestamp: '' });
  mockRotateDEK.mockResolvedValue({ path: 'tenant/app/key', old_version: 1, new_version: 2, timestamp: '' });
  mockDeleteDEK.mockResolvedValue({ path: 'tenant/app/key', deleted: true, timestamp: '' });
  mockGetDEKStatus.mockResolvedValue({ dek: dek(), timestamp: '' });
  mockSetDefaultDEK.mockResolvedValue({ path: 'tenant/app/key', timestamp: '' });
  mockSetDEKSafety.mockResolvedValue({
    path: 'tenant/app/key',
    config: { max_operations: 1000, max_bytes: 1000, warning_percent: 80 },
    timestamp: '',
  });
  mockClearDEKSafety.mockResolvedValue({ path: 'tenant/app/key', cleared: true, timestamp: '' });
  mockStartReencryption.mockResolvedValue({
    operation_id: 'op-123',
    path: 'tenant/app/key',
    started: true,
    estimated_items: 42,
    timestamp: '',
  });
  mockGetReencryptionProgress.mockResolvedValue({
    path: 'tenant/app/key',
    operation_id: 'op-123',
    status: 'running',
    progress: {
      status: 'running',
      in_progress: true,
      total_keys: 42,
      processed_keys: 10,
      failed_keys: 0,
      percent_complete: 23.8,
    },
    timestamp: '',
  });
  mockClient();
});

describe('DEKView', () => {
  it('calls listDEKs on mount', async () => {
    render(DEKView);
    await waitFor(() => {
      expect(mockListDEKs).toHaveBeenCalled();
    });
  });

  it('renders the page heading with icon', async () => {
    const { container } = render(DEKView);
    await waitFor(() => {
      const h1 = container.querySelector('h1');
      expect(h1).not.toBeNull();
      expect(h1!.textContent).toContain('DEK');
      expect(container.querySelector('div.bg-primary\\/10 svg')).not.toBeNull();
    });
  });

  it('renders an empty state when zero DEKs are returned', async () => {
    render(DEKView);
    await waitFor(() => {
      expect(screen.queryByText(/no deks found/i)).toBeTruthy();
    });
  });

  it('degrades gracefully when listDEKs fails', async () => {
    mockListDEKs.mockRejectedValue(new Error('boom'));
    render(DEKView);
    await waitFor(() => {
      expect(screen.getByText('boom')).toBeInTheDocument();
    });
  });

  it('renders DEK rows when present', async () => {
    mockListDEKs.mockResolvedValue({
      deks: [dek({ path: 'tenant/app/key', is_default: true })],
      has_more: false,
      total_count: 1,
      timestamp: '',
    });
    render(DEKView);
    await waitFor(() => {
      expect(screen.getByText('tenant/app/key')).toBeInTheDocument();
      expect(screen.getByText('default')).toBeInTheDocument();
    });
  });
});

describe('DEKView — create dialog', () => {
  it('creates a DEK with the entered path and algorithm', async () => {
    const { container } = render(DEKView);
    await waitFor(() => expect(mockListDEKs).toHaveBeenCalled());

    const createBtn = container.querySelector('button[aria-label="Create DEK"]') as HTMLButtonElement;
    await fireEvent.click(createBtn);
    await waitFor(() => expect(container.querySelector('#dek-create-path')).not.toBeNull());

    await fireEvent.input(container.querySelector('#dek-create-path') as HTMLInputElement, {
      target: { value: 'tenant/new/key' },
    });

    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Create',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(mockCreateDEK).toHaveBeenCalled());
    const req = mockCreateDEK.mock.calls[0][0];
    expect(req.path).toBe('tenant/new/key');
    expect(req.algorithm).toBe('AES-256-GCM');
  });

  it('rejects submit client-side when path is missing', async () => {
    const { container } = render(DEKView);
    const createBtn = container.querySelector('button[aria-label="Create DEK"]') as HTMLButtonElement;
    await fireEvent.click(createBtn);
    await waitFor(() => expect(container.querySelector('#dek-create-path')).not.toBeNull());

    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Create',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(screen.getByText(/path is required/i)).toBeInTheDocument());
    expect(mockCreateDEK).not.toHaveBeenCalled();
  });
});

describe('DEKView — row actions', () => {
  async function renderWithOneDEK() {
    mockListDEKs.mockResolvedValue({
      deks: [dek({ path: 'tenant/app/key' })],
      has_more: false,
      total_count: 1,
      timestamp: '',
    });
    const result = render(DEKView);
    await waitFor(() => expect(screen.getByText('tenant/app/key')).toBeInTheDocument());
    return result;
  }

  it('views DEK details via a fresh getDEKStatus fetch', async () => {
    const { container } = await renderWithOneDEK();
    const viewBtn = container.querySelector('button[aria-label="View details for tenant/app/key"]') as HTMLButtonElement;
    await fireEvent.click(viewBtn);
    await waitFor(() => expect(mockGetDEKStatus).toHaveBeenCalledWith('tenant/app/key'));
    await waitFor(() => expect(screen.getByText('DEK Details')).toBeInTheDocument());
  });

  it('shows in-flight re-encryption progress in the details panel when present', async () => {
    mockGetDEKStatus.mockResolvedValue({
      dek: dek(),
      reencryption_progress: {
        status: 'running',
        in_progress: true,
        total_keys: 100,
        processed_keys: 40,
        failed_keys: 0,
        percent_complete: 40,
      },
      timestamp: '',
    });
    const { container } = await renderWithOneDEK();
    const viewBtn = container.querySelector('button[aria-label="View details for tenant/app/key"]') as HTMLButtonElement;
    await fireEvent.click(viewBtn);
    await waitFor(() => expect(screen.getByText(/40 \/ 100 processed/)).toBeInTheDocument());
  });

  it('rotates a DEK with the selected options', async () => {
    const { container } = await renderWithOneDEK();
    const rotateBtn = container.querySelector('button[aria-label="Rotate tenant/app/key"]') as HTMLButtonElement;
    await fireEvent.click(rotateBtn);
    await waitFor(() => expect(screen.getByText('Rotate DEK?')).toBeInTheDocument());

    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Rotate',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(mockRotateDEK).toHaveBeenCalledWith('tenant/app/key', {
      background: false,
      retain_old_version: true,
    }));
  });

  it('sets a DEK as default directly (non-destructive, no confirm)', async () => {
    const { container } = await renderWithOneDEK();
    const setDefaultBtn = container.querySelector('button[aria-label="Set tenant/app/key as default"]') as HTMLButtonElement;
    await fireEvent.click(setDefaultBtn);
    await waitFor(() => expect(mockSetDefaultDEK).toHaveBeenCalledWith('tenant/app/key'));
  });

  it('sets and clears safety limits', async () => {
    const { container } = await renderWithOneDEK();
    const safetyBtn = container.querySelector('button[aria-label="Safety limits for tenant/app/key"]') as HTMLButtonElement;
    await fireEvent.click(safetyBtn);
    await waitFor(() => expect(container.querySelector('#dek-safety-max-ops')).not.toBeNull());

    await fireEvent.input(container.querySelector('#dek-safety-max-ops') as HTMLInputElement, {
      target: { value: '5000' },
    });

    const saveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Save',
    ) as HTMLButtonElement;
    await fireEvent.click(saveBtn);

    await waitFor(() => expect(mockSetDEKSafety).toHaveBeenCalled());
    expect(mockSetDEKSafety.mock.calls[0][0]).toBe('tenant/app/key');
    expect(mockSetDEKSafety.mock.calls[0][1].max_operations).toBe(5000);
  });

  it('clears safety limits when Clear is clicked', async () => {
    const { container } = await renderWithOneDEK();
    const safetyBtn = container.querySelector('button[aria-label="Safety limits for tenant/app/key"]') as HTMLButtonElement;
    await fireEvent.click(safetyBtn);
    await waitFor(() => expect(container.querySelector('#dek-safety-max-ops')).not.toBeNull());

    const clearBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Clear',
    ) as HTMLButtonElement;
    await fireEvent.click(clearBtn);

    await waitFor(() => expect(mockClearDEKSafety).toHaveBeenCalledWith('tenant/app/key'));
  });

  it('deletes a DEK after confirmation', async () => {
    const { container } = await renderWithOneDEK();
    const deleteBtn = container.querySelector('button[aria-label="Delete tenant/app/key"]') as HTMLButtonElement;
    await fireEvent.click(deleteBtn);
    await waitFor(() => expect(screen.getByText('Delete DEK?')).toBeInTheDocument());

    const confirmBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Delete',
    ) as HTMLButtonElement;
    await fireEvent.click(confirmBtn);

    await waitFor(() => expect(mockDeleteDEK).toHaveBeenCalledWith('tenant/app/key', {
      force: false,
      migrate_to: undefined,
    }));
  });

  it('does not delete when the confirmation dialog is cancelled', async () => {
    const { container } = await renderWithOneDEK();
    const deleteBtn = container.querySelector('button[aria-label="Delete tenant/app/key"]') as HTMLButtonElement;
    await fireEvent.click(deleteBtn);
    await waitFor(() => expect(screen.getByText('Delete DEK?')).toBeInTheDocument());

    const cancelBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Cancel',
    ) as HTMLButtonElement;
    await fireEvent.click(cancelBtn);

    expect(mockDeleteDEK).not.toHaveBeenCalled();
  });
});

describe('DEKView — re-encryption', () => {
  async function renderWithOneDEK() {
    mockListDEKs.mockResolvedValue({
      deks: [dek({ path: 'tenant/app/key', version: 1 })],
      has_more: false,
      total_count: 1,
      timestamp: '',
    });
    const result = render(DEKView);
    await waitFor(() => expect(screen.getByText('tenant/app/key')).toBeInTheDocument());
    return result;
  }

  it('refreshes the current version via getDEK before prefilling from/to version', async () => {
    mockGetDEK.mockResolvedValue({ dek: dek({ path: 'tenant/app/key', version: 3 }), timestamp: '' });
    const { container } = await renderWithOneDEK();
    const openBtn = container.querySelector('button[aria-label="Re-encrypt tenant/app/key"]') as HTMLButtonElement;
    await fireEvent.click(openBtn);
    await waitFor(() => expect(mockGetDEK).toHaveBeenCalledWith('tenant/app/key'));
    await waitFor(() => {
      const fromInput = container.querySelector('#reencrypt-from') as HTMLInputElement;
      expect(fromInput.value).toBe('3');
    });
  });

  it('gates the Start button behind an explicit confirmation checkbox', async () => {
    const { container } = await renderWithOneDEK();
    const openBtn = container.querySelector('button[aria-label="Re-encrypt tenant/app/key"]') as HTMLButtonElement;
    await fireEvent.click(openBtn);
    await waitFor(() => expect(screen.getByText(/Start Re-encryption/i)).toBeInTheDocument());

    const startBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim().startsWith('Start Re-encryption'),
    ) as HTMLButtonElement;
    expect(startBtn.disabled).toBe(true);

    const confirmCheckbox = Array.from(container.querySelectorAll('input[type="checkbox"]')).find(
      (el) => (el as HTMLInputElement).className.includes('checkbox-warning'),
    ) as HTMLInputElement;
    await fireEvent.click(confirmCheckbox);
    expect(startBtn.disabled).toBe(false);

    await fireEvent.click(startBtn);
    await waitFor(() => expect(mockStartReencryption).toHaveBeenCalled());
    expect(mockStartReencryption.mock.calls[0][0]).toBe('tenant/app/key');
  });

  it('does not start re-encryption without confirmation', async () => {
    const { container } = await renderWithOneDEK();
    const openBtn = container.querySelector('button[aria-label="Re-encrypt tenant/app/key"]') as HTMLButtonElement;
    await fireEvent.click(openBtn);
    await waitFor(() => expect(screen.getByText(/Start Re-encryption/i)).toBeInTheDocument());

    expect(mockStartReencryption).not.toHaveBeenCalled();
  });

  it('checks progress after starting re-encryption and surfaces the result', async () => {
    const { container } = await renderWithOneDEK();
    const openBtn = container.querySelector('button[aria-label="Re-encrypt tenant/app/key"]') as HTMLButtonElement;
    await fireEvent.click(openBtn);

    const confirmCheckbox = Array.from(container.querySelectorAll('input[type="checkbox"]')).find(
      (el) => (el as HTMLInputElement).className.includes('checkbox-warning'),
    ) as HTMLInputElement;
    await fireEvent.click(confirmCheckbox);

    const startBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim().startsWith('Start Re-encryption'),
    ) as HTMLButtonElement;
    await fireEvent.click(startBtn);
    await waitFor(() => expect(mockStartReencryption).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByText(/op-123/)).toBeInTheDocument());

    const checkBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Check Progress',
    ) as HTMLButtonElement;
    await fireEvent.click(checkBtn);

    await waitFor(() => expect(mockGetReencryptionProgress).toHaveBeenCalledWith('tenant/app/key', 'op-123'));
    await waitFor(() => expect(screen.getByText(/23.8%/)).toBeInTheDocument());
  });

  it('surfaces an error when starting re-encryption fails', async () => {
    mockStartReencryption.mockRejectedValue(new Error('already in progress'));
    const { container } = await renderWithOneDEK();
    const openBtn = container.querySelector('button[aria-label="Re-encrypt tenant/app/key"]') as HTMLButtonElement;
    await fireEvent.click(openBtn);

    const confirmCheckbox = Array.from(container.querySelectorAll('input[type="checkbox"]')).find(
      (el) => (el as HTMLInputElement).className.includes('checkbox-warning'),
    ) as HTMLInputElement;
    await fireEvent.click(confirmCheckbox);

    const startBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim().startsWith('Start Re-encryption'),
    ) as HTMLButtonElement;
    await fireEvent.click(startBtn);

    await waitFor(() => expect(screen.getByText('already in progress')).toBeInTheDocument());
  });
});
