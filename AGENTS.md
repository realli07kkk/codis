# Repository Guidelines

## Project Structure & Module Organization

Codis uses Go modules (`go.mod` / `go.sum`, `go 1.26.1`) as its default build path; the module is `github.com/CodisLabs/codis`. Executable entry points live in `cmd/` (`dashboard`, `proxy`, `admin`, `ha`, `fe`). Core packages live in `pkg/`, including proxy routing, topology management, models, Redis protocol helpers, and utilities. Runtime configs are under `config/`. Deployment resources are in `deploy/`, `kubernetes/`, and `ansible/`. Documentation and images are in `doc/`; examples and helper scripts are in `example/` and `scripts/`. Embedded Redis sources are in `extern/`; the `cgo_jemalloc` build tag uses `third_party/jemalloc-go` via `go.mod` replace. Old `vendor/` and `Godeps/` directories have been retired.

## Build, Test, and Development Commands

- `make`: builds all binaries, embedded Redis, frontend assets, and default configs.
- `make gotest`: runs `go test ./cmd/... ./pkg/...` after preparing build dependencies.
- `make gobench`: runs package benchmarks with inlining disabled.
- `make codis-proxy` or `make codis-dashboard`: builds one binary and refreshes its default config.
- `make clean`: removes `bin/`, `scripts/tmp`, and test temp files.
- `make distclean`: also cleans embedded Redis and jemalloc build output.
- `make docker`: builds the `codis-image` Docker image.

## Coding Style & Naming Conventions

Use standard Go formatting: run `gofmt` on changed `.go` files. Follow existing naming: short lowercase package names, clear exported identifiers, and `*_test.go` beside the package under test. Preserve the project’s error style (`log.PanicError`, utility wrappers, and explicit error returns). Do not restore old vendor/Godeps dependency paths or downgrade from Go modules to GOPATH builds unless that is the explicit task.

## Testing Guidelines

Tests use Go’s built-in `testing` package. Add focused unit tests near changed code, using names like `TestFormatClientList` or `TestClientListRequiresAuth`. Run `make gotest` before submitting changes. For proxy or topology changes, prefer targeted package tests first, for example `go test ./pkg/proxy -run TestName`, then run the full command.

## Commit & Pull Request Guidelines

Recent history uses concise imperative or scoped summaries such as `utils: close redis connection immediately` and `fix issue #1453, ...`. Keep commits focused and reference issues when relevant. Pull requests should describe the behavior change, compatibility impact, test commands run, and any operational notes. Include screenshots only for dashboard or frontend-facing changes.

## Agent-Specific Instructions

Avoid editing generated build output in `bin/` or vendored code unless the task specifically requires it. Preserve user changes in the working tree. Prefer small, compatible fixes over broad rewrites; this project values stable Redis/proxy behavior and backward compatibility.

## CodeStable Project Memory

This repository is onboarded to CodeStable under `.codestable/`. AI agents should read `.codestable/attention.md` before using any CodeStable workflow because it contains project-specific build, test, path, and compatibility constraints.

- `.codestable/requirements/` stores capability-level requirement documents: why a capability exists, who it serves, what it solves, and what it explicitly does not cover. Read `requirements/VISION.md` and the relevant requirement before feature design, roadmap work, or scope discussions.
- `.codestable/architecture/` stores the current system map: implemented module boundaries, runtime flow, state ownership, and code anchors. Read `architecture/ARCHITECTURE.md` before architecture-sensitive changes, bug root-cause analysis, or module onboarding.
- `.codestable/features/`, `.codestable/issues/`, `.codestable/refactors/`, and `.codestable/roadmap/` store workflow artifacts for feature, bug, refactor, and larger planning work.
- `.codestable/compound/` stores durable learnings, decisions, explorations, and reusable tricks.
- `.codestable/reference/` and `.codestable/tools/` are shared CodeStable assets copied from the skill package; refresh them through `cs-onboard` rather than hand-editing unless the task explicitly asks for it.
