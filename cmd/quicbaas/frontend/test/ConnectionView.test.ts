/**
 * ConnectionView.test.ts
 *
 * Verifies that ConnectionView renders its page header and the current
 * transport and address, lists the browser-stub transports as disabled,
 * validates the daemon address inline, saves an override (asking first when
 * it points at another origin, and only when a probe reaches it), and resets it. Also checks that the
 * 'connection' view is reachable through navigation and the hash router.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/lib/reloadApp.ts', () => ({
  reloadApp: vi.fn(),
}));

import ConnectionView from '../src/views/settings/ConnectionView.svelte';
import { showToast } from '../src/stores/toast.svelte.ts';
import { reloadApp } from '../src/lib/reloadApp.ts';
import {
  connectionOverride,
  connectionStore,
  saveConnectionOverride,
  setConnected,
} from '../src/stores/connection.svelte.ts';
import { navigate } from '../src/stores/navigation.svelte.ts';
import { routerStore } from '../src/stores/router.svelte.ts';

function addressInput(container: HTMLElement): HTMLInputElement {
  return container.querySelector('#connection-address') as HTMLInputElement;
}

function applyButton(): HTMLButtonElement {
  return screen.getByRole('button', { name: 'Apply' }) as HTMLButtonElement;
}

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  setConnected(window.location.origin, 'rest');
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('ConnectionView', () => {
  it('renders the page heading with icon', () => {
    const { container } = render(ConnectionView);
    const h1 = container.querySelector('h1');
    expect(h1).not.toBeNull();
    expect(h1!.textContent).toContain('Connection');
    expect(container.querySelector('header.page-header svg')).not.toBeNull();
  });

  it('shows the current transport and address', () => {
    const { container } = render(ConnectionView);
    expect(screen.getByTestId('connection-transport').textContent).toBe('rest');
    expect(screen.getByTestId('connection-address').textContent).toBe(window.location.origin);
    expect(screen.getByTestId('connection-source').textContent).toBe('Default');
    expect(addressInput(container).value).toBe(window.location.origin);
  });

  it('lists grpc-web as a disabled option', () => {
    const { container } = render(ConnectionView);
    const opt = container.querySelector('option[value="grpc-web"]') as HTMLOptionElement;
    expect(opt).not.toBeNull();
    expect(opt.disabled).toBe(true);
    expect(opt.textContent).toContain('not available in the browser yet');
  });

  it('shows an inline error and disables Apply for an invalid address', async () => {
    const { container } = render(ConnectionView);
    await fireEvent.input(addressInput(container), { target: { value: 'not a url' } });
    expect(screen.getByText(/enter an http or https address/i)).toBeInTheDocument();
    expect(applyButton().disabled).toBe(true);
  });

  it('disables Apply while the address is unchanged', () => {
    render(ConnectionView);
    expect(applyButton().disabled).toBe(true);
  });

  it('saves a same-origin override without asking', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    // Start from a custom address so the page origin is a change.
    saveConnectionOverride({ transport: 'rest', baseURL: 'https://other.example:8443' });
    const { container } = render(ConnectionView);
    await fireEvent.input(addressInput(container), {
      target: { value: `${window.location.origin}/` },
    });
    await fireEvent.click(applyButton());
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(connectionOverride()).toEqual({ transport: 'rest', baseURL: window.location.origin });
    expect(reloadApp).toHaveBeenCalledTimes(1);
  });

  it('does not save another origin when the confirm is declined', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    const { container } = render(ConnectionView);
    await fireEvent.input(addressInput(container), {
      target: { value: 'https://other.example:8443' },
    });
    await fireEvent.click(applyButton());
    expect(window.confirm).toHaveBeenCalledWith(
      'Requests and your session token will go to https://other.example:8443. Continue?',
    );
    expect(connectionOverride()).toBeNull();
    expect(reloadApp).not.toHaveBeenCalled();
  });

  it('saves another origin when the confirm is accepted', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const fetchMock = vi.fn().mockResolvedValue(new Response('', { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = render(ConnectionView);
    await fireEvent.input(addressInput(container), {
      target: { value: 'https://other.example:8443/' },
    });
    await fireEvent.click(applyButton());
    await waitFor(() => {
      expect(connectionOverride()).toEqual({
        transport: 'rest',
        baseURL: 'https://other.example:8443',
      });
      expect(connectionStore.address).toBe('https://other.example:8443');
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe('https://other.example:8443/api/v1/health/ready');
    await waitFor(() => {
      expect(screen.getByTestId('connection-source').textContent).toBe('Custom');
    });
    await waitFor(() => expect(reloadApp).toHaveBeenCalledTimes(1));
  });

  it('does not save another origin the page cannot reach', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));
    const { container } = render(ConnectionView);
    await fireEvent.input(addressInput(container), {
      target: { value: 'https://other.example:8443' },
    });
    await fireEvent.click(applyButton());
    await waitFor(() =>
      expect(screen.getByTestId('connection-apply-error')).toHaveTextContent(
        'https://other.example:8443 cannot be reached from this page',
      ),
    );
    expect(connectionOverride()).toBeNull();
    expect(connectionStore.address).toBe(window.location.origin);
    expect(screen.getByTestId('connection-source').textContent).toBe('Default');
    expect(showToast).not.toHaveBeenCalledWith('success', expect.anything());
    expect(reloadApp).not.toHaveBeenCalled();
  });

  it('clears the reach error when the address changes', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));
    const { container } = render(ConnectionView);
    await fireEvent.input(addressInput(container), {
      target: { value: 'https://other.example:8443' },
    });
    await fireEvent.click(applyButton());
    await waitFor(() => expect(screen.getByTestId('connection-apply-error')).toBeInTheDocument());
    await fireEvent.input(addressInput(container), {
      target: { value: 'https://another.example:8443' },
    });
    await waitFor(() => expect(screen.queryByTestId('connection-apply-error')).toBeNull());
  });

  it('reports a storage failure as an error toast', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 200 })));
    vi.spyOn(window.localStorage, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    const { container } = render(ConnectionView);
    await fireEvent.input(addressInput(container), {
      target: { value: 'https://other.example:8443' },
    });
    await fireEvent.click(applyButton());
    await waitFor(() =>
      expect(showToast).toHaveBeenCalledWith('error', 'Browser storage is unavailable'),
    );
    expect(reloadApp).not.toHaveBeenCalled();
  });

  it('resets a custom override to the default', async () => {
    saveConnectionOverride({ transport: 'rest', baseURL: 'https://other.example:8443' });
    render(ConnectionView);
    expect(screen.getByTestId('connection-source').textContent).toBe('Custom');
    await fireEvent.click(screen.getByRole('button', { name: 'Reset to default' }));
    expect(connectionOverride()).toBeNull();
    expect(connectionStore.address).toBe(window.location.origin);
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Reset to default' })).toBeNull();
    });
    await waitFor(() => expect(reloadApp).toHaveBeenCalledTimes(1));
  });

  it('hides Reset to default without an override', () => {
    render(ConnectionView);
    expect(screen.queryByRole('button', { name: 'Reset to default' })).toBeNull();
  });
});

describe('connection routing', () => {
  it('navigate("connection") sets the hash to #/connection', () => {
    window.location.hash = '#/dashboard';
    navigate('connection');
    expect(window.location.hash).toBe('#/connection');
  });

  it('the router maps #/connection to the connection route', async () => {
    window.location.hash = '#/connection';
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    await waitFor(() => expect(routerStore.name).toBe('connection'));
  });
});
