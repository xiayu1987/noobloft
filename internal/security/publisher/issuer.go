// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package publisher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/security/identity"
)

type Issuer struct{ Dir string }

func (s Issuer) Path() string         { return filepath.Join(s.Dir, "publisher-authority") }
func (s Issuer) SnapshotPath() string { return filepath.Join(s.Path(), "revocations.json") }
func (s Issuer) key() (crypto.PrivKey, error) {
	b, e := os.ReadFile(filepath.Join(s.Path(), "identity.key"))
	if e != nil {
		return nil, e
	}
	return crypto.UnmarshalPrivateKey(b)
}
func (s Issuer) ID() (string, error) {
	k, e := s.key()
	if e != nil {
		return "", e
	}
	id, e := peer.IDFromPrivateKey(k)
	return id.String(), e
}
func (s Issuer) Init() error {
	if _, e := os.Stat(s.Path()); e == nil {
		_, e = s.ID()
		return e
	} else if !os.IsNotExist(e) {
		return e
	}
	stage, e := os.MkdirTemp(s.Dir, ".authority-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	k, e := identity.LoadOrCreateIdentity(stage)
	if e != nil {
		return e
	}
	d, e := Sign(k, Document{Kind: "revocations", Sequence: 1}, 7*24*time.Hour)
	if e != nil {
		return e
	}
	if e = SaveNew(filepath.Join(stage, "revocations.json"), d); e != nil {
		return e
	}
	if e = os.Mkdir(filepath.Join(stage, "issued"), 0700); e != nil {
		return e
	}
	return os.Rename(stage, s.Path())
}
func (s Issuer) Issue(d Document, ttl time.Duration) (*Document, error) {
	if d.Kind != "service" && d.Kind != "access" {
		return nil, fmt.Errorf("only service and access can be issued")
	}
	k, e := s.key()
	if e != nil {
		return nil, e
	}
	signed, e := Sign(k, d, ttl)
	if e != nil {
		return nil, e
	}
	if e = SaveNew(filepath.Join(s.Path(), "issued", signed.ID+".json"), signed); e != nil {
		return nil, e
	}
	return signed, nil
}
func (s Issuer) Records() ([]Document, error) {
	id, e := s.ID()
	if e != nil {
		return nil, e
	}
	entries, e := os.ReadDir(filepath.Join(s.Path(), "issued"))
	if e != nil {
		return nil, e
	}
	out := []Document{}
	for _, f := range entries {
		if f.IsDir() || filepath.Ext(f.Name()) != ".json" {
			continue
		}
		d, e := Load(filepath.Join(s.Path(), "issued", f.Name()))
		if e != nil {
			return nil, e
		}
		if e = d.Verify(id, d.Kind, time.Unix(d.IssuedAt, 0)); e != nil {
			return nil, e
		}
		out = append(out, *d)
	}
	return out, nil
}

func (s Issuer) Refresh(target string, paths []string) (*Document, error) {
	k, e := s.key()
	if e != nil {
		return nil, e
	}
	id, e := peer.IDFromPrivateKey(k)
	if e != nil {
		return nil, e
	}
	old, e := Load(s.SnapshotPath())
	if e != nil {
		return nil, e
	}
	if e = old.Verify(id.String(), "revocations", time.Unix(old.IssuedAt, 0)); e != nil {
		return nil, e
	}
	seq := old.Sequence
	revoked := append([]string{}, old.Revoked...)
	changed := false
	for _, p := range paths {
		d, e := Load(p)
		if e != nil {
			return nil, e
		}
		if e = d.Verify(id.String(), "revocations", time.Unix(d.IssuedAt, 0)); e != nil {
			return nil, e
		}
		if d.Sequence > seq {
			seq = d.Sequence
			changed = true
		}
		for _, v := range d.Revoked {
			if !Contains(revoked, v) {
				revoked = append(revoked, v)
				changed = true
			}
		}
	}
	if target != "" && !Contains(revoked, target) {
		revoked = append(revoked, target)
		changed = true
	}
	if changed || old.ExpiresAt <= time.Now().Add(48*time.Hour).Unix() {
		if seq == ^uint64(0) {
			return nil, fmt.Errorf("revocation sequence exhausted")
		}
		old, e = Sign(k, Document{Kind: "revocations", Sequence: seq + 1, Revoked: revoked}, 7*24*time.Hour)
		if e != nil {
			return nil, e
		}
		if e = ReplaceRevocations(s.SnapshotPath(), old); e != nil {
			return nil, e
		}
	}
	for _, p := range paths {
		d, e := Load(p)
		if e != nil {
			return nil, e
		}
		a, _ := json.Marshal(d)
		b, _ := json.Marshal(old)
		if string(a) == string(b) {
			continue
		}
		if e = ReplaceRevocations(p, old); e != nil {
			return nil, e
		}
	}
	return old, nil
}
