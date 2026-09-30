// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package invitation

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestInvitationTrustAndTampering(t *testing.T) {
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := peer.IDFromPrivateKey(key)
	psk := bytes.Repeat([]byte{7}, 32)
	now := time.Now()
	addr := "/ip4/203.0.113.10/tcp/4001/p2p/" + id.String()
	v, err := Sign(key, psk, []string{addr}, []string{addr}, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	decoded, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := decoded.Verify(id.String(), psk, now); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Invitation){
		"address":   func(v *Invitation) { v.BootstrapPeers = []string{strings.Replace(addr, "4001", "4002", 1)} },
		"signature": func(v *Invitation) { v.Signature = []byte("invalid") },
		"issuer":    func(v *Invitation) { v.IssuerPeerID = "other" },
		"version":   func(v *Invitation) { v.Version = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			copy, _ := Decode(bytes.NewReader(raw))
			mutate(copy)
			if copy.Verify(id.String(), psk, now) == nil {
				t.Fatal("accepted tampered invitation")
			}
		})
	}
	if v.Verify("", psk, now) == nil {
		t.Fatal("accepted untrusted issuer")
	}
	if v.Verify(id.String(), bytes.Repeat([]byte{8}, 32), now) == nil {
		t.Fatal("accepted different network")
	}
	if v.Verify(id.String(), psk, now.Add(2*time.Hour)) == nil {
		t.Fatal("accepted expired invitation")
	}
	if _, err := Decode(strings.NewReader(strings.Repeat(" ", MaxSize+1))); err == nil {
		t.Fatal("accepted oversized input")
	}
	if _, err := Decode(bytes.NewReader(append(raw, []byte("{}")...))); err == nil {
		t.Fatal("accepted trailing object")
	}
}
