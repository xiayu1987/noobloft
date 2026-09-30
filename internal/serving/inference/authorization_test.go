// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package inference

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/security/publisher"
)

func TestAuthorizationLimitsAndFailClosed(t *testing.T) {
	key, _, _ := crypto.GenerateEd25519Key(nil)
	id, _ := peer.IDFromPrivateKey(key)
	sign := func(d publisher.Document) *publisher.Document {
		t.Helper()
		v, e := publisher.Sign(key, d, time.Hour)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	service := sign(publisher.Document{Kind: "service", Peers: []string{id.String()}, Models: []string{"m"}})
	access := sign(publisher.Document{Kind: "access", Subject: id.String(), Models: []string{"m"}, MaxTokens: 2, MaxConcurrent: 1, RequestsPerMinute: 2})
	rev := sign(publisher.Document{Kind: "revocations", Sequence: 1})
	path := filepath.Join(t.TempDir(), "rev.json")
	if e := publisher.SaveNew(path, rev); e != nil {
		t.Fatal(e)
	}
	a := &Authorization{Service: *service, RevocationsFile: path}
	n := 1
	h := AuthHeader{Publisher: service.Publisher, Model: "m", MaxTokens: &n, Access: access}
	release, e := a.Acquire(id, h)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Acquire(id, h); e == nil {
		t.Fatal("concurrency exceeded")
	}
	release()
	release()
	release, e = a.Acquire(id, h)
	if e != nil {
		t.Fatal(e)
	}
	release()
	h.Access = sign(publisher.Document{Kind: "access", Subject: id.String(), Models: []string{"m"}, MaxTokens: 2, MaxConcurrent: 1, RequestsPerMinute: 2})
	if _, e = a.Acquire(id, h); e == nil {
		t.Fatal("rate bypass via new credential")
	}
	a.usage = nil
	n = 3
	if _, e = a.Acquire(id, h); e == nil {
		t.Fatal("token cap bypass")
	}
	n = 1
	h.Access = nil
	if _, e = a.Acquire(id, h); e == nil {
		t.Fatal("missing credential accepted")
	}
	h.Access = access
	a.RevocationsFile = path + "missing"
	if _, e = a.Acquire(id, h); e == nil {
		t.Fatal("missing revocations accepted")
	}
	a.RevocationsFile = path
	expired := *access
	expired.ExpiresAt = time.Now().Add(-time.Second).Unix()
	h.Access = &expired
	if _, e = a.Acquire(id, h); e == nil {
		t.Fatal("expired access accepted")
	}
}
