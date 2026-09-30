// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
)

func (m *manager) authorityPaths(id string, c *config.Config) []string {
	paths := []string{}
	for _, cfg := range []*config.Config{c, m.opts.Running} {
		if cfg.Provider.Service == nil || cfg.Provider.Service.Publisher != id || cfg.Provider.RevocationsFile == "" {
			continue
		}
		p := cfg.Provider.RevocationsFile
		if !filepath.IsAbs(p) {
			p = filepath.Join(m.opts.Dir, p)
		}
		p = filepath.Clean(p)
		if !publisher.Contains(paths, p) && p != (publisher.Issuer{Dir: m.opts.Dir}).SnapshotPath() {
			paths = append(paths, p)
		}
	}
	return paths
}
func (m *manager) issuer(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		writeError(w, 405, localization.Request(r, "api.onlyGetPost"))
		return
	}
	var req struct {
		Operation string             `json:"operation"`
		Document  publisher.Document `json:"document"`
		TTLHours  int                `json:"ttlHours"`
		Target    string             `json:"target"`
	}
	if r.Method == "POST" {
		if e := decodeAdmin(w, r, &req); e != nil {
			writeError(w, 400, errText(r, e))
			return
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, e := config.AcquireManagementLock(m.opts.Dir)
	if e != nil {
		writeError(w, 409, localization.Request(r, "api.operationBusy"))
		return
	}
	defer release()
	c, e := config.Load(m.opts.Dir)
	if e != nil {
		writeError(w, 500, errText(r, e))
		return
	}
	store := publisher.Issuer{Dir: m.opts.Dir}
	if r.Method == "POST" && r.Header.Get("If-Match") != revision(c) {
		writeError(w, 409, localization.Request(r, "api.configChangedRetry"))
		return
	}
	if r.Method == "POST" && req.Operation == "init" {
		if e = store.Init(); e != nil {
			writeError(w, 400, errText(r, e))
			return
		}
	}
	id, e := store.ID()
	if e != nil {
		_, authorityErr := os.Stat(store.Path())
		if os.IsNotExist(authorityErr) && r.Method == "GET" {
			writeJSON(w, 200, map[string]any{"enabled": false})
			return
		}
		writeError(w, 400, localization.Request(r, "api.issuer.identityUnavailable")+errText(r, e))
		return
	}
	paths := m.authorityPaths(id, c)
	var issued *publisher.Document
	if r.Method == "POST" {
		switch req.Operation {
		case "init", "renew":
		case "issue":
			if req.TTLHours < 1 || req.TTLHours > 8760 {
				writeError(w, 400, localization.Request(r, "api.issuer.invalidTtl"))
				return
			}
			d := req.Document
			issued, e = store.Issue(publisher.Document{Kind: d.Kind, Subject: d.Subject, Peers: d.Peers, Models: d.Models, Addresses: d.Addresses, Relays: d.Relays, MaxTokens: d.MaxTokens, MaxConcurrent: d.MaxConcurrent, RequestsPerMinute: d.RequestsPerMinute}, time.Duration(req.TTLHours)*time.Hour)
		case "revoke":
			req.Target = strings.TrimSpace(req.Target)
			if req.Target == "" {
				e = fmt.Errorf("%s", localization.Request(r, "api.issuer.targetRequired"))
				break
			}
			records, err := store.Records()
			if err != nil {
				e = err
				break
			}
			known := false
			for _, d := range records {
				if d.ID == req.Target {
					known = true
					break
				}
			}
			if !known {
				if _, err = peer.Decode(req.Target); err != nil {
					e = fmt.Errorf("%s", localization.Request(r, "api.issuer.invalidTarget"))
				}
			}
		default:
			e = fmt.Errorf("%s", localization.Request(r, "api.issuer.unknownOperation"))
		}
		if e != nil {
			writeError(w, 400, errText(r, e))
			return
		}
	}
	var rev *publisher.Document
	if r.Method == "POST" {
		target := ""
		if req.Operation == "revoke" {
			target = req.Target
		}
		rev, e = store.Refresh(target, paths)
	} else {
		rev, e = publisher.Load(store.SnapshotPath())
	}
	syncError := ""
	if e != nil {
		syncError = errText(r, e)
		rev, _ = publisher.Load(store.SnapshotPath())
	}
	records, err := store.Records()
	if err != nil {
		writeError(w, 500, errText(r, err))
		return
	}
	applied := false
	running := m.opts.Running.Provider
	if syncError == "" && rev != nil && running.Service != nil && running.Service.Publisher == id {
		p := running.RevocationsFile
		if !filepath.IsAbs(p) {
			p = filepath.Join(m.opts.Dir, p)
		}
		local, err := publisher.Load(p)
		applied = err == nil && local.Verify(id, "revocations", time.Now()) == nil && local.ID == rev.ID
	}
	states := map[string]any{}
	for _, d := range records {
		saved := c.Provider.Service != nil && c.Provider.Service.ID == d.ID
		active := running.Service != nil && running.Service.ID == d.ID
		revoked := rev != nil && (publisher.Contains(rev.Revoked, d.ID) || (d.Subject != "" && publisher.Contains(rev.Revoked, d.Subject)))
		states[d.ID] = map[string]any{"installed": saved, "running": active, "providerEnabled": running.Enabled, "revoked": revoked, "expired": d.ExpiresAt <= time.Now().Unix(), "localRevocationApplied": active && revoked && applied, "remoteStatus": localization.Request(r, "api.issuer.unconfirmed")}
	}
	writeJSON(w, 200, map[string]any{"enabled": true, "publisher": id, "records": records, "recordStates": states, "revocations": rev, "issued": issued, "applied": applied, "syncError": syncError, "renewalError": m.renewalError, "remoteStatus": localization.Request(r, "api.issuer.remoteUnconfirmed"), "revision": revision(c)})
}

func (m *manager) renewAuthority() {
	m.mu.Lock()
	defer m.mu.Unlock()
	store := publisher.Issuer{Dir: m.opts.Dir}
	if _, e := os.Stat(store.Path()); os.IsNotExist(e) {
		return
	}
	release, e := config.AcquireManagementLock(m.opts.Dir)
	if e != nil {
		m.renewalError = "renewal skipped: " + e.Error()
		return
	}
	defer release()
	c, e := config.Load(m.opts.Dir)
	if e == nil {
		var id string
		id, e = store.ID()
		if e == nil {
			_, e = store.Refresh("", m.authorityPaths(id, c))
		}
	}
	m.renewalError = ""
	if e != nil {
		m.renewalError = e.Error()
	}
}
func (m *manager) maintainAuthority(ctx context.Context) {
	m.renewAuthority()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.renewAuthority()
		}
	}
}
