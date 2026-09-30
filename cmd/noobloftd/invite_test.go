// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/security/identity"
)

func TestInviteCLI(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "relay")
	target := filepath.Join(root, "consumer")
	if err := cmdInit([]string{"-dir", source, "-profile", "public-relay", "-announce", "/ip4/203.0.113.10/tcp/4001"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(source)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gateway.Enabled || cfg.Provider.Enabled || !cfg.Relay.Enabled || len(cfg.Backends) != 0 {
		t.Fatal("unsafe relay profile")
	}
	key, err := identity.LoadOrCreateIdentity(source)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := peerIDFromPriv(key)
	file := filepath.Join(root, "network.noobloft")
	if err := cmdInvite([]string{"export", "-dir", source, "-file", file}); err != nil {
		t.Fatal(err)
	}
	if err := cmdInit([]string{"-dir", target, "-profile", "wan", "-join", filepath.Join(source, config.SwarmKeyFileName)}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(target, config.ConfigFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := cmdInvite([]string{"import", "-dir", target, "-file", file, "-trust", "wrong"}); err == nil {
		t.Fatal("accepted wrong trust")
	}
	after, _ := os.ReadFile(filepath.Join(target, config.ConfigFileName))
	if !bytes.Equal(before, after) {
		t.Fatal("rejected invitation changed config")
	}
	for range 2 {
		if err := cmdInvite([]string{"import", "-dir", target, "-file", file, "-trust", id}); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err = config.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Network.StaticPeers) != 1 || len(cfg.Relay.StaticRelays) != 1 || cfg.Network.DHTMode != "auto" {
		t.Fatal("import failed or duplicated entries")
	}
	if cfg.Provider.Enabled || cfg.Relay.Enabled {
		t.Fatal("import exposed a service")
	}
	if err := cmdInit([]string{"-dir", target}); err == nil {
		t.Fatal("overwrote initialized node")
	}
}
