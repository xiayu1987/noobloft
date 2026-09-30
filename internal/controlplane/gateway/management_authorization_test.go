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
	"github.com/xiayu1987/noobloft/internal/serving/router"
)

func authorizationFixture(t *testing.T) (*Server, *config.Config, string, crypto.PrivKey, peer.ID) {
	t.Helper()
	dir := t.TempDir()
	selfKey, _, err := crypto.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	self, err := peer.IDFromPrivateKey(selfKey)
	if err != nil {
		t.Fatal(err)
	}
	pubKey, _, err := crypto.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.Gateway.AuthToken = "inference-test-secret"
	if err := config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Token: c.Gateway.AuthToken, Router: router.New(nil, nil, nil, nil), Management: &ManagementOptions{Dir: dir, Self: self.String(), Token: testAdminToken, Running: c}})
	if err != nil {
		t.Fatal(err)
	}
	return s, c, dir, pubKey, self
}

func authorizationBody(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rec := managementRequest(s, "GET", "/manage/authorization", testAdminToken, "", nil)
	if rec.Code != 200 {
		t.Fatalf("authorization endpoint: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func findCodes(t *testing.T, body map[string]any) map[string]string {
	t.Helper()
	out := map[string]string{}
	items, ok := body["findings"].([]any)
	if !ok {
		t.Fatalf("findings missing: %v", body)
	}
	for _, item := range items {
		f, ok := item.(map[string]any)
		if !ok {
			t.Fatal("finding is not an object")
		}
		out[f["code"].(string)] = f["severity"].(string)
	}
	return out
}

func installDelegation(t *testing.T, s *Server, c *config.Config, dir string, pub crypto.PrivKey, self peer.ID, models []string, revocations []string, ttl time.Duration) {
	t.Helper()
	doc, err := publisher.Sign(pub, publisher.Document{Kind: "service", Peers: []string{self.String()}, Models: models}, ttl)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := publisher.Sign(pub, publisher.Document{Kind: "revocations", Sequence: 1, Revoked: revocations}, ttl)
	if err != nil {
		t.Fatal(err)
	}
	c.Provider.Service = doc
	c.Provider.RevocationsFile = "revocations.json"
	if err := config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rev)
	if err := os.WriteFile(filepath.Join(dir, "revocations.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestManagementAuthorizationSignals(t *testing.T) {
	t.Run("no delegation blocks", func(t *testing.T) {
		s, _, _, _, _ := authorizationFixture(t)
		body := authorizationBody(t, s)
		if body["status"] != "blocked" {
			t.Fatalf("status = %v", body["status"])
		}
		if findCodes(t, body)["service_missing"] != "blocked" {
			t.Fatal("service_missing not reported as blocked")
		}
	})

	t.Run("installed but provider disabled blocks", func(t *testing.T) {
		s, c, dir, pub, self := authorizationFixture(t)
		installDelegation(t, s, c, dir, pub, self, []string{"m"}, nil, time.Hour)
		body := authorizationBody(t, s)
		if body["status"] != "blocked" {
			t.Fatalf("status = %v", body["status"])
		}
		codes := findCodes(t, body)
		if codes["provider_disabled"] != "blocked" {
			t.Fatalf("provider_disabled missing: %v", codes)
		}
		if _, ok := codes["service_invalid"]; ok {
			t.Fatalf("valid delegation reported invalid: %v", codes)
		}
	})

	t.Run("expired snapshot blocks", func(t *testing.T) {
		s, c, dir, pub, self := authorizationFixture(t)
		installDelegation(t, s, c, dir, pub, self, []string{"m"}, nil, time.Hour)
		path := filepath.Join(dir, "revocations.json")
		rev, err := publisher.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		rev.ExpiresAt = time.Now().Add(-time.Minute).Unix()
		raw, _ := json.Marshal(rev)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		body := authorizationBody(t, s)
		if body["status"] != "blocked" {
			t.Fatalf("status = %v", body["status"])
		}
		if findCodes(t, body)["revocations_invalid"] != "blocked" {
			t.Fatal("expired snapshot not reported as blocked")
		}
	})

	t.Run("revoking this node blocks", func(t *testing.T) {
		s, c, dir, pub, self := authorizationFixture(t)
		installDelegation(t, s, c, dir, pub, self, []string{"m"}, []string{self.String()}, time.Hour)
		body := authorizationBody(t, s)
		if body["status"] != "blocked" {
			t.Fatalf("status = %v", body["status"])
		}
		if findCodes(t, body)["revoked_node"] != "blocked" {
			t.Fatal("revoked node not reported as blocked")
		}
		rev := body["revocations"].(map[string]any)
		if rev["hitsThisNode"] != true {
			t.Fatal("hitsThisNode not set")
		}
	})

	t.Run("advertising outside delegation warns", func(t *testing.T) {
		s, c, dir, pub, self := authorizationFixture(t)
		installDelegation(t, s, c, dir, pub, self, []string{"m"}, nil, time.Hour)
		c.Provider.Enabled = true
		c.Provider.AdvertiseModels = []string{"m"}
		if err := config.Save(dir, c); err != nil {
			t.Fatal(err)
		}
		body := authorizationBody(t, s)
		if body["status"] == "blocked" {
			t.Fatalf("healthy node reported blocked: %v", findCodes(t, body))
		}
		if len(body["servedModels"].([]any)) != 1 {
			t.Fatalf("servedModels = %v", body["servedModels"])
		}
	})

	t.Run("zero intersection blocks", func(t *testing.T) {
		s, c, dir, pub, self := authorizationFixture(t)
		installDelegation(t, s, c, dir, pub, self, []string{"delegated"}, nil, time.Hour)
		c.Provider.Enabled = true
		c.Provider.AdvertiseModels = []string{"unrelated"}
		if err := config.Save(dir, c); err != nil {
			t.Fatal(err)
		}
		body := authorizationBody(t, s)
		codes := findCodes(t, body)
		if codes["no_served_models"] != "blocked" {
			t.Fatalf("no_served_models missing: %v", codes)
		}
		if codes["advertised_not_delegated"] != "warning" {
			t.Fatalf("advertised_not_delegated missing: %v", codes)
		}
	})

	t.Run("not delegated to this node blocks", func(t *testing.T) {
		s, c, dir, pub, _ := authorizationFixture(t)
		other, _, err := crypto.GenerateEd25519Key(nil)
		if err != nil {
			t.Fatal(err)
		}
		otherID, _ := peer.IDFromPrivateKey(other)
		doc, err := publisher.Sign(pub, publisher.Document{Kind: "service", Peers: []string{otherID.String()}, Models: []string{"m"}}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		rev, err := publisher.Sign(pub, publisher.Document{Kind: "revocations", Sequence: 1}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		c.Provider.Service = doc
		c.Provider.RevocationsFile = "revocations.json"
		if err := config.Save(dir, c); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(rev)
		if err := os.WriteFile(filepath.Join(dir, "revocations.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		body := authorizationBody(t, s)
		if findCodes(t, body)["node_not_delegated"] != "blocked" {
			t.Fatalf("node_not_delegated missing: %v", findCodes(t, body))
		}
	})

	t.Run("missing snapshot blocks", func(t *testing.T) {
		s, c, dir, pub, self := authorizationFixture(t)
		installDelegation(t, s, c, dir, pub, self, []string{"m"}, nil, time.Hour)
		if err := os.Remove(filepath.Join(dir, "revocations.json")); err != nil {
			t.Fatal(err)
		}
		body := authorizationBody(t, s)
		if findCodes(t, body)["revocations_unavailable"] != "blocked" {
			t.Fatalf("revocations_unavailable missing: %v", findCodes(t, body))
		}
	})

	t.Run("management credential is required", func(t *testing.T) {
		s, _, _, _, _ := authorizationFixture(t)
		rec := managementRequest(s, "GET", "/manage/authorization", "wrong-token", "", nil)
		if rec.Code != 401 {
			t.Fatalf("unauthenticated access: %d", rec.Code)
		}
		rec = managementRequest(s, "POST", "/manage/authorization", testAdminToken, "", nil)
		if rec.Code != 405 {
			t.Fatalf("non-GET accepted: %d", rec.Code)
		}
	})
}
