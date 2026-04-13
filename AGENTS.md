# AGENTS.md

Guidance for AI coding assistants working in `fluxcd/image-automation-controller`. Read this file before making changes.

## Contribution workflow for AI agents

These rules come from [`fluxcd/flux2/CONTRIBUTING.md`](https://github.com/fluxcd/flux2/blob/main/CONTRIBUTING.md) and apply to every Flux repository.

- **Do not add `Signed-off-by` or `Co-authored-by` trailers with your agent name.** Only a human can legally certify the DCO.
- **Disclose AI assistance** with an `Assisted-by` trailer naming your agent and model:
  ```sh
  git commit -s -m "Add support for X" --trailer "Assisted-by: <agent-name>/<model-id>"
  ```
  The `-s` flag adds the human's `Signed-off-by` from their git config — do not remove it.
- **Commit message format:** Subject in imperative mood ("Add feature X" instead of "Adding feature X"), capitalized, no trailing period, ≤50 characters. Body wrapped at 72 columns, explaining what and why. No `@mentions` or `#123` issue references in the commit — put those in the PR description.
- **Trim verbiage:** in PR descriptions, commit messages, and code comments. No marketing prose, no restating the diff, no emojis.
- **Rebase, don't merge:** Never merge `main` into the feature branch; rebase onto the latest `main` and push with `--force-with-lease`. Squash before merge when asked.
- **Pre-PR gate:** `make tidy fmt vet && make test` must pass and the working tree must be clean after codegen. Commit regenerated files in the same PR.
- **Flux is GA:** Backward compatibility is mandatory. Breaking changes to CRD fields, status, CLI flags, metrics, or observable behavior will be rejected. Design additive changes and keep older API versions round-tripping.
- **Copyright:** All new `.go` files must begin with the boilerplate from `hack/boilerplate.go.txt` (Apache 2.0). Update the year to the current year when copying.
- **Spec docs:** New features and API changes must be documented in `docs/spec/v1/imageupdateautomations.md`. Update it in the same PR that introduces the change.
- **Tests:** New features, improvements and fixes must have test coverage. Add unit tests in `internal/controller/*_test.go` and other `internal/*` packages as appropriate. Follow the existing patterns for test organization, fixtures, and assertions. Run tests locally before pushing.

## Code quality

Before submitting code, review your changes for the following:

- **No secrets in logs or events.** Never surface git credentials, GPG keys, cloud provider tokens, or secret contents in error messages, conditions, events, or log lines.
- **No unchecked I/O.** Close HTTP response bodies, file handles, and git working copies in `defer` statements. Check and propagate errors from I/O operations.
- **No path traversal.** `.spec.update.path` is resolved via `cyphar/filepath-securejoin` to prevent directory escapes. Do not replace it with `filepath.Join`. Any new path handling must validate that paths stay within the git working copy root.
- **No command injection.** Do not shell out via `os/exec`. All git I/O goes through `fluxcd/pkg/git/gogit`. Do not call `os/exec git` directly.
- **No hardcoded defaults for security settings.** TLS verification must remain enabled by default; SSH KEX and host-key algorithms are tunable via flags. Git auth settings come from user-provided secrets, not environment variables.
- **GPG key handling.** Signing keys are read from secrets (`git.asc` data key). Never log or surface key material. Passphrase handling must not leak to errors or events.
- **kyaml global state.** The kyaml openAPI schema is a package-level global. `UpdateWithSetters` serialises access and resets it per call. Do not parallelise setter updates without understanding this constraint.
- **Error handling.** Wrap errors with `%w` for chain inspection. Do not swallow errors silently. Return actionable error messages that help users diagnose the issue without leaking internal state.
- **Resource cleanup.** Ensure temporary files, directories, and cloned repos are cleaned up on all code paths (success and error). Use `defer` and `t.TempDir()` in tests.
- **Concurrency safety.** Do not introduce shared mutable state without synchronization. Reconcilers run concurrently; per-object work must be isolated.
- **No panics.** Never use `panic` in runtime code paths. Return errors and let the reconciler handle them gracefully.
- **Minimal surface.** Keep new exported APIs, flags, and environment variables to the minimum needed. Every export is a backward-compatibility commitment.

## Project overview

image-automation-controller is a [Flux GitOps Toolkit](https://fluxcd.io/flux/components/) controller that reconciles `ImageUpdateAutomation` custom resources. For each automation, it:

1. Lists `ImagePolicy` objects (from image-reflector-controller) in the same namespace, optionally filtered by `.spec.policySelector`.
2. Clones the `GitRepository` referenced by `.spec.sourceRef` using the checkout ref from `.spec.git.checkout` or the `GitRepository` default.
3. Scans YAML files under `.spec.update.path` for kyaml setter markers of the form `# {"$imagepolicy": "<namespace>:<policy-name>[:tag|name|digest]"}` and replaces marked field values with the latest image ref reported in each policy's `.status.latestRef`.
4. Commits the changes using the configured author, optional GPG signing key, and optional Go-templated commit message, then pushes to the same branch, a different branch, and/or a refspec as configured in `.spec.git.push`.
5. Records the push SHA, time, and observed policies in the object status.

It works alongside source-controller (for `GitRepository` artifacts) and image-reflector-controller (for image scanning). Only the `Setters` update strategy is currently implemented.

## Repository layout

- `main.go` — manager entrypoint: scheme registration, flags, feature gates, token cache, reconciler wiring.
- `api/` — standalone Go module (`github.com/fluxcd/image-automation-controller/api`) with versioned CRD types. `replace`d locally from `go.mod`.
  - `api/v1/` — current storage version (`image.toolkit.fluxcd.io/v1`): `imageupdateautomation_types.go`, `git.go` (commit/push/checkout specs), `reference.go`, `condition_types.go`, generated `zz_generated.deepcopy.go`.
  - `api/v1beta1/`, `api/v1beta2/` — older versions retained for conversion. Do not remove.
- `config/` — Kustomize bases: `crd/bases` (generated CRDs), `default`, `manager`, `rbac`, `samples`.
- `docs/spec/` — versioned human-readable API docs (`docs/spec/v1/imageupdateautomations.md` is the current reference).
- `docs/api/` — generated API reference (`make api-docs`).
- `hack/` — `boilerplate.go.txt` license header, `api-docs/` template/config.
- `internal/` — controller implementation.
  - `internal/controller` — `ImageUpdateAutomationReconciler`, watch predicates, envtest suite (`suite_test.go`), and test CRDs pulled in via `make test_deps`.
  - `internal/source` — Git source handling: builds `gitSrcCfg` from the `GitRepository` and `GitSpec`, resolves auth via `fluxcd/pkg/runtime/secrets` and `fluxcd/pkg/auth` (including workload identity and GitHub App), performs clone/commit/push via `fluxcd/pkg/git/gogit`, and renders the commit message template (Go `text/template` + `sprig.HermeticTxtFuncMap`).
  - `internal/policy` — `ApplyPolicies` entrypoint: resolves the manifest path with `filepath-securejoin`, enforces the `Setters` strategy, and calls into `internal/update`.
  - `internal/update` — the setter engine: `filereader.go` (screening reader that only loads YAML files containing the shorthand token), `setters.go` (builds kyaml setter schemas and runs the kio pipeline), `filter.go` (`SetAllCallback`), `result.go` (change tracking used by the commit message template).
  - `internal/features` — feature gate names and defaults.
  - `internal/constants` — the `SetterShortHand = "$imagepolicy"` constant.
  - `internal/testutil` — shared test helpers. Test fixtures under `internal/{policy,update,controller}/testdata`.

## APIs and CRDs

- Group: `image.toolkit.fluxcd.io`. Kind: `ImageUpdateAutomation`. Short names: `iua`, `imgupd`, `imgauto`.
- Storage version: `v1` (`//+kubebuilder:storageversion` in `api/v1/imageupdateautomation_types.go`).
- Older versions (`v1beta1`, `v1beta2`) must keep converting cleanly.
- `config/crd/bases/image.toolkit.fluxcd.io_imageupdateautomations.yaml` is generated by `make manifests`. Do not hand-edit.
- `zz_generated.deepcopy.go` files are generated via `make generate`. Do not hand-edit.
- API reference under `docs/api/` is regenerated via `make api-docs`.

## Build, test, lint

All targets live in the top-level `Makefile`. Go version tracks `go.mod`.

- `make tidy` — tidy both the root and `api/` modules.
- `make fmt` / `make vet` — run in both modules.
- `make generate` — `controller-gen object` against `api/` (deepcopy).
- `make manifests` — regenerate CRDs and RBAC under `config/crd/bases` and `config/rbac`.
- `make api-docs` — regenerate `docs/api/v1/image-automation.md`.
- `make manager` — build the controller binary to `build/bin/manager`.
- `make test` — full unit+envtest run. Fetches the required `GitRepository` and `ImagePolicy` CRDs (into `internal/controller/testdata/crds`), installs envtest assets, and runs `go test -race ./... -coverprofile cover.out`.
- `make test-api` — runs tests scoped to the `api/` module only.
- `make install-envtest` — downloads envtest binaries into `build/testbin`.
- `make run` — runs the controller locally against your current kubecontext.
- `make install` / `make uninstall` — apply/remove the CRDs via Kustomize.
- `make docker-build` / `make docker-deploy` — multi-arch build and rollout.

Requires Docker, Kustomize, and an internet connection for the first invocation (to download `controller-gen`, `setup-envtest`, and the source/reflector CRDs pinned via `SOURCE_VER`/`REFLECTOR_VER` from `go.mod`).

## Codegen and generated files

Check `go.mod` and the `Makefile` for current dependency and tool versions. After changing API types or kubebuilder markers, regenerate and commit the results:

```sh
make generate manifests api-docs
```

Generated files (never hand-edit):

- `api/*/zz_generated.deepcopy.go`
- `config/crd/bases/*.yaml`
- `config/rbac/role.yaml`
- `docs/api/v1/image-automation.md`

Load-bearing `replace` in `go.mod` — do not remove:

- `sigs.k8s.io/kustomize/kyaml` pinned via `replace`. Do not bump in isolation from `sigs.k8s.io/kustomize/api`.

Version skew with `fluxcd/image-reflector-controller/api` matters — this controller reads `policy.Status.LatestRef`. Bumping that module may require a matching image-reflector-controller release. `REFLECTOR_VER` and `SOURCE_VER` Make variables are derived from `go.mod` and determine which CRD fixtures are fetched.

Bump `fluxcd/pkg/*` modules as a set. Run `make tidy` after any bump.

## Conventions

- Standard `gofmt`, `go vet`. Exported types, funcs, vars, and constants must carry doc comments; non-trivial unexported declarations too.
- **Errors and logging.** Wrap errors with `%w`. Use the `logr.Logger` from `sigs.k8s.io/controller-runtime/pkg/log`, never `log` or `fmt.Println`. Use the trace-level logger (`fluxcd/pkg/runtime/logger`) for verbose diagnostics.
- **Reconciler.** Single `ImageUpdateAutomationReconciler` patterned on other Flux controllers: `fluxcd/pkg/runtime/patch` with owned conditions (`Ready`, `Reconciling`, `Stalled`), `runtime/conditions`, and `runtime/reconcile` summarization. Emit events via the injected `events.Recorder`.
- **Setter markers.** The shorthand is `$imagepolicy` (see `internal/constants/constants.go`). Markers: `# {"$imagepolicy": "<namespace>:<policy-name>"}` for the full image ref, or with a `:tag`, `:name`, or `:digest` suffix. A colon (not a slash) separates namespace and name — slashes would be parsed as a `$ref` path.
- **File scanning.** `internal/update/filereader.go` implements a `ScreeningLocalReader` that only loads YAML files whose contents contain the `"$imagepolicy"` token, keeping the kyaml pipeline cheap for large repos.
- **Git auth.** Resolved in `internal/source/git.go` via `fluxcd/pkg/runtime/secrets` against the `GitRepository`'s secret ref for HTTPS basic/bearer, SSH keys, and known-hosts. Cloud auth (GitHub App, AWS CodeCommit, Azure DevOps, GCP Source Repositories, workload identity) goes through `fluxcd/pkg/auth` and may use the shared `TokenCache`. SSH KEX and host-key algorithms are tunable via `--ssh-kex-algos` and `--ssh-hostkey-algos` in `main.go`.
- **GPG signing.** If `.spec.git.commit.signingKey.secretRef` is set, the controller reads the ASCII-armored key from the `git.asc` data key (and optional `passphrase`) and wraps the commit with an openpgp entity. No SSH commit-signing support; adding it requires an API change and version bump.
- **Commit message template.** `.spec.git.commit.messageTemplate` is rendered with Go `text/template` + `sprig.HermeticTxtFuncMap` against a `TemplateData` struct exposing `.AutomationObject`, `.Changed` (an `update.Result`), and `.Values` (from `.spec.git.commit.messageTemplateValues`). The legacy `.Updated` and `.Changed.ImageResult` fields have been removed — use `.Changed.FileChanges` / `.Changed.Objects`. Default message: `Update from image update automation`.
- **Push behaviours** via `.spec.git.push`:
  - Omitted → push back to the checkout branch.
  - `branch` → push to a (new or existing) branch based on the checkout branch.
  - `refspec` → push with an explicit refspec; combinable with `branch`.
  - `options` → Git push options forwarded to the server.
  Force-push of push-branches is governed by the `GitForcePushBranch` feature gate (default on). `GitAllBranchReferences` and `GitShallowClone` are on by default; `GitSparseCheckout` and `CacheSecretsAndConfigMaps` are opt-in. Be deliberate before changing these defaults.
- **PR creation.** This controller does not open pull requests itself — users push to a side branch and wire a notification/external bot. Do not add PR-opening logic here.

## Testing

- Envtest lives in `internal/controller/suite_test.go`. It consumes the `GitRepository` and `ImagePolicy` CRDs copied into `internal/controller/testdata/crds` by the `test_deps` Make target. If you run `go test` directly, first run `make test_deps install-envtest` and export `KUBEBUILDER_ASSETS` to the envtest `bin` path (see the `test` target in the Makefile for the exact shell).
- Git integration tests use local bare repositories and the in-process `github.com/fluxcd/pkg/gittestserver` to exercise HTTP(S)/SSH transports without hitting real providers.
- Fixtures for the setter engine live under `internal/update/testdata` and `internal/policy/testdata`. When adding scenarios, mirror the existing YAML tree and golden-file format.
- Run a single test: `make test GO_TEST_ARGS='-run TestName'`.

## Gotchas and non-obvious rules

- `ScreeningLocalReader` skips files that do not contain the `"$imagepolicy"` literal, so markers inside JSON-embedded strings or template files with different quoting will be missed. When supporting new formats, update both the screener and the kio reader.
- The setter key format is `namespace:name[:tag|name|digest]`. A slash separator would be parsed as a JSON `$ref` path and silently not match — easy mistake when copying from other tooling.
- Push branch creation uses `.spec.git.checkout` (or the `GitRepository`'s default ref) as the base. Changing the checkout ref mid-flight can move the base of an existing push branch and cause non-fast-forward pushes. `GitForcePushBranch` (default on) handles that case, but it will rewrite history on the push branch — surface this in any user-facing change here.
- `GitAllBranchReferences` (default on) is required for the controller to detect an existing push branch in shallow clones; disabling it regresses `fluxcd/flux2#3384`.
- SSH transports honour `--ssh-kex-algos` and `--ssh-hostkey-algos` wired into `github.com/fluxcd/pkg/git`. HTTPS auth flows through the `TokenCache` when cloud providers are used; keep cache invalidation in mind when touching auth code.
- GPG signing reads `git.asc` (and optional `passphrase`) from the secret — not `identity`/`identity.pub` like SSH signing would.
- Only the `Setters` update strategy exists. The `UpdateStrategy.Strategy` enum is constrained by `+kubebuilder:validation:Enum=Setters` — adding a new strategy is an API change that needs v1beta rollover planning.
- The `api/` sub-module must stay importable as a standalone dependency (consumed by `flux2` and others). Avoid importing anything from `internal/` into `api/`, and keep its `go.mod` minimal.
- `REFLECTOR_VER` and `SOURCE_VER` Make variables are derived from `go.mod`. Bumping `github.com/fluxcd/image-reflector-controller/api` or `github.com/fluxcd/source-controller/api` changes which CRD fixtures are fetched on the next `make test` — re-run `make clean_test_deps test_deps` if you see stale schema errors.
