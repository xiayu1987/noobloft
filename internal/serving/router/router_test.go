// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package router

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/network/discovery"
	"github.com/xiayu1987/noobloft/internal/serving/backend"
	"github.com/xiayu1987/noobloft/internal/serving/capability"
	"github.com/xiayu1987/noobloft/internal/serving/inference"
	"github.com/xiayu1987/noobloft/internal/serving/reputation"
)

type fakeDiscovery struct {
	providers []discovery.Provider
	peers     []peer.ID
	anns      map[peer.ID]*capability.Announcement
}

func (f *fakeDiscovery) FindProviders(context.Context, string, int) ([]discovery.Provider, error) {
	return f.providers, nil
}

func (f *fakeDiscovery) ConnectedPeers() []peer.ID { return f.peers }

func (f *fakeDiscovery) QueryCapability(_ context.Context, id peer.ID) (*capability.Announcement, error) {
	return f.anns[id], nil
}

type fakeClient struct {
	chat func(context.Context, peer.ID, *inference.Request, func(inference.Chunk) error) error
}

func (f *fakeClient) Chat(ctx context.Context, id peer.ID, req *inference.Request, cb func(inference.Chunk) error) error {
	return f.chat(ctx, id, req, cb)
}

func providers(ids ...peer.ID) []discovery.Provider {
	out := make([]discovery.Provider, 0, len(ids))
	for _, id := range ids {
		out = append(out, discovery.Provider{AddrInfo: peer.AddrInfo{ID: id}})
	}
	return out
}

func openReputation(t *testing.T) *reputation.Store {
	t.Helper()
	rep, err := reputation.Open(t.TempDir(), true)
	if err != nil {
		t.Fatalf("open reputation ledger failed: %v", err)
	}
	return rep
}

func TestSelectProviderPreservesOrderWithoutReputation(t *testing.T) {
	first, second := peer.ID("peer-first"), peer.ID("peer-second")
	got := selectProvider(providers(first, second), nil, 0.99)
	if got.AddrInfo.ID != first {
		t.Fatalf("picked %s with reputation disabled, want first peer %s", got.AddrInfo.ID, first)
	}
}

func TestSelectProviderExcludesDistrustedWhileTrustedExists(t *testing.T) {
	bad, fresh := peer.ID("peer-bad"), peer.ID("peer-fresh")
	rep := openReputation(t)
	for range reputation.MinSamples {
		rep.Observe(bad.String(), false, 0)
	}
	for _, draw := range []float64{0, 0.5, 0.999} {
		got := selectProvider(providers(bad, fresh), rep, draw)
		if got.AddrInfo.ID != fresh {
			t.Fatalf("draw=%v picked untrusted peer %s", draw, got.AddrInfo.ID)
		}
	}
}

func TestSelectProviderWeightedDrawCanReachTrustedCandidates(t *testing.T) {
	good, neutral := peer.ID("peer-good"), peer.ID("peer-neutral")
	rep := openReputation(t)
	for range 4 {
		rep.Observe(good.String(), true, 20)
	}
	if got := selectProvider(providers(good, neutral), rep, 0); got.AddrInfo.ID != good {
		t.Fatalf("low draw should pick the heavy peer, got %s", got.AddrInfo.ID)
	}
	if got := selectProvider(providers(good, neutral), rep, 0.999); got.AddrInfo.ID != neutral {
		t.Fatalf("high draw should keep exploring new peers, got %s", got.AddrInfo.ID)
	}
}

func TestChatRecordsRemoteOutcome(t *testing.T) {
	id := peer.ID("peer-remote")
	rep := openReputation(t)
	disc := &fakeDiscovery{providers: providers(id)}
	client := &fakeClient{chat: func(_ context.Context, _ peer.ID, _ *inference.Request, cb func(inference.Chunk) error) error {
		return cb(inference.Chunk{Done: true})
	}}
	r := New(nil, disc, client, rep)
	err := r.Chat(context.Background(), &inference.Request{
		Model: "model-a", Messages: []backend.Message{{Role: "user", Content: "hello"}},
	}, func(inference.Chunk) error { return nil })
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	rec, ok := rep.Record(id.String())
	if !ok || rec.Success != 1 || rec.Failure != 0 {
		t.Fatalf("remote success not recorded correctly: %+v, found=%v", rec, ok)
	}
}

func TestChatDoesNotPenalizeConsumerFailure(t *testing.T) {
	id := peer.ID("peer-remote")
	rep := openReputation(t)
	disc := &fakeDiscovery{providers: providers(id)}
	client := &fakeClient{chat: func(_ context.Context, _ peer.ID, _ *inference.Request, cb func(inference.Chunk) error) error {
		return cb(inference.Chunk{Delta: "data"})
	}}
	r := New(nil, disc, client, rep)
	wantErr := errors.New("downstream disconnected")
	err := r.Chat(context.Background(), &inference.Request{
		Model: "model-a", Messages: []backend.Message{{Role: "user", Content: "hello"}},
	}, func(inference.Chunk) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("Chat error = %v, want %v", err, wantErr)
	}
	if _, ok := rep.Record(id.String()); ok {
		t.Fatal("caller write failure should not penalize provider")
	}
}

func TestChatDoesNotPenalizeCanceledContext(t *testing.T) {
	id := peer.ID("peer-remote")
	rep := openReputation(t)
	disc := &fakeDiscovery{providers: providers(id)}
	client := &fakeClient{chat: func(ctx context.Context, _ peer.ID, _ *inference.Request, _ func(inference.Chunk) error) error {
		return ctx.Err()
	}}
	r := New(nil, disc, client, rep)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = r.Chat(ctx, &inference.Request{
		Model: "model-a", Messages: []backend.Message{{Role: "user", Content: "hello"}},
	}, func(inference.Chunk) error { return nil })
	if _, ok := rep.Record(id.String()); ok {
		t.Fatal("caller cancel should not penalize provider")
	}
}

func TestProbeCountsOnceAndLimitsGeneration(t *testing.T) {
	id := peer.ID("peer-probe")
	rep := openReputation(t)
	disc := &fakeDiscovery{
		peers: []peer.ID{id},
		anns: map[peer.ID]*capability.Announcement{
			id: {Models: []capability.Model{{Name: "model-a"}}},
		},
	}
	client := &fakeClient{chat: func(_ context.Context, gotID peer.ID, req *inference.Request, _ func(inference.Chunk) error) error {
		if gotID != id || req.Model != "model-a" || req.MaxTokens == nil || *req.MaxTokens != 1 {
			t.Fatalf("probe request violates minimal-cost constraint: peer=%s req=%+v", gotID, req)
		}
		return nil
	}}
	r := New(nil, disc, client, rep)
	if err := r.probeOnce(context.Background(), time.Hour); err != nil {
		t.Fatalf("probeOnce failed: %v", err)
	}
	rec, ok := rep.Record(id.String())
	if !ok || rec.ProbesPassed != 1 || rec.Success != 0 || rec.Samples() != 1 {
		t.Fatalf("probe should record exactly one probe sample: %+v, found=%v", rec, ok)
	}
}
