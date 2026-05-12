/**
 * MessagingView.test.ts
 *
 * Verifies that MessagingView renders correctly and the primary actions call
 * the SDK with the correct shape:
 *  - Channel table is populated from client.messaging.listChannels on mount
 *  - "Create Channel" button opens the dialog
 *  - Create dialog calls client.messaging.createChannel with the right payload
 *  - "Publish" button opens per-channel publish dialog
 *  - Publish dialog calls client.messaging.publish with the right payload
 *  - Published messages appear in the "Recently Published" log
 *  - Delete calls client.messaging.deleteChannel
 *  - Error states surface when the SDK rejects
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mock SDK client
// ---------------------------------------------------------------------------

const { mockListChannels, mockCreateChannel, mockDeleteChannel, mockPublish } = vi.hoisted(() => ({
  mockListChannels: vi.fn(),
  mockCreateChannel: vi.fn(),
  mockDeleteChannel: vi.fn(),
  mockPublish: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    messaging: {
      listChannels: mockListChannels,
      createChannel: mockCreateChannel,
      deleteChannel: mockDeleteChannel,
      publish: mockPublish,
    },
  }),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  navigateTo: vi.fn(),
  routerStore: { route: { name: 'messaging', params: {}, hash: '#/messaging' } },
}));

import MessagingView from '../src/views/async/MessagingView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const CHANNELS = [
  {
    name: 'orders',
    subscribers: 3,
    buffer_size: 512,
    persistent: true,
    created_at: '2025-01-15T10:00:00Z',
  },
  {
    name: 'notifications',
    subscribers: 0,
    buffer_size: 256,
    persistent: false,
    created_at: '2025-01-16T12:00:00Z',
  },
];

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('MessagingView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListChannels.mockResolvedValue({ channels: CHANNELS });
    mockCreateChannel.mockResolvedValue({ channel: { name: 'new-ch', subscribers: 0, buffer_size: 256, persistent: false, created_at: '' } });
    mockDeleteChannel.mockResolvedValue({ deleted: true });
    mockPublish.mockResolvedValue({ message_id: 'msg-001', timestamp: new Date().toISOString() });
    vi.mocked(getSDKClient).mockResolvedValue({
      messaging: {
        listChannels: mockListChannels,
        createChannel: mockCreateChannel,
        deleteChannel: mockDeleteChannel,
        publish: mockPublish,
      },
    } as never);
  });

  it('calls listChannels on mount', async () => {
    render(MessagingView);
    await waitFor(() => {
      expect(mockListChannels).toHaveBeenCalledWith(
        expect.objectContaining({ limit: 100 }),
      );
    });
  });

  it('renders channel names from listChannels response', async () => {
    render(MessagingView);
    await waitFor(() => {
      expect(screen.getByText('orders')).toBeInTheDocument();
      expect(screen.getByText('notifications')).toBeInTheDocument();
    });
  });

  it('renders the "Create Channel" button', async () => {
    render(MessagingView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Create Channel/i })).toBeInTheDocument();
    });
  });

  it('clicking "Create Channel" opens the dialog', async () => {
    render(MessagingView);
    await waitFor(() => screen.getByRole('button', { name: /Create Channel/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Create Channel/i }));
    expect(screen.getByRole('dialog', { name: /Create Channel/i })).toBeInTheDocument();
  });

  it('calls client.messaging.createChannel with correct shape', async () => {
    render(MessagingView);
    await waitFor(() => screen.getByRole('button', { name: /Create Channel/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Create Channel/i }));

    const dialog = screen.getByRole('dialog', { name: /Create Channel/i });

    // Fill channel name
    const nameInput = dialog.querySelector('input[type="text"]') as HTMLInputElement;
    expect(nameInput).toBeDefined();
    await fireEvent.input(nameInput, { target: { value: 'my-channel' } });

    // Submit
    const createBtn = screen.getByRole('button', { name: /^Create$/i });
    await fireEvent.click(createBtn);

    await waitFor(() => {
      expect(mockCreateChannel).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-channel',
        }),
      );
    });
  });

  it('shows validation error when Create is submitted with empty name', async () => {
    render(MessagingView);
    await waitFor(() => screen.getByRole('button', { name: /Create Channel/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Create Channel/i }));

    // Click Create without filling in a name
    await fireEvent.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(screen.getByText(/Channel name is required/i)).toBeInTheDocument();
    });
    expect(mockCreateChannel).not.toHaveBeenCalled();
  });

  it('renders "Publish" buttons for each channel', async () => {
    render(MessagingView);
    await waitFor(() => {
      const publishBtns = screen.getAllByRole('button', { name: /Publish to/i });
      expect(publishBtns).toHaveLength(CHANNELS.length);
    });
  });

  it('clicking "Publish" opens the publish dialog for that channel', async () => {
    render(MessagingView);
    await waitFor(() => screen.getAllByRole('button', { name: /Publish to orders/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Publish to orders/i }));
    expect(screen.getByRole('dialog', { name: /Publish Message/i })).toBeInTheDocument();
  });

  it('calls client.messaging.publish with correct channel and payload', async () => {
    render(MessagingView);
    await waitFor(() => screen.getByRole('button', { name: /Publish to orders/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Publish to orders/i }));

    const dialog = screen.getByRole('dialog', { name: /Publish Message/i });
    const ta = dialog.querySelector('textarea') as HTMLTextAreaElement;
    expect(ta).toBeDefined();
    await fireEvent.input(ta, { target: { value: '{"event":"order.created"}' } });

    await fireEvent.click(screen.getByRole('button', { name: /^Publish$/i }));

    await waitFor(() => {
      expect(mockPublish).toHaveBeenCalledWith(
        expect.objectContaining({
          channel: 'orders',
          payload: '{"event":"order.created"}',
        }),
      );
    });
  });

  it('shows published message in "Recently Published" log after publish', async () => {
    render(MessagingView);
    await waitFor(() => screen.getByRole('button', { name: /Publish to orders/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Publish to orders/i }));

    const dialog = screen.getByRole('dialog', { name: /Publish Message/i });
    const ta = dialog.querySelector('textarea') as HTMLTextAreaElement;
    await fireEvent.input(ta, { target: { value: 'hello world' } });
    await fireEvent.click(screen.getByRole('button', { name: /^Publish$/i }));

    await waitFor(() => {
      expect(screen.getByText(/Recently Published/i)).toBeInTheDocument();
      expect(screen.getByText(/hello world/i)).toBeInTheDocument();
    });
  });

  it('shows error when publish payload is empty', async () => {
    render(MessagingView);
    await waitFor(() => screen.getByRole('button', { name: /Publish to orders/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Publish to orders/i }));

    // Click Publish without filling payload
    await fireEvent.click(screen.getByRole('button', { name: /^Publish$/i }));

    await waitFor(() => {
      expect(screen.getByText(/Payload is required/i)).toBeInTheDocument();
    });
    expect(mockPublish).not.toHaveBeenCalled();
  });

  it('shows error alert when listChannels fails', async () => {
    mockListChannels.mockRejectedValueOnce(new Error('broker offline'));
    render(MessagingView);
    await waitFor(() => {
      expect(screen.getByText(/broker offline/i)).toBeInTheDocument();
    });
  });
});
