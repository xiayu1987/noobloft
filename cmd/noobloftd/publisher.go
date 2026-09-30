// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/controlplane/management"
	"github.com/xiayu1987/noobloft/internal/security/identity"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
)

func providerAllows(cfg *config.Config, model string) bool {
	if len(cfg.Provider.AdvertiseModels) > 0 && !publisher.Contains(cfg.Provider.AdvertiseModels, model) {
		return false
	}
	return cfg.Provider.Service == nil || publisher.Contains(cfg.Provider.Service.Models, model)
}
func csv(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" && !publisher.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func cmdPublisher(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", tr("cli.publisher.usage"))
	}
	op := args[0]
	fs := flag.NewFlagSet("publisher "+op, flag.ContinueOnError)
	root := fs.String("root", "", tr("cli.publisher.root"))
	file := fs.String("file", "", tr("cli.publisher.file"))
	trust := fs.String("trust", "", tr("cli.publisher.trust"))
	peers := fs.String("peers", "", tr("cli.publisher.peers"))
	models := fs.String("models", "", tr("cli.publisher.models"))
	addresses := fs.String("addresses", "", tr("cli.publisher.addresses"))
	relays := fs.String("relays", "", tr("cli.publisher.relays"))
	subject := fs.String("subject", "", tr("cli.publisher.subject"))
	revoked := fs.String("revoked", "", tr("cli.publisher.revoked"))
	previous := fs.String("previous", "", tr("cli.publisher.previous"))
	revFile := fs.String("revocations", "", tr("cli.publisher.revocations"))
	ttl := fs.Duration("ttl", 24*time.Hour, tr("cli.publisher.ttl"))
	tokens := fs.Int("max-tokens", 1024, tr("cli.publisher.tokens"))
	concurrent := fs.Int("concurrency", 1, tr("cli.publisher.concurrency"))
	rpm := fs.Int("rpm", 30, tr("cli.publisher.rpm"))
	dir, e := resolveDir(fs, args[1:])
	if e != nil {
		return e
	}
	if op == "init" {
		if *root == "" {
			return fmt.Errorf("%s", tr("cli.publisher.rootRequired"))
		}
		if _, e := os.Stat(filepath.Join(*root, config.IdentityFileName)); !os.IsNotExist(e) {
			return fmt.Errorf("%s", tr("cli.publisher.newRoot"))
		}
		key, e := identity.LoadOrCreateIdentity(*root)
		if e != nil {
			return e
		}
		id, e := peer.IDFromPrivateKey(key)
		if e != nil {
			return e
		}
		fmt.Println(tr("cli.publisher.id"), id.String())
		return nil
	}
	if *file == "" {
		return fmt.Errorf("%s", tr("cli.publisher.fileRequired"))
	}
	if op == "service" || op == "grant" || op == "revocations" {
		if *root == "" {
			return fmt.Errorf("%s", tr("cli.publisher.rootRequired"))
		}
		raw, e := os.ReadFile(filepath.Join(*root, config.IdentityFileName))
		if e != nil {
			return e
		}
		key, e := crypto.UnmarshalPrivateKey(raw)
		if e != nil {
			return e
		}
		kind := op
		if kind == "grant" {
			kind = "access"
		}
		d := publisher.Document{Kind: kind, Peers: csv(*peers), Models: csv(*models), Addresses: csv(*addresses), Relays: csv(*relays), Subject: *subject}
		if kind == "access" {
			d.MaxTokens = *tokens
			d.MaxConcurrent = *concurrent
			d.RequestsPerMinute = *rpm
		}
		if kind == "revocations" {
			d.Revoked = csv(*revoked)
			d.Sequence = 1
			if *previous != "" {
				old, e := publisher.Load(*previous)
				if e != nil {
					return e
				}
				id, _ := peer.IDFromPrivateKey(key)
				if e = old.Verify(id.String(), "revocations", time.Unix(old.IssuedAt, 0)); e != nil {
					return e
				}
				d.Revoked = mergeAddresses(old.Revoked, d.Revoked)
				d.Sequence = old.Sequence + 1
			}
		}
		doc, e := publisher.Sign(key, d, *ttl)
		if e != nil {
			return e
		}
		if e = publisher.SaveNew(*file, doc); e != nil {
			return e
		}
		fmt.Printf(tr("cli.publisher.signed"), kind, *file, doc.Publisher, doc.ID)
		return nil
	}
	release, e := config.AcquireManagementLock(dir)
	if e != nil {
		return e
	}
	defer release()
	cfg, e := config.Load(dir)
	if e != nil {
		return e
	}
	doc, e := publisher.Load(*file)
	if e != nil {
		return e
	}
	key, e := identity.LoadOrCreateIdentity(dir)
	if e != nil {
		return e
	}
	self, e := peer.IDFromPrivateKey(key)
	if e != nil {
		return e
	}
	path := *revFile
	if path != "" {
		path, e = filepath.Abs(path)
		if e != nil {
			return e
		}
	}
	if e = management.Apply(dir, self.String(), cfg, op, *trust, doc, path); e != nil {
		return e
	}
	if op == "apply-revocations" {
		return nil
	}
	if e = cfg.Validate(); e != nil {
		return e
	}
	if e = config.Save(dir, cfg); e != nil {
		return e
	}
	fmt.Println(tr("cli.publisher.saved"))
	return nil
}
