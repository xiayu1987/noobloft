// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/controlplane/management"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
)

type ManagementOptions struct {
	Dir, Self, Token string
	Running          *config.Config
	Restart          func() bool
}
type manager struct {
	opts         ManagementOptions
	mu           sync.Mutex
	started      time.Time
	renewalError string
}

func ManagementToken(dir string) (string, error) {
	path := filepath.Join(dir, "admin.token")
	b, err := os.ReadFile(path)
	if err == nil {
		s := strings.TrimSpace(string(b))
		if len(s) < 32 {
			return "", fmt.Errorf("invalid admin.token")
		}
		return s, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	b = make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(token + "\n")
	ce := f.Close()
	if err != nil {
		return "", err
	}
	return token, ce
}

const (
	sessionCookie = "noobloft_session"
	sessionTTL    = 7 * 24 * time.Hour
)

func (m *manager) sessionMAC(expiry string) string {
	h := hmac.New(sha256.New, []byte(m.opts.Token))
	h.Write([]byte("noobloft-management-session:" + expiry))
	return hex.EncodeToString(h.Sum(nil))
}
func (m *manager) validSession(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	expiry, mac, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	sec, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil || time.Now().Unix() >= sec {
		return false
	}
	return hmac.Equal([]byte(mac), []byte(m.sessionMAC(expiry)))
}
func setSessionCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: "/manage", MaxAge: maxAge, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil})
}

func sameOrigin(r *http.Request, require bool) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return !require
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}
func (m *manager) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		byBearer := bearer && subtle.ConstantTimeCompare([]byte(token), []byte(m.opts.Token)) == 1
		if !byBearer && !m.validSession(r) {
			writeError(w, 401, localization.Request(r, "api.adminInvalid"))
			return
		}
		safe := r.Method == http.MethodGet || r.Method == http.MethodHead
		if !sameOrigin(r, !byBearer && !safe) {
			writeError(w, 403, localization.Request(r, "api.crossOrigin"))
			return
		}
		next(w, r)
	}
}

func (m *manager) session(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !sameOrigin(r, true) {
		writeError(w, 403, localization.Request(r, "api.crossOrigin"))
		return
	}
	switch r.Method {
	case http.MethodPost:
		var body struct {
			Token string `json:"token"`
		}
		if err := decodeAdmin(w, r, &body); err != nil {
			writeError(w, 400, errText(r, err))
			return
		}
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(body.Token)), []byte(m.opts.Token)) != 1 {
			writeError(w, 401, localization.Request(r, "api.adminInvalid"))
			return
		}
		expiry := strconv.FormatInt(time.Now().Add(sessionTTL).Unix(), 10)
		setSessionCookie(w, r, expiry+"."+m.sessionMAC(expiry), int(sessionTTL/time.Second))
		writeJSON(w, 200, map[string]any{"expiresAt": expiry})
	case http.MethodDelete:
		setSessionCookie(w, r, "", -1)
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		writeError(w, 405, localization.Request(r, "api.onlyPostDelete"))
	}
}
func (s *Server) registerManagement(mux *http.ServeMux, opts *ManagementOptions) error {
	if opts == nil {
		return nil
	}
	if len(opts.Token) < 32 || opts.Token == s.token || opts.Running == nil {
		return fmt.Errorf("management requires independent token and runtime config")
	}
	raw, _ := json.Marshal(opts.Running)
	var running config.Config
	_ = json.Unmarshal(raw, &running)
	copyOpts := *opts
	copyOpts.Running = &running
	m := &manager{opts: copyOpts, started: time.Now()}
	s.manager = m
	mux.HandleFunc("/manage/issuer", m.auth(m.issuer))
	mux.HandleFunc("/manage/resources", m.auth(m.resources))
	mux.HandleFunc("/manage/lifecycle", m.auth(m.lifecycle))
	mux.HandleFunc("/manage/session", m.session)
	mux.HandleFunc("/manage/state", m.auth(m.state))
	mux.HandleFunc("/manage/config", m.auth(m.settings))
	mux.HandleFunc("/manage/publisher", m.auth(m.publisher))
	mux.HandleFunc("/manage/publisher/export", m.auth(m.exportService))
	mux.HandleFunc("/manage/subscription", m.auth(m.subscription))
	mux.HandleFunc("/manage/authorization", m.auth(m.authorization))
	mux.HandleFunc("/manage/models", m.auth(s.handleModels))
	mux.HandleFunc("/manage/chat", m.auth(s.handleChatCompletions))
	mux.HandleFunc("/manage/peers", m.auth(func(w http.ResponseWriter, r *http.Request) {
		if s.peers == nil {
			if r.Method != "GET" {
				writeError(w, 405, localization.Request(r, "api.onlyGet"))
				return
			}
			writeJSON(w, 200, AdminPeersResponse{Peers: []AdminPeer{}})
			return
		}
		s.handleAdminPeers(w, r)
	}))
	return nil
}
func revision(c *config.Config) string {
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func sanitized(c *config.Config) *config.Config {
	b, _ := json.Marshal(c)
	var out config.Config
	_ = json.Unmarshal(b, &out)
	out.Gateway.AuthToken = ""
	for i := range out.Backends {
		out.Backends[i].APIKey = ""
	}
	return &out
}
func (m *manager) state(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, 405, localization.Request(r, "api.onlyGet"))
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := config.Load(m.opts.Dir)
	if err != nil {
		writeError(w, 500, errText(r, err))
		return
	}
	var rev *publisher.Document
	var revErr string
	if cfg.Provider.Service != nil {
		p := cfg.Provider.RevocationsFile
		if !filepath.IsAbs(p) {
			p = filepath.Join(m.opts.Dir, p)
		}
		rev, err = publisher.Load(p)
		if err == nil {
			err = rev.Verify(cfg.Provider.Service.Publisher, "revocations", time.Now())
		}
		if err != nil {
			revErr = errText(r, err)
		}
	}
	writeJSON(w, 200, map[string]any{"peerId": m.opts.Self, "startedAt": m.started.Unix(), "revision": revision(cfg), "pendingRestart": !reflect.DeepEqual(cfg, m.opts.Running), "saved": sanitized(cfg), "running": sanitized(m.opts.Running), "revocations": rev, "revocationError": revErr})
}
func (m *manager) exportService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, localization.Request(r, "api.onlyGet"))
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var doc *publisher.Document
	switch r.URL.Query().Get("source") {
	case "running":
		doc = m.opts.Running.Provider.Service
	case "saved":
		cfg, err := config.Load(m.opts.Dir)
		if err != nil {
			writeError(w, 500, errText(r, err))
			return
		}
		doc = cfg.Provider.Service
	default:
		writeError(w, 400, localization.Request(r, "api.export.invalidSource"))
		return
	}
	if doc == nil {
		writeError(w, 404, localization.Request(r, "api.export.noService"))
		return
	}
	if err := doc.Verify(doc.Publisher, "service", time.Now()); err != nil {
		writeError(w, 400, localization.Request(r, "api.export.invalidService")+errText(r, err))
		return
	}
	writeJSON(w, 200, doc)
}

