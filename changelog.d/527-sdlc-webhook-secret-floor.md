### Security

- SDLC project bindings now refuse a webhook secret shorter than 14 bytes (112 bits, the FIPS 140-3 HMAC key floor). Onboarding and webhook ingest return a typed `WebhookSecretTooShortError` (matching `ErrWebhookSecretTooShort` and `ErrWebhookSecretMissing`, so a delivery is answered 401), and the Gitea and GitHub `VerifyWebhook` refuse such a key as unverified instead of panicking under `GODEBUG=fips140=only`. Secrets the onboarding flow generates are unaffected (#527).
