// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
	"github.com/xiayu1987/noobloft/internal/serving/backend"
	"github.com/xiayu1987/noobloft/internal/serving/router"
)

const testAdminToken = "test-admin-credential-not-for-production"

func managementFixture(t *testing.T) (*Server, *config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	c := config.Default()
	c.Gateway.AuthToken = "inference-test-secret"
	c.Backends[0].APIKey = "backend-test-secret"
	if err := config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Token: c.Gateway.AuthToken, Router: router.New(nil, nil, nil, nil), Management: &ManagementOptions{Dir: dir, Self: "consumer", Token: testAdminToken, Running: c}})
	if err != nil {
		t.Fatal(err)
	}
	return s, c, dir
}
func managementRequest(s *Server, method, path, token, rev string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("If-Match", rev)
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)
	return rec
}
func TestManagementAuthorizationAndSavedState(t *testing.T) {
	s, c, dir := managementFixture(t)
	for _, tok := range []string{"", c.Gateway.AuthToken} {
		r := managementRequest(s, "GET", "/manage/state", tok, "", nil)
		if r.Code != 401 {
			t.Fatalf("inference credential gained management access: %d", r.Code)
		}
	}
	req := httptest.NewRequest("GET", "/manage/state", nil)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	req.Header.Set("Origin", "https://other.example")
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("cross-origin request accepted")
	}
	rec = managementRequest(s, "GET", "/manage/state", testAdminToken, "", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "test-secret") {
		t.Fatal("secret leaked")
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	rev := body["revision"].(string)
	var settings settingsRequest
	raw, _ := json.Marshal(c)
	json.Unmarshal(raw, &settings)
	settings.Provider.MaxConcurrent = 7
	settings.Backends[0].APIKey = ""
	rec = managementRequest(s, "PUT", "/manage/config", testAdminToken, "stale", settings)
	if rec.Code != 409 {
		t.Fatal("stale configuration accepted")
	}
	rec = managementRequest(s, "PUT", "/manage/config", testAdminToken, rev, settings)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	saved, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Provider.MaxConcurrent != 7 || saved.Backends[0].APIKey != "backend-test-secret" {
		t.Fatal("configuration or secret preservation failed")
	}
	rec = managementRequest(s, "GET", "/manage/state", testAdminToken, "", nil)
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["pendingRestart"] != true {
		t.Fatal("missing restart marker")
	}
	running := body["running"].(map[string]any)
	if running["provider"].(map[string]any)["maxConcurrent"] != float64(2) {
		t.Fatal("saved configuration modified runtime snapshot")
	}
	rec = managementRequest(s, "POST", "/manage/publisher", testAdminToken, body["revision"].(string), map[string]any{"operation": "install"})
	if rec.Code != 400 {
		t.Fatalf("missing document: %d", rec.Code)
	}
	rec = managementRequest(s, "GET", "/", "", "", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "/assets/") {
		t.Fatal("embedded UI missing")
	}
}
func TestManagementSessionCookie(t *testing.T) {
	s, _, _ := managementFixture(t)
	do := func(method, path, origin string, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		s.httpSrv.Handler.ServeHTTP(rec, req)
		return rec
	}
	const self = "http://example.com"
	if r := do("POST", "/manage/session", self, nil, `{"token":"wrong"}`); r.Code != 401 {
		t.Fatalf("wrong token accepted: %d", r.Code)
	}
	if r := do("POST", "/manage/session", "https://evil.example", nil, `{"token":"`+testAdminToken+`"}`); r.Code != 403 {
		t.Fatalf("cross-origin login accepted: %d", r.Code)
	}
	if r := do("POST", "/manage/session", "", nil, `{"token":"`+testAdminToken+`"}`); r.Code != 403 {
		t.Fatalf("login without Origin accepted: %d", r.Code)
	}
	r := do("POST", "/manage/session", self, nil, `{"token":"`+testAdminToken+`"}`)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range r.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/manage" || strings.Contains(cookie.Value, testAdminToken) {
		t.Fatalf("unsafe session cookie: %+v", cookie)
	}
	if r := do("GET", "/manage/state", "", cookie, ""); r.Code != 200 {
		t.Fatalf("session cookie rejected: %d", r.Code)
	}
	if r := do("DELETE", "/manage/subscription", "", cookie, `{"publisher":"x"}`); r.Code != 403 {
		t.Fatalf("cookie write without Origin accepted: %d", r.Code)
	}
	if r := do("DELETE", "/manage/subscription", "https://evil.example", cookie, `{"publisher":"x"}`); r.Code != 403 {
		t.Fatalf("cross-origin cookie write accepted: %d", r.Code)
	}
	forged := *cookie
	forged.Value = "9999999999." + strings.Repeat("0", 64)
	if r := do("GET", "/manage/state", "", &forged, ""); r.Code != 401 {
		t.Fatalf("forged session accepted: %d", r.Code)
	}
	other := &manager{opts: ManagementOptions{Token: "another-admin-token-for-rotation-check"}}
	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	rotated := *cookie
	rotated.Value = exp + "." + other.sessionMAC(exp)
	if r := do("GET", "/manage/state", "", &rotated, ""); r.Code != 401 {
		t.Fatalf("session from rotated token accepted: %d", r.Code)
	}
	expired := *cookie
	past := strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	expired.Value = past + "." + (&manager{opts: ManagementOptions{Token: testAdminToken}}).sessionMAC(past)
	if r := do("GET", "/manage/state", "", &expired, ""); r.Code != 401 {
		t.Fatalf("expired session accepted: %d", r.Code)
	}
	r = do("DELETE", "/manage/session", self, cookie, "")
	if r.Code != 200 || len(r.Result().Cookies()) == 0 || r.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout did not clear cookie: %d", r.Code)
	}
}

