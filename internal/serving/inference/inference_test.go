// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package inference

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/serving/backend"
)

type stubAdapter struct {
	model   string
	deltas  []string
	entered chan struct{}
	release chan struct{}
}

func (s *stubAdapter) Name() string   { return "stub" }
func (s *stubAdapter) Kind() string   { return "stub" }
func (s *stubAdapter) External() bool { return false }

func (s *stubAdapter) ListModels(context.Context) ([]backend.ModelInfo, error) {
	return []backend.ModelInfo{{Name: s.model}}, nil
}

func (s *stubAdapter) Chat(ctx context.Context, req backend.ChatRequest, onChunk func(backend.ChatChunk) error) error {
	if s.entered != nil {
		s.entered <- struct{}{}
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for _, d := range s.deltas {
		if err := onChunk(backend.ChatChunk{Delta: d}); err != nil {
			return err
		}
	}
	return onChunk(backend.ChatChunk{
		Done:             true,
		FinishReason:     "stop",
		PromptTokens:     7,
		CompletionTokens: len(s.deltas),
	})
}

func newLinkedHosts(t *testing.T) (provider host.Host, client host.Host) {
	t.Helper()
	opts := []libp2p.Option{
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.DisableRelay(),
	}
	provider, err := libp2p.New(opts...)
	if err != nil {
		t.Fatalf("start provider node failed: %v", err)
	}
	client, err = libp2p.New(opts...)
	if err != nil {
		_ = provider.Close()
		t.Fatalf("start client node failed: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = provider.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.Connect(ctx, peer.AddrInfo{
		ID:    provider.ID(),
		Addrs: provider.Addrs(),
	}); err != nil {
		t.Fatalf("connect peers failed: %v", err)
	}
	return provider, client
}

func TestChatStreamsDeltasEndToEnd(t *testing.T) {
	providerHost, clientHost := newLinkedHosts(t)

	stub := &stubAdapter{model: "test-model", deltas: []string{"你", "好", "世界"}}
	srv, err := NewServer(providerHost, func(_ context.Context, model string) (backend.Adapter, error) {
		if model != stub.model {
			return nil, fmt.Errorf("no model %s", model)
		}
		return stub, nil
	}, nil, 2, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var sb strings.Builder
	var last Chunk
	err = NewClient(clientHost, nil).Chat(ctx, providerHost.ID(), &Request{
		Model:    "test-model",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	}, func(c Chunk) error {
		sb.WriteString(c.Delta)
		last = c
		return nil
	})
	if err != nil {
		t.Fatalf("remote inference failed: %v", err)
	}
	if got := sb.String(); got != "你好世界" {
		t.Errorf("joined deltas = %q, want %q", got, "你好世界")
	}
	if !last.Done {
		t.Error("last frame Done should be true")
	}
	if last.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", last.FinishReason)
	}
}

func TestChatUnknownModelReturnsRemoteError(t *testing.T) {
	providerHost, clientHost := newLinkedHosts(t)

	srv, err := NewServer(providerHost, func(_ context.Context, model string) (backend.Adapter, error) {
		return nil, fmt.Errorf("no model %s", model)
	}, nil, 1, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	err = NewClient(clientHost, nil).Chat(ctx, providerHost.ID(), &Request{
		Model:    "not-exist",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	}, func(Chunk) error { return nil })
	if err == nil {
		t.Fatal("unknown model should return an error")
	}
	var remote *ErrRemote
	if !asErrRemote(err, &remote) {
		t.Fatalf("error type = %T (%v), want *ErrRemote", err, err)
	}
	if !strings.Contains(remote.Detail, "not-exist") {
		t.Errorf("error message missing model name: %q", remote.Detail)
	}
}

func TestConcurrencyGateRejectsOverflow(t *testing.T) {
	providerHost, clientHost := newLinkedHosts(t)

	stub := &stubAdapter{
		model:   "busy-model",
		deltas:  []string{"ok"},
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	srv, err := NewServer(providerHost, func(context.Context, string) (backend.Adapter, error) {
		return stub, nil
	}, nil, 1, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cli := NewClient(clientHost, nil)
	req := func() *Request {
		return &Request{
			Model:    "busy-model",
			Messages: []backend.Message{{Role: "user", Content: "hi"}},
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	firstErr := make(chan error, 1)
	go func() {
		defer wg.Done()
		firstErr <- cli.Chat(ctx, providerHost.ID(), req(), func(Chunk) error { return nil })
	}()

	select {
	case <-stub.entered:
	case <-time.After(20 * time.Second):
		t.Fatal("first request did not reach backend")
	}

	secondErr := cli.Chat(ctx, providerHost.ID(), req(), func(Chunk) error { return nil })
	if secondErr == nil {
		t.Error("second request should be rejected with concurrency limit 1")
	}

	close(stub.release)
	wg.Wait()
	if err := <-firstErr; err != nil {
		t.Errorf("first request should succeed, got: %v", err)
	}
}

func asErrRemote(err error, out **ErrRemote) bool {
	if e, ok := err.(*ErrRemote); ok {
		*out = e
		return true
	}
	return false
}
