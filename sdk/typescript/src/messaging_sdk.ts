// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// ---------------------------------------------------------------------------
// Messaging domain types
// ---------------------------------------------------------------------------

export interface MessagingChannel {
  name: string;
  buffer_size: number;
  max_subscribers: number;
  persistent: boolean;
  ttl_seconds: number;
  subscribers: number;
  created_at: string;
  description?: string;
}

export interface ListChannelsRequest {
  limit?: number;
  cursor?: string;
  name_prefix?: string;
}

export interface ListChannelsResponse {
  channels: MessagingChannel[];
  next_cursor: string;
  has_more: boolean;
  total_count: number;
}

export interface CreateChannelRequest {
  name: string;
  buffer_size?: number;
  max_subscribers?: number;
  persistent?: boolean;
  ttl_seconds?: number;
  description?: string;
}

export interface CreateChannelResponse {
  channel: MessagingChannel;
}

export interface DeleteChannelResponse {
  name: string;
  deleted: boolean;
}

export interface PublishRequest {
  channel: string;
  payload: string;
  dedup_key?: string;
  metadata?: Record<string, string>;
}

export interface PublishResponse {
  channel: string;
  message_id: string;
  timestamp: string;
  is_duplicate: boolean;
}

export interface MessagingMessage {
  id: string;
  channel: string;
  payload: string;
  timestamp: string;
  metadata?: Record<string, string>;
}

// ---------------------------------------------------------------------------
// MessagingClient interface
// ---------------------------------------------------------------------------

// MessagingClient exposes channel and message operations.
// Maps to /api/v1/messaging/* REST endpoints.
export interface MessagingClient {
  // listChannels returns all notification channels.
  // GET /api/v1/messaging/channels
  listChannels(req?: ListChannelsRequest): Promise<ListChannelsResponse>;

  // createChannel creates a new notification channel.
  // POST /api/v1/messaging/channels
  createChannel(req: CreateChannelRequest): Promise<CreateChannelResponse>;

  // deleteChannel removes a channel and disconnects all subscribers.
  // DELETE /api/v1/messaging/channels/:name
  deleteChannel(name: string): Promise<DeleteChannelResponse>;

  // publish sends a message to a channel.
  // POST /api/v1/messaging/publish/:channel
  publish(req: PublishRequest): Promise<PublishResponse>;
}
