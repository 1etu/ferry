# Contributing to Ferry

## Toolchain

Go 1.27.1 (from `go.mod`), Node 22, pnpm 10 (from `web/package.json`), gofumpt and golangci-lint 2.14. Commands are for Git Bash or any POSIX shell. `-race` needs cgo and a C compiler; without one, run `go test ./...`.

## Commands

| Task | Command |
|---|---|
| Web install | `pnpm -C web install` |
| Web dev | `pnpm -C web dev` with `FERRY_DEV=1 go run ./cmd/ferry` |
| Web checks | `pnpm -C web check && pnpm -C web lint` |
| Go format | `gofumpt -l -w .` |
| Go lint | `golangci-lint run ./...` |
| Tests | `go run ./tools/test` (see [Tests](#tests)) |
| Comment check | `node tools/check-comments.mjs` |
| Full build | `pnpm -C web build && CGO_ENABLED=0 go build -o bin/Ferry.exe ./cmd/ferry` |

`go build` embeds `internal/webui/dist`, so run `pnpm -C web build` before any Go command.

## Rules

CI fails unless:

- `node tools/check-comments.mjs` finds no comments in Go, TypeScript, JavaScript, Svelte, CSS or HTML. Names, types, small functions and tests carry the meaning. Compiler directives (`//go:embed`, `//go:build`, `//go:generate`) are the only exception.
- `gofumpt -l .` prints nothing.
- `golangci-lint run ./...` passes with the committed `.golangci.yml`.
- `pnpm -C web check` and `lint` pass.
- `go run ./tools/test all --race` passes on Windows.

Review also requires no file over 300 lines and no comments in YAML.

## Tests

```sh
go run ./tools/test              # Go and web
go run ./tools/test e2e          # end-to-end flows, builds the app first
go run ./tools/test all --race   # everything, as CI runs it
go run ./tools/test server       # one area
go run ./tools/test --run Pair   # only tests whose names match
```

Layout:

- `tests/<area>/` holds black-box suites that drive a package through its public API, such as the HTTP server with a paired, sealed device. They share helpers from `tests/kit`.
- Unit tests that need a package's unexported internals stay next to the code, as Go requires.
- Web unit tests sit beside their modules as `*.test.ts`, and end-to-end flows live in `web/e2e/`.

End-to-end tests need the Playwright browsers once: `pnpm -C web exec playwright install chromium webkit`. `FERRY_BIN` points a manual `pnpm -C web e2e` run at a specific binary.

## Dependencies

No new dependencies without an issue first. Explain what it replaces and why the standard library or an existing dependency is not enough.

## Commits

Imperative subject under 72 characters, an optional body and no trailers preferably.

## Releases

A release is a tag `vX.Y.Z` on `main`. Pushing the tag runs `release.yml`, which builds `Ferry.exe`, signs the update manifest and publishes the release.
