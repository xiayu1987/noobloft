// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/ipfs/go-cid"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"

	"github.com/xiayu1987/noobloft/internal/security/publisher"
	"github.com/xiayu1987/noobloft/internal/serving/capability"
)

const keyPrefix = "noobloft:model:"

const RefreshInterval = capability.MaxAge / 3

func init() {
	if RefreshInterval >= capability.MaxAge {
		panic("discovery: RefreshInterval must be shorter than capability.MaxAge or announcements expire before refresh")
	}
}

const queryTimeout = 15 * time.Second

func ModelCID(model string) (cid.Cid, error) {
	key := []byte(keyPrefix + model)
	if pub, name, ok := publisher.Split(model); ok {
		key, _ = json.Marshal([]string{"noobloft:model:v2", pub, name})
	}
	mh, err := multihash.Sum(key, multihash.SHA2_256, -1)
	if err != nil {
		return cid.Undef, localization.Errorf("errors.discovery.hash", model, err)
	}
	return cid.NewCidV1(cid.Raw, mh), nil
}

type Provider struct {
	AddrInfo     peer.AddrInfo
	Announcement *capability.Announcement
}

type ContentRouter interface {
	Provide(ctx context.Context, c cid.Cid, brdcst bool) error
	FindProvidersAsync(ctx context.Context, c cid.Cid, count int) <-chan peer.AddrInfo
}

type Service struct {
	PublisherService *publisher.Document
	host             host.Host
	router           ContentRouter

	mu               sync.RWMutex
	selfAnnouncement *capability.Announcement

	signer *selfSigner
}

type selfSigner struct {
	priv          libp2pcrypto.PrivKey
	maxConcurrent int
	modelsFn      func() []capability.Model
}

func New(h host.Host, router ContentRouter) *Service {
	s := &Service{host: h, router: router}
	h.SetStreamHandler(capability.Protocol, s.handleCapabilityRequest)
	return s
}

func (s *Service) SetAnnouncement(a *capability.Announcement) {
	s.mu.Lock()
	s.selfAnnouncement = a
	s.mu.Unlock()
}

func (s *Service) Announcement() *capability.Announcement {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selfAnnouncement
}

func (s *Service) ConnectedPeers() []peer.ID {
	return s.host.Network().Peers()
}

func (s *Service) handleCapabilityRequest(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(queryTimeout))

	ann := s.Announcement()
	if ann == nil {
		ann = &capability.Announcement{
			PeerID:   s.host.ID().String(),
			Models:   []capability.Model{},
			IssuedAt: time.Now().UTC(),
		}
	}
	if err := json.NewEncoder(stream).Encode(ann); err != nil {
		_ = stream.Reset()
	}
}

func (s *Service) QueryCapability(ctx context.Context, p peer.ID) (*capability.Announcement, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	stream, err := s.host.NewStream(network.WithAllowLimitedConn(ctx, "capability-relay-fallback"), p, capability.Protocol)
	if err != nil {
		return nil, localization.Errorf("errors.discovery.openStream", err)
	}
	defer stream.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = stream.Reset() })
	defer stopCancel()
	_ = stream.SetDeadline(time.Now().Add(queryTimeout))

	var ann capability.Announcement
	if err := json.NewDecoder(io.LimitReader(stream, 256<<10)).Decode(&ann); err != nil {
		return nil, localization.Errorf("errors.discovery.decode", err)
	}
	if len(ann.Signature) == 0 && len(ann.Models) == 0 {
		return &ann, nil
	}
	if err := capability.Verify(&ann, p); err != nil {
		return nil, localization.Errorf("errors.discovery.verify", err)
	}
	return &ann, nil
}

