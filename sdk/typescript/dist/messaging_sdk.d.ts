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
export interface MessagingClient {
    listChannels(req?: ListChannelsRequest): Promise<ListChannelsResponse>;
    createChannel(req: CreateChannelRequest): Promise<CreateChannelResponse>;
    deleteChannel(name: string): Promise<DeleteChannelResponse>;
    publish(req: PublishRequest): Promise<PublishResponse>;
}
//# sourceMappingURL=messaging_sdk.d.ts.map