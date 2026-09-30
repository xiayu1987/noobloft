// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
	"github.com/xiayu1987/noobloft/internal/serving/router"
)

func TestManagementInstallStagesRevocations(t *testing.T) {
	dir := t.TempDir()
	key, _, _ := crypto.GenerateEd25519Key(nil)
	id, _ := peer.IDFromPrivateKey(key)
	service, err := publisher.Sign(key, publisher.Document{Kind: "service", Peers: []string{id.String()}, Models: []string{"model"}}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	old, err := publisher.Sign(key, publisher.Document{Kind: "revocations", Sequence: 2, Revoked: []string{"old-access"}}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "live-revocations.json")
	raw, _ := json.Marshal(old)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.Gateway.AuthToken = "test-inference"
	c.Provider.Service = service
	c.Provider.RevocationsFile = path
	if err = config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Token: c.Gateway.AuthToken, Router: router.New(nil, nil, nil, nil), Management: &ManagementOptions{Dir: dir, Self: id.String(), Token: testAdminToken, Running: c}})
	if err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []uint64{1, 3} {
		doc, err := publisher.Sign(key, publisher.Document{Kind: "revocations", Sequence: sequence, Revoked: []string{"old-access", "new-access"}}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		res := managementRequest(s, "POST", "/manage/publisher", testAdminToken, revision(c), map[string]any{"operation": "install", "trust": id.String(), "document": service, "revocations": doc})
		if sequence == 1 && res.Code != 400 {
			t.Fatal("rollback accepted")
		}
		if sequence == 3 && res.Code != 200 {
			t.Fatal(res.Body.String())
		}
		live, _ := os.ReadFile(path)
		if !bytes.Equal(raw, live) {
			t.Fatal("install changed live authorization")
		}
		files, _ := filepath.Glob(filepath.Join(dir, "revocations-*.json"))
		if sequence == 1 && len(files) != 0 {
			t.Fatal("failed install leaked staged snapshot")
		}
		if sequence == 3 {
			saved, err := config.Load(dir)
			if err != nil || saved.Provider.RevocationsFile == path {
				t.Fatal("new snapshot not installed")
			}
		}
	}
}
