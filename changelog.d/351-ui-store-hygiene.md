### Fixed

- **A session's history spinner is its own (go-quicbaas#351).** The UI session
  store held one `historyLoading` flag for every session, so when a second
  session was opened in the same tenant while the first was still loading, the
  first load finishing cleared the second's spinner. The flag is now the
  in-flight load's own token, cleared only by the load that set it, and the
  history cursor is keyed to the session it pages and pinned before a request
  goes out.

- **Sidebar dashboard-label test no longer times out under suite load
  (go-quicbaas#349).** It read `MainApp.svelte` through dynamic
  `node:*` imports inside each case; it now reads the file once at
  collection through static imports.
