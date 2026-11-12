### Fixed

- The Go SDK's REST and QUIC transports now send `GetSessionHistory` to the server's JSON session-events page (`GET /api/v1/orchestrator/sessions/{id}/events?follow=false`) instead of a generic route no server serves. `FromSequence` 0 starts at the first event, and `NextSeq` is the server's: `0` when no event follows the page, even when the last page is exactly full (#1847).
