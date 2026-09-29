/**
 * CostSettingsPanel.test.ts
 *
 * go-quicbaas#1058: the tenant's electricity rate and engine draws. The panel
 * shows the rate in force and where it came from, and saves a replacing
 * request built from the form.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

const { getCostSettings, setCostSettings } = vi.hoisted(() => ({
  getCostSettings: vi.fn(),
  setCostSettings: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({ orchestrator: { getCostSettings, setCostSettings } }),
}));

import CostSettingsPanel from '../src/components/orchestrator/CostSettingsPanel.svelte';

const inherited = {
  settings: { tenant_id: 'acme' },
  electricity: { price_per_kwh: 0.12, source: 'default' },
  engine_power_watts: { ollama: 250 },
};

beforeEach(() => {
  getCostSettings.mockReset();
  setCostSettings.mockReset();
  getCostSettings.mockResolvedValue(inherited);
});

describe('CostSettingsPanel', () => {
  it('shows the default rate and the inherited draws', async () => {
    render(CostSettingsPanel, { tenantID: 'acme' });
    expect((await screen.findByTestId('cost-settings-rate')).textContent).toContain('$0.12/kWh');
    expect(screen.getByText(/the default/)).toBeTruthy();
    expect(screen.getByTestId('cost-settings-inherited').textContent).toContain('ollama 250 W');
  });

  it('saves the rate and an engine draw', async () => {
    setCostSettings.mockResolvedValue({
      settings: { tenant_id: 'acme', electricity_price_per_kwh: 0.2, engine_power_watts: { openvino: 12 } },
      electricity: { price_per_kwh: 0.2, source: 'tenant_settings' },
      engine_power_watts: { openvino: 12, ollama: 250 },
    });
    render(CostSettingsPanel, { tenantID: 'acme' });
    await screen.findByTestId('cost-settings-rate');

    await fireEvent.input(document.getElementById('cost-settings-price') as HTMLInputElement, { target: { value: '0.2' } });
    await fireEvent.click(screen.getByText('Engine'));
    await fireEvent.input(screen.getByLabelText('Engine 1'), { target: { value: 'openvino' } });
    await fireEvent.input(screen.getByLabelText('Watts for engine 1'), { target: { value: '12' } });
    await fireEvent.click(screen.getByText('Save'));

    await waitFor(() => expect(setCostSettings).toHaveBeenCalled());
    expect(setCostSettings).toHaveBeenCalledWith({
      tenant_id: 'acme', electricity_price_per_kwh: 0.2, engine_power_watts: { openvino: 12 },
    });
    expect((await screen.findByTestId('cost-settings-rate')).textContent).toContain('set for this tenant');
  });

  it('refuses a bad price before the round trip', async () => {
    render(CostSettingsPanel, { tenantID: 'acme' });
    await screen.findByTestId('cost-settings-rate');
    await fireEvent.input(document.getElementById('cost-settings-price') as HTMLInputElement, { target: { value: '-1' } });
    await fireEvent.click(screen.getByText('Save'));
    expect((await screen.findByRole('alert')).textContent).toContain('non-negative');
    expect(setCostSettings).not.toHaveBeenCalled();
  });

  it('clears the form and disables Save while the next tenant is still loading', async () => {
    getCostSettings.mockResolvedValueOnce({
      settings: { tenant_id: 'acme', electricity_price_per_kwh: 0.3 },
      electricity: { price_per_kwh: 0.3, source: 'tenant_settings' },
    });
    const { rerender } = render(CostSettingsPanel, { tenantID: 'acme' });
    await screen.findByTestId('cost-settings-rate');
    expect((document.getElementById('cost-settings-price') as HTMLInputElement).value).toBe('0.3');

    // beta's read never arrives.
    getCostSettings.mockReturnValueOnce(new Promise(() => {}));
    await rerender({ tenantID: 'beta' });
    await waitFor(() => expect((document.getElementById('cost-settings-price') as HTMLInputElement).value).toBe(''));
    expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(true);
    await fireEvent.click(screen.getByText('Save'));
    expect(setCostSettings).not.toHaveBeenCalled();
  });

  it('keeps Save disabled when the next tenant\'s read fails', async () => {
    const { rerender } = render(CostSettingsPanel, { tenantID: 'acme' });
    await screen.findByTestId('cost-settings-rate');
    getCostSettings.mockRejectedValueOnce(Object.assign(new Error('down'), { status: 503 }));
    await rerender({ tenantID: 'beta' });
    expect(await screen.findByRole('alert')).toBeTruthy();
    expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(true);
    await fireEvent.click(screen.getByText('Save'));
    expect(setCostSettings).not.toHaveBeenCalled();
  });

  it('reports a read that failed instead of showing a rate', async () => {
    getCostSettings.mockRejectedValue(Object.assign(new Error('forbidden'), { status: 403 }));
    render(CostSettingsPanel, { tenantID: 'acme' });
    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.queryByTestId('cost-settings-rate')).toBeNull();
  });
});
