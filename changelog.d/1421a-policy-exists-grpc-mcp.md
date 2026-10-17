- IAM: creating a policy whose name is already taken now answers
  `AlreadyExists` on gRPC and the already-exists tool error on MCP, instead of
  an internal error (#1421).
