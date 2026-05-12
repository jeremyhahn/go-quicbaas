# Async → Messaging

## Purpose

The Messaging view manages publish-subscribe channels and deduplication policies for async event distribution. Use this view to create channels, publish messages, subscribe to topics, and configure message dedup and retention settings.

## Layout

The page is structured as a tabbed interface:

**Tabs** — "Channels", "Publish", "Subscribe", "Dedup Policies"

**Tab: Channels**

```
Create Channel:     [+ New Channel]

Channels:

Name                Status      Subscribers   Messages/24h   Actions
────────────────────────────────────────────────────────────────────
user-events         ✓ Active    3             1,245          [⋮]
order-notifications ✓ Active    2             456            [⋮]
analytics-sink      ⚠ Inactive  0             0              [⋮]

[Edit] [Delete] [View Stats]  (per channel)
```

- **Status** — Active (green), Inactive (gray)
- **Subscribers** — count of active subscribers
- **Messages/24h** — throughput indicator
- Three-dot menu for management actions

**Tab: Publish**

```
Channel:     [user-events________]
Message:     [{"event":"login","user":"alice"}___________]
             (JSON or text)
             
Tags:        [source:api, env:prod________]
             (optional; for dedup)

             [Clear] [Publish]

Publish Status:
  Message ID: msg_abc123def456
  Status: ✓ Published
  Subscribers notified: 3
```

- **Channel** — dropdown of available channels
- **Message** — JSON or text content
- **Tags** — optional dedup keys
- **Status** — shows result of publishing

**Tab: Subscribe**

```
Channel:        [user-events________]
Subscriber ID:  [sub_my-app________]  (auto-generated or custom)

                [Subscribe]

Active Subscriptions:

Subscription ID        Channel            Connected   Messages Received
──────────────────────────────────────────────────────────────────────
sub_my-app             user-events        ✓ Yes       156
sub_analytics          user-events        ✓ Yes       156
sub_old-service        order-notifications ✓ No        (offline)

[View Messages] [Unsubscribe]  (per subscription)
```

- Create new subscriptions
- List and manage active subscriptions

**Tab: Dedup Policies**

```
Channel:       [user-events________]
Dedup Mode:    [Content Hash ▼]  (or Tag-based, Time Window, None)

Content Hash:
  Algorithm:   [SHA256 ▼]
  Window:      [5 min ▼]  (time window for dedup)

Tag-based:
  Dedup Tags:  [source, user_id________]
  Window:      [1 hour ▼]

              [Save Policy]

Policy Statistics:
  Duplicates Filtered (24h): 234
  Dedup Efficiency: 15.8%
```

- Configure deduplication by content, tags, or time
- View dedup statistics

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Create Channel | Plus | Registers a new pub-sub channel | `POST /api/v1/messaging/channels` |
| Delete Channel | Trash | Removes a channel (show warning if subscribers exist) | `DELETE /api/v1/messaging/channels/{name}` |
| Publish Message | Send | Publishes a message to a channel | `POST /api/v1/messaging/publish/{channel}` |
| Subscribe | Check | Creates a subscription to a channel | `POST /api/v1/messaging/subscribe` |
| Unsubscribe | X | Removes a subscription | `DELETE /api/v1/messaging/subscribe/{id}` |
| Update Dedup Policy | Save | Configures deduplication rules | (dedup config is channel-level) |
| View Channel Stats | Eye | Shows throughput, subscriber count, dedup stats | `GET /api/v1/messaging/stats/{channel}` |

## Walkthrough

### Create a channel and publish messages

1. Open the Messaging view (`#/async/messaging`).
2. Click the **Channels** tab.
3. Click **+ New Channel**.
4. A form appears:

```
Create Channel

Name:        [order-events__________]
Description: [Events from the order service]  (optional)
Retention:   [24 hours ▼]  (or 7d, 30d, infinite)

[Cancel] [Create]
```

