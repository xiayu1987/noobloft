// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package config

import "testing"

func TestValidateBoundsTimeoutAndConcurrency(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"timeout zero":      func(c *Config) { c.Gateway.RequestTimeoutSec = 0 },
		"timeout too large": func(c *Config) { c.Gateway.RequestTimeoutSec = MaxRequestTimeoutSec + 1 },
		"concurrency zero":  func(c *Config) { c.Provider.Enabled = true; c.Provider.MaxConcurrent = 0 },
		"concurrency large": func(c *Config) { c.Provider.Enabled = true; c.Provider.MaxConcurrent = MaxProviderConcurrent + 1 },
	} {
		cfg := validConfig(t)
		mutate(cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: want validation error", name)
		}
	}
	cfg := validConfig(t)
	cfg.Gateway.RequestTimeoutSec = MaxRequestTimeoutSec
	if err := cfg.Validate(); err != nil {
		t.Errorf("upper bound timeout should be accepted: %v", err)
	}
}
