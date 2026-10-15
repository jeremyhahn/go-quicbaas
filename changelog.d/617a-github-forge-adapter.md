### Added

- **SDLC projects can bind to GitHub (go-quicbaas#617).** A binding with
  `forge.type: github` is now accepted: the GitHub adapter implements the
  whole forge surface (issues, labels, pull requests, reviews, check runs and
  commit statuses, Actions rerun, cancel and logs, webhooks, onboarding and
  collaborators) against the GitHub REST API, on github.com or a GitHub
  Enterprise Server API root. A merge names the head it was reviewed at and
  is refused, never retried, when the head moved. Deliveries are verified by
  `X-Hub-Signature-256`. GitHub's native merge queue is not used; its REST API
  has no route for it.

### Changed

- **Webhook deliveries are read by the bound forge's header names.** The event,
  delivery id and signature are taken from the headers of the forge the
  project is bound to. A delivery carrying another forge's headers is refused
  as unsigned.
