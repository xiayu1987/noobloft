// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package config

import (
	"reflect"
	"testing"
)

func TestRecoveryDefaultsBackupAndVerification(t *testing.T) {
	for _, mode := range []string{"private", "shared"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			c := Default()
			c.Network.Mode = mode
			c.Gateway.AuthToken = "test-only-token"
			c.Gateway.BindAddr = "127.0.0.1:23456"
			c.Backends = nil
			c.Network.EnableMDNS = false
			if err := Save(dir, c); err != nil {
				t.Fatal(err)
			}
			target := InitialConfiguration(c, c)
			if len(target.Backends) != 1 || !target.Network.EnableMDNS || target.Gateway.BindAddr != c.Gateway.BindAddr || target.Network.Mode != mode {
				t.Fatal("wrong defaults")
			}
			s, err := PrepareRecovery(dir, target)
			if err != nil {
				t.Fatal(err)
			}
			backup, err := LoadRecoveryBackup(dir, s.Backup)
			if err != nil || !reflect.DeepEqual(backup, c) {
				t.Fatal("backup mismatch", err)
			}
			if _, err = LoadRecoveryBackup(dir, "../config.json"); err == nil {
				t.Fatal("unsafe path accepted")
			}
			if _, err = PrepareRecovery(dir, c); err == nil {
				t.Fatal("concurrent recovery allowed")
			}
			if err = CompleteRecovery(dir, target); err != nil {
				t.Fatal(err)
			}
			s, err = RecoveryState(dir)
			if err != nil || s.Status != "completed" {
				t.Fatal("not completed")
			}
			if _, err = PrepareRecovery(dir, backup); err != nil {
				t.Fatal(err)
			}
			if err = CompleteRecovery(dir, target); err != nil {
				t.Fatal(err)
			}
			s, err = RecoveryState(dir)
			if err != nil || s.Status != "failed" {
				t.Fatal("mismatched running configuration accepted")
			}
		})
	}
}
