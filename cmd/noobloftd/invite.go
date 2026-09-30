// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/security/identity"
	"github.com/xiayu1987/noobloft/internal/security/invitation"
)

func cmdInvite(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", tr("cli.invite.usage"))
	}
	fs := flag.NewFlagSet("invite "+args[0], flag.ContinueOnError)
	file := fs.String("file", "", tr("cli.invite.file"))
	trust := fs.String("trust", "", tr("cli.invite.trust"))
	ttl := fs.Duration("ttl", 7*24*time.Hour, tr("cli.invite.ttl"))
	dir, err := resolveDir(fs, args[1:])
	if err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("%s", tr("cli.fileRequired"))
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if cfg.Network.Mode == "shared" {
		return fmt.Errorf("shared mode uses publisher service/subscribe instead of PSK invitations")
	}
	psk, err := identity.LoadSwarmKey(dir)
	if err != nil {
		return err
	}
	switch args[0] {
	case "export":
		key, err := identity.LoadOrCreateIdentity(dir)
		if err != nil {
			return err
		}
		id, err := peerIDFromPriv(key)
		if err != nil {
			return err
		}
		bootstrap := append([]string{}, cfg.Network.StaticPeers...)
		relays := append([]string{}, cfg.Relay.StaticRelays...)
		for _, addr := range cfg.Network.AnnounceAddrs {
			full := addr + "/p2p/" + id
			bootstrap = mergeAddresses(bootstrap, []string{full})
			if cfg.Relay.Enabled {
				relays = mergeAddresses(relays, []string{full})
			}
		}
		v, err := invitation.Sign(key, psk, bootstrap, relays, time.Now(), *ttl)
		if err != nil {
			return err
		}
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		f, err := os.OpenFile(*file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(append(raw, '\n'))
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Printf(tr("cli.invite.exported"), *file, id)
		return nil
	case "import":
		f, err := os.Open(*file)
		if err != nil {
			return err
		}
		defer f.Close()
		v, err := invitation.Decode(f)
		if err != nil {
			return err
		}
		if err := v.Verify(*trust, psk, time.Now()); err != nil {
			return err
		}
		cfg.Network.StaticPeers = mergeAddresses(cfg.Network.StaticPeers, v.BootstrapPeers)
		cfg.Relay.StaticRelays = mergeAddresses(cfg.Relay.StaticRelays, v.RelayPeers)
		cfg.Network.EnableDHT = true
		if !cfg.Relay.Enabled {
			cfg.Network.DHTMode = "auto"
		}
		cfg.Network.EnableHolePunch = true
		if len(v.RelayPeers) > 0 {
			cfg.Relay.UseRelays = true
		}
		if err := cfg.Validate(); err != nil {
			return err
		}
		if err := config.Save(dir, cfg); err != nil {
			return err
		}
		fmt.Println(tr("cli.invite.imported"))
		return nil
	default:
		return fmt.Errorf(tr("cli.invite.unknown"), args[0])
	}
}

func mergeAddresses(existing, incoming []string) []string {
	out := append([]string{}, existing...)
	seen := make(map[string]bool, len(existing)+len(incoming))
	for _, s := range out {
		seen[s] = true
	}
	for _, s := range incoming {
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}

func applyProfile(cfg *config.Config, profile, announce string) error {
	switch profile {
	case "lan":
		if announce != "" {
			return fmt.Errorf("%s", tr("cli.init.announceProfile"))
		}
	case "wan":
		cfg.Network.EnableMDNS = false
		cfg.Network.DHTMode = "auto"
	case "public-relay":
		if strings.TrimSpace(announce) == "" {
			return fmt.Errorf("%s", tr("cli.init.announceRequired"))
		}
		cfg.Network.ListenAddrs = []string{"/ip4/0.0.0.0/tcp/4001", "/ip6/::/tcp/4001"}
		cfg.Network.AnnounceAddrs = []string{announce}
		cfg.Network.EnableMDNS = false
		cfg.Network.DHTMode = "server"
		cfg.Gateway.Enabled = false
		cfg.Provider.Enabled = false
		cfg.Relay.Enabled = true
		cfg.Relay.UseRelays = false
		cfg.Backends = nil
	default:
		return fmt.Errorf("%s", tr("cli.init.invalidProfile"))
	}
	return nil
}

func checkInitTarget(dir string) error {
	for _, name := range []string{config.ConfigFileName, config.IdentityFileName, config.SwarmKeyFileName} {
		_, err := os.Stat(filepath.Join(dir, name))
		if err == nil {
			return fmt.Errorf(tr("cli.init.exists"), name)
		}
		if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
