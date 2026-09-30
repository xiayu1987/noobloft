// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package inference

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"github.com/xiayu1987/noobloft/internal/serving/backend"
	"github.com/xiayu1987/noobloft/internal/serving/capability"
)

func TestPrivateRelayCapabilityAndInference(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	newNode := func(relay bool) (*node.Node, crypto.PrivKey) {
		t.Helper()
		cfg := config.Default()
		cfg.Network.ListenAddrs = []string{"/ip4/127.0.0.1/tcp/0"}
		cfg.Network.EnableMDNS = false
		cfg.Network.EnableDHT = false
		cfg.Network.EnableHolePunch = false
		cfg.Relay.Enabled = relay
		key, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		n, err := node.New(ctx, node.Options{Cfg: cfg, PrivKey: key, PSK: bytes.Repeat([]byte{9}, 32)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { n.Close() })
		return n, key
	}
	relay, _ := newNode(true)
	provider, key := newNode(false)
	consumer, _ := newNode(false)
	relayInfo := peer.AddrInfo{ID: relay.ID(), Addrs: relay.Host().Addrs()}
	if _, err := relayclient.Reserve(ctx, provider.Host(), relayInfo); err != nil {
		t.Fatal(err)
	}
	ann, err := capability.Sign(key, provider.ID(), []capability.Model{{Name: "relay-model"}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	discovery.New(provider.Host(), nil).SetAnnouncement(ann)
	stub := &stubAdapter{model: "relay-model", deltas: []string{"relay", " works"}}
	srv, err := NewServer(provider.Host(), func(context.Context, string) (backend.Adapter, error) { return stub, nil }, nil, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	circuit := relay.Host().Addrs()[0].Encapsulate(ma.StringCast("/p2p/" + relay.ID().String() + "/p2p-circuit"))
	if err := consumer.Host().Connect(network.WithAllowLimitedConn(ctx, "test"), peer.AddrInfo{ID: provider.ID(), Addrs: []ma.Multiaddr{circuit}}); err != nil {
		t.Fatal(err)
	}
	assertRelay := func() {
		t.Helper()
		cs := consumer.Host().Network().ConnsToPeer(provider.ID())
		if len(cs) == 0 {
			t.Fatal("missing connection")
		}
		for _, c := range cs {
			if !c.Stat().Limited {
				t.Fatal("test accidentally used direct connection")
			}
		}
	}
	assertRelay()
	remote, err := discovery.New(consumer.Host(), nil).QueryCapability(ctx, provider.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !remote.HasModel("relay-model") {
		t.Fatal("missing signed capability")
	}
	var out strings.Builder
	done := false
	err = NewClient(consumer.Host(), nil).Chat(ctx, provider.ID(), &Request{Model: "relay-model", Messages: []backend.Message{{Role: "user", Content: "test"}}}, func(c Chunk) error { out.WriteString(c.Delta); done = c.Done; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "relay works" || !done {
		t.Fatalf("invalid result %q done=%v", out.String(), done)
	}
	assertRelay()
}
