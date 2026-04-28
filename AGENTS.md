# AGENTS.md

Guidance for AI coding assistants working in `fluxcd/flux-schema`. Read this file before making changes.

## Contribution workflow for AI agents

These rules come from [`fluxcd/flux2/CONTRIBUTING.md`](https://github.com/fluxcd/flux2/blob/main/CONTRIBUTING.md) and apply to every Flux repository.

- **Do not add `Signed-off-by` or `Co-authored-by` trailers with your agent name.** Only a human can legally certify the DCO.
- **Disclose AI assistance** with an `Assisted-by` trailer naming your agent and model:
  ```sh
  git commit -s -m "Add feature X" --trailer "Assisted-by: <agent-name>/<model-id>"
  ```
  The `-s` flag adds the human's `Signed-off-by` from their git config — do not remove it.
- **Commit message format:** Subject in imperative mood ("Add feature X" instead of "Adding feature X"), capitalized, no trailing period, ≤50 characters.
- **Commit body:** Add a succinct explanation explaining what and why, wrap at 72 characters.
- **Trim verbiage:** in PR descriptions, commit messages, and code comments. No marketing prose, no restating the diff, no emojis.
- **Rebase, don't merge:** Never merge `main` into the feature branch; rebase onto the latest `main` and push with `--force-with-lease`. Squash before merge when asked.
- **Tests:** New features, improvements and fixes must have test coverage.

## Project

`flux-schema` is a Flux CLI plugin for Kubernetes schema extraction and manifest validation. Single Go binary, cobra-based.

Read the [README](README.md) for an overview of the project and its features.

### Code Structure

- `cmd/flux-schema/` — the `main` package. One file per cobra subcommand (`version.go`, etc.), each registering itself in `init()` via `rootCmd.AddCommand(...)`. `main.VERSION` is overridden at build time by the Makefile.
- `internal/extractor/` — OpenAPI v2 swagger and CRD → standalone-strict JSON Schema extraction. `ExtractKubernetes`/`ExtractOpenShift`/`ExtractCRDs` are the entry points; `transformers.go` holds the pipeline steps (`inlineRefs`, `injectGVK`, `replaceIntOrString`, `nullableOptional`, `closeAdditionalProperties`, `stripVendorExtensions`) plus the exported `StripDescriptions` post-process.
- `internal/validator/` — JSON Schema validation of Kubernetes YAML manifests. `loader.go` compiles schemas via `santhosh-tekuri/jsonschema/v6` with `Draft2020` as the default draft; `formats.go` registers the Kubernetes string formats (`duration`, `date`, etc.) that the library doesn't assert by default.
- `internal/tmpl/` — Go `text/template` renderer for the output-path and `--schema-location` templates. `SchemaVars` (`Group`, `GroupPrefix`, `Kind`, `Version`) is the shared variable set; values are lowercased at render time and `Group: ""` is normalized to `core`.
- `internal/yamldoc/` — line-oriented `bufio.SplitFunc` that splits a byte stream on `\n---` boundaries. Matches kubectl's `splitYAMLDocument` behavior.
- `internal/flags/` — reusable `pflag.Value` implementations for CLI flags shared across commands.
- `cmd/flux-schema/main_test.go` — hosts `TestMain`, the shared `executeCommand(args)` test helper, and `resetCmdArgs()` which restores every command's flag defaults between tests. New commands must add their flag reset here or tests will leak state across subtests.

### Build, Test, and Lint

All development goes through the Makefile — do not invoke `go build` directly, because the Makefile stamps `main.VERSION` via `-ldflags` and runs `tidy`/`fmt`/`vet` as prerequisites.

- `make build` — build `./bin/flux-schema` with VERSION stamped from git
- `make test` — runs `tidy`, `fmt`, `vet`, then `go test ./... -coverprofile cover.out`
    - Single test: `make test GO_TEST_ARGS="-run TestVersionCmd ./cmd/flux-schema/"`
- `make lint` — runs golangci-lint with revive, staticcheck, and goimports
- `make run GO_RUN_ARGS="version -o json"` — build then run the CLI with args

CI (`.github/workflows/test.yaml`) runs `make test` + `make lint` and fails if the working tree becomes dirty, so always run `make test` before committing.

### Code Conventions

- File header: every `.go` file must start with the two-line Apache-2.0 header — enforced by golangci-lint's `revive.file-header` rule.
- Struct tags: only `json` and `inline` are permitted on struct fields (revive `struct-tag` rule).
- Flag wiring: for any flag with a fixed set of accepted values, add a type under `internal/flags/` and register it with `cmd.Flags().VarP(&args.x, "name", "n", args.x.Description())` rather than `StringVarP` — this gets validation, the `a|b|c` type hint in `--help`, and consistent error messages for free.
- Command output: inside cobra `RunE` handlers (and helpers they call), emit via `cmd.Printf` / `cmd.PrintErrf` and pass `*cobra.Command` to helpers — not `fmt.Fprintf(cmd.OutOrStdout(), ...)`. `rootCmd.SetOut(os.Stdout)` in `main.go` already routes `cmd.Print*` to stdout, and the pattern avoids the `_, _ = fmt.Fprintf(...)` noise that errcheck forces.
- Config file sync: any new flag added to a subcommand that supports `--config` (currently `validate`) must also be added to the matching section struct in `cmd/flux-schema/config.go` (`validateConfig` for validate) with a hyphenated `json:` tag that matches the flag name, and wired into `applyValidateConfig` with the same `!flags.Changed("<name>")` gate as the existing fields.
- Tests use Gomega (`. "github.com/onsi/gomega"` dot-import is accepted — staticcheck ST1001 is disabled project-wide). Table-driven tests are the norm.

## Writing Documentation

User-facing changes (flags, commands, report shape, GitHub Action inputs) must be reflected in the docs. The tree is:

- `README.md` — features list, install, quickstart, commands table, doc links.
- `docs/guides/manifests-validation.md` — `validate` reference: flag table, schema resolution, skip rules, CEL rules, config file with example `.fluxschema.yml`.
- `docs/guides/custom-schema-catalog.md` — `extract crd`/`extract k8s`/`extract openshift` reference and catalog hosting/refresh.
- `docs/report/README.md` + `docs/report/schema-1.0.0.json` — JSON/YAML report envelope and its JSON Schema.
- `actions/setup/README.md` + `actions/validate/README.md` — GitHub Action inputs and example workflows.

Apply these rules:

- New or changed flag: update the flag table in the matching guide. For `validate` flags that are config-file-eligible, also extend the example `.fluxschema.yml` block in the "Config file" section.
- New subcommand: add a row to `README.md`'s "Commands" table and either a new section in an existing guide or a dedicated guide under `docs/guides/`.
- Report shape change: update `docs/report/README.md` (and `schema-1.0.0.json` per its versioning rules) and the JSON example under "Output" in `docs/guides/manifests-validation.md`.
- GitHub Action change: when a `validate.sh` argument or an `action.yaml` input changes, refresh the inputs table and example workflow in `actions/validate/README.md`.

## Builtin Schema Catalog

`catalog/latest/` holds the JSON Schemas for Kubernetes and Flux ecosystem CRDs. It is the `default` schema source for the `validate` command.

- `scripts/gen-k8s-schemas.sh -d <dir> [-v <version>]` — generates the native Kubernetes schemas.
- `scripts/gen-flux-schemas.sh -d <dir> [-v <version>]` — generates the Flux CRD schemas.
- `scripts/gen-crd-schemas.sh -r <owner/repo> -d <dir> [-v <version>]` — generates CRD schemas for an arbitrary upstream repo (used for gateway-api and flux-operator).
- `scripts/update-catalog-readme.sh -f <readme>` — rewrites the versions table in `catalog/README.md` from env vars exported by the `gen-*` scripts.

All generator scripts pass `--strip-description` so catalog schemas stay small (about 54% reduction on native K8s). If you add another generator, keep that flag on.
Don't hand-edit files under `catalog/latest/` — they are kept up-to-date by the `.github/workflows/update-catalog.yaml` workflow.
