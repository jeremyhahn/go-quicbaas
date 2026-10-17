### Fixed

- **A spawned child job no longer goes missing from its parent's status
  (go-quicbaas#1474).** A JobRun status publish that raced `AddChildJob` (or
  `RecordParent`) could store a snapshot built before the change, overwriting
  the republish that carried it, so `Status().ChildJobIDs` lacked the child
  until the next publish. The publish now copies the lineage and stores the
  status under the same lock that the lineage writers hold.
