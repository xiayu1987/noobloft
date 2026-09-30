// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package publisher

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestDocumentTrustAndExpiry(t *testing.T) {
	k, _, e := crypto.GenerateEd25519Key(nil)
	if e != nil {
		t.Fatal(e)
	}
	id, _ := peer.IDFromPrivateKey(k)
	d, e := Sign(k, Document{Kind: "service", Peers: []string{id.String()}, Models: []string{"model"}}, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(d)
	round, e := Decode(strings.NewReader(string(b)))
	if e != nil {
		t.Fatal(e)
	}
	if e = round.Verify(id.String(), "service", time.Now()); e != nil {
		t.Fatal(e)
	}
	if e = round.Verify(id.String(), "access", time.Now()); e == nil {
		t.Fatal("cross-purpose replay")
	}
	if e = round.Verify(id.String(), "service", time.Now().Add(2*time.Hour)); e == nil {
		t.Fatal("expired accepted")
	}
	round.Models = []string{"changed"}
	if e = round.Verify(id.String(), "service", time.Now()); e == nil {
		t.Fatal("tampering accepted")
	}
	if _, e = Decode(strings.NewReader(strings.Repeat("x", MaxDocumentBytes+1))); e == nil {
		t.Fatal("oversized document accepted")
	}
}
func TestRevocationsCannotRollbackOrRemove(t *testing.T) {
	k, _, _ := crypto.GenerateEd25519Key(nil)
	first, e := Sign(k, Document{Kind: "revocations", Sequence: 1, Revoked: []string{"consumer"}}, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "revocations.json")
	if e = SaveNew(path, first); e != nil {
		t.Fatal(e)
	}
	if e = ReplaceRevocations(path, first); e == nil {
		t.Fatal("rollback accepted")
	}
	second, _ := Sign(k, Document{Kind: "revocations", Sequence: 2}, time.Hour)
	if e = ReplaceRevocations(path, second); e == nil {
		t.Fatal("removed revocation accepted")
	}
	second, _ = Sign(k, Document{Kind: "revocations", Sequence: 2, Revoked: []string{"consumer", "other"}}, time.Hour)
	if e = ReplaceRevocations(path, second); e != nil {
		t.Fatal(e)
	}
	got, e := Load(path)
	if e != nil || got.Sequence != 2 {
		t.Fatalf("snapshot: %v %v", got, e)
	}
}
