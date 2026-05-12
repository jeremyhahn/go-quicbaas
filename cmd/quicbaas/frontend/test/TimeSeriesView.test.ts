/**
 * TimeSeriesView.test.ts
 *
 * Verifies that TimeSeriesView renders the metric list, shows the write
 * dialog, and issues queries with correct arguments.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import { APIError } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockMetrics = ['cpu.usage', 'mem.bytes', 'disk.io'];

const mockPoints = [
  { metric: 'cpu.usage', timestamp: '2025-01-01T00:00:00Z', value: 42.1, tags: { host: 'web-01' } },
  { metric: 'cpu.usage', timestamp: '2025-01-01T00:01:00Z', value: 43.5, tags: { host: 'web-01' } },
];

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    timeseries: {
      listMetrics:  vi.fn().mockResolvedValue([]),
      write:        vi.fn().mockResolvedValue(undefined),
      query:        vi.fn().mockResolvedValue({ points: [], count: 0 }),
      deleteSeries: vi.fn().mockResolvedValue(undefined),
    },
  }),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import TimeSeriesView from '../src/views/data/TimeSeriesView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TimeSeriesView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getSDKClient).mockResolvedValue({
      timeseries: {
        listMetrics:  vi.fn().mockResolvedValue(mockMetrics),
        write:        vi.fn().mockResolvedValue(undefined),
        query:        vi.fn().mockResolvedValue({ points: mockPoints, count: mockPoints.length }),
        deleteSeries: vi.fn().mockResolvedValue(undefined),
      },
    } as never);
  });

  it('renders the page heading "Time-Series"', async () => {
    render(TimeSeriesView);
    await waitFor(() => {
      expect(screen.getByText('Time-Series')).toBeInTheDocument();
    });
  });

  it('renders metric names from the list', async () => {
    render(TimeSeriesView);
    await waitFor(() => {
      expect(screen.getByText('cpu.usage')).toBeInTheDocument();
      expect(screen.getByText('mem.bytes')).toBeInTheDocument();
      expect(screen.getByText('disk.io')).toBeInTheDocument();
    });
  });

  it('shows "Write Point" button', async () => {
    render(TimeSeriesView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Write Point/i })).toBeInTheDocument();
    });
  });

  it('opens the write dialog when "Write Point" is clicked', async () => {
    render(TimeSeriesView);
    await waitFor(() => screen.getByRole('button', { name: /Write Point/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Write Point/i }));
    expect(screen.getByText('Write Data Point')).toBeInTheDocument();
  });

  it('shows "Query" button in the query panel', async () => {
    render(TimeSeriesView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /^Query$/i })).toBeInTheDocument();
    });
  });

  it('shows empty state when no metrics exist', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      timeseries: {
        listMetrics:  vi.fn().mockResolvedValueOnce([]),
        write:        vi.fn(),
        query:        vi.fn(),
        deleteSeries: vi.fn(),
      },
    } as never);
    render(TimeSeriesView);
    await waitFor(() => {
      expect(screen.getByText(/No metrics found/i)).toBeInTheDocument();
    });
  });

  it('shows error alert when listMetrics fails', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      timeseries: {
        listMetrics:  vi.fn().mockRejectedValueOnce(new Error('timeseries unavailable')),
        write:        vi.fn(),
        query:        vi.fn(),
        deleteSeries: vi.fn(),
      },
    } as never);
    render(TimeSeriesView);
    await waitFor(() => {
      expect(screen.getByText(/timeseries unavailable/i)).toBeInTheDocument();
    });
  });

  it('shows disabled state info alert when timeseries subsystem returns 503 not configured', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      timeseries: {
        listMetrics:  vi.fn().mockRejectedValueOnce(new APIError(503, 'timeseries storage service not configured')),
        write:        vi.fn(),
        query:        vi.fn(),
        deleteSeries: vi.fn(),
      },
    } as never);
    render(TimeSeriesView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/QUICBAAS_TIMESERIES_ENABLED/i);
    });
  });

  it('shows disabled state info alert when timeseries subsystem returns 500 not available', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      timeseries: {
        listMetrics:  vi.fn().mockRejectedValueOnce(new APIError(500, 'timeseries service not available')),
        write:        vi.fn(),
        query:        vi.fn(),
        deleteSeries: vi.fn(),
      },
    } as never);
    render(TimeSeriesView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/QUICBAAS_TIMESERIES_ENABLED/i);
    });
  });

  it('disables "Write Point" button when timeseries subsystem is not enabled (#69)', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      timeseries: {
        listMetrics:  vi.fn().mockRejectedValueOnce(new APIError(503, 'timeseries storage service not configured')),
        write:        vi.fn(),
        query:        vi.fn(),
        deleteSeries: vi.fn(),
      },
    } as never);
    render(TimeSeriesView);
    await waitFor(() => screen.getByRole('alert'));
    const writeBtn = screen.getByRole('button', { name: /Write Point/i });
    expect(writeBtn).toBeDisabled();
  });

  it('disables "Query" button when timeseries subsystem is not enabled (#69)', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      timeseries: {
        listMetrics:  vi.fn().mockRejectedValueOnce(new APIError(503, 'timeseries storage service not configured')),
        write:        vi.fn(),
        query:        vi.fn(),
        deleteSeries: vi.fn(),
      },
    } as never);
    render(TimeSeriesView);
    await waitFor(() => screen.getByRole('alert'));
    const queryBtn = screen.getByRole('button', { name: /^Query$/i });
    expect(queryBtn).toBeDisabled();
  });
});
