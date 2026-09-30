// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/xiayu1987/noobloft/internal/localization"
	"github.com/xiayu1987/noobloft/internal/security/publisher"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	ConfigFileName   = "config.json"
	IdentityFileName = "identity.key"
	SwarmKeyFileName = "swarm.key"
	AuditFileName    = "audit.jsonl"
)

const (
	MaxRequestTimeoutSec  = 3600
	MaxProviderConcurrent = 64
)

type Config struct {
	Subscriptions []publisher.Subscription `json:"subscriptions,omitempty"`
	Network       NetworkConfig            `json:"network"`
	Gateway       GatewayConfig            `json:"gateway"`
	Provider      ProviderConfig           `json:"provider"`
	Relay         RelayConfig              `json:"relay"`
	Reputation    ReputationConfig         `json:"reputation"`
	Backends      []BackendConfig          `json:"backends"`
	Policy        PolicyConfig             `json:"policy"`
}

type NetworkConfig struct {
	Mode            string   `json:"mode"`
	ListenAddrs     []string `json:"listenAddrs"`
	StaticPeers     []string `json:"staticPeers"`
	EnableMDNS      bool     `json:"enableMdns"`
	EnableDHT       bool     `json:"enableDht"`
	DHTMode         string   `json:"dhtMode"`
	AnnounceAddrs   []string `json:"announceAddrs"`
	EnableHolePunch bool     `json:"enableHolePunch"`
	LowWater        int      `json:"lowWater"`
	HighWater       int      `json:"highWater"`
}

type GatewayConfig struct {
	Enabled           bool   `json:"enabled"`
	BindAddr          string `json:"bindAddr"`
	AuthToken         string `json:"authToken"`
	RequestTimeoutSec int    `json:"requestTimeoutSec"`
}

type ProviderConfig struct {
	Service         *publisher.Document `json:"service,omitempty"`
	RevocationsFile string              `json:"revocationsFile,omitempty"`
	Enabled         bool                `json:"enabled"`
	AdvertiseModels []string            `json:"advertiseModels"`
	MaxConcurrent   int                 `json:"maxConcurrent"`
}

type RelayConfig struct {
	AllowedPeers         []string `json:"allowedPeers,omitempty"`
	Enabled              bool     `json:"enabled"`
	UseRelays            bool     `json:"useRelays"`
	StaticRelays         []string `json:"staticRelays"`
	MaxCircuits          int      `json:"maxCircuits"`
	PerPeerBandwidthKBps int      `json:"perPeerBandwidthKBps"`
}

type ReputationConfig struct {
	Enabled          bool `json:"enabled"`
	ProbeEnabled     bool `json:"probeEnabled"`
	ProbeIntervalSec int  `json:"probeIntervalSec"`
	ProbeCooldownSec int  `json:"probeCooldownSec"`
}

type BackendConfig struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey,omitempty"`
	Enabled bool   `json:"enabled"`
}

type PolicyConfig struct {
	AllowExternalAPIForwarding bool `json:"allowExternalApiForwarding"`
	AllowAnonymousRouting      bool `json:"allowAnonymousRouting"`
	PublicService              bool `json:"publicService"`
	AuditEnabled               bool `json:"auditEnabled"`
}

func Default() *Config {
	return &Config{
		Network: NetworkConfig{
			Mode:            "private",
			ListenAddrs:     []string{"/ip4/0.0.0.0/tcp/0", "/ip6/::/tcp/0"},
			StaticPeers:     []string{},
			EnableMDNS:      true,
			EnableDHT:       true,
			DHTMode:         "server",
			EnableHolePunch: true,
			LowWater:        32,
			HighWater:       128,
		},
		Gateway: GatewayConfig{
			Enabled:           true,
			BindAddr:          "127.0.0.1:8760",
			AuthToken:         "",
			RequestTimeoutSec: 600,
		},
		Provider: ProviderConfig{
			Enabled:         false,
			AdvertiseModels: []string{},
			MaxConcurrent:   2,
		},
		Relay: RelayConfig{
			Enabled:              false,
			UseRelays:            true,
			StaticRelays:         []string{},
			MaxCircuits:          16,
			PerPeerBandwidthKBps: 0,
		},
		Reputation: ReputationConfig{
			Enabled:          false,
			ProbeEnabled:     false,
			ProbeIntervalSec: 600,
			ProbeCooldownSec: 1800,
		},
		Backends: []BackendConfig{
			{
				Name:    "local-ollama",
				Kind:    "ollama",
				BaseURL: "http://127.0.0.1:11434",
				Enabled: true,
			},
		},
		Policy: PolicyConfig{
			AllowExternalAPIForwarding: false,
			AllowAnonymousRouting:      false,
			PublicService:              false,
			AuditEnabled:               true,
		},
	}
}

func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", localization.Errorf("errors.config.homeDir", err)
	}
	return filepath.Join(home, ".noobloft"), nil
}

