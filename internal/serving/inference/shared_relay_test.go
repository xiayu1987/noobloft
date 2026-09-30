// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package inference

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	relayclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/network/discovery"
	"github.com/xiayu1987/noobloft/internal/network/node"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
	"github.com/xiayu1987/noobloft/internal/serving/backend"
	"github.com/xiayu1987/noobloft/internal/serving/capability"
)

func TestSharedRelayAuthorizationAndACL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	newNode := func(allowed []string) (*node.Node, crypto.PrivKey) {
		t.Helper()
		cfg := config.Default()
		cfg.Network.Mode = "shared"
		cfg.Network.ListenAddrs = []string{"/ip4/127.0.0.1/tcp/0"}
		cfg.Network.EnableMDNS = false
		cfg.Network.EnableDHT = false
		cfg.Network.EnableHolePunch = false
		cfg.Relay.Enabled = len(allowed) > 0
		cfg.Relay.AllowedPeers = allowed
		k, _, e := crypto.GenerateEd25519Key(nil)
		if e != nil {
			t.Fatal(e)
		}
		n, e := node.New(ctx, node.Options{Cfg: cfg, PrivKey: k})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { n.Close() })
		return n, k
	}
	provider, key := newNode(nil)
	consumer, _ := newNode(nil)
	outsider, _ := newNode(nil)
	relay, _ := newNode([]string{provider.ID().String(), consumer.ID().String()})
	info := peer.AddrInfo{ID: relay.ID(), Addrs: relay.Host().Addrs()}
	if _, e := relayclient.Reserve(ctx, provider.Host(), info); e != nil {
		t.Fatal(e)
	}
	if _, e := relayclient.Reserve(ctx, outsider.Host(), info); e == nil {
		t.Fatal("unauthorized relay reservation accepted")
	}
	root, _, _ := crypto.GenerateEd25519Key(nil)
	sign := func(d publisher.Document) *publisher.Document {
		t.Helper()
		v, e := publisher.Sign(root, d, time.Hour)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	svc := sign(publisher.Document{Kind: "service", Peers: []string{provider.ID().String()}, Models: []string{"relay-model"}})
	access := sign(publisher.Document{Kind: "access", Subject: consumer.ID().String(), Models: []string{"relay-model"}, MaxTokens: 4, MaxConcurrent: 1, RequestsPerMinute: 5})
	rev := sign(publisher.Document{Kind: "revocations", Sequence: 1})
	path := filepath.Join(t.TempDir(), "rev.json")
	if e := publisher.SaveNew(path, rev); e != nil {
		t.Fatal(e)
	}
	ann, e := capability.Sign(key, provider.ID(), []capability.Model{{Name: "relay-model"}}, 2, svc)
	if e != nil {
		t.Fatal(e)
	}
	discovery.New(provider.Host(), nil).SetAnnouncement(ann)
	stub := &stubAdapter{model: "relay-model", deltas: []string{"authorized", " relay"}}
	srv, e := NewServer(provider.Host(), func(context.Context, string) (backend.Adapter, error) { return stub, nil }, nil, 2, &Authorization{Service: *svc, RevocationsFile: path})
	if e != nil {
		t.Fatal(e)
	}
	defer srv.Close()
	circuit := relay.Host().Addrs()[0].Encapsulate(ma.StringCast("/p2p/" + relay.ID().String() + "/p2p-circuit"))
	target := peer.AddrInfo{ID: provider.ID(), Addrs: []ma.Multiaddr{circuit}}
	if e = consumer.Host().Connect(network.WithAllowLimitedConn(ctx, "test"), target); e != nil {
		t.Fatal(e)
	}
	if e = outsider.Host().Connect(network.WithAllowLimitedConn(ctx, "test"), target); e == nil {
		t.Fatal("unauthorized relay connect accepted")
	}
	for _, c := range consumer.Host().Network().ConnsToPeer(provider.ID()) {
		if !c.Stat().Limited {
			t.Fatal("accidental direct connection")
		}
	}
	got, e := discovery.New(consumer.Host(), nil).QueryCapability(ctx, provider.ID())
	if e != nil || got.Service.Publisher != svc.Publisher {
		t.Fatalf("capability %v %v", got, e)
	}
	var b strings.Builder
	n := 4
	e = NewClient(consumer.Host(), nil).Chat(ctx, provider.ID(), &Request{Publisher: svc.Publisher, Model: "relay-model", Access: access, MaxTokens: &n, Messages: []backend.Message{{Role: "user", Content: "test"}}}, func(c Chunk) error { b.WriteString(c.Delta); return nil })
	if e != nil || b.String() != "authorized relay" {
		t.Fatalf("relay inference: %q %v", b.String(), e)
	}
}
