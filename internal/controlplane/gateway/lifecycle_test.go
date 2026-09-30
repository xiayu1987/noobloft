// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
)

func TestLifecycleResetAndProtection(t *testing.T) {
	s, c, dir := managementFixture(t)
	s.manager.opts.Restart = func() bool { return true }
	authority := publisher.Issuer{Dir: dir}
	if e := authority.Init(); e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(authority.SnapshotPath())
	if e != nil {
		t.Fatal(e)
	}
	id, e := authority.ID()
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"resources", "lifecycle"} {
		r := managementRequest(s, "POST", "/manage/"+path, "", "", map[string]string{"operation": "reset", "confirm": "RESET CONFIG"})
		if r.Code != 401 {
			t.Fatal("unauthorized", r.Code)
		}
	}
	r := managementRequest(s, "POST", "/manage/lifecycle", testAdminToken, revision(c), map[string]string{"operation": "reset"})
	if r.Code != 400 {
		t.Fatal(r.Body.String())
	}
	r = managementRequest(s, "POST", "/manage/lifecycle", testAdminToken, "stale", map[string]string{"operation": "reset", "confirm": "RESET CONFIG"})
	if r.Code != 409 {
		t.Fatal(r.Body.String())
	}
	r = managementRequest(s, "POST", "/manage/lifecycle", testAdminToken, revision(c), map[string]string{"operation": "reset", "confirm": "RESET CONFIG"})
	if r.Code != 202 {
		t.Fatal(r.Body.String())
	}
	saved, e := config.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(saved.Backends) != 1 || saved.Backends[0].Name != "local-ollama" || saved.Provider.Enabled || saved.Relay.Enabled {
		t.Fatal("reset incomplete")
	}
	if saved.Gateway.AuthToken != c.Gateway.AuthToken || saved.Network.Mode != c.Network.Mode {
		t.Fatal("access identity settings changed")
	}
	if len(s.manager.opts.Running.Backends) == 0 {
		t.Fatal("mutated runtime")
	}
	after, e := os.ReadFile(authority.SnapshotPath())
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("snapshot changed")
	}
	afterID, e := authority.ID()
	if e != nil || afterID != id {
		t.Fatal("authority changed")
	}
	backups, e := filepath.Glob(filepath.Join(dir, "config.before-reset-*.json"))
	if e != nil || len(backups) != 1 {
		t.Fatal("backup missing")
	}
	r = managementRequest(s, "GET", "/manage/resources", testAdminToken, "", nil)
	if r.Code != 200 || bytes.Contains(r.Body.Bytes(), []byte(c.Gateway.AuthToken)) {
		t.Fatal("resource leak/error")
	}
}
func TestLifecycleRestartRevision(t *testing.T) {
	s, c, _ := managementFixture(t)
	count := 0
	s.manager.opts.Restart = func() bool { count++; return count == 1 }
	for _, test := range []struct {
		rev  string
		code int
	}{{"stale", 409}, {revision(c), 202}, {revision(c), 409}} {
		r := managementRequest(s, "POST", "/manage/lifecycle", testAdminToken, test.rev, map[string]string{"operation": "restart"})
		if r.Code != test.code {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	if count != 2 {
		t.Fatal("stale request restarted node")
	}
}
