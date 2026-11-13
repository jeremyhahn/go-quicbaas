### Fixed

- The web console's Tenant Barrier view no longer breaks after a Shamir initialization. The tenant init response is share-free (`threshold`, `total_shares`, `share_count`), so the view now shows how many shares were sealed to Security Officer enclaves rather than trying to reveal plaintext shares. The TypeScript SDK's `TenantBarrierInitResponse` drops the never-sent `shares` field and gains `share_count` (#1888).
