# Contributing

Thank you for helping improve Volcengine Agent Development Kit for Go. This guide explains how to choose the right version line, prepare a change, and share fixes between V1 and V2.

中文版本: [CONTRIBUTING_zh.md](CONTRIBUTING_zh.md)

## Choose The Right Version

VeADK Go maintains two major versions:

| Version | Branch | Module path | ADK dependency | Contribution policy |
| --- | --- | --- | --- | --- |
| V2 | `main` | `github.com/volcengine/veadk-go/v2` | `google.golang.org/adk/v2` | Active development. New features and breaking changes go here. |
| V1 | `release/v1` | `github.com/volcengine/veadk-go` | `google.golang.org/adk` | Maintenance only. Security fixes, critical bug fixes, and low-risk compatibility fixes go here. |

Use V2 for new work unless you are fixing an issue that affects existing V1 users.

## What Can Be Shared Between V1 And V2

Some changes should be applied to both version lines, while others should stay on V2.

| Change type | V2 | V1 |
| --- | --- | --- |
| Security fix | Yes | Yes |
| Critical production bug fix | Yes | Usually yes |
| Low-risk compatibility fix | Yes | Case by case |
| New feature | Yes | No |
| Breaking API change | Yes | No |
| ADK v2 or A2A v2 migration | Yes | No |
| Refactor without user-visible fix | Yes, if useful | No |
| Documentation | Yes | Yes, when it applies to V1 |

When a fix affects both versions, make the change on V2 first, then backport the smallest compatible patch to V1. Do not merge V2 into `release/v1`.

## Branch And Pull Request Workflow

For V2 changes:

```bash
git checkout main
git pull
git checkout -b feat/your-change
```

For V1 fixes:

```bash
git checkout release/v1
git pull
git checkout -b fix/v1-your-change
```

For fixes shared by V1 and V2:

1. Open the primary pull request against `main`.
2. After the V2 fix is accepted, cherry-pick or manually backport the minimal patch onto `release/v1`.
3. Open a separate V1 pull request and mention the V2 pull request.

Keep V1 and V2 pull requests separate so reviewers can verify the correct import paths, dependencies, and compatibility rules for each version line.

## Compatibility Rules

V2 code must use V2 module paths:

```go
import (
	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"google.golang.org/adk/v2/agent"
)
```

V1 code must use V1 module paths:

```go
import (
	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"google.golang.org/adk/agent"
)
```

Do not mix V1 and V2 import paths in the same version line. In particular:

- Do not add `github.com/volcengine/veadk-go/v2` imports to `release/v1`.
- Do not add `google.golang.org/adk/v2` imports to `release/v1`.
- Do not add V1 ADK or A2A APIs to new V2 code.
- Keep examples and README snippets aligned with the branch they live on.

## Development Checks

Before sending a pull request, run the checks that match your change:

```bash
gofmt -w <changed-go-files>
go test ./...
```

For dependency or module path changes, also inspect:

```bash
go mod tidy
git diff --check
```

For V2 work, check that changed Go files use `/v2` import paths where required. For V1 work, check that no `/v2` VeADK or ADK imports were introduced.

## Pull Request Checklist

Before requesting review, make sure:

- The pull request targets the correct branch.
- The change is scoped to one version line unless it is a deliberate backport.
- Tests or examples cover the behavior when the change affects runtime behavior.
- Public APIs, configuration, and examples are documented when they change.
- Security issues are reported through the process in `SECURITY.md`, not through public issues.

## Releases

V2 releases use tags such as `v2.0.0` and later.

V1 releases use tags such as `v1.0.1` and later from the `release/v1` branch.

Backports should receive their own V1 patch release when they are important for V1 users.
