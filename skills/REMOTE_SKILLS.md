# Remote skills

`RemoteSource` supports AgentKit Skill Spaces. `sp-` SkillHub spaces are
explicitly rejected. Constructors do not read credentials, contact services or create cache
files. `Registry.Refresh(ctx)` discovers metadata; `SkillSnapshot.Get(ctx, name)`
downloads the selected skill on demand.

```go
source, err := skills.NewRemoteSource(skills.RemoteSourceConfig{
    SpaceID: "ss-example",
    Credentials: veauth.NewRoleCredentialSource(veauth.RoleCredentialSourceConfig{}),
})
if err != nil { return err }
cache, err := skills.NewSkillCache(skills.SkillCacheConfig{
    Directory: "/private/agent-cache/skills",
})
if err != nil { return err }
registry, err := skills.NewRegistry(skills.RegistryConfig{
    LocalDirectories: []string{"/app/builtin-skills"},
    Sources: []skills.SourceBinding{{Source: source}},
    Cache: cache,
})
if err != nil { return err }
defer registry.Close()
issues, err := registry.Refresh(ctx)
if err != nil { return err }
_ = issues // Report optional-source failures without logging provider responses.
snapshot := registry.Snapshot()
defer snapshot.Close() // Only after every run using this snapshot finishes.
toolset, err := skilltool.NewRegistrySkillToolset(snapshot)
if err != nil { return err }
// Attach toolset through llmagent.Config.Toolsets, using the same mechanism as
// other ADK toolsets. It exposes list_skills, load_skill and load_skill_resource.
_ = toolset
```

The registry toolset loads instructions and resources. To execute a downloaded
skill, obtain it with `snapshot.Get` and pass it to the application's explicitly
configured executor or local runtime. Loading remote instructions never grants
execution permission by itself. The guarded local runtime in PR #85 is a separate
change and is not required to build this feature.

## Sources and authentication

| Source | Discovery | Archive download |
| --- | --- | --- |
| Ordinary Skill Space | Signed `ListSkillsBySpaceId`, API version `2025-10-30`; the Python contract is unpaginated | TOS bucket/object returned by discovery |

Skill Space accepts `Version` or `SkillVersion`. A revision or object key must
identify immutable content for snapshots to guarantee version pinning. Changing
bytes behind unchanged descriptors requires explicit cache pruning once all
snapshots and the registry's current generation have been released.

Default credentials resolve lazily through the existing Role credential source
(environment, then the mounted IAM file). Explicit `CredentialSource` injection
supports other providers and tests. Credentials are resolved again on each
request, including bounded retries for 429/500/502/503/504; 401/403 are returned
without retry. No credentials or server response bodies are logged by this code.

Set `Endpoint`, `Region`, `Service`, and, when needed, `TOSRegion` and
`TOSEndpoint` explicitly for other deployments, including BytePlus. The SDK does
not interpret `AGENTKIT_SKILL_HOST`, which also names a Sandbox listen address.
HTTP endpoints exist for controlled testing; production endpoints should use
HTTPS. The caller may inject an HTTP client/transport. For TOS, the SDK uses the
client's transport and the operation context timeout.

VeStack-style `UseTemporaryDownloadURL` calls `GenTempTosObjectDownloadUrl`.
It requires an explicit `AllowedDownloadHosts` list, only permits HTTPS download
URLs, never forwards control-plane auth headers, and rejects redirects. Signed
control-plane redirects are also rejected.

## Refresh and ownership

- Local roots win name collisions, in configured order, then remote sources in
  configured order. Duplicates within a remote source fail discovery.
- Every required source must succeed before publishing a refresh. Required
  failure leaves the current generation unchanged.
- `Optional: true` retains that source's previous list on failure and returns a
  `RegistryIssue`. The first failed discovery contributes no entries.
- A snapshot pins metadata, instructions for local skills, and remote descriptor
  choices. Its list is sorted. Local resource files remain application-owned.
- A toolset retains its snapshot. After a successful refresh, construct a new
  toolset/Agent for new runs and drain old runs before closing their snapshots.
  Existing Agents do not silently adopt new generations midway through a run.
- `Close` releases pins. `Cache.Prune(ctx)` removes unpinned revisions and
  abandoned staging directories. It never evicts the registry's current
  generation or live snapshots. Stop refreshes before closing the registry.

## Archive and disk boundaries

The cache directory must be private, application-owned, and writable by only one
`SkillCache` instance/process. It is not an OS sandbox and does not defend against
a malicious process with the same filesystem privileges. Separate tenants and
processes must use separate caches. Direct `Materialize` callers must coordinate
pruning; registry snapshots manage pins automatically.

Defaults are 64 MiB compressed, 256 MiB expanded, 10,000 ZIP entries, and 1 GiB
cache space. Downloads stage on the cache filesystem; validation completes before
atomic rename. ZIP traversal, absolute/Windows paths, symlinks, special files,
duplicate/case-colliding entries and excessive expansion are rejected. Archive
permissions are not trusted. Cached files have SHA-256 manifests; damaged caches
are downloaded again. Failed staging is removed. The quota includes temporary
archive/expanded data and retained revisions; callers must prune unused versions
when space runs out. Materialization is serialized with cancellable waiting to
make quota accounting and duplicate-download behavior deterministic.

ZIP layout follows veadk-python PR #1084 (`5aac81e0`): root `SKILL.md` first,
otherwise the shallowest candidate directory, breaking ties by lexical path.
One wrapper, extra root files, and deeper nested layouts are supported. Legacy
`skill.md` is also accepted. The selected document must parse and its declared
name must match remote metadata. Resources remain relative to the selected
skill root. Temporary ZIPs never remain in the published directory.

## Verification

`examples/skills/remote` discovers skills and optionally loads one resource. It
prints names, byte counts and SHA-256, not document content or credentials:

```sh
go run ./examples/skills/remote \
  -space ss-example -credential-file /var/run/secrets/iam/credential \
  -cache /private/agent-cache/skills \
  -skill example-skill -resource SKILL.md
```

The explicit credential-file flag uses only that file; it cannot accidentally
prefer another key from the environment. Omit it to use the default Role source.

Tests cover fake signed control planes, TOS, temporary URLs, malformed
responses, cancellation, ZIP layouts/security, disk quotas, concurrency, cache
repair/restart, source priority and refresh rollback. The standalone binary
blackbox verifies HTTP discovery/download and cross-process cache reuse. Real
Skill Space smoke results are recorded in the local engineering status document;
SkillHub support is outside this change.
