// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"

	"github.com/xiayu1987/noobloft/internal/serving/capability"
)

func newTestHost(t *testing.T) host.Host {
	t.Helper()
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.DisableRelay(),
	)
	if err != nil {
		t.Fatalf("start test node failed: %v", err)
	}
	return h
}

func TestRefreshIntervalIsSafelyBelowMaxAge(t *testing.T) {
	if RefreshInterval >= capability.MaxAge {
		t.Fatalf("RefreshInterval(%v) must be less than capability.MaxAge(%v)", RefreshInterval, capability.MaxAge)
	}
	if RefreshInterval*2 >= capability.MaxAge {
		t.Fatalf("RefreshInterval(%v) should be less than MaxAge/2(%v) to tolerate one failed refresh",
			RefreshInterval, capability.MaxAge/2)
	}
}

func TestRefreshAnnouncementRenewsIssuedAt(t *testing.T) {
	h := newTestHost(t)
	defer h.Close()

	svc := New(h, nil)
	svc.EnableSelfAnnouncement(h.Peerstore().PrivKey(h.ID()), 2, func() []capability.Model {
		return []capability.Model{{Name: "qwen3:14b", ParameterSize: "14.8B"}}
	})

	if err := svc.RefreshAnnouncement(); err != nil {
		t.Fatalf("initial re-sign failed: %v", err)
	}
	first := svc.Announcement()
	if first == nil {
		t.Fatal("announcement still nil after re-sign")
	}
	if err := capability.Verify(first, h.ID()); err != nil {
		t.Fatalf("initial announcement verification failed: %v", err)
	}

	stale := *first
	stale.IssuedAt = time.Now().Add(-capability.MaxAge - time.Minute)
	svc.SetAnnouncement(&stale)
	if err := capability.Verify(&stale, h.ID()); err == nil {
		t.Fatal("expired announcement should fail verification, MaxAge not enforced")
	}

	if err := svc.RefreshAnnouncement(); err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	renewed := svc.Announcement()
	if err := capability.Verify(renewed, h.ID()); err != nil {
		t.Fatalf("announcement still invalid after renew: %v", err)
	}
	if !renewed.IssuedAt.After(stale.IssuedAt) {
		t.Fatalf("renew did not advance IssuedAt: stale=%v renewed=%v", stale.IssuedAt, renewed.IssuedAt)
	}
}

func TestMaintenanceLoopRunsWithoutDHT(t *testing.T) {
	h := newTestHost(t)
	defer h.Close()

	svc := New(h, nil)
	svc.EnableSelfAnnouncement(h.Peerstore().PrivKey(h.ID()), 1, func() []capability.Model {
		return []capability.Model{{Name: "llama3:8b"}}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.StartMaintenanceLoop(ctx, func() []string { return []string{"llama3:8b"} })

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ann := svc.Announcement(); ann != nil && len(ann.Signature) > 0 {
			if err := capability.Verify(ann, h.ID()); err != nil {
				t.Fatalf("announcement signed by loop is invalid: %v", err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("maintenance loop did not sign an announcement without DHT")
}

func TestRefreshAnnouncementNoopWithoutSigner(t *testing.T) {
	h := newTestHost(t)
	defer h.Close()

	svc := New(h, nil)
	if err := svc.RefreshAnnouncement(); err != nil {
		t.Fatalf("should be a no-op without signer but returned error: %v", err)
	}
	if svc.Announcement() != nil {
		t.Fatal("announcement produced while provider is disabled")
	}
}

func TestModelCIDIsStable(t *testing.T) {
	a, err := ModelCID("qwen3:14b")
	if err != nil {
		t.Fatalf("compute CID failed: %v", err)
	}
	b, err := ModelCID("qwen3:14b")
	if err != nil {
		t.Fatalf("compute CID failed: %v", err)
	}
	if !a.Equals(b) {
		t.Fatalf("CID unstable for same model: %s != %s", a, b)
	}
	c, err := ModelCID("qwen3:32b")
	if err != nil {
		t.Fatalf("compute CID failed: %v", err)
	}
	if a.Equals(c) {
		t.Fatal("different model names mapped to the same CID")
	}
}
