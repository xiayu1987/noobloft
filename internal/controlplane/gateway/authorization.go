// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"net/http"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
)

const expiryWarning = 72 * time.Hour

type authorizationFinding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Detail   string `json:"detail"`
	Action   string `json:"action"`
}

type authorizationService struct {
	Publisher  string   `json:"publisher"`
	ID         string   `json:"id"`
	ExpiresAt  int64    `json:"expiresAt"`
	Valid      bool     `json:"valid"`
	Error      string   `json:"error,omitempty"`
	Models     []string `json:"models"`
	Peers      []string `json:"peers"`
	Delegates  bool     `json:"delegates"`
	ExpiringIn int64    `json:"expiresInSeconds"`
}

type authorizationRevocations struct {
	Sequence   uint64   `json:"sequence"`
	ExpiresAt  int64    `json:"expiresAt"`
	Valid      bool     `json:"valid"`
	Error      string   `json:"error,omitempty"`
	File       string   `json:"file"`
	Revoked    []string `json:"revoked"`
	HitsSelf   bool     `json:"hitsThisNode"`
	HitsServed bool     `json:"hitsThisService"`
	ExpiringIn int64    `json:"expiresInSeconds"`
}

type authorizationProvider struct {
	Enabled         bool     `json:"enabled"`
	MaxConcurrent   int      `json:"maxConcurrent"`
	AdvertiseModels []string `json:"advertiseModels"`
}

