// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/security/identity"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
	"github.com/xiayu1987/noobloft/internal/serving/backend"
	"github.com/xiayu1987/noobloft/internal/serving/inference"
)

func newSharedConfig(t *testing.T) (string, *config.Config, peer.ID) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Network.Mode = "shared"
	cfg.Network.ListenAddrs = []string{"/ip4/127.0.0.1/tcp/0"}
	cfg.Network.EnableMDNS = false
	cfg.Network.EnableDHT = false
	cfg.Network.EnableHolePunch = false
	cfg.Gateway.Enabled = false
	cfg.Backends = nil
	cfg.Policy.AuditEnabled = false
	key, e := identity.LoadOrCreateIdentity(dir)
	if e != nil {
		t.Fatal(e)
	}
	id, e := peer.IDFromPrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	return dir, cfg, id
}
func signDoc(t *testing.T, key crypto.PrivKey, d publisher.Document) *publisher.Document {
	t.Helper()
	v, e := publisher.Sign(key, d, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func startShared(t *testing.T, ctx context.Context, dir string, cfg *config.Config) *runtime {
	t.Helper()
	if e := config.Save(dir, cfg); e != nil {
		t.Fatal(e)
	}
	rt, e := buildRuntime(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { rt.close() })
	return rt
}

func TestPublisherRuntimeIsolationAndRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cdir, ccfg, cid := newSharedConfig(t)
	type fixture struct {
		rt          *runtime
		root        crypto.PrivKey
		svc, access *publisher.Document
		rev         string
		calls       *atomic.Int32
	}
	var providers []fixture
	for _, label := range []string{"A", "B"} {
		calls := &atomic.Int32{}
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/tags":
				fmt.Fprint(w, `{"models":[{"name":"same"},{"name":"hidden"}]}`)
			case "/api/chat":
				calls.Add(1)
				json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": label}, "done": true})
			default:
				http.NotFound(w, r)
			}
		}))
		defer upstream.Close()
		dir, cfg, id := newSharedConfig(t)
		root, _, e := crypto.GenerateEd25519Key(nil)
		if e != nil {
			t.Fatal(e)
		}
		svc := signDoc(t, root, publisher.Document{Kind: "service", Peers: []string{id.String()}, Models: []string{"same"}})
		access := signDoc(t, root, publisher.Document{Kind: "access", Subject: cid.String(), Models: []string{"same"}, MaxTokens: 16, MaxConcurrent: 1, RequestsPerMinute: 30})
		rev := signDoc(t, root, publisher.Document{Kind: "revocations", Sequence: 1})
		revPath := filepath.Join(dir, "revocations.json")
		if e = publisher.SaveNew(revPath, rev); e != nil {
			t.Fatal(e)
		}
		cfg.Provider.Enabled = true
		cfg.Provider.Service = svc
		cfg.Provider.RevocationsFile = revPath
		cfg.Backends = []config.BackendConfig{{Name: "stub", Kind: "ollama", BaseURL: upstream.URL, Enabled: true}}
		rt := startShared(t, ctx, dir, cfg)
		providers = append(providers, fixture{rt, root, svc, access, revPath, calls})
		ccfg.Subscriptions = append(ccfg.Subscriptions, publisher.Subscription{Service: *svc, Access: access})
	}
	consumer := startShared(t, ctx, cdir, ccfg)
	for _, p := range providers {
		if e := consumer.nd.Host().Connect(ctx, peer.AddrInfo{ID: p.rt.nd.ID(), Addrs: p.rt.nd.Host().Addrs()}); e != nil {
			t.Fatal(e)
		}
	}
	ask := func(model string) (string, error) {
		var b strings.Builder
		e := consumer.rtr.Chat(ctx, &inference.Request{Model: model, Messages: []backend.Message{{Role: "user", Content: "private prompt"}}}, func(c inference.Chunk) error { b.WriteString(c.Delta); return nil })
		return b.String(), e
	}
	for i, p := range providers {
		got, e := ask(publisher.Qualified(p.svc.Publisher, "same"))
		if e != nil || got != []string{"A", "B"}[i] {
			t.Fatalf("isolation: %q %v", got, e)
		}
	}
	if _, e := ask("same"); e == nil {
		t.Fatal("shared unscoped request accepted")
	}
	if _, e := ask(publisher.Qualified(providers[0].svc.Publisher, "hidden")); e == nil {
		t.Fatal("unauthorized model accepted")
	}
	names := consumer.rtr.RemoteModelNames(ctx)
	if len(names) != 2 {
		t.Fatalf("model list %v", names)
	}
	if _, e := consumer.nd.Host().NewStream(ctx, providers[0].rt.nd.ID(), inference.Protocol); e == nil {
		t.Fatal("legacy protocol bypass")
	}
	adir, acfg, _ := newSharedConfig(t)
	attacker := startShared(t, ctx, adir, acfg)
	p := providers[0]
	if e := attacker.nd.Host().Connect(ctx, peer.AddrInfo{ID: p.rt.nd.ID(), Addrs: p.rt.nd.Host().Addrs()}); e != nil {
		t.Fatal(e)
	}
	n := 1
	e := inference.NewClient(attacker.nd.Host(), nil).Chat(ctx, p.rt.nd.ID(), &inference.Request{Publisher: p.svc.Publisher, Model: "same", Access: p.access, MaxTokens: &n, Messages: []backend.Message{{Role: "user", Content: "stolen credential"}}}, func(inference.Chunk) error { return nil })
	if e == nil {
		t.Fatal("stolen credential accepted")
	}
	rev := signDoc(t, p.root, publisher.Document{Kind: "revocations", Sequence: 2, Revoked: []string{p.access.ID}})
	if e = publisher.ReplaceRevocations(p.rev, rev); e != nil {
		t.Fatal(e)
	}
	if _, e = ask(publisher.Qualified(p.svc.Publisher, "same")); e == nil {
		t.Fatal("revocation not applied")
	}
	if got, e := ask(publisher.Qualified(providers[1].svc.Publisher, "same")); e != nil || got != "B" {
		t.Fatalf("A revocation affected B: %q %v", got, e)
	}
	if p.calls.Load() != 1 {
		t.Fatalf("denied requests reached backend: %d", p.calls.Load())
	}
	p.rt.srv.Close()
	if _, e = ask(publisher.Qualified(p.svc.Publisher, "same")); e == nil {
		t.Fatal("offline publisher unexpectedly served")
	}
}

