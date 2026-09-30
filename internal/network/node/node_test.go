// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package node

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/config"
)

func TestAnchorReconnectAndStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	newNode := func() *Node {
		t.Helper()
		cfg := config.Default()
		cfg.Network.ListenAddrs = []string{"/ip4/127.0.0.1/tcp/0"}
		cfg.Network.EnableMDNS = false
		cfg.Network.EnableDHT = false
		cfg.Network.EnableHolePunch = false
		key, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		n, err := New(ctx, Options{Cfg: cfg, PrivKey: key, PSK: bytes.Repeat([]byte{4}, 32)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { n.Close() })
		return n
	}
	a, b := newNode(), newNode()
	loopCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.maintainPeer(loopCtx, peer.AddrInfo{ID: b.ID(), Addrs: b.Host().Addrs()}, 20*time.Millisecond)
	}()
	defer stop()
	wait := func() {
		t.Helper()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for a.Host().Network().Connectedness(b.ID()) != network.Connected {
			select {
			case <-ctx.Done():
				t.Fatal("anchor did not reconnect")
			case <-ticker.C:
			}
		}
	}
	wait()
	if err := a.Host().Network().ClosePeer(b.ID()); err != nil {
		t.Fatal(err)
	}
	wait()
	stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconnect loop leaked")
	}
}
