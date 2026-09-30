// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package publisher

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

const MaxDocumentBytes = 64 << 10

type Document struct {
	Sequence          uint64   `json:"sequence,omitempty"`
	Version           int      `json:"version"`
	Kind              string   `json:"kind"`
	Publisher         string   `json:"publisher"`
	ID                string   `json:"id"`
	IssuedAt          int64    `json:"issuedAt"`
	ExpiresAt         int64    `json:"expiresAt"`
	Subject           string   `json:"subject,omitempty"`
	Peers             []string `json:"peers,omitempty"`
	Models            []string `json:"models,omitempty"`
	Addresses         []string `json:"addresses,omitempty"`
	Relays            []string `json:"relays,omitempty"`
	MaxTokens         int      `json:"maxTokens,omitempty"`
	MaxConcurrent     int      `json:"maxConcurrent,omitempty"`
	RequestsPerMinute int      `json:"requestsPerMinute,omitempty"`
	Revoked           []string `json:"revoked,omitempty"`
	Signature         []byte   `json:"signature,omitempty"`
}

func (d Document) payload() ([]byte, error) {
	d.Signature = nil
	b, e := json.Marshal(d)
	return append([]byte("noobloft:publisher:v2\n"), b...), e
}
func Sign(key crypto.PrivKey, d Document, ttl time.Duration) (*Document, error) {
	if ttl <= 0 || ttl > 365*24*time.Hour {
		return nil, fmt.Errorf("validity must be within (0, 8760h]")
	}
	id, e := peer.IDFromPrivateKey(key)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	d.Version = 2
	d.Publisher = id.String()
	d.ID = hex.EncodeToString(nonce)
	d.IssuedAt = time.Now().Unix()
	d.ExpiresAt = time.Now().Add(ttl).Unix()
	b, e := d.payload()
	if e != nil {
		return nil, e
	}
	d.Signature, e = key.Sign(b)
	if e != nil {
		return nil, e
	}
	if e = d.Verify(d.Publisher, d.Kind, time.Now()); e != nil {
		return nil, e
	}
	return &d, nil
}
func (d *Document) Verify(trust, kind string, now time.Time) error {
	if d == nil || trust == "" || d.Publisher != trust || d.Version != 2 || d.Kind != kind || d.ID == "" {
		return fmt.Errorf("invalid publisher document or trust binding")
	}
	if d.ExpiresAt <= now.Unix() || d.IssuedAt > now.Add(time.Minute).Unix() || d.ExpiresAt <= d.IssuedAt || d.ExpiresAt-d.IssuedAt > int64((365*24*time.Hour)/time.Second) {
		return fmt.Errorf("expired or invalid document validity")
	}
	id, e := peer.Decode(trust)
	if e != nil {
		return e
	}
	pub, e := id.ExtractPublicKey()
	if e != nil {
		return e
	}
	b, e := d.payload()
	if e != nil {
		return e
	}
	ok, e := pub.Verify(b, d.Signature)
	if e != nil || !ok {
		return fmt.Errorf("invalid publisher signature")
	}
	switch kind {
	case "service":
		if len(d.Peers) == 0 || len(d.Models) == 0 {
			return fmt.Errorf("service requires peers and explicit models")
		}
		for _, p := range d.Peers {
			if _, e := peer.Decode(p); e != nil {
				return e
			}
		}
		for _, s := range append(append([]string{}, d.Addresses...), d.Relays...) {
			a, e := ma.NewMultiaddr(s)
			if e != nil {
				return e
			}
			pi, e := peer.AddrInfoFromP2pAddr(a)
			if e != nil || len(pi.Addrs) == 0 {
				return fmt.Errorf("invalid publisher address")
			}
		}
	case "access":
		if _, e := peer.Decode(d.Subject); e != nil {
			return e
		}
		if len(d.Models) == 0 || d.MaxTokens <= 0 || d.MaxConcurrent <= 0 || d.RequestsPerMinute <= 0 {
			return fmt.Errorf("access requires explicit models and positive limits")
		}
	case "revocations":
		if d.Sequence == 0 {
			return fmt.Errorf("revocation sequence must be positive")
		}
	default:
		return fmt.Errorf("unknown document kind")
	}
	for _, m := range d.Models {
		if strings.TrimSpace(m) == "" || m == "*" || strings.Contains(m, "::") {
			return fmt.Errorf("invalid explicit model name")
		}
	}
	if len(b) > MaxDocumentBytes-1024 {
		return fmt.Errorf("document too large")
	}
	return nil
}
func Contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
func (d *Document) AllowsPeer(p peer.ID, model string) bool {
	return d != nil && Contains(d.Peers, p.String()) && Contains(d.Models, model)
}
func Qualified(pub, model string) string { return pub + "::" + model }
func Split(model string) (string, string, bool) {
	a, b, ok := strings.Cut(model, "::")
	return a, b, ok
}
func Decode(r io.Reader) (*Document, error) {
	b, e := io.ReadAll(io.LimitReader(r, MaxDocumentBytes+1))
	if e != nil {
		return nil, e
	}
	if len(b) > MaxDocumentBytes {
		return nil, fmt.Errorf("document too large")
	}
	var d Document
	e = json.Unmarshal(b, &d)
	return &d, e
}
func Load(path string) (*Document, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return Decode(f)
}

func SaveNew(path string, d *Document) error {
	b, e := json.MarshalIndent(d, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(append(b, '\n'))
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}

type Subscription struct {
	Service Document  `json:"service"`
	Access  *Document `json:"access,omitempty"`
}

func (s Subscription) Validate(self peer.ID) error {
	if e := s.Service.Verify(s.Service.Publisher, "service", time.Now()); e != nil {
		return e
	}
	if s.Access != nil {
		if e := s.Access.Verify(s.Service.Publisher, "access", time.Now()); e != nil {
			return e
		}
		if s.Access.Subject != self.String() {
			return fmt.Errorf("access credential belongs to another consumer")
		}
	}
	return nil
}

func ReplaceRevocations(path string, d *Document) error {
	if e := d.Verify(d.Publisher, "revocations", time.Now()); e != nil {
		return e
	}
	lock, e := os.OpenFile(path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	lock.Close()
	defer os.Remove(path + ".lock")
	old, e := Load(path)
	if e != nil {
		return e
	}
	if e = old.Verify(d.Publisher, "revocations", time.Unix(old.IssuedAt, 0)); e != nil {
		return e
	}
	if d.Sequence <= old.Sequence {
		return fmt.Errorf("revocation rollback rejected")
	}
	for _, id := range old.Revoked {
		if !Contains(d.Revoked, id) {
			return fmt.Errorf("revocations must retain previous entries")
		}
	}
	b, e := json.MarshalIndent(d, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".revocations-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
