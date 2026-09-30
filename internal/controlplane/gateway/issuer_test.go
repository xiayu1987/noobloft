// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
	"github.com/xiayu1987/noobloft/internal/serving/inference"
)

func TestIssuerLifecycleAndAdmission(t *testing.T) {
	s, c, dir := managementFixture(t)
	key, _, _ := crypto.GenerateEd25519Key(nil)
	self, _ := peer.IDFromPrivateKey(key)
	s.manager.opts.Self = self.String()
	call := func(body any) map[string]json.RawMessage {
		t.Helper()
		cfg, e := config.Load(dir)
		if e != nil {
			t.Fatal(e)
		}
		rec := managementRequest(s, "POST", "/manage/issuer", testAdminToken, revision(cfg), body)
		if rec.Code != 200 {
			t.Fatalf("%d: %s", rec.Code, rec.Body.String())
		}
		var out map[string]json.RawMessage
		if e = json.Unmarshal(rec.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		var syncError string
		json.Unmarshal(out["syncError"], &syncError)
		if syncError != "" {
			t.Fatal(syncError)
		}
		return out
	}
	for _, tok := range []string{"", c.Gateway.AuthToken} {
		if r := managementRequest(s, "POST", "/manage/issuer", tok, revision(c), map[string]any{"operation": "init"}); r.Code != 401 {
			t.Fatal("issuer authentication bypass")
		}
	}
	if r := managementRequest(s, "POST", "/manage/issuer", testAdminToken, "stale", map[string]any{"operation": "init"}); r.Code != 409 {
		t.Fatal("stale init accepted")
	}
	call(map[string]any{"operation": "init"})
	store := publisher.Issuer{Dir: dir}
	id, e := store.ID()
	if e != nil {
		t.Fatal(e)
	}
	if id == self.String() {
		t.Fatal("publisher reused transport identity")
	}
	issue := func(d publisher.Document) *publisher.Document {
		t.Helper()
		out := call(map[string]any{"operation": "issue", "ttlHours": 24, "document": d})
		var signed publisher.Document
		json.Unmarshal(out["issued"], &signed)
		if e := signed.Verify(id, d.Kind, time.Now()); e != nil {
			t.Fatal(e)
		}
		return &signed
	}
	service := issue(publisher.Document{Kind: "service", Peers: []string{self.String()}, Models: []string{"model"}})
	access := issue(publisher.Document{Kind: "access", Subject: self.String(), Models: []string{"model"}, MaxTokens: 10, MaxConcurrent: 1, RequestsPerMinute: 30})
	rev, e := publisher.Load(store.SnapshotPath())
	if e != nil {
		t.Fatal(e)
	}
	rec := managementRequest(s, "POST", "/manage/publisher", testAdminToken, revision(c), map[string]any{"operation": "install", "trust": id, "document": service, "revocations": rev})
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	saved, e := config.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	s.manager.opts.Running = saved
	auth := inference.Authorization{Service: *service, RevocationsFile: saved.Provider.RevocationsFile}
	n := 1
	header := inference.AuthHeader{Publisher: id, Model: "model", Access: access, MaxTokens: &n}
	release, e := auth.Acquire(self, header)
	if e != nil {
		t.Fatal(e)
	}
	release()
	out := call(map[string]any{"operation": "revoke", "target": access.ID})
	var applied bool
	json.Unmarshal(out["applied"], &applied)
	if !applied {
		t.Fatal("live application not confirmed")
	}
	if _, e = auth.Acquire(self, header); e == nil {
		t.Fatal("revoked credential still admitted")
	}
	before, _ := publisher.Load(store.SnapshotPath())
	call(map[string]any{"operation": "revoke", "target": access.ID})
	after, _ := publisher.Load(store.SnapshotPath())
	if before.Sequence != after.Sequence {
		t.Fatal("repeat revocation not idempotent")
	}
	records, e := (publisher.Issuer{Dir: dir}).Records()
	if e != nil || len(records) != 2 {
		t.Fatalf("records not durable: %v", e)
	}
	raw, e := os.ReadFile(filepath.Join(store.Path(), "identity.key"))
	if e != nil {
		t.Fatal(e)
	}
	root, e := crypto.UnmarshalPrivateKey(raw)
	if e != nil {
		t.Fatal(e)
	}
	short, e := publisher.Sign(root, publisher.Document{Kind: "revocations", Sequence: after.Sequence + 1, Revoked: after.Revoked}, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	if e = publisher.ReplaceRevocations(store.SnapshotPath(), short); e != nil {
		t.Fatal(e)
	}
	s.manager.renewAuthority()
	renewed, _ := publisher.Load(store.SnapshotPath())
	if renewed.Sequence <= short.Sequence || renewed.ExpiresAt <= short.ExpiresAt {
		t.Fatal("automatic renewal did not run")
	}
	if _, e = auth.Acquire(self, header); e == nil {
		t.Fatal("renewal resurrected revoked credential")
	}
	other := issue(publisher.Document{Kind: "access", Subject: self.String(), Models: []string{"model"}, MaxTokens: 10, MaxConcurrent: 1, RequestsPerMinute: 30})
	header.Access = other
	release, e = auth.Acquire(self, header)
	if e != nil {
		t.Fatal(e)
	}
	release()
	call(map[string]any{"operation": "revoke", "target": self.String()})
	if _, e = auth.Acquire(self, header); e == nil {
		t.Fatal("consumer revocation ignored")
	}
	r := managementRequest(s, "GET", "/manage/issuer", testAdminToken, "", nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
}
