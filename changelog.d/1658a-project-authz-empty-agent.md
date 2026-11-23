### Security

- **A project-scoped authorization that names no agent is refused** (#1658).
  It no longer passes as if the task were unscoped; a task with no project is
  unchanged.
