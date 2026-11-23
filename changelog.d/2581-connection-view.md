### Added

- Web UI: a Settings > Connection view shows the transport and daemon address the UI uses, and lets you point the browser's REST client at another daemon. The choice is kept in browser storage, and you are asked to confirm before requests and your session token go to another origin; an address on another origin is saved only when the page can reach it. Applying or resetting the address reloads the UI, and sign-in, session checks and sign-out stay on the page's own daemon (go-quicbaas#2581).
