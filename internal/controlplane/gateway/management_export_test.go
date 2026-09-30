// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
)

func TestManagementServiceExport(t *testing.T) {
	s, cfg, dir := managementFixture(t)
	path := "/manage/publisher/export?source=saved"
	for _, token := range []string{"", cfg.Gateway.AuthToken} {
		if r := managementRequest(s, "GET", path, token, "", nil); r.Code != 401 {
			t.Fatalf("unauthorized export: %d", r.Code)
		}
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"POST", path, 405}, {"GET", "/manage/publisher/export?source=invalid", 400}, {"GET", path, 404},
	} {
		if r := managementRequest(s, tc.method, tc.path, testAdminToken, "", nil); r.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, r.Code)
		}
	}
	key, _, err := crypto.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := peer.IDFromPrivateKey(key)
	doc, err := publisher.Sign(key, publisher.Document{Kind: "service", Peers: []string{id.String()}, Models: []string{"test-model"}, Addresses: []string{"/ip4/127.0.0.1/tcp/4001/p2p/" + id.String()}}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Provider.Service = doc
	cfg.Provider.RevocationsFile = "test-revocations.json"
	if err := config.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	r := managementRequest(s, "GET", path, testAdminToken, "", nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("export must not be cached")
	}
	var exported publisher.Document
	if err := json.Unmarshal(r.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc, &exported) {
		t.Fatal("export altered signed document")
	}
	if err := exported.Verify(doc.Publisher, "service", time.Now()); err != nil {
		t.Fatal(err)
	}
	if r := managementRequest(s, "GET", "/manage/publisher/export?source=running", testAdminToken, "", nil); r.Code != 404 {
		t.Fatal("saved document leaked into running state")
	}
	state := managementRequest(s, "GET", "/manage/state", testAdminToken, "", nil)
	var data struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(state.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"operation": "subscribe", "trust": doc.Publisher, "document": &exported}
	if r := managementRequest(s, "POST", "/manage/publisher", testAdminToken, data.Revision, body); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	cfg.Provider.Service.Models = []string{"tampered"}
	if err := config.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	if r := managementRequest(s, "GET", path, testAdminToken, "", nil); r.Code != 400 {
		t.Fatal("invalid signature exported")
	}
}
