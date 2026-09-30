// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/xiayu1987/noobloft/internal/config"
)

func TestRecoveryStartupFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	if err := cmdInit([]string{"-dir", dir, "-mode", "shared"}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	c.Gateway.BindAddr = "127.0.0.1:0"
	c.Network.EnableMDNS = false
	c.Network.EnableDHT = false
	if err = config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	target := config.InitialConfiguration(c, c)
	target.Gateway.BindAddr = occupied.Addr().String()
	if _, err = config.PrepareRecovery(dir, target); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- runNode(ctx, dir, io.Discard, nodeHooks{Ready: func(*runtime) { ready <- struct{}{} }})
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("rollback failed: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("timeout")
	}
	s, err := config.RecoveryState(dir)
	if err != nil || s.Status != "rolled_back" || s.Error == "" {
		t.Fatalf("state: %+v %v", s, err)
	}
	restored, err := config.Load(dir)
	if err != nil || restored.Gateway.BindAddr != c.Gateway.BindAddr || restored.Gateway.AuthToken != c.Gateway.AuthToken {
		t.Fatal("configuration not restored", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown timeout")
	}
}
