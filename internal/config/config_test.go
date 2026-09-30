// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func validConfig(t *testing.T) *Config {
	t.Helper()
	cfg := Default()
	cfg.Gateway.AuthToken = "test-token-not-a-real-secret"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default() with authToken is still invalid, test fixture is broken: %v", err)
	}
	return cfg
}

func TestLoadAcceptsUTF8BOM(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, validConfig(t)); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	path := filepath.Join(dir, ConfigFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reload config failed: %v", err)
	}
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, raw...)
	if err := os.WriteFile(path, withBOM, 0o600); err != nil {
		t.Fatalf("write config with BOM failed: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load should tolerate UTF-8 BOM but failed: %v", err)
	}
	if cfg.Gateway.AuthToken != "test-token-not-a-real-secret" {
		t.Errorf("fields misparsed after BOM strip: authToken=%q", cfg.Gateway.AuthToken)
	}
}

func TestLoadWithoutBOMStillWorks(t *testing.T) {
	dir := t.TempDir()
	want := validConfig(t)
	if err := Save(dir, want); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load config without BOM failed: %v", err)
	}
	if got.Gateway.AuthToken != want.Gateway.AuthToken {
		t.Errorf("authToken mismatch: got=%q want=%q", got.Gateway.AuthToken, want.Gateway.AuthToken)
	}
	if got.Policy.AllowExternalAPIForwarding {
		t.Error("closed-source API forwarding must default to off (fail-closed)")
	}
}

func TestLoadRejectsBrokenJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(path, []byte("\xEF\xBB\xBF{\"network\": "), 0o600); err != nil {
		t.Fatalf("write corrupt config failed: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("truncated JSON should fail but parsed successfully")
	}
}

func TestReputationDefaultsAreFailClosed(t *testing.T) {
	cfg := Default()
	if cfg.Reputation.Enabled || cfg.Reputation.ProbeEnabled {
		t.Fatal("reputation ledger and active probing must default to off")
	}
	if cfg.Reputation.ProbeIntervalSec <= 0 || cfg.Reputation.ProbeCooldownSec <= 0 {
		t.Fatalf("default reputation intervals must be usable once enabled: %+v", cfg.Reputation)
	}
}

func TestValidateRejectsProbeWithoutLedger(t *testing.T) {
	cfg := validConfig(t)
	cfg.Reputation.ProbeEnabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabling probing with the ledger disabled should be rejected")
	}
}

func TestValidateRejectsInvalidReputationIntervals(t *testing.T) {
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Reputation.ProbeIntervalSec = 0 },
		func(c *Config) { c.Reputation.ProbeCooldownSec = 0 },
	} {
		cfg := validConfig(t)
		cfg.Reputation.Enabled = true
		mutate(cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatalf("invalid reputation interval should be rejected: %+v", cfg.Reputation)
		}
	}
}
