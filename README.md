English | [简体中文](README.zh-CN.md)

# noobloft

A P2P node for LLM discovery, NAT traversal and forwarding. It supports PSK private networks and publisher
micro-networks on a shared transport: publishers independently sign service delegations and consumer
credentials, and one consumer can subscribe to multiple publishers.

**Scope: personal / internal networking only.** The project does not operate public bootstrap nodes or public
relays, and by default does not forward to closed-source commercial APIs. These are deliberate design
constraints, not missing features. See `docs/COMPLIANCE.md` for the reasoning.

## Quick start

```bash
# Build the frontend (Node.js 20.19+ / 22.12+) and embed it into the Go binary
./scripts/build.sh             # Linux / macOS
# powershell -File scripts/build.ps1  # Windows

# First node: create an internal network
bin/noobloftd init -dir ~/.noobloft
bin/noobloftd run  -dir ~/.noobloft

# Other nodes: securely copy the generated swarm.key, then join
bin/noobloftd init -dir ~/.noobloft -join /path/to/swarm.key
bin/noobloftd run  -dir ~/.noobloft
```

`internal/controlplane/webui/dist/` is a generated directory and is not version-controlled. Do not build a fresh
checkout without the build scripts; when frontend artifacts are missing, Go `embed` intentionally refuses to
compile, so no binary without the management console is produced.

The above uses the default `network.mode=private`. `swarm.key` is the transport-layer admission credential and
must be delivered securely. Explicit `shared` mode neither generates nor loads a PSK; model services must install
a publisher-signed delegation and verify consumer credentials. A successful connection does not grant permission
to call models. See [Publisher micro-networks](docs/PUBLISHERS.md).

Subcommands: `init` / `run` / `info` / `peers` / `models` / `invite export` / `invite import` / `publisher`.

For cross-LAN deployment see [Private WAN networking and signed invitations](docs/WAN.md). You can deploy
private bootstrap and relay nodes on your own public VPS and import anchors from a signed `.noobloft` file;
keys are delivered separately. That document targets private mode; shared mode uses publisher documents instead
of legacy PSK invitations.

`peers` and `models` are query tools and must be used after `run`: they ask the running daemon through the local
gateway's management endpoint instead of starting a temporary node. This is deliberate: a temporary node can
neither see the connections the daemon has established nor avoid competing for its listen ports, so its result
would be meaningless. The daemon is therefore the single source of truth for the peer view.

## Visual management

After `run`, open the gateway address (default `http://127.0.0.1:8760/`) and sign in to the Vue 3 + Element Plus
console with `admin.token` from the node directory. This credential is independent of the inference token.
Signing in issues a 7-day HttpOnly session cookie, so neither page reloads nor node restarts require signing in
again. The console covers node overview, multi-publisher subscriptions and credential import, published service
installation, signed revocation snapshot updates, streaming model debugging, connections and reputation, and node
configuration editing. All forms use a right-side drawer.

Saved subscriptions and configuration require a manual restart, and the page shows a pending-restart state;
revocation snapshots of running services are hot-reloaded. For full operation, build and test details see
[Management console](docs/CONSOLE.md).

"My published services → Publisher issuance" can issue service delegations and consumer credentials directly,
view records and revoke them. When enabled, the local backend holds an independent publisher identity (it does not
reuse the node identity and does not automatically take over an old offline identity), maintains the revocation
list automatically and applies it to local services of the same publisher. Other nodes still need to import the
exported signed list; success on this node does not mean the change is effective network-wide. Back up
`publisher-authority` in the node directory.

## One-step start and background running

Stop a node (idempotent; returns success when already stopped; keeps configuration, identity and logs):

```powershell
.\scripts\stop.ps1                       # default %USERPROFILE%\.noobloft
.\scripts\stop.ps1 --dir D:\node1        # same node directory as used with start.ps1
```

```sh
sudo sh scripts/stop.sh                  # stops noobloft.service
```

On Windows the script asks the tray to exit gracefully without a confirmation click and returns an error on
timeout. If an old tray does not support this request, exit it from the tray menu first, then run `start.ps1` to
build and start the new version. The same `stop.ps1` also stops, by default and without extra switches, command-line
nodes started from this project's `bin\noobloftd.exe` with an explicit absolute `run -dir` matching the directory.
Command-line nodes currently have no graceful-exit interface for scripts, so the script warns and terminates the
process; in-flight requests are interrupted and in-memory data is not guaranteed to be flushed. For a graceful exit,
press Ctrl+C in the original window first. Configuration, identity and logs are never deleted. Other programs, other
directories and nodes that cannot be reliably identified are never terminated.

