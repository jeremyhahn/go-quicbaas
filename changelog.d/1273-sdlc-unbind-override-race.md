### Fixed

- **A reviewer override set during an unbind no longer reaches the next
  binding (go-quicbaas#1273).** Setting an override checked the project's
  binding, then wrote the override; unbinding deleted the binding, then
  cleared the overrides. An override whose check passed before an unbind and
  whose write landed after it survived, and the project's next binding
  silently ran its reviews with it. A binding now carries an `epoch` the
  store draws when the project is bound and keeps across updates. Each
  override records the epoch it was set under, and reviewer selection ignores
  an override whose epoch is not the current binding's.
