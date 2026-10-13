### Changed

- **A 3-node integration proof that cluster-wide scheduler jobs fire once
  per tick (go-quicbaas#105).** The cluster integration config now enables
  the scheduler. The new `make integration-test-cluster-scheduler` target
  checks run records on every node. First, a node-affine job runs only on
  its node. Next, a cluster-wide job fires once per period with matching
  history on all nodes. Then the target stops the leader, and the job
  resumes under the new leader with no duplicate or missed firing. Cluster
  test readiness now accepts any live node, so a stopped node1 no longer
  fails the suite at start-up.
