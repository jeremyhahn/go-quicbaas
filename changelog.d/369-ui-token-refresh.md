### Fixed

- **The web UI reconciles the session identity before it attaches a bearer
  (go-quicbaas#369).** Previously, when a sibling tab signed in while a request
  was being prepared, the request could go out under the sibling's session and
  its response commit under this tab's previous one. It now goes out under the
  session in effect, and the stale response is discarded. A sibling tab's
  renewal of the same login is still adopted without ending the session.
