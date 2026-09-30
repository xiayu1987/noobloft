// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const ManagementLockFileName = ".management.lock"

var ManagementLockStaleAfter = 2 * time.Minute

var ErrManagementLockBusy = errors.New("management lock is held by another operation")

func AcquireManagementLock(dir string) (func(), error) {
	path := filepath.Join(dir, ManagementLockFileName)
	token, err := lockToken()
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			_, werr := fmt.Fprintf(f, "pid=%d\ntime=%s\ntoken=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339), token)
			cerr := f.Close()
			if werr != nil || cerr != nil {
				_ = os.Remove(path)
				return nil, errors.Join(werr, cerr)
			}
			return func() { releaseManagementLock(path, token) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if attempt > 0 || !takeOverStaleLock(path) {
			break
		}
	}
	return nil, ErrManagementLockBusy
}

func takeOverStaleLock(path string) bool {
	reap := path + ".reap"
	g, err := os.OpenFile(reap, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if st, serr := os.Stat(reap); serr == nil && time.Since(st.ModTime()) >= ManagementLockStaleAfter {
			_ = os.Remove(reap)
		}
		return false
	}
	_ = g.Close()
	defer os.Remove(reap)
	st, err := os.Stat(path)
	if err != nil {
		return os.IsNotExist(err)
	}
	if time.Since(st.ModTime()) < ManagementLockStaleAfter {
		return false
	}
	err = os.Remove(path)
	return err == nil || os.IsNotExist(err)
}

func releaseManagementLock(path, token string) {
	b, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(b, []byte("token="+token+"\n")) {
		return
	}
	_ = os.Remove(path)
}

func lockToken() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
