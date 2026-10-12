/**
 * DeployFleetNodeDialog.test.ts
 *
 * Verifies the deploy dialog mounts, validates, submits, and surfaces
 * errors. Also verifies the three auth method radio options:
 *   - private-key  → ssh_key_path
 *   - stored-key   → picks from sshKeys.list(), sets ssh_key_path
 *   - password     → ssh_password
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, fireEvent, waitFor, screen } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mock SDK client.
// ---------------------------------------------------------------------------

const { deployAgentMock, getTransportMock, listSSHKeysMock } = vi.hoisted(() => ({
  deployAgentMock: vi.fn(),
  getTransportMock: vi.fn(() => 'wails'),
  listSSHKeysMock: vi.fn().mockResolvedValue({ keys: [] }),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    connection: {
      getTransport: getTransportMock,
    },
    orchestrator: {
      deployFleetNode: deployAgentMock,
    },
    sshKeys: {
      list: listSSHKeysMock,
    },
  }),
}));

import DeployFleetNodeDialog from '../src/components/orchestrator/DeployFleetNodeDialog.svelte';
import { setAuthenticated, setUnauthenticated } from '../src/stores/auth.svelte.ts';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** sessionToken builds an unsigned access token whose tenant claim is tenantID. */
function sessionToken(tenantID: string): string {
  const b64url = (o: unknown): string =>
    btoa(JSON.stringify(o)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  const claims = {
    sub: 'usr_1',
    tenant_id: tenantID,
    roles: ['operator'],
    sid: 'sid_1',
    exp: Math.floor(Date.now() / 1000) + 3600,
  };
  return `${b64url({ alg: 'none' })}.${b64url(claims)}.sig`;
}

function fillRequiredFields(container: HTMLElement): void {
  (container.querySelector('[aria-label="SSH user"]') as HTMLInputElement).value = 'deploy';
  (container.querySelector('[aria-label="SSH user"]') as HTMLInputElement).dispatchEvent(
    new Event('input', { bubbles: true }),
  );
  (container.querySelector('[aria-label="SSH host"]') as HTMLInputElement).value = 'node01';
  (container.querySelector('[aria-label="SSH host"]') as HTMLInputElement).dispatchEvent(
    new Event('input', { bubbles: true }),
  );
  (container.querySelector('[aria-label="Server URL"]') as HTMLInputElement).value = 'https://c:8443';
  (container.querySelector('[aria-label="Server URL"]') as HTMLInputElement).dispatchEvent(
    new Event('input', { bubbles: true }),
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('DeployFleetNodeDialog', () => {
  beforeEach(() => {
    deployAgentMock.mockReset();
    listSSHKeysMock.mockReset();
    listSSHKeysMock.mockResolvedValue({ keys: [] });
    setAuthenticated({ access_token: sessionToken('acme') });
  });

  afterEach(() => {
    setUnauthenticated();
  });

  it('mounts closed and does not call the SDK', () => {
    render(DeployFleetNodeDialog, { props: { open: false } });
    expect(deployAgentMock).not.toHaveBeenCalled();
  });

  it('renders form fields when open', () => {
    render(DeployFleetNodeDialog, { props: { open: true } });
    expect(screen.getByLabelText('SSH user')).toBeTruthy();
    expect(screen.getByLabelText('SSH host')).toBeTruthy();
    expect(screen.getByLabelText('Server URL')).toBeTruthy();
    expect(screen.getByLabelText('Deploy node')).toBeTruthy();
  });

  it('renders all three auth method radio buttons', () => {
    render(DeployFleetNodeDialog, { props: { open: true } });
    expect(screen.getByLabelText('Private Key File')).toBeTruthy();
    expect(screen.getByLabelText('Stored SSH Key')).toBeTruthy();
    expect(screen.getByLabelText('Username + Password')).toBeTruthy();
  });

  it('defaults to private-key auth method and shows SSH key path field', () => {
    render(DeployFleetNodeDialog, { props: { open: true } });
    const pkRadio = screen.getByLabelText('Private Key File') as HTMLInputElement;
    expect(pkRadio.checked).toBe(true);
    expect(screen.getByLabelText('SSH key path')).toBeTruthy();
  });

  it('shows password field when password auth is selected', async () => {
    render(DeployFleetNodeDialog, { props: { open: true } });
    const pwRadio = screen.getByLabelText('Username + Password') as HTMLInputElement;
    await fireEvent.click(pwRadio);
    await waitFor(() => expect(screen.getByLabelText('SSH password')).toBeTruthy());
    // Key path field should no longer be visible.
    expect(screen.queryByLabelText('SSH key path')).toBeNull();
  });

  it('shows stored key selector when stored-key auth is selected', async () => {
    listSSHKeysMock.mockResolvedValue({
      keys: [{ name: 'prod-key', fingerprint: 'SHA256:abc', created_at: '2025-01-01T00:00:00Z' }],
    });
    render(DeployFleetNodeDialog, { props: { open: true } });
    const skRadio = screen.getByLabelText('Stored SSH Key') as HTMLInputElement;
    await fireEvent.click(skRadio);
    await waitFor(() => expect(listSSHKeysMock).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByLabelText('Stored SSH key')).toBeTruthy());
  });

  it('shows empty state message when stored-key list is empty', async () => {
    listSSHKeysMock.mockResolvedValue({ keys: [] });
    render(DeployFleetNodeDialog, { props: { open: true } });
    const skRadio = screen.getByLabelText('Stored SSH Key') as HTMLInputElement;
    await fireEvent.click(skRadio);
    await waitFor(() => expect(screen.getByText(/No stored keys found/)).toBeTruthy());
  });

  it('disables Deploy button when required fields are empty', () => {
    render(DeployFleetNodeDialog, { props: { open: true } });
    const button = screen.getByLabelText('Deploy node') as HTMLButtonElement;
    expect(button.disabled).toBe(true);
  });

  it('submits with private-key auth and passes ssh_key_path in spec', async () => {
    deployAgentMock.mockResolvedValue({
      host: 'node01',
      installed_path: '/home/deploy/.quicbaas/bin/quicbaas',
      service_status: 'systemd',
      node_id: 'agent-xyz',
      online_at: '2099-01-01T00:00:00Z',
    });

    const { container } = render(DeployFleetNodeDialog, { props: { open: true } });
    fillRequiredFields(container);

    const keyPath = container.querySelector('[aria-label="SSH key path"]') as HTMLInputElement;
    keyPath.value = '~/.ssh/id_ed25519';
    keyPath.dispatchEvent(new Event('input', { bubbles: true }));

    const button = screen.getByLabelText('Deploy node') as HTMLButtonElement;
    await waitFor(() => expect(button.disabled).toBe(false));
    await fireEvent.click(button);

    await waitFor(() => expect(deployAgentMock).toHaveBeenCalled());
    const spec = deployAgentMock.mock.calls[0][0];
    expect(spec.ssh_key_path).toBe('~/.ssh/id_ed25519');
    expect(spec.ssh_password).toBeUndefined();
  });

  it('submits with password auth and passes ssh_password in spec', async () => {
    deployAgentMock.mockResolvedValue({
      host: 'node01',
      installed_path: '/x',
      service_status: 'nohup',
    });

    const { container } = render(DeployFleetNodeDialog, { props: { open: true } });
    fillRequiredFields(container);

    // Switch to password auth.
    const pwRadio = screen.getByLabelText('Username + Password') as HTMLInputElement;
    await fireEvent.click(pwRadio);

    await waitFor(() => expect(screen.getByLabelText('SSH password')).toBeTruthy());
    const pwField = screen.getByLabelText('SSH password') as HTMLInputElement;
    pwField.value = 's3cr3t';
    pwField.dispatchEvent(new Event('input', { bubbles: true }));

    const button = screen.getByLabelText('Deploy node') as HTMLButtonElement;
    await waitFor(() => expect(button.disabled).toBe(false));
    await fireEvent.click(button);

    await waitFor(() => expect(deployAgentMock).toHaveBeenCalled());
    const spec = deployAgentMock.mock.calls[0][0];
    expect(spec.ssh_password).toBe('s3cr3t');
    expect(spec.ssh_key_path).toBeUndefined();
  });

  it('submits with stored-key auth and passes the selected key name as ssh_key_path', async () => {
    listSSHKeysMock.mockResolvedValue({
      keys: [
        { name: 'prod-key', fingerprint: 'SHA256:abc', created_at: '2025-01-01T00:00:00Z' },
        { name: 'staging-key', fingerprint: 'SHA256:def', created_at: '2025-01-02T00:00:00Z' },
      ],
    });
    deployAgentMock.mockResolvedValue({
      host: 'node01',
      installed_path: '/x',
      service_status: 'systemd',
    });

    const { container } = render(DeployFleetNodeDialog, { props: { open: true } });
    fillRequiredFields(container);

    const skRadio = screen.getByLabelText('Stored SSH Key') as HTMLInputElement;
    await fireEvent.click(skRadio);

    // Wait for the select to appear and pick a key.
    await waitFor(() => expect(screen.getByLabelText('Stored SSH key')).toBeTruthy());
    const select = screen.getByLabelText('Stored SSH key') as HTMLSelectElement;
    select.value = 'staging-key';
    select.dispatchEvent(new Event('change', { bubbles: true }));

    const button = screen.getByLabelText('Deploy node') as HTMLButtonElement;
    await waitFor(() => expect(button.disabled).toBe(false));
    await fireEvent.click(button);

    await waitFor(() => expect(deployAgentMock).toHaveBeenCalled());
    const spec = deployAgentMock.mock.calls[0][0];
    expect(spec.ssh_key_path).toBe('staging-key');
    expect(spec.ssh_password).toBeUndefined();
  });

  it('shows an error alert when deploy fails', async () => {
    deployAgentMock.mockRejectedValue(new Error('ssh dial failed'));

    const { container } = render(DeployFleetNodeDialog, { props: { open: true } });
    fillRequiredFields(container);

    const button = screen.getByLabelText('Deploy node') as HTMLButtonElement;
    await waitFor(() => expect(button.disabled).toBe(false));
    await fireEvent.click(button);

    await waitFor(() => {
      expect(container.textContent).toContain('ssh dial failed');
    });
  });

  it('rejects empty backends list', async () => {
    const { container } = render(DeployFleetNodeDialog, { props: { open: true } });
    fillRequiredFields(container);
    const backends = container.querySelector('[aria-label="Backends"]') as HTMLInputElement;
    backends.value = '';
    backends.dispatchEvent(new Event('input', { bubbles: true }));

    const button = screen.getByLabelText('Deploy node') as HTMLButtonElement;
    await waitFor(() => expect(button.disabled).toBe(false));
    await fireEvent.click(button);
    await waitFor(() => {
      expect(container.textContent).toContain('At least one backend is required');
    });
    expect(deployAgentMock).not.toHaveBeenCalled();
  });

  it('converts wait_online seconds to Go time.Duration nanoseconds', async () => {
    deployAgentMock.mockResolvedValue({
      host: 'node01',
      installed_path: '/x',
      service_status: 'systemd',
    });
    const { container } = render(DeployFleetNodeDialog, { props: { open: true } });
    fillRequiredFields(container);

    const wait = container.querySelector('[aria-label="Wait online"]') as HTMLInputElement;
    wait.value = '30';
    wait.dispatchEvent(new Event('input', { bubbles: true }));

    const button = screen.getByLabelText('Deploy node') as HTMLButtonElement;
    await waitFor(() => expect(button.disabled).toBe(false));
    await fireEvent.click(button);
    await waitFor(() => expect(deployAgentMock).toHaveBeenCalled());
    expect(deployAgentMock.mock.calls[0][0].wait_online).toBe(30 * 1_000_000_000);
  });

  it('maps the systemd dropdown to the tri-state spec field', async () => {
    deployAgentMock.mockResolvedValue({ host: 'h', installed_path: '/x', service_status: 'nohup' });
    const { container } = render(DeployFleetNodeDialog, { props: { open: true } });
    fillRequiredFields(container);

    const systemd = container.querySelector('[aria-label="Supervisor"]') as HTMLSelectElement;
    systemd.value = 'no';
    systemd.dispatchEvent(new Event('change', { bubbles: true }));

    const button = screen.getByLabelText('Deploy node') as HTMLButtonElement;
    await waitFor(() => expect(button.disabled).toBe(false));
    await fireEvent.click(button);
    await waitFor(() => expect(deployAgentMock).toHaveBeenCalled());
    expect(deployAgentMock.mock.calls[0][0].systemd).toBe(false);
  });

  it('defaults the tenant to the session tenant and submits it (#312)', async () => {
    deployAgentMock.mockResolvedValue({ host: 'h', installed_path: '/x', service_status: 'systemd' });
    const { container } = render(DeployFleetNodeDialog, { props: { open: true } });
    fillRequiredFields(container);

    const tenant = container.querySelector('[aria-label="Tenant"]') as HTMLInputElement;
    expect(tenant.value).toBe('acme');

    const button = screen.getByLabelText('Deploy node') as HTMLButtonElement;
    await waitFor(() => expect(button.disabled).toBe(false));
    await fireEvent.click(button);
    await waitFor(() => expect(deployAgentMock).toHaveBeenCalled());
    expect(deployAgentMock.mock.calls[0][0].tenant).toBe('acme');
  });

  it('leaves the tenant empty and blocks submission without a session (#312)', async () => {
    setUnauthenticated();
    const { container } = render(DeployFleetNodeDialog, { props: { open: true } });
    fillRequiredFields(container);

    const tenant = container.querySelector('[aria-label="Tenant"]') as HTMLInputElement;
    expect(tenant.value).toBe('');
    const button = screen.getByLabelText('Deploy node') as HTMLButtonElement;
    expect(button.disabled).toBe(true);
  });
});
