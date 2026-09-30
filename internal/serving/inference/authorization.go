// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package inference

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/security/publisher"
)

const AuthorizedProtocol = "/noobloft/2.0.0/infer"
const authTimeout = 10 * time.Second

type AuthHeader struct {
	Publisher string              `json:"publisher"`
	Model     string              `json:"model"`
	MaxTokens *int                `json:"maxTokens"`
	Access    *publisher.Document `json:"access"`
}
type admission struct {
	active, requests int
	window           time.Time
}

type Authorization struct {
	Service         publisher.Document
	RevocationsFile string
	mu              sync.Mutex
	usage           map[string]*admission
}

func (a *Authorization) Acquire(remote peer.ID, h AuthHeader) (func(), error) {
	now := time.Now()
	if e := a.Service.Verify(a.Service.Publisher, "service", now); e != nil {
		return nil, e
	}
	if h.Publisher != a.Service.Publisher || !publisher.Contains(a.Service.Models, h.Model) {
		return nil, fmt.Errorf("publisher or model not permitted")
	}
	if e := h.Access.Verify(a.Service.Publisher, "access", now); e != nil {
		return nil, e
	}
	if h.Access.Subject != remote.String() || !publisher.Contains(h.Access.Models, h.Model) {
		return nil, fmt.Errorf("consumer or model not permitted")
	}
	if h.MaxTokens == nil || *h.MaxTokens <= 0 || *h.MaxTokens > h.Access.MaxTokens {
		return nil, fmt.Errorf("maxTokens exceeds credential limit")
	}
	rev, e := publisher.Load(a.RevocationsFile)
	if e != nil {
		return nil, fmt.Errorf("revocations unavailable")
	}
	if e = rev.Verify(a.Service.Publisher, "revocations", now); e != nil {
		return nil, e
	}
	if publisher.Contains(rev.Revoked, h.Access.ID) || publisher.Contains(rev.Revoked, remote.String()) || publisher.Contains(rev.Revoked, a.Service.ID) {
		return nil, fmt.Errorf("authorization revoked")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.usage == nil {
		a.usage = map[string]*admission{}
	}
	for id, s := range a.usage {
		if s.active == 0 && now.Sub(s.window) >= time.Minute {
			delete(a.usage, id)
		}
	}
	id := remote.String()
	s := a.usage[id]
	if s == nil {
		s = &admission{window: now}
		a.usage[id] = s
	}
	if now.Sub(s.window) >= time.Minute {
		s.window = now
		s.requests = 0
	}
	if s.active >= h.Access.MaxConcurrent || s.requests >= h.Access.RequestsPerMinute {
		return nil, fmt.Errorf("consumer concurrency or rate limit exceeded")
	}
	s.active++
	s.requests++
	var once sync.Once
	return func() { once.Do(func() { a.mu.Lock(); s.active--; a.mu.Unlock() }) }, nil
}

func readAuth(r io.Reader) (AuthHeader, error) {
	var h AuthHeader
	b := make([]byte, 0, 1024)
	one := []byte{0}
	for len(b) <= publisher.MaxDocumentBytes {
		_, e := io.ReadFull(r, one)
		if e != nil {
			return h, e
		}
		if one[0] == '\n' {
			e = json.Unmarshal(b, &h)
			return h, e
		}
		b = append(b, one[0])
	}
	return h, fmt.Errorf("authorization header too large")
}
