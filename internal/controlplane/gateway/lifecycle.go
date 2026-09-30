// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/localization"
)

func (m *manager) resources(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, 405, localization.Request(r, "api.onlyGet"))
		return
	}
	type resource struct {
		Kind    string `json:"kind"`
		Name    string `json:"name"`
		Path    string `json:"path"`
		Purpose string `json:"purpose"`
		Source  string `json:"source"`
		Exists  bool   `json:"exists"`
	}
	rows := []resource{}
	add := func(kind, name, path, purpose, source string) {
		p := path
		if !filepath.IsAbs(p) {
			p = filepath.Join(m.opts.Dir, path)
		}
		_, e := os.Stat(p)
		rows = append(rows, resource{kind, name, p, purpose, source, e == nil})
	}
	add("config", localization.Request(r, "config.saved"), "config.json", localization.Request(r, "resources.config.purpose"), localization.Request(r, "resources.config.source"))
	add("node_identity", localization.Request(r, "resources.identity.name"), "identity.key", localization.Request(r, "resources.identity.purpose"), localization.Request(r, "resources.identity.source"))
	add("admin_credential", localization.Request(r, "login.credential"), "admin.token", localization.Request(r, "resources.admin.purpose"), localization.Request(r, "resources.admin.source"))
	add("network_key", localization.Request(r, "resources.networkKey.name"), "swarm.key", localization.Request(r, "resources.networkKey.purpose"), localization.Request(r, "resources.networkKey.source"))
	add("publisher_identity", localization.Request(r, "resources.publisher.name"), "publisher-authority/identity.key", localization.Request(r, "resources.publisher.purpose"), localization.Request(r, "resources.publisher.source"))
	add("issued_documents", localization.Request(r, "resources.issued.name"), "publisher-authority/issued", localization.Request(r, "resources.issued.purpose"), localization.Request(r, "resources.issued.source"))
	add("publisher_revocations", localization.Request(r, "resources.revocations.name"), "publisher-authority/revocations.json", localization.Request(r, "resources.revocations.purpose"), localization.Request(r, "resources.revocations.source"))
	add("audit", localization.Request(r, "resources.audit.name"), "audit.jsonl", localization.Request(r, "resources.audit.purpose"), localization.Request(r, "resources.audit.source"))
	add("reputation", localization.Request(r, "resources.reputation.name"), "reputation.json", localization.Request(r, "resources.reputation.purpose"), localization.Request(r, "resources.reputation.source"))
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := config.Load(m.opts.Dir)
	if err != nil {
		writeError(w, 500, errText(r, err))
		return
	}
	for kind, cfg := range map[string]*config.Config{"saved_revocations": c, "running_revocations": m.opts.Running} {
		if cfg.Provider.RevocationsFile != "" {
			label := "resources.savedList.name"
			if kind == "running_revocations" {
				label = "resources.runningList.name"
			}
			add(kind, localization.Request(r, label), cfg.Provider.RevocationsFile, localization.Request(r, "resources.appliedList.purpose"), localization.Request(r, "resources.appliedList.source"))
		}
	}
	backups, err := filepath.Glob(filepath.Join(m.opts.Dir, "config.before-reset-*.json"))
	if err != nil {
		writeError(w, 500, errText(r, err))
		return
	}
	for _, p := range backups {
		add("config_backup", localization.Request(r, "resources.backupName"), p, localization.Request(r, "resources.backup.purpose"), localization.Request(r, "resources.backup.source"))
	}
	s, err := config.RecoveryState(m.opts.Dir)
	if err != nil {
		writeError(w, 500, errText(r, err))
		return
	}
	add("recovery", localization.Request(r, "resources.recovery.name"), "recovery.json", localization.Request(r, "resources.recovery.purpose"), localization.Request(r, "resources.recovery.source"))
	writeJSON(w, 200, map[string]any{"directory": m.opts.Dir, "resources": rows, "revision": revision(c), "recovery": s, "restartSupported": m.opts.Restart != nil, "resetScope": localization.Request(r, "recovery.scope")})
}

func (m *manager) lifecycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, 405, localization.Request(r, "api.onlyPost"))
		return
	}
	var req struct {
		Operation string `json:"operation"`
		Confirm   string `json:"confirm"`
		Backup    string `json:"backup"`
	}
	if e := decodeAdmin(w, r, &req); e != nil {
		writeError(w, 400, errText(r, e))
		return
	}
	if req.Operation == "restart" {
		m.mu.Lock()
		defer m.mu.Unlock()
		c, e := config.Load(m.opts.Dir)
		if e != nil {
			writeError(w, 400, errText(r, e))
			return
		}
		if r.Header.Get("If-Match") != revision(c) {
			writeError(w, 409, localization.Request(r, "api.configChanged"))
			return
		}
		if e = c.Validate(); e != nil {
			writeError(w, 400, errText(r, e))
			return
		}
		if m.opts.Restart == nil {
			writeError(w, 409, localization.Request(r, "api.restartUnsupported"))
			return
		}
		if !m.opts.Restart() {
			writeError(w, 409, localization.Request(r, "api.restartBusy"))
			return
		}
		writeJSON(w, 202, map[string]any{"accepted": true, "message": localization.Request(r, "api.restartAccepted")})
		return
	}
	if req.Operation != "reset" {
		writeError(w, 400, localization.Request(r, "api.unknownOperation"))
		return
	}
	if req.Confirm != "RESET CONFIG" {
		writeError(w, 400, localization.Request(r, "api.resetConfirmation"))
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opts.Restart == nil {
		writeError(w, 409, localization.Request(r, "api.autoRestartUnsupported"))
		return
	}
	release, e := config.AcquireManagementLock(m.opts.Dir)
	if e != nil {
		writeError(w, 409, localization.Request(r, "api.configBusy"))
		return
	}
	defer release()
	c, e := config.Load(m.opts.Dir)
	if e != nil {
		writeError(w, 500, errText(r, e))
		return
	}
	if r.Header.Get("If-Match") != revision(c) {
		writeError(w, 409, localization.Request(r, "api.previewChanged"))
		return
	}
	target := config.InitialConfiguration(c, m.opts.Running)
	if req.Backup != "" {
		target, e = config.LoadRecoveryBackup(m.opts.Dir, req.Backup)
		if e != nil {
			writeError(w, 400, errText(r, e))
			return
		}
		target.Network.Mode = c.Network.Mode
		target.Gateway.Enabled = m.opts.Running.Gateway.Enabled
		target.Gateway.BindAddr = m.opts.Running.Gateway.BindAddr
		target.Gateway.AuthToken = c.Gateway.AuthToken
	}
	s, e := config.PrepareRecovery(m.opts.Dir, target)
	if e != nil {
		writeError(w, 409, errText(r, e))
		return
	}
	if !m.opts.Restart() {
		e = config.RollbackRecovery(m.opts.Dir, fmt.Errorf("%s", localization.Request(r, "api.restartRejected")))
		if e != nil {
			writeError(w, 500, errText(r, e))
			return
		}
		writeError(w, 409, localization.Request(r, "api.restartRolledBack"))
		return
	}
	writeJSON(w, 202, s)
}