func decodeAdmin(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("Content-Type must be application/json")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON object")
	}
	return nil
}
func (m *manager) mutate(w http.ResponseWriter, r *http.Request, fn func(*config.Config) (bool, error)) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, err := config.RecoveryState(m.opts.Dir); err != nil || (s != nil && s.Status == "pending") {
		writeError(w, 409, localization.Request(r, "api.recoveryBusy"))
		return false
	}
	release, err := config.AcquireManagementLock(m.opts.Dir)
	if err != nil {
		writeError(w, 409, localization.Request(r, "api.configBusyRetry"))
		return false
	}
	defer release()
	cfg, err := config.Load(m.opts.Dir)
	if err != nil {
		writeError(w, 500, errText(r, err))
		return false
	}
	if r.Header.Get("If-Match") != revision(cfg) {
		writeError(w, 409, localization.Request(r, "api.configChangedRetry"))
		return false
	}
	save, err := fn(cfg)
	if err == nil && save {
		err = cfg.Validate()
		if err == nil {
			err = config.Save(m.opts.Dir, cfg)
		}
	}
	if err != nil {
		writeError(w, 400, errText(r, err))
		return false
	}
	liveRevocations := cfg.Provider.Service != nil && m.opts.Running.Provider.Service != nil && cfg.Provider.Service.Publisher == m.opts.Running.Provider.Service.Publisher && cfg.Provider.RevocationsFile == m.opts.Running.Provider.RevocationsFile
	writeJSON(w, 200, map[string]any{"pendingRestart": !reflect.DeepEqual(cfg, m.opts.Running), "revision": revision(cfg), "applied": !save && liveRevocations})
	return true
}

type settingsRequest struct {
	Network    config.NetworkConfig    `json:"network"`
	Relay      config.RelayConfig      `json:"relay"`
	Reputation config.ReputationConfig `json:"reputation"`
	Backends   []config.BackendConfig  `json:"backends"`
	Provider   struct {
		Enabled         bool     `json:"enabled"`
		MaxConcurrent   int      `json:"maxConcurrent"`
		AdvertiseModels []string `json:"advertiseModels"`
	} `json:"provider"`
	Gateway struct {
		BindAddr          string `json:"bindAddr"`
		RequestTimeoutSec int    `json:"requestTimeoutSec"`
	} `json:"gateway"`
}

