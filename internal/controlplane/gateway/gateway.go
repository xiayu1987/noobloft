// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/controlplane/webui"
	"github.com/xiayu1987/noobloft/internal/serving/backend"
	"github.com/xiayu1987/noobloft/internal/serving/capability"
	"github.com/xiayu1987/noobloft/internal/serving/inference"
	"github.com/xiayu1987/noobloft/internal/serving/reputation"
	"github.com/xiayu1987/noobloft/internal/serving/router"
)

type PeerLister interface {
	ConnectedPeers() []peer.ID
	QueryCapability(ctx context.Context, p peer.ID) (*capability.Announcement, error)
}

type Server struct {
	httpSrv           *http.Server
	router            *router.Router
	token             string
	timeout           time.Duration
	peers             PeerLister
	reputation        *reputation.Store
	manager           *manager
	maintenanceCancel context.CancelFunc
}

type Options struct {
	Management *ManagementOptions
	BindAddr   string
	Token      string
	Timeout    time.Duration
	Router     *router.Router
	Reputation *reputation.Store
	Peers      PeerLister
}

func New(opts Options) (*Server, error) {
	if opts.Token == "" {
		return nil, errors.New(localization.Text(localization.Environment(), "gateway.tokenRequired"))
	}
	if opts.Router == nil {
		return nil, errors.New(localization.Text(localization.Environment(), "gateway.routerMissing"))
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Minute
	}

	s := &Server{
		router:     opts.Router,
		token:      opts.Token,
		timeout:    opts.Timeout,
		peers:      opts.Peers,
		reputation: opts.Reputation,
	}

	mux := http.NewServeMux()
	if err := s.registerManagement(mux, opts.Management); err != nil {
		return nil, err
	}
	mux.Handle("/", webui.Handler())
	mux.HandleFunc("/v1/models", s.withAuth(s.handleModels))
	mux.HandleFunc("/v1/chat/completions", s.withAuth(s.handleChatCompletions))
	if s.peers != nil {
		mux.HandleFunc("/admin/peers", s.withAuth(s.handleAdminPeers))
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	s.httpSrv = &http.Server{
		Addr:              opts.BindAddr,
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	return s, nil
}

func IsLoopback(bindAddr string) bool {
	host, _, err := net.SplitHostPort(bindAddr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.httpSrv.Addr)
	if err != nil {
		return fmt.Errorf(localization.Text(localization.Environment(), "gateway.listenFailed"), s.httpSrv.Addr, err)
	}
	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf(localization.Text(localization.Environment(), "gateway.exited"), err)
		}
	}()
	if s.manager != nil {
		ctx, cancel := context.WithCancel(context.Background())
		s.maintenanceCancel = cancel
		go s.manager.maintainAuthority(ctx)
	}
	return nil
}

func (s *Server) Close(ctx context.Context) error {
	if s.maintenanceCancel != nil {
		s.maintenanceCancel()
	}
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, localization.Request(r, "gateway.unauthorized"))
			return
		}
		next(w, r)
	}
}

type modelsResponse struct {
	Object string      `json:"object"`
	Data   []modelItem `json:"data"`
}

type modelItem struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, localization.Request(r, "api.onlyGet"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	items := make([]modelItem, 0, 8)
	for _, name := range s.router.LocalModelNames(ctx) {
		items = append(items, modelItem{ID: name, Object: "model", OwnedBy: "local"})
	}
	localSet := make(map[string]struct{}, len(items))
	for _, it := range items {
		localSet[it.ID] = struct{}{}
	}
	for _, name := range s.router.RemoteModelNames(ctx) {
		if _, dup := localSet[name]; dup {
			continue
		}
		items = append(items, modelItem{ID: name, Object: "model", OwnedBy: "p2p-peer"})
	}

	writeJSON(w, http.StatusOK, modelsResponse{Object: "list", Data: items})
}

type chatRequest struct {
	Model       string   `json:"model"`
	Messages    []msg    `json:"messages"`
	Stream      bool     `json:"stream"`
	Temperature *float64 `json:"temperature"`
	MaxTokens   *int     `json:"max_tokens"`
}

type msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, localization.Request(r, "api.onlyPost"))
		return
	}
	var req chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, inference.MaxRequestBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(localization.Request(r, "gateway.parseFailed"), errText(r, err)))
		return
	}

	infReq := &inference.Request{
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	for _, m := range req.Messages {
		infReq.Messages = append(infReq.Messages, backend.Message{Role: m.Role, Content: m.Content})
	}
	if err := infReq.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, errText(r, err))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()

	if req.Stream {
		s.streamChat(ctx, w, localization.FromRequest(r), infReq)
		return
	}
	s.blockingChat(ctx, w, localization.FromRequest(r), infReq)
}