func TestPublisherCLI(t *testing.T) {
	root := filepath.Join(t.TempDir(), "offline-root")
	run := func(args ...string) {
		t.Helper()
		if e := cmdPublisher(args); e != nil {
			t.Fatalf("%v: %v", args, e)
		}
	}
	run("init", "-root", root)
	dir, cfg, id := newSharedConfig(t)
	if e := config.Save(dir, cfg); e != nil {
		t.Fatal(e)
	}
	svcFile := filepath.Join(t.TempDir(), "publisher.noobloft")
	run("service", "-root", root, "-file", svcFile, "-peers", id.String(), "-models", "same")
	svc, e := publisher.Load(svcFile)
	if e != nil {
		t.Fatal(e)
	}
	if e = cmdPublisher([]string{"subscribe", "-dir", dir, "-file", svcFile, "-trust", id.String()}); e == nil {
		t.Fatal("wrong publisher trust accepted")
	}
	run("subscribe", "-dir", dir, "-file", svcFile, "-trust", svc.Publisher)
	run("subscribe", "-dir", dir, "-file", svcFile, "-trust", svc.Publisher)
	grant := filepath.Join(t.TempDir(), "access.json")
	run("grant", "-root", root, "-file", grant, "-subject", id.String(), "-models", "same")
	run("credential", "-dir", dir, "-file", grant, "-trust", svc.Publisher)
	rev := filepath.Join(t.TempDir(), "rev.json")
	run("revocations", "-root", root, "-file", rev)
	run("install", "-dir", dir, "-file", svcFile, "-trust", svc.Publisher, "-revocations", rev)
	got, e := config.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(got.Subscriptions) != 1 || got.Subscriptions[0].Access == nil || got.Provider.Service == nil {
		t.Fatal("CLI state not persisted")
	}
	next := filepath.Join(t.TempDir(), "next.json")
	run("revocations", "-root", root, "-file", next, "-previous", rev, "-revoked", id.String())
	run("apply-revocations", "-dir", dir, "-file", next, "-trust", svc.Publisher)
	if e = cmdPublisher([]string{"apply-revocations", "-dir", dir, "-file", next, "-trust", svc.Publisher}); e == nil {
		t.Fatal("snapshot replay accepted")
	}
}
