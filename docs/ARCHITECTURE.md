English | [简体中文](ARCHITECTURE.zh-CN.md)

# Project Layout and Responsibilities

This project is a Go node application with an embedded Vue console. `internal` keeps Go's internal-import protection; the next level groups directories by responsibility. These groups are not extra Go packages and do not imply strict one-way layering.

```text
cmd/noobloftd/             Executable, CLI, runtime orchestration and platform tray entry
internal/
  network/
    node/                  libp2p host, connections, discovery infrastructure and network admission
    discovery/             Model service discovery, announcement and renewal
  serving/
    backend/               Ollama / OpenAI-compatible model backend adapters
    capability/            Model capability declarations and verification
    inference/             Node-to-node inference protocol, client, server and authorization checks
    router/                Local/remote model selection, request routing and probing
    reputation/            Service quality accounting, scoring and persistence
  security/
    identity/              Node keys, PeerID and private network key
    invitation/            Signed private network invitations and verification
    publisher/             Publisher identity, signed authorization documents, issuance history and revocation
  controlplane/
    gateway/               Shared HTTP entry: management API, OpenAI API and authentication
    management/            Publisher document operations shared by CLI and HTTP
    webui/                 Embedded console static files and HTTP security headers
      dist/                Frontend build output, not committed
  config/                  Config validation, defaults, saving, recovery and rollback records
  audit/                   Audit log without conversation content
  localization/            Go language selection, translation fallback and request language parsing
locales/                   Semantic-key message catalogs shared by frontend and backend; single source of truth
  embed.go                 Go embed bridge, no language selection logic
web/
  src/
    App.vue                Console shell and page composition
    main.ts                Vue entry
    api/client.ts          HTTP requests and streaming calls
    components/            Cross-feature shared components
    features/
      authorization/       Authorization status and service export
      issuer/              Issuance and revocation
      resources/           Resources, restart, recovery and usage notes
      settings/            Configuration form
    i18n/                  Frontend language adapter and preference storage
    styles/                Global styles
  tests/                   Browser end-to-end tests
scripts/                   Paired Windows/Linux build, start, stop and check entries
docs/                      Operations, architecture, deployment and compliance docs (English X.md, Chinese X.zh-CN.md)
bin/                       Local build output, not committed
testrun/                   Local runtime data, not committed
```

## Dependencies and Boundaries

- `cmd/noobloftd` handles wiring, command dispatch and process lifecycle. It stays a single main package to remain compatible with the existing CLI and Windows tray build flags.
- The HTTP gateway currently serves both the management and inference APIs and reuses the same listener, authentication and tests. Directory grouping does not mean the two APIs are separate services.
- `management` holds application operations; `publisher` holds signed documents and publisher persistence. Do not push HTTP request types down into the signed document package.
- Network discovery needs capability declarations and routing needs discovery. These are real dependencies; do not introduce duplicated types or forwarding packages just for directory grouping.
- Frontend features use the shared API client and i18n adapter. Shared components go in `components`; feature-specific components live with their feature.
- Unit tests live next to their Go packages. Browser tests live in `web/tests` and run against a real HTTP API started by the gateway tests.
- `locales/*.json` is imported by the frontend and embedded by Go. Do not copy the catalogs or maintain a second generated translation source.

## Build and Verification

Use `scripts/build.ps1` or `scripts/build.sh`. The frontend builds into `internal/controlplane/webui/dist`, which Go then embeds into the executable. A clean checkout must build the frontend first.

```sh
cd web
npm ci
npm run build
cd ..
go test ./...
go vet ./...
```

Quality checks (formatting, lint, complexity, type checks and tests) run through `scripts/check.ps1` or `scripts/check.sh`.

Browser regression: on Windows PowerShell set `$env:SWARM_BROWSER_TEST='1'` and run `go test ./internal/controlplane/gateway -run TestManagementBrowser -count=1 -v`; on Linux run `SWARM_BROWSER_TEST=1 go test ./internal/controlplane/gateway -run TestManagementBrowser -count=1 -v`. Playwright Chromium must be installed first.

## Source and Runtime Data

The layout only concerns source locations. Node data is still determined by `-dir`; the default directory, config format, PeerID, credential files, authorization and backup paths were not migrated. `bin`, `testrun` and generated frontend output are not business source and must not be committed. Existing entry points for start/stop scripts and CLI commands stay compatible.
