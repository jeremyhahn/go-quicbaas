### Added

- **Replicated (drbd) workload volumes** (#1683). The fleet node agent
  accepts the `drbd` volume provider. Each launch promotes its replicated
  volumes to primary after attaching them and before mounting, and demotes
  them after unmounting on stop, remove or a failed start. The promotion is
  recorded first, so a node that stops part way demotes the volume when it
  recovers.
