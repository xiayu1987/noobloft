// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package node

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/xiayu1987/noobloft/internal/audit"
	"github.com/xiayu1987/noobloft/internal/config"
)

const DHTProtocolPrefix = "/noobloft"

const MDNSServiceTag = "noobloft-internal"

type Node struct {
	host    host.Host
	dht     *dht.IpfsDHT
	mdnsSvc mdns.Service
	relay   *relayv2.Relay
	auditor *audit.Logger
	cfg     *config.Config

	closeOnce sync.Once
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

type Options struct {
	Cfg     *config.Config
	PrivKey crypto.PrivKey
	PSK     []byte
	Auditor *audit.Logger
}

func New(ctx context.Context, opts Options) (*Node, error) {
	if opts.Cfg == nil {
		return nil, localization.Errorf("errors.node.missingConfig")
	}
	if opts.Cfg.Network.Mode != "private" && opts.Cfg.Network.Mode != "shared" {
		return nil, fmt.Errorf("invalid network mode")
	}
	if opts.Cfg.Network.Mode == "shared" && len(opts.PSK) > 0 {
		return nil, fmt.Errorf("shared mode does not accept a PSK")
	}
	if opts.Cfg.Network.Mode == "shared" && opts.Cfg.Provider.Enabled && opts.Cfg.Provider.Service == nil {
		return nil, fmt.Errorf("shared provider requires authorization")
	}
	if opts.Cfg.Network.Mode != "shared" && len(opts.PSK) != 32 {
		return nil, localization.Errorf("errors.node.missingPSK")
	}
	if opts.PrivKey == nil {
		return nil, localization.Errorf("errors.node.missingIdentity")
	}

	netCfg := opts.Cfg.Network
	ctx, cancel := context.WithCancel(ctx)
	success := false
	defer func() {
		if !success {
			cancel()
		}
	}()
	announced := make([]ma.Multiaddr, 0, len(netCfg.AnnounceAddrs))
	for _, addr := range netCfg.AnnounceAddrs {
		a, err := ma.NewMultiaddr(addr)
		if err != nil {
			return nil, localization.Errorf("errors.config.announceAddr", err)
		}
		announced = append(announced, a)
	}

	cm, err := connmgr.NewConnManager(
		netCfg.LowWater,
		netCfg.HighWater,
		connmgr.WithGracePeriod(time.Minute),
	)
	if err != nil {
		return nil, localization.Errorf("errors.node.connManager", err)
	}

	staticRelays, err := parsePeers(opts.Cfg.Relay.StaticRelayAddrs())
	if err != nil {
		return nil, localization.Errorf("errors.node.staticRelays", err)
	}

	var kadDHT *dht.IpfsDHT
	bootstrapPeers, err := parsePeers(netCfg.StaticPeers)
	if err != nil {
		return nil, localization.Errorf("errors.node.staticPeers", err)
	}

	libp2pOpts := []libp2p.Option{
		libp2p.Identity(opts.PrivKey),
		libp2p.ListenAddrStrings(netCfg.ListenAddrs...),
		libp2p.ConnectionManager(cm),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
	}

	if netCfg.Mode == "private" {
		libp2pOpts = append(libp2pOpts, libp2p.PrivateNetwork(opts.PSK))
	}
	if netCfg.EnableHolePunch {
		libp2pOpts = append(libp2pOpts, libp2p.EnableHolePunching())
	}
	if len(announced) > 0 {
		libp2pOpts = append(libp2pOpts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			return append(addrs, announced...)
		}))
	}

	if opts.Cfg.Relay.UseRelays && len(staticRelays) > 0 {
		libp2pOpts = append(libp2pOpts,
			libp2p.EnableAutoRelayWithStaticRelays(staticRelays))
	}

	if netCfg.EnableDHT {
		libp2pOpts = append(libp2pOpts, libp2p.Routing(
			func(h host.Host) (routing.PeerRouting, error) {
				dhtOpts := []dht.Option{
					dht.ProtocolPrefix(DHTProtocolPrefix),
					dht.Mode(dhtMode(netCfg.DHTMode)),
				}
				if len(bootstrapPeers) > 0 {
					dhtOpts = append(dhtOpts, dht.BootstrapPeers(bootstrapPeers...))
				}
				d, err := dht.New(h, dhtOpts...)
				if err != nil {
					return nil, err
				}
				kadDHT = d
				return d, nil
			}))
	}

	h, err := libp2p.New(libp2pOpts...)
	if err != nil {
		return nil, localization.Errorf("errors.node.host", err)
	}

	n := &Node{
		host:    h,
		dht:     kadDHT,
		auditor: opts.Auditor,
		cfg:     opts.Cfg,
		cancel:  cancel,
	}

	if opts.Cfg.Relay.Enabled {
		limit := &relayv2.RelayLimit{
			Duration: 30 * time.Minute,
			Data:     1 << 30,
		}
		resources := relayv2.DefaultResources()
		resources.MaxCircuits = opts.Cfg.Relay.MaxCircuits
		resources.MaxReservations = opts.Cfg.Relay.MaxCircuits
		resources.Limit = limit
		relayOpts := []relayv2.Option{
			relayv2.WithLimit(limit),
			relayv2.WithResources(resources),
		}
		if netCfg.Mode == "shared" {
			relayOpts = append(relayOpts, relayv2.WithACL(peerACL(opts.Cfg.Relay.AllowedPeers)))
		}
		r, err := relayv2.New(h, relayOpts...)
		if err != nil {
			_ = h.Close()
			return nil, localization.Errorf("errors.node.relay", err)
		}
		n.relay = r
	}

	h.Network().Notify(&connNotifee{node: n})

	if netCfg.EnableMDNS {
		svc := mdns.NewMdnsService(h, MDNSServiceTag, &mdnsNotifee{node: n})
		if err := svc.Start(); err != nil {
			_ = h.Close()
			return nil, localization.Errorf("errors.node.mdns", err)
		}
		n.mdnsSvc = svc
	}

	for _, p := range append(bootstrapPeers, staticRelays...) {
		if p.ID == h.ID() {
			continue
		}
		h.ConnManager().Protect(p.ID, "configured-anchor")
		n.wg.Add(1)
		go func(p peer.AddrInfo) {
			defer n.wg.Done()
			n.maintainPeer(ctx, p, 30*time.Second)
		}(p)
	}

	if kadDHT != nil {
		if err := kadDHT.Bootstrap(ctx); err != nil {
			_ = n.Close()
			return nil, localization.Errorf("errors.node.bootstrap", err)
		}
	}

	_ = n.audit(audit.Event{Type: audit.EventNodeStart, PeerID: h.ID().String()})
	success = true
	return n, nil
}

