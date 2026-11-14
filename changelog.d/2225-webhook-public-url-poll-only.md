### Fixed

- **A poll-only node starts without `sdlc.webhook.public_url`
  (go-quicbaas#2225).** An empty value now loads, and the node onboards the
  bindings that name no webhook secret while a malformed value is still
  refused at load. On such a node, onboarding refuses a binding that names a
  webhook secret with a `WebhookPublicURLMissingError` (matching
  `ErrWebhookPublicURLMissing`), and conformance reports it as a
  `webhook_missing` finding.
