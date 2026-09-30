// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
)

func TestManageRestartAndResetLiveNode(t *testing.T) {
	dir := t.TempDir()
	if e := cmdInit([]string{"-dir", dir}); e != nil {
		t.Fatal(e)
	}
	c, e := config.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	c.Gateway.BindAddr = l.Addr().String()
	l.Close()
	c.Network.EnableMDNS = false
	c.Network.EnableDHT = false
	c.Network.ListenAddrs = []string{"/ip4/127.0.0.1/tcp/0"}
	c.Backends = nil
	if e = config.Save(dir, c); e != nil {
		t.Fatal(e)
	}
	authority := publisher.Issuer{Dir: dir}
	if e = authority.Init(); e != nil {
		t.Fatal(e)
	}
	originalSnapshot, e := os.ReadFile(authority.SnapshotPath())
	if e != nil {
		t.Fatal(e)
	}
	original, e := authority.ID()
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 4)
	done := make(chan error, 1)
	go func() {
		done <- runNode(ctx, dir, io.Discard, nodeHooks{Ready: func(r *runtime) { ready <- r.nd.Host().ID().String() }})
	}()
	wait := func() string {
		t.Helper()
		select {
		case id := <-ready:
			return id
		case e := <-done:
			t.Fatalf("node stopped: %v", e)
		case <-time.After(15 * time.Second):
			t.Fatal("node timeout")
		}
		return ""
	}
	first := wait()
	for _, op := range []string{"resources", "state", "issuer", "reset"} {
		if e = cmdManage([]string{op, "-dir", dir}); e != nil {
			t.Fatal(e)
		}
	}
	if e = cmdManage([]string{"restart", "-dir", dir}); e != nil {
		t.Fatal(e)
	}
	if next := wait(); next != first {
		t.Fatal("restart changed node identity")
	}
	c, e = config.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(c)
	rev := fmt.Sprintf("%x", sha256.Sum256(b))
	if e = cmdManage([]string{"reset", "-confirm", "RESET CONFIG", "-revision", rev, "-dir", dir}); e != nil {
		t.Fatal(e)
	}
	if next := wait(); next != first {
		t.Fatal("reset changed node identity")
	}
	if id, e := authority.ID(); e != nil || id != original {
		t.Fatal("reset changed publisher")
	}
	s, e := config.RecoveryState(dir)
	if e != nil || s.Status != "completed" {
		t.Fatal("recovery not completed", e)
	}
	backup := s.Backup
	c, e = config.Load(dir)
	if e != nil || len(c.Backends) != 1 {
		t.Fatal("defaults not restored", e)
	}
	b, _ = json.Marshal(c)
	rev = fmt.Sprintf("%x", sha256.Sum256(b))
	if e = cmdManage([]string{"reset", "-confirm", "RESET CONFIG", "-revision", rev, "-backup", backup, "-dir", dir}); e != nil {
		t.Fatal(e)
	}
	if next := wait(); next != first {
		t.Fatal("backup restore changed identity")
	}
	c, e = config.Load(dir)
	if e != nil || len(c.Backends) != 0 || c.Network.EnableMDNS {
		t.Fatal("backup business configuration not restored", e)
	}
	if id, e := authority.ID(); e != nil || id != original {
		t.Fatal("backup restore changed publisher")
	}
	after, e := os.ReadFile(authority.SnapshotPath())
	if e != nil || !bytes.Equal(after, originalSnapshot) {
		t.Fatal("revocations changed", e)
	}
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown timeout")
	}
}