func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, ConfigFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, localization.Errorf("errors.config.missing", path, err)
		}
		return nil, localization.Errorf("errors.config.read", err)
	}
	cfg := Default()
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, localization.Errorf("errors.config.parse", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Save(dir string, cfg *Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return localization.Errorf("errors.config.createDir", err)
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return localization.Errorf("errors.config.encode", err)
	}
	path := filepath.Join(dir, ConfigFileName)
	f, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(append(raw, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return localization.Errorf("errors.config.write", err)
	}
	return nil
}

func (c *Config) Validate() error {
	for _, p := range c.Relay.AllowedPeers {
		if _, err := peer.Decode(p); err != nil {
			return err
		}
	}
	if c.Network.Mode == "shared" && c.Relay.Enabled && len(c.Relay.AllowedPeers) == 0 {
		return errors.New("shared relay requires relay.allowedPeers")
	}
	if c.Network.Mode != "private" && c.Network.Mode != "shared" {
		return errors.New("network.mode must be private or shared")
	}
	if c.Network.Mode == "shared" && c.Provider.Enabled && c.Provider.Service == nil {
		return errors.New("shared provider requires signed service delegation")
	}
	if c.Provider.Service != nil && c.Provider.RevocationsFile == "" {
		return errors.New("publisher provider requires revocationsFile (fail closed)")
	}
	for _, s := range c.Network.ListenAddrs {
		if _, err := ma.NewMultiaddr(s); err != nil {
			return localization.Errorf("errors.config.listenAddr", err)
		}
	}
	for _, s := range c.Network.AnnounceAddrs {
		a, err := ma.NewMultiaddr(s)
		if err != nil {
			return localization.Errorf("errors.config.announceAddr", err)
		}
		if _, err := a.ValueForProtocol(ma.P_P2P); err == nil {
			return localization.Errorf("errors.config.announcePeerID")
		}
		port, err := a.ValueForProtocol(ma.P_TCP)
		n, convErr := strconv.Atoi(port)
		if err != nil || convErr != nil || n <= 0 {
			return localization.Errorf("errors.config.announcePort")
		}
	}
	for _, s := range append(append([]string{}, c.Network.StaticPeers...), c.Relay.StaticRelays...) {
		a, err := ma.NewMultiaddr(s)
		if err != nil {
			return localization.Errorf("errors.config.anchorAddr", err)
		}
		p, err := peer.AddrInfoFromP2pAddr(a)
		if err != nil || len(p.Addrs) == 0 {
			return localization.Errorf("errors.config.anchorIncomplete", s)
		}
		port, err := a.ValueForProtocol(ma.P_TCP)
		if err != nil || port == "0" {
			return localization.Errorf("errors.config.anchorPort")
		}
	}
	if c.Network.DHTMode != "server" && c.Network.DHTMode != "auto" && c.Network.DHTMode != "client" {
		return localization.Errorf("errors.config.dhtMode")
	}
	if c.Relay.PerPeerBandwidthKBps != 0 {
		return localization.Errorf("errors.config.relayBandwidth")
	}
	if len(c.Network.ListenAddrs) == 0 {
		return localization.Errorf("errors.config.listenAddrsEmpty")
	}
	if c.Network.LowWater <= 0 || c.HighWaterInvalid() {
		return localization.Errorf("errors.config.watermarks", c.Network.LowWater, c.Network.HighWater)
	}
	if c.Gateway.Enabled {
		if c.Gateway.BindAddr == "" {
			return localization.Errorf("errors.config.bindAddrEmpty")
		}
		if c.Gateway.AuthToken == "" {
			return localization.Errorf("errors.config.authTokenEmpty")
		}
		if c.Gateway.RequestTimeoutSec <= 0 || c.Gateway.RequestTimeoutSec > MaxRequestTimeoutSec {
			return localization.Errorf("errors.config.requestTimeout", MaxRequestTimeoutSec)
		}
	}
	if c.Provider.Enabled && (c.Provider.MaxConcurrent <= 0 || c.Provider.MaxConcurrent > MaxProviderConcurrent) {
		return localization.Errorf("errors.config.maxConcurrent", MaxProviderConcurrent)
	}
	if c.Relay.Enabled && c.Relay.MaxCircuits <= 0 {
		return localization.Errorf("errors.config.maxCircuits")
	}
	for i, b := range c.Backends {
		if b.Name == "" {
			return localization.Errorf("errors.config.backendName", i)
		}
		if b.Kind == "" {
			return localization.Errorf("errors.config.backendKind", i)
		}
		if b.BaseURL == "" {
			return localization.Errorf("errors.config.backendBaseUrl", i)
		}
	}
	if c.Reputation.ProbeEnabled && !c.Reputation.Enabled {
		return localization.Errorf("errors.config.probeRequiresLedger")
	}
	if c.Reputation.Enabled {
		if c.Reputation.ProbeIntervalSec <= 0 {
			return localization.Errorf("errors.config.probeInterval")
		}
		if c.Reputation.ProbeCooldownSec <= 0 {
			return localization.Errorf("errors.config.probeCooldown")
		}
	}
	if c.Policy.AllowAnonymousRouting {
		return localization.Errorf("errors.config.anonymousRouting")
	}
	return nil
}

func (c *Config) HighWaterInvalid() bool {
	return c.Network.HighWater <= 0 || c.Network.HighWater < c.Network.LowWater
}

func (r RelayConfig) StaticRelayAddrs() []string {
	return r.StaticRelays
}
