# quicbaas version

Display version information for QuicBaaS.

## Command

### version

Display detailed version information.

```bash
quicbaas version
```

#### Flags
- `-s, --short`: Print only the version number

#### Examples

```bash
# Display full version information
quicbaas version

# Display only version number
quicbaas version --short

# Display version number (short flag)
quicbaas version -s
```

## Output

### Full Version (default)

```
QuicBaaS Version: 1.0.0
Git Commit: a1b2c3d4e5f6
Build Timestamp: 2025-12-19T10:30:00Z
Go Version: go1.21.5
Platform: linux/amd64
```

### Short Version

```
1.0.0
```

## Version Information

The version command displays:
- **Version Number**: Semantic version (e.g., 1.0.0)
- **Git Commit**: Git commit hash from build
- **Build Timestamp**: When the binary was built
- **Go Version**: Go compiler version used
- **Platform**: Operating system and architecture

## Notes

- Version information is embedded at build time
- Useful for bug reports and compatibility checking
- Short flag is useful for scripting and automation
- Platform shows OS (linux, darwin, windows) and architecture (amd64, arm64)