func (s *Service) Advertise(ctx context.Context, models []string) error {
	if s.router == nil {
		return nil
	}
	var firstErr error
	for _, m := range models {
		if s.PublisherService != nil {
			m = publisher.Qualified(s.PublisherService.Publisher, m)
		}
		c, err := ModelCID(m)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := s.router.Provide(ctx, c, true); err != nil {
			if firstErr == nil {
				firstErr = localization.Errorf("errors.discovery.announce", m, err)
			}
		}
	}
	return firstErr
}

func (s *Service) EnableSelfAnnouncement(priv libp2pcrypto.PrivKey, maxConcurrent int, modelsFn func() []capability.Model) {
	s.mu.Lock()
	s.signer = &selfSigner{priv: priv, maxConcurrent: maxConcurrent, modelsFn: modelsFn}
	s.mu.Unlock()
}

func (s *Service) RefreshAnnouncement() error {
	s.mu.RLock()
	sg := s.signer
	s.mu.RUnlock()
	if sg == nil {
		return nil
	}
	ann, err := capability.Sign(sg.priv, s.host.ID(), sg.modelsFn(), sg.maxConcurrent, s.PublisherService)
	if err != nil {
		return localization.Errorf("errors.discovery.resign", err)
	}
	s.SetAnnouncement(ann)
	return nil
}

func (s *Service) StartMaintenanceLoop(ctx context.Context, modelNamesFn func() []string) {
	go func() {
		ticker := time.NewTicker(RefreshInterval)
		defer ticker.Stop()
		for {
			if err := s.RefreshAnnouncement(); err != nil {
				fmt.Printf("warning: %v\n", err)
			}
			if s.router != nil {
				if models := modelNamesFn(); len(models) > 0 {
					if err := s.Advertise(ctx, models); err != nil {
						fmt.Printf("warning: model announcement failed: %v\n", err)
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Service) FindProviders(ctx context.Context, model string, limit int) ([]Provider, error) {
	pub, plain, scoped := publisher.Split(model)
	if limit <= 0 {
		limit = 8
	}

	candidates := make([]peer.AddrInfo, 0, limit)
	seen := map[peer.ID]struct{}{s.host.ID(): {}}

	if s.router != nil {
		c, err := ModelCID(model)
		if err != nil {
			return nil, err
		}
		for pi := range s.router.FindProvidersAsync(ctx, c, limit*2) {
			if _, dup := seen[pi.ID]; dup {
				continue
			}
			seen[pi.ID] = struct{}{}
			candidates = append(candidates, pi)
			if len(candidates) >= limit*2 {
				break
			}
		}
	}

	for _, p := range s.host.Network().Peers() {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		candidates = append(candidates, peer.AddrInfo{ID: p})
	}

	type result struct {
		provider Provider
		ok       bool
	}
	results := make([]result, len(candidates))
	var wg sync.WaitGroup
	for i, pi := range candidates {
		wg.Add(1)
		go func(idx int, info peer.AddrInfo) {
			defer wg.Done()
			if len(info.Addrs) > 0 {
				s.host.Peerstore().AddAddrs(info.ID, info.Addrs, time.Hour)
			}
			ann, err := s.QueryCapability(ctx, info.ID)
			wanted := model
			if scoped {
				wanted = plain
			}
			if err != nil || ann == nil || !ann.HasModel(wanted) {
				return
			}
			if scoped && (ann.Service == nil || ann.Service.Publisher != pub || !ann.Service.AllowsPeer(info.ID, plain)) {
				return
			}
			results[idx] = result{
				provider: Provider{AddrInfo: info, Announcement: ann},
				ok:       true,
			}
		}(i, pi)
	}
	wg.Wait()

	out := make([]Provider, 0, limit)
	for _, r := range results {
		if !r.ok {
			continue
		}
		out = append(out, r.provider)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Service) ConnectionPaths(p peer.ID) []string {
	var paths []string
	for _, c := range s.host.Network().ConnsToPeer(p) {
		kind := `direct`
		if c.Stat().Limited {
			kind = `relay`
		}
		paths = append(paths, kind+`:`+c.RemoteMultiaddr().String())
	}
	return paths
}
