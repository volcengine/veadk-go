# Local Skill Runtime security boundary

`NewLocalSkillToolset` deliberately enables filesystem writes and local process
execution. It is intended for trusted Skills running inside an existing
container or sandbox. It is not an operating-system sandbox.

## Enforced by the SDK

- Each ADK session receives a hashed workspace containing `skills/`, `uploads/`,
  `tmp/`, and `outputs/`.
- File-tool paths must be relative. `os.Root` confines reads and writes to the
  workspace, rejects external symbolic-link traversal, and regular-file checks
  reject directories and devices.
- File writes and edits use same-workspace temporary files followed by atomic
  rename. Per-session locking prevents concurrent edits from losing updates.
- File size, aggregate workspace size, command duration, argument count, and
  captured stdout/stderr are bounded.
- Commands run in a process group. Cancellation and timeout kill that group.
- Child processes inherit only a small default environment allowlist. Additional
  variables require explicit configuration.
- Audit events contain the session, command, and description only as hashes;
  stdout, stderr, arguments, environment values, and raw session IDs are absent.
- Explicit session cleanup, opportunistic TTL cleanup, and toolset `Close` avoid
  unbounded workspace retention.

## Not enforced by the SDK

- `bash` and Skill scripts run with the same UID and kernel privileges as the Go
  process. They can access container paths outside the workspace, open network
  connections, and invoke installed programs.
- The `skills/` view is read-only through SDK file tools, but a command with
  sufficient filesystem permission can modify the underlying Skill source.
- Workspace accounting detects and terminates commands which exceed the logical
  quota, but it is not a filesystem or cgroup quota and cannot prevent a short
  write burst.
- Process-group termination cannot contain a malicious child which deliberately
  creates a new session or uses another escape available in the container.
- Environment filtering does not make mounted credential files or same-UID
  process state inaccessible.

## Deployment requirements

Run the agent as an unprivileged user in a container with a read-only root
filesystem where practical. Mount Skill sources read-only, give the workspace a
dedicated size-limited volume, avoid host sockets and broad credentials, and
apply the required network policy. Use a stronger process/container sandbox for
untrusted Skill archives or instructions.

Values in `LocalRuntimeConfig.Environment` are intentionally granted to every
command and may be printed by that command. Do not place credentials there
unless the Skill is authorized to use and disclose them within the container's
security boundary.
