### Security

- An SDLC escalation after failed verification no longer posts the commands' output (redacted or not) on the forge issue. The comment lists each failed command, its exit status and how many bytes of output were withheld, and names the job whose `sdlc:verification` audit record holds the failing lines (#213).