5. Enter a channel name: `order-events`.
6. Optionally add a description: "Events from the order service".
7. Set retention to "24 hours".
8. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Channel created: order-events"
✓ Channel appears in the Channels list with status ✓ Active
```

9. Click the **Publish** tab.
10. **Channel** dropdown shows "order-events". Select it (or it defaults if it's the only one).
11. In **Message**, enter a sample message:

```json
{
  "event": "order_placed",
  "order_id": "ORD-12345",
  "user": "alice",
  "amount": 99.99,
  "timestamp": "2026-04-25T14:35:00Z"
}
```

12. In **Tags**, optionally enter: `source:order-service, env:prod`.
13. Click **Publish**.

**Expected outcome:**

```
✓ Publish Status shows:
  Message ID: msg_abc123def456
  Status: ✓ Published
  Subscribers notified: 0 (no subscribers yet)
```

14. Publish a few more messages:

```json
{"event": "order_shipped", "order_id": "ORD-12345", "status": "in_transit"}
{"event": "order_delivered", "order_id": "ORD-12345", "status": "delivered"}
```

### Subscribe to a channel

15. Click the **Subscribe** tab.
16. **Channel** should show "order-events" (or select it).
17. **Subscriber ID** auto-generates something like `sub_abc123`. You can customize it to `sub_my-app`.
18. Click **Subscribe**.

**Expected outcome:**

```
✓ Modal closes (or tab resets)
✓ Success toast: "Subscription created: sub_my-app"
✓ Active Subscriptions table shows:
  sub_my-app    order-events    ✓ Yes    3
  (Messages Received = 3, from the previous publishes)
```

19. Click **[View Messages]** on your subscription.
20. A panel or modal shows the messages received:

```
Messages Received (3):

Message 1:
{
  "event": "order_placed",
  "order_id": "ORD-12345",
  ...
}

Message 2:
{
  "event": "order_shipped",
  ...
}

Message 3:
{
  "event": "order_delivered",
  ...
}
```

### Configure deduplication

21. Click the **Dedup Policies** tab.
22. **Channel** defaults to "order-events".
23. **Dedup Mode** defaults to "Content Hash".
24. Leave **Algorithm** as "SHA256" and **Window** as "5 min".
25. Click **Save Policy**.

**Expected outcome:**

```
✓ Success toast: "Dedup policy updated"
✓ Policy Statistics show (initially):
  Duplicates Filtered (24h): 0
  Dedup Efficiency: 0%
```

26. Publish the same message twice:

```json
{"event": "test_duplicate", "id": "1"}
```

27. The second publish will be deduplicated (filtered by content hash).

**Expected outcome:**

```
✓ Second publish shows:
  Status: ⚠ Deduplicated (not published; matched hash)
✓ Subscribers do not receive a duplicate
✓ Dedup statistics update:
  Duplicates Filtered (24h): 1
  Dedup Efficiency: 0.5%
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Create Channel form shows validation error | Channel name is empty or already exists | Enter a unique channel name |
| Publish button is disabled | Message field is empty | Enter message content (JSON or text) |
| Publish shows "Channel not found" | Channel was deleted or does not exist | Create the channel first via Channels tab |
| Dedup policy does not filter duplicates | Dedup mode is "None" or window has expired | Set Dedup Mode to "Content Hash" or "Tag-based"; adjust window if needed |
| Subscription shows 0 subscribers after publishing | Subscriber list is cached or slow to update | Refresh the page; wait a few seconds |
| Unsubscribe button is disabled | Subscription is already closed or being used | Wait a moment and try again |
| Channel delete fails with "Subscribers exist" | Channel has active subscribers; subscribers must unsubscribe first | Unsubscribe all subscribers before deleting the channel |

## See also

- [CLI: quicbaas messaging](../cli/messaging.md) — manage messaging via CLI
- [handlers_messaging.go](../../pkg/api/rest/handlers_messaging.go) — messaging implementation
- [Async → Scheduler](async-scheduler.md) — scheduled job execution
