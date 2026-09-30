// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagementLockExclusiveAndRelease(t *testing.T) {
	dir := t.TempDir()
	release, err := AcquireManagementLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireManagementLock(dir); !errors.Is(err, ErrManagementLockBusy) {
		t.Fatalf("second acquire: want busy, got %v", err)
	}
	release()
	if _, err := os.Stat(filepath.Join(dir, ManagementLockFileName)); !os.IsNotExist(err) {
		t.Fatalf("lock file should be removed, stat err=%v", err)
	}
	release2, err := AcquireManagementLock(dir)
	if err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
	release2()
}

func TestManagementLockTakesOverStaleLegacyLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ManagementLockFileName)
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-ManagementLockStaleAfter - time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	release, err := AcquireManagementLock(dir)
	if err != nil {
		t.Fatalf("stale lock should be taken over: %v", err)
	}
	defer release()
	b, _ := os.ReadFile(path)
	if len(b) == 0 {
		t.Fatal("new lock should record owner pid/time/token")
	}
	matches, _ := filepath.Glob(path + ".stale-*")
	if len(matches) != 0 {
		t.Fatalf("tombstones left behind: %v", matches)
	}
}

func TestManagementLockKeepsFreshLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ManagementLockFileName), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireManagementLock(dir); !errors.Is(err, ErrManagementLockBusy) {
		t.Fatalf("fresh foreign lock must not be taken over, got %v", err)
	}
}

func TestManagementLockReleaseDoesNotRemoveNewOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ManagementLockFileName)
	releaseOld, err := AcquireManagementLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-ManagementLockStaleAfter - time.Minute)
	_ = os.Chtimes(path, old, old)
	releaseNew, err := AcquireManagementLock(dir)
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	releaseOld()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("new owner's lock was removed: %v", err)
	}
	releaseNew()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lock should be gone after owner release, err=%v", err)
	}
}

func TestManagementLockConcurrentStaleTakeoverSingleWinner(t *testing.T) {
	for round := 0; round < 20; round++ {
		dir := t.TempDir()
		path := filepath.Join(dir, ManagementLockFileName)
		_ = os.WriteFile(path, nil, 0600)
		old := time.Now().Add(-ManagementLockStaleAfter - time.Minute)
		_ = os.Chtimes(path, old, old)
		var wins atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, err := AcquireManagementLock(dir); err == nil {
					wins.Add(1)
				}
			}()
		}
		close(start)
		wg.Wait()
		if n := wins.Load(); n != 1 {
			t.Fatalf("round %d: want exactly one winner, got %d", round, n)
		}
	}
}