func (m *manager) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		writeError(w, 405, localization.Request(r, "api.onlyPut"))
		return
	}
	var req settingsRequest
	if err := decodeAdmin(w, r, &req); err != nil {
		writeError(w, 400, errText(r, err))
		return
	}
	m.mutate(w, r, func(c *config.Config) (bool, error) {
		seen := map[string]bool{}
		for i, b := range req.Backends {
			if seen[b.Name] {
				return false, fmt.Errorf("duplicate backend name")
			}
			seen[b.Name] = true
			if b.Kind != "ollama" && b.Kind != "openai-compatible" {
				return false, fmt.Errorf("unsupported backend kind")
			}
			u, err := url.Parse(b.BaseURL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
				return false, fmt.Errorf("invalid backend URL")
			}
			if b.APIKey == "" {
				for _, old := range c.Backends {
					if old.Name == b.Name {
						req.Backends[i].APIKey = old.APIKey
					}
				}
			}
		}
		if req.Network.Mode != c.Network.Mode {
			return false, fmt.Errorf("network mode migration requires CLI preparation; cannot switch from web")
		}
		if req.Gateway.BindAddr != c.Gateway.BindAddr && !IsLoopback(req.Gateway.BindAddr) {
			return false, fmt.Errorf("web management only permits changing bind address to loopback")
		}
		c.Network = req.Network
		c.Relay = req.Relay
		c.Reputation = req.Reputation
		c.Backends = req.Backends
		c.Provider.Enabled = req.Provider.Enabled
		c.Provider.MaxConcurrent = req.Provider.MaxConcurrent
		c.Provider.AdvertiseModels = req.Provider.AdvertiseModels
		c.Gateway.BindAddr = req.Gateway.BindAddr
		c.Gateway.RequestTimeoutSec = req.Gateway.RequestTimeoutSec
		return true, nil
	})
}
func (m *manager) publisher(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, 405, localization.Request(r, "api.onlyPost"))
		return
	}
	var req struct {
		Operation   string              `json:"operation"`
		Trust       string              `json:"trust"`
		Document    *publisher.Document `json:"document"`
		Revocations *publisher.Document `json:"revocations"`
	}
	if err := decodeAdmin(w, r, &req); err != nil {
		writeError(w, 400, errText(r, err))
		return
	}
	createdPath := ""
	ok := m.mutate(w, r, func(c *config.Config) (bool, error) {
		revPath := ""
		if req.Operation == "install" {
			if err := req.Document.Verify(req.Trust, "service", time.Now()); err != nil {
				return false, err
			}
			if !publisher.Contains(req.Document.Peers, m.opts.Self) {
				return false, fmt.Errorf("service does not delegate to this node")
			}
			if err := req.Revocations.Verify(req.Trust, "revocations", time.Now()); err != nil {
				return false, err
			}
			initial := req.Revocations
			if c.Provider.Service != nil && c.Provider.Service.Publisher == req.Trust {
				revPath = c.Provider.RevocationsFile
				if !filepath.IsAbs(revPath) {
					revPath = filepath.Join(m.opts.Dir, revPath)
				}
				old, err := publisher.Load(revPath)
				if err != nil {
					return false, err
				}
				initial = old
			}
			{
				f, err := os.CreateTemp(m.opts.Dir, "revocations-*.json")
				if err != nil {
					return false, err
				}
				revPath = f.Name()
				createdPath = revPath
				raw, _ := json.Marshal(initial)
				_, err = f.Write(raw)
				ce := f.Close()
				if err != nil {
					return false, err
				}
				if ce != nil {
					return false, ce
				}
				if !reflect.DeepEqual(initial, req.Revocations) {
					if err = publisher.ReplaceRevocations(revPath, req.Revocations); err != nil {
						return false, err
					}
				}
			}
		}
		err := management.Apply(m.opts.Dir, m.opts.Self, c, req.Operation, req.Trust, req.Document, revPath)
		return req.Operation != "apply-revocations", err
	})
	if !ok && createdPath != "" {
		_ = os.Remove(createdPath)
	}
}
func (m *manager) subscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		writeError(w, 405, localization.Request(r, "api.onlyDelete"))
		return
	}
	var req struct {
		Publisher string `json:"publisher"`
	}
	if err := decodeAdmin(w, r, &req); err != nil {
		writeError(w, 400, errText(r, err))
		return
	}
	m.mutate(w, r, func(c *config.Config) (bool, error) {
		for i, s := range c.Subscriptions {
			if s.Service.Publisher == req.Publisher {
				c.Subscriptions = append(c.Subscriptions[:i], c.Subscriptions[i+1:]...)
				return true, nil
			}
		}
		return false, fmt.Errorf("subscription not found")
	})
}
