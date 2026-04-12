# Terraform Preflight (`tf-preflight`)

Terraform Preflight CLI for Terraform + provider plans, with Azure support implemented first.
It runs before `terraform apply` and verifies deployability signals from both
HCL and plan output.

## Stack

This implementation is Go-based and lives under:

- `cmd/preflight` CLI entrypoint
- `internal/discovery` HCL + plan parsing
- `internal/azure` REST checks against Azure management endpoints
- `internal/report` report rendering

A thin wrapper script is provided at `scripts/tf-preflight` (preferred) and
`scripts/preflight_check` (legacy alias for compatibility).

## Install

Install in your local `$PATH`:

```bash
make install
# optionally in system path
sudo make install-system
```

The script installs `tf-preflight` to `$(HOME)/.local/bin` by default.
If that directory is not in your `PATH`, add it:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Quick bootstrap with curl

From a local clone:

```bash
./scripts/install.sh
```

From a remote URL (upstream, no env vars required):

```bash
bash -c 'curl -fsSL https://raw.githubusercontent.com/tf-preflight/tf-preflight/main/scripts/install.sh | bash'
```

From a remote URL (forks with explicit repo):

```bash
PRE_FLIGHT_REPO=<owner>/<repo> bash -c 'curl -fsSL https://raw.githubusercontent.com/<owner>/<repo>/main/scripts/install.sh | bash'
```

`PRE_FLIGHT_VERSION` defaults to resolving the repository's latest release tag via the GitHub API. If a pinned tag is not provided and release download fails (for example if assets are not published yet), the script falls back to cloning the repository and building from source.

You can also pin a versioned release:

```bash
PRE_FLIGHT_REPO=tf-preflight/tf-preflight PRE_FLIGHT_VERSION=v1.0.0 bash -c 'curl -fsSL https://raw.githubusercontent.com/tf-preflight/tf-preflight/main/scripts/install.sh | bash'
```

For forks, replace `tf-preflight/tf-preflight` with your GitHub `owner/repo`:

```bash
PRE_FLIGHT_REPO=<owner>/<repo> bash -c 'curl -fsSL https://raw.githubusercontent.com/<owner>/<repo>/main/scripts/install.sh | bash'
```

## Command

```bash
# Either use a binary plan OR auto-plan mode
./scripts/tf-preflight scan --tf-dir /path/to/task06 --plan /path/to/plan.json
./scripts/tf-preflight scan --tf-dir /path/to/task06 --auto-plan
./scripts/tf-preflight scan --tf-dir /path/to/task06 --auto-plan --output json --report-path /tmp/report.json
# Interactive guided mode (current directory)
./scripts/tf-preflight scan --interactive

# Control CI behavior
./scripts/tf-preflight scan --tf-dir /path/to/task06 --auto-plan --severity-threshold warn
```

### Options

- `--tf-dir` required
- `--plan` optional path to a Terraform plan (JSON or binary `.tfplan`)
- `--auto-plan` when plan path is not supplied
- `--interactive` run guided interactive prompt flow (directory defaults to `.` when omitted)
- `--subscription-id` optional override
- `--severity-threshold` `warn|error` (default: `error`)
- `--output` `text|json` (default: `text`)
- `--report-path` optional report artifact path
- `--verbose` enables streaming output by default in interactive mode and detailed command output

## What it checks

1. Provider registration by namespace (Azure REST `/providers/{namespace}`)
2. Location availability in subscription (`/subscriptions/{id}/locations`)
3. Quota checks from usages endpoints when mappings are available
4. Safe existence probes for planned names on create/update
5. Unknown resource types and unresolved/static-unknown locations are reported as warnings
6. Local module imports are validated:
   - verifies local module source paths exist
   - verifies local module directories contain `.tf` files
   - flags dynamic/unresolved module sources
   - warns for module directories under `modules/` that are not imported
   - reports local module directories discovered under root `modules/` as `MODULE_UNUSED_DIR` when not imported

### Module import diagnostics

- `MODULE_SOURCE_NOT_FOUND` / `MODULE_SOURCE_INVALID` (error): local module source path missing or not a directory.
- `MODULE_SOURCE_EMPTY` (error): imported module path exists but has no `.tf` files.
- `MODULE_SOURCE_UNKNOWN` (warn): module source could not be evaluated statically.
- `MODULE_SOURCE_UNREADABLE` (warn): module source exists but cannot be inspected.
- `MODULE_UNUSED_DIR` (warn): directory under `<tf-dir>/modules/` exists but is not imported.

### HCL discovery behavior

- Static discovery is intentionally permissive and uses partial parsing to avoid false positives from provider-specific nested blocks (for example, `features {}` in provider configuration).
- The CLI currently evaluates intent from Terraform `.tf` files during discovery and merges runtime/resolved values from plan output when available.
- `.tfvars` values are not parsed as configuration blocks in discovery and are expected to be represented through plan values or explicit defaults.

## Exit codes

- `0` pass (or no blocking findings for configured threshold)
- `1` fail (checks failed / execution errors)
- `2` usage errors (missing flags, unsupported arguments)

`--interactive` requires a TTY (`stdin` terminal). In automated CI, use explicit `--plan` or `--auto-plan`.

## Status matrix

| Finding type | Trigger | Severity | Threshold fail impact |
|---|---|---|---|
| `error` | invalid location / provider not registered / quota exceeded | hard | always fail |
| `warn` | unsupported type / location unknown / existence probe / quota endpoint unavailable | warning | fail only with `--severity-threshold warn` |
| `none` | all checks clean | pass | pass |

## Authentication and Azure API usage

This tool uses direct Azure REST calls (not the Terraform AzureRM provider SDK). It reads `Authorization: Bearer` tokens and calls endpoints like:

- `/subscriptions/{id}/locations`
- `/subscriptions/{id}/providers/{namespace}`
- namespace usage endpoints used for quota checks

Token resolution order:

1. `AZURE_ACCESS_TOKEN`
2. `ARM_ACCESS_TOKEN`
3. `AZURE_CLI_TOKEN`
4. `az account get-access-token --resource https://management.azure.com...`

So yes: if you are authenticated with `az login`, CLI fallback uses that same session for authorization.

## CLI output and progress

- Text mode displays a lightweight progress line for:
  - loading/parsing Terraform
  - plan resolve
  - candidate preparation
  - Azure checks per resource
  - report generation
- Use `--verbose` to stream `terraform init/plan` command output.
- In JSON mode (`--output json`) output is machine-focused and progress line output is minimized.

## Version and SDK information

- `tf-preflight version` prints build metadata and stack dependencies:
  - Azure query transport: direct `management.azure.com` REST
  - HCL parser: `github.com/hashicorp/hcl/v2@v2.23.0`
  - Terraform value model: `github.com/zclconf/go-cty@v1.15.1`

## Test coverage

Current tests are lightweight and focus on:

- HCL extraction
- plan merge behavior (plan values override static intent)
- report generation
- hard-fail decision helpers

Run:

```bash
go test ./...
```

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