On Linux run `sudo sh scripts/stop.sh`, adding `--dir /srv/noobloft` for a custom directory (same as at start; the
default is `.noobloft` in the caller's home directory). Both the systemd service and manual command-line nodes of
that directory are stopped without Force; command-line nodes receive SIGTERM first and SIGKILL if still running after
30 seconds. Python 3.9+ and Linux 5.3+ pidfd support are required to avoid killing a reused PID; only nodes from this
project's build or install paths are managed, and other directories are left alone. After stopping, the service is
not restarted by `Restart=on-failure`, but autostart stays enabled; to disable it as well run
`sudo systemctl disable noobloft`.

`scripts/start.ps1` (Windows) and `scripts/start.sh` (Linux) combine "build → initialize → start in background" into
one step. Both are safe to run repeatedly, and no separate initialization script is needed:

```powershell
.\scripts\start.ps1                                                  # default node directory %USERPROFILE%\.noobloft
.\scripts\start.ps1 --dir D:\node1 -- -profile wan -join D:\swarm.key   # custom directory; args after `--` go to the first init
```

```sh
scripts/start.sh                                                     # default node directory ~/.noobloft
scripts/start.sh --dir /srv/noobloft --user llm                     # custom directory and run-as user
scripts/start.sh --dir /srv/noobloft -- -profile wan -join /tmp/swarm.key
```

Initialization is built in: `init` runs only when the node directory has no `config.json`. Idempotency covers four
things: the binary is built only when missing or older than the sources; existing configuration is never
overwritten; installation to `/usr/local/lib/noobloft/` skips identical content; the systemd unit is not rewritten
when unchanged, and a running service is not restarted when neither binary nor unit changed. Re-running is
therefore safe, and a second run usually just prints a list of "skip" lines.

On Windows the node runs in the background as a **tray program** without a console window:

`build.ps1` produces two programs: `bin\noobloftd.exe` keeps console output (for `init`, `run`, `publisher`, etc.);
`bin\noobloftd-tray.exe` is built with `-H=windowsgui`, creates no console at startup and goes straight to the tray
when double-clicked. Run `scripts\start.ps1` the first time to initialize; afterwards you can double-click the tray
program (default `%USERPROFILE%\.noobloft`) or use `noobloftd-tray.exe -dir D:\node1` for an existing node
directory. Startup failures are shown in a message box or in the status window. The old `noobloftd tray` entry still
works, but use the standalone tray program to avoid a console flash. Before updating, exit the old tray / stop the old
node, then run the start script; the script never ends a running node on its own. To build for verification without
overwriting a running program, run `scripts\build.ps1 -OutputDir D:\swarm-build`.

- Click the tray icon in the lower-right corner to open the status window, showing gateway address, run state and
  log path;
- Closing the window only hides it to the tray and the node keeps running, whether you click X or minimize;
- Only "Exit" in the tray icon's right-click menu, or "Stop and exit" in the status window, actually stops the node;
- Running `start.ps1` again does not start a second instance; it just brings the existing status window to the front;
- If the target port is already used by another process, the script reports the owner's PID and process name and
  exits, instead of mistaking someone else's `/healthz` for a successful start.

On Linux the node is registered as the systemd service `noobloft` with autostart enabled:

```sh
systemctl status noobloft
systemctl restart noobloft
systemctl stop noobloft
journalctl -u noobloft -f
```

Registering a system service with `start.sh` requires root; when invoked as non-root it re-executes itself via
`sudo`. The node process runs as the caller (`SUDO_USER` under `sudo`, or set with `--user`) so node data does not
become root-owned. Arguments after `--` are passed to `init` only on first initialization. `noobloftd tray` is
Windows-only; on Linux it refuses to run and suggests `systemctl` instead.

## Integration

The gateway exposes an OpenAI-compatible API, binds to `127.0.0.1:8760` by default and enforces Bearer token
authentication:

```bash
curl http://127.0.0.1:8760/v1/chat/completions \
  -H "Authorization: Bearer <token generated by init>" \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3.1:8b","messages":[{"role":"user","content":"Hello"}],"stream":true}'
```

A bare model name prefers local backends; private mode may look up legacy remotes, and shared mode forbids remote
routing without a publisher. With `publisherID::modelName`, requests are routed only to service nodes authorized by a
subscription, are never replaced by a local model with the same name, and never switch publishers automatically.
With the local reputation ledger enabled, trusted candidates are weighted by historical success rate and recent
latency; nodes below the threshold are used only as a fallback when no trusted candidate exists. The ledger stores
only counts, latencies and timestamps, never request or response bodies. Point any existing OpenAI client's
base_url here.

Endpoints:

| Endpoint | Auth | Description |
| --- | --- | --- |
| `POST /v1/chat/completions` | Bearer | OpenAI-compatible, SSE with `stream: true` |
| `GET /v1/models` | Bearer | Local and network models; `owned_by` shows the source |
| `GET /admin/peers` | Bearer | Read-only peer view: signed capabilities, reputation and direct/relayed connectionPaths |
| `GET /healthz` | None | Liveness only; leaks no model or node information |

`/admin` is the local, read-only management surface and accepts no mutations. A failed capability query for a
single peer only degrades to that entry's `error` field without failing the whole response: in an internal network
nodes come and go, and one unreachable node should not fail the entire query. When `Peers` is not injected (pure
local mode) the endpoint is not registered.

## Architecture

```
cmd/noobloftd                    CLI: init / run / info / peers / models
internal/config                   Configuration and fail-closed validation
internal/security/identity        Ed25519 node identity + pnet swarm.key
internal/security/invitation      Keyless signed invitations, publisher trust and network fingerprint checks
internal/network/node             libp2p host: pnet, mDNS, DHT, DCUtR hole punching, relay
internal/serving/capability       Signing and verification of capability announcements
internal/security/publisher       Publisher-signed documents, delegations, credentials, revocation, subscriptions
internal/network/discovery        Legacy model name or publisher+model -> CID, reusing DHT provider records
internal/serving/inference        v1 private protocol and v2 authorized protocol, rate limiting, server/client
internal/serving/router           Publisher isolation, reputation-weighted authorized candidates, active probing
internal/serving/backend          Backend adapter registry (ollama / openai-compatible)
internal/controlplane/gateway     OpenAI-compatible HTTP gateway (loopback + token)
internal/audit                    JSONL audit log, structurally free of conversation content
internal/serving/reputation       Local reputation ledger storing only service-quality metadata
```

For the full tree, responsibility boundaries, frontend modules and build notes see
[Project layout](docs/ARCHITECTURE.md). Shared language resources live in `locales/`; the frontend is organized by
feature under `web/src/features/`.

Discovery uses DHT provider records; publisher models use versioned publisher/model discovery keys, while the legacy
private network keeps model-name keys. Candidates pass capability signature verification, and publisher routing also
requires the service delegation to match the subscription. DHT and reputation scores cannot grant access; signatures
and probes cannot prove the actual model weights, specification or output correctness.

Capability announcements carry an issue time and are considered expired after `capability.MaxAge`, preventing replay
of old announcements. Providers must therefore re-sign continuously: `discovery.Service` holds the private key and
model source, and `StartMaintenanceLoop` re-signs and re-announces to the DHT every `RefreshInterval`
(= `MaxAge / 3`). The two constants are bound by this derivation and guarded by an `init()` assertion: if the refresh
period exceeded the validity, a node would silently disappear between rounds, with connections intact and no log
errors, while every provider query returns empty. This constraint is enforced by tests and assertions, not memory.

Traversal combines libp2p features: AutoNAT determines reachability, DCUtR punches holes, and relays are used when
hole punching fails. Because no public relay is operated, fill `relay.staticRelays` with internally reachable node
addresses yourself.

## Switches off by default

These defaults lean toward safety; consider the consequences before enabling them:

| Setting | Default | Enabling means |
|---|---|---|
| `provider.enabled` | false | Offer local model compute to others |
| `relay.enabled` | false | Relay traffic for others, consuming your bandwidth |
| `reputation.enabled` | false | Write a reputation ledger to the config directory and use it for remote weighting |
| `reputation.probeEnabled` | false | Periodically send real probes generating at most 1 token, consuming peer compute |
| `policy.allowExternalApiForwarding` | false | Forward to closed-source commercial APIs; you bear their ToS risk |
| `policy.allowAnonymousRouting` | false | Hard-rejected in code, see COMPLIANCE |
| `policy.publicService` | false | Serve the public, triggering additional compliance obligations |

The gateway refuses to start with an empty `authToken`, and prints a warning when bound to a non-loopback address.

## Extending backends

A new inference backend only needs to implement `backend.Adapter` and register it, without touching a single line of
gateway, router or discovery:

```go
backend.Register("my-vendor", func(spec backend.Spec) (backend.Adapter, error) {
    return &myAdapter{spec: spec}, nil
})
```

If the backend points to a third-party hosted service, `External()` returns `true`. The registry then decides at
construction time according to `policy.allowExternalApiForwarding`, rejecting by default. This is the extension point
reserved for closed-source APIs: the structure is ready, and whether to enable it is your decision.

## Testing

```bash
go test ./... -count=1
```

Covered: capability signature verification (including tampering, impersonation and expiry), end-to-end streaming of
the inference protocol, the provider concurrency gate, gateway authentication and loopback checks, timing constraints
between announcement renewal and refresh period, renewal without DHT, and reputation smoothing, persistence, weighted
routing, request accounting and low-cost probing. Publisher tests cover same-name model isolation, credential misuse
and expiry, rejection of legacy protocol bypass, per-consumer limits, revocation with rollback protection, shared relay
allowlists and authorized streaming inference. Local integration is not equivalent to public-network acceptance.

## Compliance

`docs/COMPLIANCE.md` records primary sources on DMCA §512(a) mere conduit, Grokster inducement liability, China's
Interim Measures for the Management of Generative Artificial Intelligence Services, the EU AI Act and the Llama
license, along with historical design requirements. Plans in that document do not mean they are implemented; for
the current authorization boundary see PUBLISHERS.md.

This project ships no model weights. When using third-party models, their license obligations (such as Llama's
attribution and naming requirements) are the deployer's responsibility.