func (m *manager) authorization(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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
	now := time.Now()
	provider := authorizationProvider{Enabled: cfg.Provider.Enabled, MaxConcurrent: cfg.Provider.MaxConcurrent, AdvertiseModels: cfg.Provider.AdvertiseModels}
	service := authorizationService{Models: []string{}, Peers: []string{}}
	revocations := authorizationRevocations{Revoked: []string{}}
	findings := []authorizationFinding{}
	add := func(severity, code, detail, action string) {
		findings = append(findings, authorizationFinding{Severity: severity, Code: code, Detail: detail, Action: action})
	}

	if cfg.Provider.Service == nil {
		add("blocked", "service_missing", localization.Request(r, "authorization.finding.serviceMissing"), localization.Request(r, "authorization.action.install"))
	} else {
		doc := cfg.Provider.Service
		service.Publisher, service.ID = doc.Publisher, doc.ID
		service.ExpiresAt, service.Models, service.Peers = doc.ExpiresAt, doc.Models, doc.Peers
		if service.Models == nil {
			service.Models = []string{}
		}
		if service.Peers == nil {
			service.Peers = []string{}
		}
		service.Delegates = publisher.Contains(doc.Peers, m.opts.Self)
		service.ExpiringIn = doc.ExpiresAt - now.Unix()
		if err = doc.Verify(doc.Publisher, "service", now); err != nil {
			service.Error = errText(r, err)
			add("blocked", "service_invalid", localization.Request(r, "authorization.finding.serviceInvalid")+errText(r, err), localization.Request(r, "authorization.action.reissueService"))
		} else if service.ExpiringIn <= int64(expiryWarning/time.Second) {
			service.Valid = true
			add("warning", "service_expiring", localization.Request(r, "authorization.finding.serviceExpiryPrefix")+within(service.ExpiringIn, localization.FromRequest(r))+localization.Request(r, "authorization.finding.expirySuffix"), localization.Request(r, "authorization.action.renewService"))
		} else {
			service.Valid = true
		}
		if !service.Delegates {
			add("blocked", "node_not_delegated", localization.Request(r, "authorization.finding.nodeMissingPrefix")+m.opts.Self+localization.Request(r, "authorization.finding.nodeMissingSuffix"), localization.Request(r, "authorization.action.includeNode"))
		}

		path := cfg.Provider.RevocationsFile
		revocations.File = path
		if path == "" {
			add("blocked", "revocations_missing", localization.Request(r, "authorization.finding.revocationsMissing"), localization.Request(r, "authorization.action.installRevocations"))
		} else {
			if !filepath.IsAbs(path) {
				path = filepath.Join(m.opts.Dir, path)
			}
			rev, loadErr := publisher.Load(path)
			if loadErr != nil {
				add("blocked", "revocations_unavailable", localization.Request(r, "authorization.finding.revocationsUnreadable")+errText(r, loadErr), localization.Request(r, "authorization.action.applyRevocations"))
			} else if verifyErr := rev.Verify(doc.Publisher, "revocations", now); verifyErr != nil {
				revocations.ExpiresAt = rev.ExpiresAt
				add("blocked", "revocations_invalid", localization.Request(r, "authorization.finding.revocationsInvalid")+errText(r, verifyErr), localization.Request(r, "authorization.action.obtainRevocations"))
			} else {
				revocations.Valid = true
				revocations.Sequence = rev.Sequence
				revocations.ExpiresAt = rev.ExpiresAt
				revocations.Revoked = append(revocations.Revoked, rev.Revoked...)
				revocations.ExpiringIn = rev.ExpiresAt - now.Unix()
				revocations.HitsSelf = publisher.Contains(rev.Revoked, m.opts.Self)
				revocations.HitsServed = doc.ID != "" && publisher.Contains(rev.Revoked, doc.ID)
				if revocations.HitsSelf {
					add("blocked", "revoked_node", localization.Request(r, "authorization.finding.nodeRevoked"), localization.Request(r, "authorization.action.nodeRevoked"))
				}
				if revocations.HitsServed {
					add("blocked", "revoked_service", localization.Request(r, "authorization.finding.serviceRevoked"), localization.Request(r, "authorization.action.replaceService"))
				}
				if revocations.ExpiringIn <= int64(expiryWarning/time.Second) {
					add("warning", "revocations_expiring", localization.Request(r, "authorization.finding.revocationsExpiryPrefix")+within(revocations.ExpiringIn, localization.FromRequest(r))+localization.Request(r, "authorization.finding.expirySuffix"), localization.Request(r, "authorization.action.renewRevocations"))
				}
			}
		}
	}

	served := intersect(service.Models, provider.AdvertiseModels)
	if cfg.Provider.Service != nil && service.Valid {
		if !provider.Enabled {
			add("blocked", "provider_disabled", localization.Request(r, "authorization.finding.providerDisabled"), localization.Request(r, "authorization.action.enableProvider"))
		}
		if len(provider.AdvertiseModels) > 0 && len(served) == 0 {
			add("blocked", "no_served_models", localization.Request(r, "authorization.finding.noModelOverlap"), localization.Request(r, "authorization.action.fixModelOverlap"))
		}
	}
	if provider.Enabled && len(served) == 0 {
		add("warning", "no_local_models", localization.Request(r, "authorization.finding.noModels"), localization.Request(r, "authorization.action.addLocalModels"))
	}
	for _, name := range provider.AdvertiseModels {
		if !publisher.Contains(service.Models, name) {
			add("warning", "advertised_not_delegated", localization.Request(r, "authorization.finding.modelPrefix")+name+localization.Request(r, "authorization.finding.modelSuffix"), localization.Request(r, "authorization.action.extendModels"))
		}
	}
	if !reflect.DeepEqual(cfg, m.opts.Running) {
		add("info", "pending_restart", localization.Request(r, "authorization.finding.pendingRestart"), localization.Request(r, "authorization.action.restart"))
	}

	status := "active"
	for _, f := range findings {
		if f.Severity == "blocked" {
			status = "blocked"
			break
		}
		if f.Severity == "warning" {
			status = "warning"
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return severityRank(findings[i].Severity) < severityRank(findings[j].Severity) })
	writeJSON(w, 200, map[string]any{"status": status, "self": m.opts.Self, "service": service, "revocations": revocations, "provider": provider, "servedModels": served, "findings": findings})
}

func severityRank(s string) int {
	switch s {
	case "blocked":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

func within(seconds int64, languages ...string) string {
	language := "zh-CN"
	if len(languages) > 0 {
		language = languages[0]
	}
	if seconds <= 0 {
		return localization.Text(language, "credential.expired")
	}
	d := (time.Duration(seconds) * time.Second).Round(time.Minute)
	if d >= 24*time.Hour {
		days, rest := int(d/(24*time.Hour)), d%(24*time.Hour)
		if rest == 0 {
			return strconv.Itoa(days) + localization.Text(language, "duration.days")
		}
		return strconv.Itoa(days) + localization.Text(language, "duration.daysHours") + rest.Round(time.Hour).String()
	}
	return d.String()
}

func intersect(delegated, advertised []string) []string {
	out := []string{}
	for _, m := range delegated {
		if len(advertised) == 0 || publisher.Contains(advertised, m) {
			out = append(out, m)
		}
	}
	return out
}
