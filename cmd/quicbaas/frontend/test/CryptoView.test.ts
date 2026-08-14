/**
 * CryptoView.test.ts
 *
 * Verifies that CryptoView renders its primitive panels and that each
 * action (encrypt, decrypt, sign, verify, hash, random, data-key) calls the
 * corresponding CryptoClient method with base64-encoded UTF-8 text where
 * applicable, and degrades gracefully when a call fails.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import CryptoView from '../src/views/security/CryptoView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

const mockEncrypt = vi.fn();
const mockDecrypt = vi.fn();
const mockSign = vi.fn();
const mockVerify = vi.fn();
const mockHash = vi.fn();
const mockRandom = vi.fn();
const mockDataKey = vi.fn();

function mockClient(): void {
  vi.mocked(getSDKClient).mockResolvedValue({
    crypto: {
      encrypt: mockEncrypt,
      decrypt: mockDecrypt,
      sign: mockSign,
      verify: mockVerify,
      hash: mockHash,
      random: mockRandom,
      dataKey: mockDataKey,
    },
  } as never);
}

beforeEach(() => {
  vi.clearAllMocks();
  mockEncrypt.mockResolvedValue({ key_id: 'k1', key_version: 1, ciphertext: 'Y2lwaGVy', timestamp: '' });
  mockDecrypt.mockResolvedValue({ key_id: 'k1', key_version: 1, plaintext: btoa('hello world'), timestamp: '' });
  mockSign.mockResolvedValue({ key_id: 'k1', key_version: 1, signature: 'c2ln', hash_algorithm: 'SHA256', timestamp: '' });
  mockVerify.mockResolvedValue({ key_id: 'k1', key_version: 1, valid: true, timestamp: '' });
  mockHash.mockResolvedValue({ algorithm: 'SHA256', hash: 'aGFzaA==', timestamp: '' });
  mockRandom.mockResolvedValue({ random_bytes: 'cmFuZG9t', timestamp: '' });
  mockDataKey.mockResolvedValue({ key_id: 'kek1', plaintext: 'cGxhaW50ZXh0', ciphertext_blob: 'd3JhcHBlZA==', timestamp: '' });
  mockClient();
  Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
});

describe('CryptoView', () => {
  it('renders the page heading with icon', () => {
    const { container } = render(CryptoView);
    const h1 = container.querySelector('h1');
    expect(h1).not.toBeNull();
    expect(h1!.textContent).toContain('Crypto Console');
    expect(container.querySelector('div.bg-primary\\/10 svg')).not.toBeNull();
  });

  it('renders all seven primitive panels', () => {
    const { container } = render(CryptoView);
    const headings = Array.from(container.querySelectorAll('h3')).map((h) => h.textContent?.trim());
    expect(headings).toEqual([
      'Encrypt',
      'Decrypt',
      'Sign',
      'Verify',
      'Hash',
      'Random Bytes',
      'Generate Data Key',
    ]);
  });
});

describe('CryptoView — encrypt / decrypt', () => {
  it('base64-encodes UTF-8 plaintext before calling encrypt', async () => {
    const { container } = render(CryptoView);
    await fireEvent.input(container.querySelector('input[aria-label="Encrypt key name"]') as HTMLInputElement, {
      target: { value: 'my-key' },
    });
    await fireEvent.input(container.querySelector('textarea[aria-label="Plaintext"]') as HTMLTextAreaElement, {
      target: { value: 'hello world' },
    });
    const encryptBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Encrypt',
    ) as HTMLButtonElement;
    await fireEvent.click(encryptBtn);

    await waitFor(() => expect(mockEncrypt).toHaveBeenCalled());
    const req = mockEncrypt.mock.calls[0][0];
    expect(req.key_name).toBe('my-key');
    expect(atob(req.plaintext)).toBe('hello world');
  });

  it('decodes the returned base64 plaintext to UTF-8 text', async () => {
    const { container } = render(CryptoView);
    await fireEvent.input(container.querySelector('input[aria-label="Decrypt key name"]') as HTMLInputElement, {
      target: { value: 'my-key' },
    });
    await fireEvent.input(container.querySelector('textarea[aria-label="Ciphertext"]') as HTMLTextAreaElement, {
      target: { value: 'Y2lwaGVy' },
    });
    const decryptBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Decrypt',
    ) as HTMLButtonElement;
    await fireEvent.click(decryptBtn);

    await waitFor(() => expect(mockDecrypt).toHaveBeenCalledWith({ key_name: 'my-key', ciphertext: 'Y2lwaGVy' }));
    await waitFor(() => expect(screen.getByText('hello world')).toBeInTheDocument());
  });

  it('degrades gracefully when encrypt fails', async () => {
    mockEncrypt.mockRejectedValue(new Error('key not found'));
    const { container } = render(CryptoView);
    await fireEvent.input(container.querySelector('input[aria-label="Encrypt key name"]') as HTMLInputElement, {
      target: { value: 'missing-key' },
    });
    const encryptBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Encrypt',
    ) as HTMLButtonElement;
    await fireEvent.click(encryptBtn);
    await waitFor(() => expect(screen.getByText('key not found')).toBeInTheDocument());
  });
});

describe('CryptoView — sign / verify', () => {
  it('signs a message and displays the returned signature', async () => {
    const { container } = render(CryptoView);
    await fireEvent.input(container.querySelector('input[aria-label="Sign key name"]') as HTMLInputElement, {
      target: { value: 'sign-key' },
    });
    await fireEvent.input(container.querySelector('textarea[aria-label="Message to sign"]') as HTMLTextAreaElement, {
      target: { value: 'sign me' },
    });
    const signBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Sign',
    ) as HTMLButtonElement;
    await fireEvent.click(signBtn);

    await waitFor(() => expect(mockSign).toHaveBeenCalled());
    expect(atob(mockSign.mock.calls[0][0].message)).toBe('sign me');
    await waitFor(() => expect(screen.getByText('c2ln')).toBeInTheDocument());
  });

  it('verifies a signature and reports validity', async () => {
    const { container } = render(CryptoView);
    await fireEvent.input(container.querySelector('input[aria-label="Verify key name"]') as HTMLInputElement, {
      target: { value: 'verify-key' },
    });
    await fireEvent.input(container.querySelector('textarea[aria-label="Signature"]') as HTMLTextAreaElement, {
      target: { value: 'c2ln' },
    });
    const verifyBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Verify',
    ) as HTMLButtonElement;
    await fireEvent.click(verifyBtn);

    await waitFor(() => expect(mockVerify).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByText(/signature is valid/i)).toBeInTheDocument());
  });
});

describe('CryptoView — hash / random / dataKey', () => {
  it('hashes UTF-8 data and displays the returned digest', async () => {
    const { container } = render(CryptoView);
    await fireEvent.input(container.querySelector('textarea[aria-label="Data to hash"]') as HTMLTextAreaElement, {
      target: { value: 'hash me' },
    });
    const hashBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Hash',
    ) as HTMLButtonElement;
    await fireEvent.click(hashBtn);

    await waitFor(() => expect(mockHash).toHaveBeenCalled());
    expect(atob(mockHash.mock.calls[0][0].data)).toBe('hash me');
    await waitFor(() => expect(screen.getByText('aGFzaA==')).toBeInTheDocument());
  });

  it('generates random bytes with the requested count', async () => {
    const { container } = render(CryptoView);
    await fireEvent.input(container.querySelector('input[aria-label="Number of random bytes"]') as HTMLInputElement, {
      target: { value: '64' },
    });
    const genBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Generate',
    ) as HTMLButtonElement;
    await fireEvent.click(genBtn);

    await waitFor(() => expect(mockRandom).toHaveBeenCalledWith(64));
    await waitFor(() => expect(screen.getByText('cmFuZG9t')).toBeInTheDocument());
  });

  it('generates a data key and shows both plaintext and wrapped forms', async () => {
    const { container } = render(CryptoView);
    await fireEvent.input(container.querySelector('input[aria-label="Key encryption key name"]') as HTMLInputElement, {
      target: { value: 'kek1' },
    });
    const genBtn = screen.getByRole('button', { name: /^generate data key$/i });
    await fireEvent.click(genBtn);

    await waitFor(() => expect(mockDataKey).toHaveBeenCalledWith({ key_name: 'kek1', key_size_bits: 256 }));
    await waitFor(() => {
      expect(screen.getByText('cGxhaW50ZXh0')).toBeInTheDocument();
      expect(screen.getByText('d3JhcHBlZA==')).toBeInTheDocument();
    });
  });

  it('rejects data-key generation client-side when the KEK name is missing', async () => {
    render(CryptoView);
    const genBtn = screen.getByRole('button', { name: /^generate data key$/i });
    await fireEvent.click(genBtn);
    await waitFor(() => expect(screen.getByText(/kek name is required/i)).toBeInTheDocument());
    expect(mockDataKey).not.toHaveBeenCalled();
  });
});
