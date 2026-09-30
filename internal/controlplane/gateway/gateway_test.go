// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/serving/capability"
	"github.com/xiayu1987/noobloft/internal/serving/reputation"
)

type stubPeers struct {
	ids  []peer.ID
	anns map[peer.ID]*capability.Announcement
	errs map[peer.ID]error
}

func (s *stubPeers) ConnectedPeers() []peer.ID { return s.ids }

func (s *stubPeers) QueryCapability(_ context.Context, p peer.ID) (*capability.Announcement, error) {
	if err, ok := s.errs[p]; ok {
		return nil, err
	}
	return s.anns[p], nil
}

func TestHandleAdminPeers(t *testing.T) {
	good := peer.ID("peer-good")
	bad := peer.ID("peer-offline")

	srv := &Server{
		token: "tok",
		peers: &stubPeers{
			ids: []peer.ID{good, bad},
			anns: map[peer.ID]*capability.Announcement{
				good: {
					MaxConcurrent: 3,
					Models:        []capability.Model{{Name: "qwen2.5:7b"}},
				},
			},
			errs: map[peer.ID]error{bad: errors.New("stream reset")},
		},
	}

	rec := httptest.NewRecorder()
	srv.handleAdminPeers(rec, httptest.NewRequest(http.MethodGet, "/admin/peers", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var resp AdminPeersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response body failed: %v", err)
	}
	if len(resp.Peers) != 2 {
		t.Fatalf("peers count = %d, want 2", len(resp.Peers))
	}

	if got := resp.Peers[0]; got.ID != good.String() || got.MaxConcurrent != 3 ||
		len(got.Models) != 1 || got.Models[0].Name != "qwen2.5:7b" || got.Error != "" {
		t.Errorf("healthy peer entry mismatch: %+v", got)
	}
	if got := resp.Peers[1]; got.ID != bad.String() || got.Error != "stream reset" ||
		len(got.Models) != 0 || got.MaxConcurrent != 0 {
		t.Errorf("lost peer entry mismatch: %+v", got)
	}
}

func TestHandleAdminPeersRejectsNonGET(t *testing.T) {
	srv := &Server{token: "tok", peers: &stubPeers{}}
	rec := httptest.NewRecorder()
	srv.handleAdminPeers(rec, httptest.NewRequest(http.MethodPost, "/admin/peers", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHandleAdminPeersIncludesReputationMetadata(t *testing.T) {
	id := peer.ID("peer-observed")
	rep, err := reputation.Open(t.TempDir(), true)
	if err != nil {
		t.Fatalf("open reputation ledger failed: %v", err)
	}
	rep.Observe(id.String(), true, 42)
	srv := &Server{
		peers: &stubPeers{
			ids:  []peer.ID{id},
			anns: map[peer.ID]*capability.Announcement{id: {}},
		},
		reputation: rep,
	}
	rec := httptest.NewRecorder()
	srv.handleAdminPeers(rec, httptest.NewRequest(http.MethodGet, "/admin/peers", nil))
	var resp AdminPeersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response body failed: %v", err)
	}
	if len(resp.Peers) != 1 || resp.Peers[0].Reputation == nil {
		t.Fatalf("missing reputation summary: %+v", resp)
	}
	got := resp.Peers[0].Reputation
	if got.Samples != 1 || !got.Trusted || got.LastLatencyMS != 42 {
		t.Fatalf("reputation summary mismatch: %+v", got)
	}
}

func TestNewRejectsEmptyToken(t *testing.T) {
	if _, err := New(Options{BindAddr: "127.0.0.1:0", Token: ""}); err == nil {
		t.Fatal("empty token must reject gateway construction")
	}
}

func TestNewRejectsNilRouter(t *testing.T) {
	if _, err := New(Options{BindAddr: "127.0.0.1:0", Token: "t0ken"}); err == nil {
		t.Fatal("missing router must reject gateway construction")
	}
}

func TestWithAuth(t *testing.T) {
	srv := &Server{token: "correct-token"}
	handler := srv.withAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	cases := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{"missing header", "", http.StatusUnauthorized},
		{"wrong token", "Bearer wrong-token", http.StatusUnauthorized},
		{"token prefix only", "Bearer correct-tok", http.StatusUnauthorized},
		{"token with extra chars", "Bearer correct-token-extra", http.StatusUnauthorized},
		{"correct token", "Bearer correct-token", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			handler(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestIsLoopback(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8760", true},
		{"[::1]:8760", true},
		{"localhost:8760", true},
		{"0.0.0.0:8760", false},
		{"192.0.2.20:8760", false},
		{"no-port", false},
	}
	for _, tc := range cases {
		if got := IsLoopback(tc.addr); got != tc.want {
			t.Errorf("IsLoopback(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}