func TestManagementSubscriptionTrust(t *testing.T) {
	s, _, _ := managementFixture(t)
	key, _, _ := crypto.GenerateEd25519Key(nil)
	id, _ := peer.IDFromPrivateKey(key)
	doc, err := publisher.Sign(key, publisher.Document{Kind: "service", Peers: []string{id.String()}, Models: []string{"model"}}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	state := managementRequest(s, "GET", "/manage/state", testAdminToken, "", nil)
	var b map[string]any
	json.Unmarshal(state.Body.Bytes(), &b)
	rev := b["revision"].(string)
	body := map[string]any{"operation": "subscribe", "trust": "wrong", "document": doc}
	if r := managementRequest(s, "POST", "/manage/publisher", testAdminToken, rev, body); r.Code != 400 {
		t.Fatal("untrusted publisher accepted")
	}
	body["trust"] = id.String()
	if r := managementRequest(s, "POST", "/manage/publisher", testAdminToken, rev, body); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	state = managementRequest(s, "GET", "/manage/state", testAdminToken, "", nil)
	json.Unmarshal(state.Body.Bytes(), &b)
	if r := managementRequest(s, "DELETE", "/manage/subscription", testAdminToken, b["revision"].(string), map[string]string{"publisher": id.String()}); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
}

func TestManagementBrowser(t *testing.T) {
	if os.Getenv("NOOBLOFT_BROWSER_TEST") != "1" {
		t.Skip("set NOOBLOFT_BROWSER_TEST=1 after npm ci and playwright install chromium")
	}
	s, cfg, dir := managementFixture(t)
	s.manager.opts.Restart = func() bool {
		go func() {
			s.manager.mu.Lock()
			defer s.manager.mu.Unlock()
			c, err := config.Load(dir)
			if err == nil {
				s.manager.opts.Running = c
				_ = config.CompleteRecovery(dir, c)
			}
		}()
		return true
	}
	key, _, err := crypto.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := peer.IDFromPrivateKey(key)
	doc, err := publisher.Sign(key, publisher.Document{Kind: "service", Peers: []string{id.String()}, Models: []string{"test-model"}}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Provider.Service = doc
	cfg.Provider.RevocationsFile = "test-revocations.json"
	cfg.Network.AnnounceAddrs = []string{"/ip4/127.0.0.1/tcp/4001"}
	if err := config.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[{"name":"test-model"}]}`))
			return
		}
		w.Write([]byte("{\"message\":{\"content\":\"浏览器流式测试成功\"},\"done\":false}\n{\"done\":true}\n"))
	}))
	defer mock.Close()
	adapter, err := backend.New(backend.Spec{Name: "test", Kind: "ollama", BaseURL: mock.URL}, false)
	if err != nil {
		t.Fatal(err)
	}
	s.router = router.New([]backend.Adapter{adapter}, nil, nil, nil)
	srv := httptest.NewServer(s.httpSrv.Handler)
	defer srv.Close()
	command := "npm"
	args := []string{"run", "test:e2e"}
	if runtime.GOOS == "windows" {
		command = "cmd.exe"
		args = []string{"/c", "npm", "run", "test:e2e"}
	}
	cmd := exec.Command(command, args...)
	cmd.Dir = filepath.Join("..", "..", "..", "web")
	cmd.Env = append(os.Environ(), "NOOBLOFT_TEST_URL="+srv.URL, "NOOBLOFT_TEST_TOKEN="+testAdminToken)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
}
