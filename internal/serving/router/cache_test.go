// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package router

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/network/discovery"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
	"github.com/xiayu1987/noobloft/internal/serving/backend"
	"github.com/xiayu1987/noobloft/internal/serving/capability"
	"github.com/xiayu1987/noobloft/internal/serving/inference"
)

type countingDiscovery struct {
	fakeDiscovery
	delay   time.Duration
	queries atomic.Int32
	finds   atomic.Int32
}

func (c *countingDiscovery) QueryCapability(ctx context.Context, id peer.ID) (*capability.Announcement, error) {
	c.queries.Add(1)
	select {
	case <-time.After(c.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return c.fakeDiscovery.QueryCapability(ctx, id)
}

func (c *countingDiscovery) FindProviders(ctx context.Context, key string, n int) ([]discovery.Provider, error) {
	c.finds.Add(1)
	return c.fakeDiscovery.FindProviders(ctx, key, n)
}

func newPeerID(t *testing.T) (crypto.PrivKey, peer.ID) {
	t.Helper()
	key, _, err := crypto.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return key, id
}

func TestRemoteSnapshotQueriesInParallelAndCoalesces(t *testing.T) {
	const peers, delay = 4, 200 * time.Millisecond
	disc := &countingDiscovery{delay: delay, fakeDiscovery: fakeDiscovery{anns: map[peer.ID]*capability.Announcement{}}}
	for i := range peers {
		_, id := newPeerID(t)
		disc.peers = append(disc.peers, id)
		disc.anns[id] = &capability.Announcement{Models: []capability.Model{{Name: []string{"a", "b", "a", "c"}[i]}}}
	}
	r := New(nil, disc, &fakeClient{}, nil)

	start := time.Now()
	var wg sync.WaitGroup
	results := make([][]string, 5)
	for i := range results {
		wg.Go(func() { results[i] = r.RemoteModelNames(context.Background()) })
	}
	wg.Wait()
	if elapsed := time.Since(start); elapsed >= peers*delay {
		t.Fatalf("capability queries look serial: took %v", elapsed)
	}
	if got := disc.queries.Load(); got != peers {
		t.Fatalf("concurrent calls should coalesce to one query per peer, got %d", got)
	}
	for _, names := range results {
		if len(names) != 3 || names[0] != "a" || names[1] != "b" || names[2] != "c" {
			t.Fatalf("model list should be deduplicated and sorted: %v", names)
		}
	}
	r.RemoteModelNames(context.Background())
	if got := disc.queries.Load(); got != peers {
		t.Fatalf("cache not effective: %d queries", got)
	}
}

func TestRemoteSnapshotReturnsWhenCallerCanceled(t *testing.T) {
	_, id := newPeerID(t)
	disc := &countingDiscovery{delay: 2 * time.Second, fakeDiscovery: fakeDiscovery{
		peers: []peer.ID{id},
		anns:  map[peer.ID]*capability.Announcement{id: {Models: []capability.Model{{Name: "a"}}}},
	}}
	r := New(nil, disc, &fakeClient{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if names := r.RemoteModelNames(ctx); len(names) != 0 {
		t.Fatalf("no result expected on cancel: %v", names)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("still blocked %v after caller cancel", elapsed)
	}
}

func TestProbeUsesSnapshotForPublisherModels(t *testing.T) {
	pubKey, _ := newPeerID(t)
	_, provider := newPeerID(t)
	_, consumer := newPeerID(t)
	svc, err := publisher.Sign(pubKey, publisher.Document{Kind: "service", Peers: []string{provider.String()}, Models: []string{"m", "hidden"}}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	access, err := publisher.Sign(pubKey, publisher.Document{Kind: "access", Subject: consumer.String(), Models: []string{"m"}, MaxTokens: 16, MaxConcurrent: 1, RequestsPerMinute: 10}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	disc := &countingDiscovery{fakeDiscovery: fakeDiscovery{
		peers: []peer.ID{provider},
		anns: map[peer.ID]*capability.Announcement{provider: {
			Service: svc,
			Models:  []capability.Model{{Name: "m"}, {Name: "hidden"}},
		}},
	}}
	var calls int
	client := &fakeClient{chat: func(_ context.Context, id peer.ID, req *inference.Request, _ func(inference.Chunk) error) error {
		calls++
		if id != provider || req.Model != "m" || req.Publisher != svc.Publisher || req.Access != access || req.MaxTokens == nil || *req.MaxTokens != 1 {
			t.Fatalf("unexpected publisher probe request: peer=%s req=%+v", id, req)
		}
		return nil
	}}
	rep := openReputation(t)
	r := New(nil, disc, client, rep)
	if err := r.ConfigurePublishers(consumer, true, []publisher.Subscription{{Service: *svc, Access: access}}); err != nil {
		t.Fatal(err)
	}

	names := r.RemoteModelNames(context.Background())
	if len(names) != 1 || names[0] != publisher.Qualified(svc.Publisher, "m") {
		t.Fatalf("only access-granted models should be exposed: %v", names)
	}
	if err := r.probeOnce(context.Background(), time.Hour); err != nil {
		t.Fatalf("probeOnce failed: %v", err)
	}
	if calls != 1 || disc.finds.Load() != 0 {
		t.Fatalf("probe should use the capability snapshot directly: chat=%d find=%d", calls, disc.finds.Load())
	}
	if rec, ok := rep.Record(provider.String()); !ok || rec.ProbesPassed != 1 {
		t.Fatalf("probe result not recorded: %+v found=%v", rec, ok)
	}
	if err := r.probeOnce(context.Background(), time.Hour); err != nil || calls != 1 {
		t.Fatalf("probed again during cooldown: calls=%d err=%v", calls, err)
	}
}

type countingBackend struct {
	name   string
	models []backend.ModelInfo
	delay  time.Duration
	lists  atomic.Int32
}

func (b *countingBackend) Name() string   { return b.name }
func (b *countingBackend) Kind() string   { return "fake" }
func (b *countingBackend) External() bool { return false }
func (b *countingBackend) ListModels(context.Context) ([]backend.ModelInfo, error) {
	b.lists.Add(1)
	time.Sleep(b.delay)
	return b.models, nil
}
func (b *countingBackend) Chat(context.Context, backend.ChatRequest, func(backend.ChatChunk) error) error {
	return nil
}

func TestLocalSnapshotCoalescesAndIsSingleSource(t *testing.T) {
	first := &countingBackend{name: "first", delay: 100 * time.Millisecond, models: []backend.ModelInfo{{Name: "z"}, {Name: "shared", ParameterSize: "first"}}}
	second := &countingBackend{name: "second", delay: 100 * time.Millisecond, models: []backend.ModelInfo{{Name: "shared", ParameterSize: "second"}, {Name: "a"}}}
	r := New([]backend.Adapter{first, second}, nil, nil, nil)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if b, err := r.ResolveLocal(context.Background(), "shared"); err != nil || b != first {
				t.Errorf("duplicate model should be served by the first registered backend: %v %v", b, err)
			}
		})
	}
	wg.Wait()
	if first.lists.Load() != 1 || second.lists.Load() != 1 {
		t.Fatalf("concurrent refreshes not coalesced: first=%d second=%d", first.lists.Load(), second.lists.Load())
	}
	infos := r.LocalModelInfos(context.Background())
	if len(infos) != 3 || infos[0].Name != "a" || infos[1].Name != "shared" || infos[1].ParameterSize != "first" || infos[2].Name != "z" {
		t.Fatalf("LocalModelInfos should match the routing index and be sorted: %+v", infos)
	}
	if first.lists.Load() != 1 {
		t.Fatal("LocalModelInfos bypassed the cache")
	}
	infos[0].Name = "mutated"
	if names := r.LocalModelNames(context.Background()); names[0] != "a" {
		t.Fatalf("caller mutation polluted the shared snapshot: %v", names)
	}
}
