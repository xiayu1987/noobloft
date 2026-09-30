English | [简体中文](WAN.zh-CN.md)

# Private P2P Deployment Across the Internet

This document only applies to the default `network.mode=private`. For shared-substrate authorization see [PUBLISHERS.md](PUBLISHERS.md); that mode does not use swarm.key or the legacy invite commands.

## Topology and Boundaries

Deploy at least one public VPS reachable by all members. It acts as private DHT server, bootstrap and Circuit Relay v2.
Remote nodes connect directly when possible. AutoRelay keeps reservations for nodes behind NAT and DCUtR tries to upgrade to a direct connection; if that fails, the application protocol may keep using the limited relay connection.
Every node must still hold the same swarm.key. The public node is not a public IPFS node and does not serve unknown networks.
This release uses PSK + TCP; PSK + QUIC is not supported. Connectivity is not guaranteed when neither side can reach a relay.

## 1. Public Entry

Build: `go build -o bin/noobloftd ./cmd/noobloftd` (add .exe on Windows).
Replace the public IP, paths and PeerIDs below with your own values. Do not use the placeholders literally.

```sh
noobloftd init -dir ./relay-data -profile public-relay -announce /ip4/<public-IP>/tcp/4001
noobloftd invite export -dir ./relay-data -file network.noobloft -ttl 168h
noobloftd run -dir ./relay-data
```

You can also announce `/dns4/<domain>/tcp/4001`. Cloud security groups and the host firewall must allow members to reach TCP 4001.
If the public IP is mapped by an upstream NAT, forward the announced port to the local listening port 4001.
`init -profile public-relay` generates the config template: fixed TCP 4001, DHT server, mDNS, gateway, provider and model backends disabled, private relay enabled.
`network.announceAddrs` holds reachable transport addresses without `/p2p/PeerID`. Do not use 0.0.0.0 or dynamic port 0.

`invite export` signs with the local identity, merges staticPeers, staticRelays and announce addresses, and refuses to overwrite an existing export file.
If you already have a private network, add `-join <existing-swarm.key-path>` when initializing the public entry; otherwise a different network is created.

## 2. Remote Nodes

Deliver swarm.key through a trusted channel. The invitation file contains no PSK, identity private key or HTTP token.
Verify the exporter's PeerID through a separate trusted channel; do not trust only the identity claimed by the invitation itself.

```sh
noobloftd init -dir ./node-data -profile wan -join /safe/path/swarm.key
noobloftd invite import -dir ./node-data -file network.noobloft -trust <publisher-PeerID>
noobloftd run -dir ./node-data
```

Import checks the version, file size, signature, publisher identity, validity period and the networkId derived from the local PSK. On error the config is not modified.
Addresses are merged incrementally with deduplication and the config is written atomically. Re-importing does not add duplicate anchors.
Import enables DHT, hole punching and the relays named by the invitation; non-relay nodes are set to DHT auto.
It does not enable provider or relay services and does not change the local identity or key. Restart a running node to apply the config.
Invitations default to 7 days, up to 30 days. Expiry only blocks new imports; it does not revoke members who already joined.
Removing a member requires rotating the shared PSK and updating the remaining members. Invitation signatures are not an independent access control system.

Model providers set provider.enabled=true in node-data/config.json and configure local backends themselves.
The model list still comes from live signed capability declarations; invitations do not carry a second model list.
Consumers should not deploy a local model with the same name, otherwise the local-first policy uses the local model directly.

## 3. Queries and Acceptance

```sh
noobloftd peers -dir ./node-data
noobloftd models -dir ./node-data
```

connectionPaths in peers and `/admin/peers` show `direct:` or `relay:` followed by the actual remote address.
This is a snapshot of current connections, not a record of DCUtR success events; two concurrent connections show as two entries.
The public relay disables the HTTP gateway by default, so peers cannot be queried on it; query from a regular node or read the entry logs.
DHT convergence, reachability detection and relay reservations take time; models are not guaranteed to be visible right after startup.
Model announcements retry on the existing maintenance cycle. Anchors are checked and redialed every 30 seconds with a 10 second dial timeout; offline anchors do not block startup.

Real network acceptance should use two nodes on different carriers/LANs:

1. Verify the PSK matches, both sides reach the entry, and the provider is found by model name.
2. Send stream=false and stream=true inference; confirm complete content and a proper end frame.
3. Block direct traffic between the two while keeping entry access; confirm connectionPaths is relay and capability queries and inference still succeed.
4. Restore direct connectivity and watch the connection path; hole punching cannot be required under strict NAT.
5. Restart the public entry and provider, wait for re-reservation/announcement, and verify inference again.
6. A second private entry reduces single-point risk: add its full address, re-export the invitation and let members import incrementally.

## 4. Relay Limits and Privacy

Fixed an issue where relay resource config did not inherit defaults: library defaults such as reservation TTL and buffers are kept and only concurrency quotas are overridden.
Each circuit is currently limited to 30 minutes and 1 GiB; maxCircuits controls circuit and reservation quotas, other limits use library defaults.
perPeerBandwidthKBps rate limiting is not implemented; a non-zero value is rejected at startup so the setting never appears to work when it does not.
The relay forwards end-to-end encrypted traffic but can see metadata such as PeerIDs, traffic volume and timing.
Invitations are network topology metadata. Even though they contain no keys, share them only with members.

## Verification Scope

Automated coverage: rejection of tampered/expired signatures, wrong trust, wrong PSK and oversized files; CLI import/export and re-import; anchor disconnect/reconnect and cancellation.
A same-host three-node integration test uses real PSK libp2p nodes and real Relay v2 reservations, forces a relay-only path, and completes signed capability queries and streaming inference (with a test backend).
This proves the application-level relay protocol end to end, but it is not equivalent to real cross-carrier NAT or public VPS acceptance. Real deployment needs actual public addresses, host access and remote nodes.