func (n *Node) Host() host.Host { return n.host }

func (n *Node) DHT() *dht.IpfsDHT { return n.dht }

func (n *Node) Auditor() *audit.Logger { return n.auditor }

func (n *Node) ID() peer.ID { return n.host.ID() }

func (n *Node) Addrs() []string {
	id := n.host.ID().String()
	out := make([]string, 0, len(n.host.Addrs()))
	for _, a := range n.host.Addrs() {
		out = append(out, a.String()+"/p2p/"+id)
	}
	return out
}

func (n *Node) Close() error {
	var firstErr error
	n.closeOnce.Do(func() {
		if n.cancel != nil {
			n.cancel()
		}
		n.wg.Wait()
		_ = n.audit(audit.Event{Type: audit.EventNodeStop, PeerID: n.host.ID().String()})
		if n.mdnsSvc != nil {
			if err := n.mdnsSvc.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if n.relay != nil {
			if err := n.relay.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if n.dht != nil {
			if err := n.dht.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if err := n.host.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	})
	return firstErr
}

func (n *Node) audit(ev audit.Event) error {
	if n.auditor == nil {
		return nil
	}
	return n.auditor.Log(ev)
}

func dhtMode(mode string) dht.ModeOpt {
	switch mode {
	case "auto":
		return dht.ModeAuto
	case "client":
		return dht.ModeClient
	default:
		return dht.ModeServer
	}
}

func (n *Node) maintainPeer(ctx context.Context, p peer.AddrInfo, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		state := n.host.Network().Connectedness(p.ID)
		if state != network.Connected && state != network.Limited {
			dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := n.host.Connect(dialCtx, p); err != nil && ctx.Err() == nil {
				fmt.Printf("warning: anchor %s connection failed, will retry: %v\n", p.ID, err)
			}
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func parsePeers(addrs []string) ([]peer.AddrInfo, error) {
	out := make([]peer.AddrInfo, 0, len(addrs))
	for _, s := range addrs {
		if s == "" {
			continue
		}
		maddr, err := ma.NewMultiaddr(s)
		if err != nil {
			return nil, localization.Errorf("errors.node.multiaddr", s, err)
		}
		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			return nil, localization.Errorf("errors.node.multiaddrPeer", s, err)
		}
		out = append(out, *info)
	}
	return out, nil
}

type mdnsNotifee struct {
	node *Node
}

func (m *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == m.node.host.ID() {
		return
	}
	if m.node.host.Network().Connectedness(pi.ID) == network.Connected {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.node.host.Connect(ctx, pi); err != nil {
		return
	}
}

type connNotifee struct {
	node *Node
}

func (c *connNotifee) Connected(_ network.Network, conn network.Conn) {
	_ = c.node.audit(audit.Event{
		Type:   audit.EventPeerConnected,
		PeerID: conn.RemotePeer().String(),
	})
}

func (c *connNotifee) Disconnected(_ network.Network, conn network.Conn) {
	_ = c.node.audit(audit.Event{
		Type:   audit.EventPeerDisconnected,
		PeerID: conn.RemotePeer().String(),
	})
}

func (c *connNotifee) Listen(_ network.Network, _ ma.Multiaddr)      {}
func (c *connNotifee) ListenClose(_ network.Network, _ ma.Multiaddr) {}
