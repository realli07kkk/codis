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

## Project Knowledge Base

Long-form knowledge lives in `doc/` (see `doc/README.md` for the full index). Read `doc/architecture_zh.md` (system map, module boundaries, code anchors) before architecture-sensitive changes, bug root-cause analysis, or module onboarding. Build, test, and release constraints are in `doc/build_and_release_zh.md`; per-capability docs cover ACL (`doc/codis_acl_zh.md`), PITR (`doc/pitr_flashback_zh.md`), Redis 8 codis-server extensions (`doc/redis8_codis_server_zh.md`), and proxy protocol behavior (`doc/proxy_protocol_extensions_zh.md`).

Hard constraints worth repeating here:

- Do not run a full `go mod tidy` to "fix" the module graph; drive minimal, mechanical `go.mod`/`go.sum` changes from the commands that actually verify the change.
- Regenerate `config/dashboard.toml` with `go run ./cmd/dashboard --default-config` (prints `DefaultConfig` with comments), not `Config.String()`.
- Embedded Redis 8 sources are pinned under `extern/redis-8.6.3/` and must stay git-tracked so patches show up in `git status`.
- Building `codis-server` requires a C/C++ toolchain and autotools (`gcc`, `gcc-c++`, `autoconf`, `automake`, `libtool` on EL/Rocky or apt equivalents); jemalloc autogen/configure/make runs automatically.
- `codis-server-redis3` does not accept the Redis 8 `codis-enabled yes` config; do not reuse Redis 8 configs for the Redis 3 fallback binary in cross-version checks.
- On darwin, `/bin/true` and `/bin/false` usually live under `/usr/bin`; in `os/exec` tests use `exec.LookPath` or a multi-path fallback instead of hardcoding `/bin/xxx`.
- Use `python3`, not `python`, for repository scripts.
- Do not modify Dockerfiles or Docker scripts unless the task explicitly asks for it.
