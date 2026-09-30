// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package capability

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/serving/backend"
)

func newTestIdentity(t *testing.T) (crypto.PrivKey, peer.ID) {
	t.Helper()
	priv, pub, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("generate test key failed: %v", err)
	}
	id, err := peer.IDFromPublicKey(pub)
	if err != nil {
		t.Fatalf("derive PeerID failed: %v", err)
	}
	return priv, id
}

func TestSignAndVerify(t *testing.T) {
	priv, id := newTestIdentity(t)
	models := []Model{{Name: "llama3.1:8b", ParameterSize: "8B", License: "llama"}}

	ann, err := Sign(priv, id, models, 2)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	if err := Verify(ann, id); err != nil {
		t.Fatalf("verification should succeed, got: %v", err)
	}
}

func TestVerifyRejectsForeignPeer(t *testing.T) {
	priv, id := newTestIdentity(t)
	_, otherID := newTestIdentity(t)

	ann, err := Sign(priv, id, []Model{{Name: "m"}}, 1)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	if err := Verify(ann, otherID); err == nil {
		t.Fatal("announcement with mismatched origin must be rejected")
	}
}

func TestVerifyRejectsTamperedModels(t *testing.T) {
	priv, id := newTestIdentity(t)
	ann, err := Sign(priv, id, []Model{{Name: "llama3.1:8b", ParameterSize: "8B"}}, 1)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	ann.Models[0].ParameterSize = "70B"
	if err := Verify(ann, id); err == nil {
		t.Fatal("tampered announcement must fail verification")
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	priv, id := newTestIdentity(t)
	ann, err := Sign(priv, id, []Model{{Name: "m"}}, 1)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	ann.IssuedAt = time.Now().UTC().Add(-2 * MaxAge)
	payload, err := ann.signingPayload()
	if err != nil {
		t.Fatalf("build payload failed: %v", err)
	}
	sig, err := priv.Sign(payload)
	if err != nil {
		t.Fatalf("re-sign failed: %v", err)
	}
	ann.Signature = sig

	err = Verify(ann, id)
	if err == nil {
		t.Fatal("expired announcement must be rejected")
	}
}

func TestFromModelInfosAllowlist(t *testing.T) {
	infos := []backend.ModelInfo{
		{Name: "keep-me", ParameterSize: "8B"},
		{Name: "drop-me", ParameterSize: "70B"},
	}

	all := FromModelInfos(infos, nil)
	if len(all) != 2 {
		t.Fatalf("empty allowlist should announce all models, got %d", len(all))
	}

	filtered := FromModelInfos(infos, []string{"keep-me"})
	if len(filtered) != 1 || filtered[0].Name != "keep-me" {
		t.Fatalf("allowlist filter mismatch: %+v", filtered)
	}
}

func TestHasModel(t *testing.T) {
	a := &Announcement{Models: []Model{{Name: "a"}, {Name: "b"}}}
	if !a.HasModel("b") {
		t.Fatal("model b should match")
	}
	if a.HasModel("c") {
		t.Fatal("missing model c should not match")
	}
}
