// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"
)

type Recovery struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Backup string `json:"backup"`
	Error  string `json:"error,omitempty"`
	Target string `json:"target"`
}

func configurationDigest(c *Config) string {
	b, _ := json.Marshal(c)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func LoadRecoveryBackup(dir, name string) (*Config, error) {
	if filepath.Base(name) != name || !strings.HasPrefix(name, "config.before-reset-") || !strings.HasSuffix(name, ".json") {
		return nil, localization.Errorf("errors.recovery.foreignBackup")
	}
	p := filepath.Join(dir, name)
	st, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, localization.Errorf("errors.recovery.notRegular")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	c := Default()
	if err = json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	if err = c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func InitialConfiguration(current, running *Config) *Config {
	c := Default()
	c.Network.Mode = current.Network.Mode
	c.Gateway.Enabled = running.Gateway.Enabled
	c.Gateway.BindAddr = running.Gateway.BindAddr
	c.Gateway.AuthToken = current.Gateway.AuthToken
	return c
}

func RecoveryState(dir string) (*Recovery, error) {
	b, err := os.ReadFile(filepath.Join(dir, "recovery.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s Recovery
	if err = json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
func SaveRecovery(dir string, s *Recovery) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".recovery-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "recovery.json"))
}
func PrepareRecovery(dir string, target *Config) (*Recovery, error) {
	if err := target.Validate(); err != nil {
		return nil, err
	}
	previous, err := RecoveryState(dir)
	if err != nil {
		return nil, err
	}
	if previous != nil && previous.Status == "pending" {
		return nil, localization.Errorf("errors.recovery.inProgress")
	}
	raw, err := os.ReadFile(filepath.Join(dir, ConfigFileName))
	if err != nil {
		return nil, err
	}
	id := fmt.Sprint(time.Now().UnixNano())
	s := &Recovery{ID: id, Status: "pending", Backup: "config.before-reset-" + id + ".json", Target: configurationDigest(target)}
	if err = os.WriteFile(filepath.Join(dir, s.Backup), raw, 0600); err != nil {
		return nil, err
	}
	if err = SaveRecovery(dir, s); err != nil {
		return nil, err
	}
	if err = Save(dir, target); err != nil {
		s.Status = "failed"
		s.Error = err.Error()
		_ = SaveRecovery(dir, s)
		return nil, err
	}
	return s, nil
}
func RollbackRecovery(dir string, cause error) error {
	s, err := RecoveryState(dir)
	if err != nil {
		return err
	}
	if s == nil || s.Status != "pending" {
		return localization.Errorf("errors.recovery.nothingPending")
	}
	if filepath.Base(s.Backup) != s.Backup {
		return localization.Errorf("errors.recovery.invalidPath")
	}
	b, err := os.ReadFile(filepath.Join(dir, s.Backup))
	if err != nil {
		return err
	}
	var c Config
	if err = json.Unmarshal(b, &c); err != nil {
		return err
	}
	if err = c.Validate(); err != nil {
		return err
	}
	if err = Save(dir, &c); err != nil {
		return err
	}
	s.Status = "rolled_back"
	s.Error = cause.Error()
	return SaveRecovery(dir, s)
}
func CompleteRecovery(dir string, running *Config) error {
	s, err := RecoveryState(dir)
	if err != nil {
		return err
	}
	if s == nil || s.Status != "pending" {
		return nil
	}
	if configurationDigest(running) != s.Target {
		s.Status = "failed"
		s.Error = "loaded configuration does not match the recovery target; check whether it was modified externally"
		return SaveRecovery(dir, s)
	}
	s.Status = "completed"
	return SaveRecovery(dir, s)
}