func (s *Server) streamChat(ctx context.Context, w http.ResponseWriter, lang string, req *inference.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, localization.Text(lang, "gateway.streamUnsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	created := time.Now().Unix()
	id := newCompletionID()

	err := s.router.Chat(ctx, req, func(c inference.Chunk) error {
		delta := map[string]any{"content": c.Delta}
		if c.Reasoning != "" {
			delta["reasoning_content"] = c.Reasoning
		}
		payload := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   req.Model,
			"choices": []map[string]any{{
				"index": 0,
				"delta": delta,
				"finish_reason": func() any {
					if c.Done {
						if c.FinishReason != "" {
							return c.FinishReason
						}
						return "stop"
					}
					return nil
				}(),
			}},
		}
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return marshalErr
		}
		if _, writeErr := fmt.Fprintf(w, "data: %s\n\n", raw); writeErr != nil {
			return writeErr
		}
		flusher.Flush()
		return nil
	})
	if err != nil {
		raw, _ := json.Marshal(map[string]any{"error": map[string]string{"message": localization.Localize(err, lang)}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		flusher.Flush()
		return
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Server) blockingChat(ctx context.Context, w http.ResponseWriter, lang string, req *inference.Request) {
	var sb strings.Builder
	var reasoning strings.Builder
	var finishReason string
	var promptTokens, completionTokens int

	err := s.router.Chat(ctx, req, func(c inference.Chunk) error {
		sb.WriteString(c.Delta)
		reasoning.WriteString(c.Reasoning)
		if c.PromptTokens > 0 {
			promptTokens = c.PromptTokens
		}
		if c.CompletionTokens > 0 {
			completionTokens = c.CompletionTokens
		}
		if c.Done && c.FinishReason != "" {
			finishReason = c.FinishReason
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, localization.Localize(err, lang))
		return
	}
	if finishReason == "" {
		finishReason = "stop"
	}

	created := time.Now().Unix()
	message := map[string]string{"role": "assistant", "content": sb.String()}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      newCompletionID(),
		"object":  "chat.completion",
		"created": created,
		"model":   req.Model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
		}},
		"usage": map[string]int{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	})
}

type AdminPeer struct {
	PublisherID     string             `json:"publisherId,omitempty"`
	ServiceID       string             `json:"serviceId,omitempty"`
	ConnectionPaths []string           `json:"connectionPaths,omitempty"`
	ID              string             `json:"id"`
	Error           string             `json:"error,omitempty"`
	MaxConcurrent   int                `json:"maxConcurrent,omitempty"`
	Models          []capability.Model `json:"models,omitempty"`
	Reputation      *AdminReputation   `json:"reputation,omitempty"`
}

type AdminReputation struct {
	Score         float64 `json:"score"`
	Samples       int64   `json:"samples"`
	Trusted       bool    `json:"trusted"`
	LastLatencyMS int64   `json:"lastLatencyMs,omitempty"`
}

type AdminPeersResponse struct {
	Peers []AdminPeer `json:"peers"`
}

func (s *Server) handleAdminPeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, localization.Request(r, "api.onlyGet"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	ids := s.peers.ConnectedPeers()
	out := make([]AdminPeer, 0, len(ids))
	for _, id := range ids {
		item := AdminPeer{ID: id.String()}
		if paths, ok := s.peers.(interface{ ConnectionPaths(peer.ID) []string }); ok {
			item.ConnectionPaths = paths.ConnectionPaths(id)
		}
		if rec, ok := s.reputation.Record(id.String()); ok {
			item.Reputation = &AdminReputation{
				Score:         rec.Score(),
				Samples:       rec.Samples(),
				Trusted:       rec.Trusted(),
				LastLatencyMS: rec.LastLatencyMS,
			}
		}
		queryCtx, qc := context.WithTimeout(ctx, 5*time.Second)
		ann, err := s.peers.QueryCapability(queryCtx, id)
		qc()
		switch {
		case err != nil:
			item.Error = errText(r, err)
		case ann != nil:
			if ann.Service != nil {
				item.PublisherID = ann.Service.Publisher
				item.ServiceID = ann.Service.ID
			}
			item.MaxConcurrent = ann.MaxConcurrent
			item.Models = ann.Models
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, AdminPeersResponse{Peers: out})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"message": message},
	})
}

func errText(r *http.Request, err error) string {
	return localization.Localize(err, localization.FromRequest(r))
}

func newCompletionID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "chatcmpl-" + hex.EncodeToString(b[:])
}
