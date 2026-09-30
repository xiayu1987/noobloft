// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package management

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
)

func merge(a, b []string) []string {
	for _, v := range b {
		if !publisher.Contains(a, v) {
			a = append(a, v)
		}
	}
	return a
}

func Apply(dir, self string, cfg *config.Config, op, trust string, doc *publisher.Document, revFile string) error {
	kind := "service"
	if op == "credential" {
		kind = "access"
	}
	if op == "apply-revocations" {
		kind = "revocations"
	}
	if err := doc.Verify(trust, kind, time.Now()); err != nil {
		return err
	}
	switch op {
	case "subscribe":
		found := false
		for i, s := range cfg.Subscriptions {
			if s.Service.Publisher == doc.Publisher {
				cfg.Subscriptions[i].Service = *doc
				found = true
				break
			}
		}
		if !found {
			cfg.Subscriptions = append(cfg.Subscriptions, publisher.Subscription{Service: *doc})
		}
		cfg.Network.StaticPeers = merge(cfg.Network.StaticPeers, doc.Addresses)
		cfg.Relay.StaticRelays = merge(cfg.Relay.StaticRelays, doc.Relays)
	case "credential":
		if doc.Subject != self {
			return fmt.Errorf("credential belongs to a different consumer")
		}
		found := false
		for i, s := range cfg.Subscriptions {
			if s.Service.Publisher == doc.Publisher {
				cfg.Subscriptions[i].Access = doc
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("subscribe to publisher first")
		}
	case "install":
		if !publisher.Contains(doc.Peers, self) {
			return fmt.Errorf("service does not delegate to this node")
		}
		if revFile == "" {
			return fmt.Errorf("revocations file required")
		}
		if !filepath.IsAbs(revFile) {
			revFile = filepath.Join(dir, revFile)
		}
		rev, err := publisher.Load(revFile)
		if err != nil {
			return err
		}
		if err = rev.Verify(doc.Publisher, "revocations", time.Now()); err != nil {
			return err
		}
		cfg.Provider.Service = doc
		cfg.Provider.RevocationsFile = revFile
	case "apply-revocations":
		if cfg.Provider.Service == nil || cfg.Provider.Service.Publisher != doc.Publisher {
			return fmt.Errorf("no matching installed publisher service")
		}
		path := cfg.Provider.RevocationsFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		return publisher.ReplaceRevocations(path, doc)
	default:
		return fmt.Errorf("unknown publisher operation %q", op)
	}
	return cfg.Validate()
}
