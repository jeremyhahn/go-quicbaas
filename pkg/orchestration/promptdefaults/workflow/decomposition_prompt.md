Decompose the following task into discrete work items.

Task: ${task}

${projects}${backends}Respond with a JSON array of work items:
[
  {
    "id": "unique-id",
    "project_id": "target-project-id",
    "title": "Short title",
    "description": "Detailed description",
    "priority": 1,
    "dependencies": ["id-of-dependency"],
    "backend": "backend-name",
    "required_capabilities": ["persistent_sessions"]
  }
]
