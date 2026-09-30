// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package router

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/sync/singleflight"

	"github.com/xiayu1987/noobloft/internal/network/discovery"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
	"github.com/xiayu1987/noobloft/internal/serving/backend"
	"github.com/xiayu1987/noobloft/internal/serving/capability"
	"github.com/xiayu1987/noobloft/internal/serving/inference"
	"github.com/xiayu1987/noobloft/internal/serving/reputation"
)

const modelCacheTTL = 30 * time.Second

const (
	remoteCacheTTL     = 10 * time.Second
	remoteQueryTimeout = 5 * time.Second
	remoteQueryWorkers = 8
	localListTimeout   = 10 * time.Second
)

const (
	probePrompt  = "Reply with OK."
	probeTimeout = 30 * time.Second
)

type remoteClient interface {
	Chat(context.Context, peer.ID, *inference.Request, func(inference.Chunk) error) error
}

type providerDiscovery interface {
	FindProviders(context.Context, string, int) ([]discovery.Provider, error)
	ConnectedPeers() []peer.ID
	QueryCapability(context.Context, peer.ID) (*capability.Announcement, error)
}

type Router struct {
	subscriptions map[string]publisher.Subscription
	shared        bool
	backends      []backend.Adapter
	discovery     providerDiscovery
	client        remoteClient
	reputation    *reputation.Store
	draw          func() float64

	mu           sync.RWMutex
	localAt      time.Time
	local        *localIndex
	localFlight  singleflight.Group
	remoteAt     time.Time
	remote       []remoteOffer
	remoteFlight singleflight.Group
}

type localIndex struct {
	adapters map[string]backend.Adapter
	infos    []backend.ModelInfo
}

type remoteOffer struct {
	peer  peer.ID
	model string
}

func New(backends []backend.Adapter, disc providerDiscovery, client remoteClient, rep *reputation.Store) *Router {
	return &Router{
		backends:   backends,
		discovery:  disc,
		client:     client,
		reputation: rep,
		draw:       rand.Float64,
	}
}

func cached[T any](ctx context.Context, r *Router, flight *singleflight.Group, ttl time.Duration,
	read func() (T, time.Time, bool), load func(context.Context) T, store func(T)) T {
	r.mu.RLock()
	val, at, ok := read()
	r.mu.RUnlock()
	if ok && time.Since(at) < ttl {
		return val
	}
	ch := flight.DoChan("refresh", func() (any, error) {
		fresh := load(context.WithoutCancel(ctx))
		r.mu.Lock()
		store(fresh)
		r.mu.Unlock()
		return fresh, nil
	})
	select {
	case res := <-ch:
		return res.Val.(T)
	case <-ctx.Done():
		return val
	}
}

func (r *Router) localSnapshot(ctx context.Context) *localIndex {
	idx := cached(ctx, r, &r.localFlight, modelCacheTTL,
		func() (*localIndex, time.Time, bool) { return r.local, r.localAt, r.local != nil },
		r.loadLocal,
		func(v *localIndex) { r.local, r.localAt = v, time.Now() })
	if idx == nil {
		return &localIndex{adapters: map[string]backend.Adapter{}}
	}
	return idx
}

func (r *Router) loadLocal(ctx context.Context) *localIndex {
	lists := make([][]backend.ModelInfo, len(r.backends))
	var wg sync.WaitGroup
	for i, b := range r.backends {
		wg.Go(func() {
			listCtx, cancel := context.WithTimeout(ctx, localListTimeout)
			defer cancel()
			if models, err := b.ListModels(listCtx); err == nil {
				lists[i] = models
			}
		})
	}
	wg.Wait()
	idx := &localIndex{adapters: make(map[string]backend.Adapter)}
	for i, models := range lists {
		for _, m := range models {
			if _, dup := idx.adapters[m.Name]; !dup {
				idx.adapters[m.Name] = r.backends[i]
				idx.infos = append(idx.infos, m)
			}
		}
	}
	sort.Slice(idx.infos, func(i, j int) bool { return idx.infos[i].Name < idx.infos[j].Name })
	return idx
}

func (r *Router) ResolveLocal(ctx context.Context, model string) (backend.Adapter, error) {
	if b, ok := r.localSnapshot(ctx).adapters[model]; ok {
		return b, nil
	}
	return nil, localization.Errorf("errors.router.localMissing", model)
}

func (r *Router) LocalModelNames(ctx context.Context) []string {
	infos := r.localSnapshot(ctx).infos
	out := make([]string, 0, len(infos))
	for _, m := range infos {
		out = append(out, m.Name)
	}
	return out
}

func (r *Router) LocalModelInfos(ctx context.Context) []backend.ModelInfo {
	return append([]backend.ModelInfo(nil), r.localSnapshot(ctx).infos...)
}

type Decision struct {
	Local       backend.Adapter
	Remote      peer.ID
	RemoteValid bool
}

func (r *Router) Route(ctx context.Context, model string) (Decision, error) {
	if pub, plain, ok := publisher.Split(model); ok {
		return r.routePublisher(ctx, pub, plain)
	}
	if b, err := r.ResolveLocal(ctx, model); err == nil {
		return Decision{Local: b}, nil
	}
	if r.shared {
		return Decision{}, fmt.Errorf("shared network requires publisherId::model")
	}
	if r.discovery == nil || r.client == nil {
		return Decision{}, localization.Errorf("errors.router.noDiscovery", model)
	}

	findCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	providers, err := r.discovery.FindProviders(findCtx, model, 8)
	if err != nil {
		return Decision{}, localization.Errorf("errors.router.findProviders", model, err)
	}
	if len(providers) == 0 {
		return Decision{}, localization.Errorf("errors.router.noProviders", model)
	}
	chosen := selectProvider(providers, r.reputation, r.draw())
	return Decision{Remote: chosen.AddrInfo.ID, RemoteValid: true}, nil
}

func selectProvider(providers []discovery.Provider, rep *reputation.Store, draw float64) discovery.Provider {
	type ranked struct {
		provider discovery.Provider
		trusted  bool
		weight   float64
	}
	if !rep.Enabled() {
		return providers[0]
	}
	rankedProviders := make([]ranked, 0, len(providers))
	observed := false
	hasTrusted := false
	for _, p := range providers {
		item := ranked{
			provider: p,
			trusted:  true,
			weight:   reputation.NeutralScore,
		}
		if rec, ok := rep.Record(p.AddrInfo.ID.String()); ok {
			observed = true
			item.trusted = rec.Trusted()
			item.weight = rec.Score()
			if rec.LastLatencyMS > 0 {
				item.weight /= 1 + float64(rec.LastLatencyMS)/10_000
			}
		}
		hasTrusted = hasTrusted || item.trusted
		rankedProviders = append(rankedProviders, item)
	}
	if !observed {
		return providers[0]
	}
	total := 0.0
	fallback := rankedProviders[0].provider
	for _, item := range rankedProviders {
		if hasTrusted && !item.trusted {
			continue
		}
		total += item.weight
		fallback = item.provider
	}
	if draw < 0 {
		draw = 0
	}
	if draw >= 1 {
		draw = 0.9999999999999999
	}
	target := draw * total
	for _, item := range rankedProviders {
		if hasTrusted && !item.trusted {
			continue
		}
		target -= item.weight
		if target < 0 {
			return item.provider
		}
	}
	return fallback
}

type callbackError struct{ err error }

func (e *callbackError) Error() string { return e.err.Error() }
func (e *callbackError) Unwrap() error { return e.err }

func (r *Router) Chat(ctx context.Context, req *inference.Request, onChunk func(inference.Chunk) error) error {
	decision, err := r.Route(ctx, req.Model)
	if err != nil {
		return err
	}
	if pub, plain, ok := publisher.Split(req.Model); ok {
		sub := r.subscriptions[pub]
		copyReq := *req
		copyReq.Model = plain
		copyReq.Publisher = pub
		copyReq.Access = sub.Access
		if copyReq.MaxTokens == nil {
			limit := sub.Access.MaxTokens
			copyReq.MaxTokens = &limit
		}
		req = &copyReq
	}

	if decision.Local != nil {
		return decision.Local.Chat(ctx, backend.ChatRequest{
			Model:       req.Model,
			Messages:    req.Messages,
			Stream:      true,
			Temperature: req.Temperature,
			MaxTokens:   req.MaxTokens,
		}, func(c backend.ChatChunk) error {
			return onChunk(inference.Chunk{
				Delta:            c.Delta,
				Reasoning:        c.Reasoning,
				Done:             c.Done,
				FinishReason:     c.FinishReason,
				PromptTokens:     c.PromptTokens,
				CompletionTokens: c.CompletionTokens,
			})
		})
	}

	if !decision.RemoteValid {
		return localization.Errorf("errors.router.noExecutor")
	}
	start := time.Now()
	err = r.client.Chat(ctx, decision.Remote, req, func(c inference.Chunk) error {
		if err := onChunk(c); err != nil {
			return &callbackError{err: err}
		}
		return nil
	})
	var cbErr *callbackError
	var remoteErr *inference.ErrRemote
	if errors.As(err, &remoteErr) && (remoteErr.Code == "policy_denied" || remoteErr.Code == "busy") {
		return err
	}
	if errors.As(err, &cbErr) || ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return err
	}
	r.reputation.Observe(decision.Remote.String(), err == nil, time.Since(start).Milliseconds())
	return err
}

func (r *Router) StartProbeLoop(ctx context.Context, interval, cooldown time.Duration) {
	if r.discovery == nil || r.client == nil || !r.reputation.Enabled() || interval <= 0 || cooldown <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				timeout := probeTimeout
				if interval < timeout {
					timeout = interval
				}
				probeCtx, cancel := context.WithTimeout(ctx, timeout)
				_ = r.probeOnce(probeCtx, cooldown)
				cancel()
			}
		}
	}()
}

func (r *Router) probeOnce(ctx context.Context, cooldown time.Duration) error {
	type candidate struct {
		offer remoteOffer
		last  time.Time
	}
	var candidates []candidate
	seen := make(map[peer.ID]struct{})
	for _, o := range r.remoteSnapshot(ctx) {
		if _, dup := seen[o.peer]; dup {
			continue
		}
		seen[o.peer] = struct{}{}
		var last time.Time
		if rec, ok := r.reputation.Record(o.peer.String()); ok {
			last = rec.LastProbedAt
		}
		if time.Since(last) < cooldown {
			continue
		}
		candidates = append(candidates, candidate{offer: o, last: last})
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].last.Before(candidates[j].last)
	})

	target := candidates[0].offer
	limit := 1
	req := &inference.Request{
		Model:     target.model,
		Messages:  []backend.Message{{Role: "user", Content: probePrompt}},
		MaxTokens: &limit,
	}
	if pub, model, ok := publisher.Split(target.model); ok {
		req.Model, req.Publisher, req.Access = model, pub, r.subscriptions[pub].Access
	}
	start := time.Now()
	err := r.client.Chat(ctx, target.peer, req, func(inference.Chunk) error { return nil })
	var remoteErr *inference.ErrRemote
	if ctx.Err() != nil || (errors.As(err, &remoteErr) && (remoteErr.Code == "policy_denied" || remoteErr.Code == "busy")) {
		return err
	}
	r.reputation.ObserveProbe(target.peer.String(), err == nil)
	if err == nil {
		r.reputation.NoteLatency(target.peer.String(), time.Since(start).Milliseconds())
	}
	return err
}

func (r *Router) RemoteModelNames(ctx context.Context) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(seen))
	for _, o := range r.remoteSnapshot(ctx) {
		if _, dup := seen[o.model]; !dup {
			seen[o.model] = struct{}{}
			out = append(out, o.model)
		}
	}
	sort.Strings(out)
	return out
}

func (r *Router) remoteSnapshot(ctx context.Context) []remoteOffer {
	if r.discovery == nil {
		return nil
	}
	return cached(ctx, r, &r.remoteFlight, remoteCacheTTL,
		func() ([]remoteOffer, time.Time, bool) { return r.remote, r.remoteAt, !r.remoteAt.IsZero() },
		r.loadRemote,
		func(v []remoteOffer) { r.remote, r.remoteAt = v, time.Now() })
}

func (r *Router) loadRemote(ctx context.Context) []remoteOffer {
	peers := r.discovery.ConnectedPeers()
	perPeer := make([][]remoteOffer, len(peers))
	sem := make(chan struct{}, remoteQueryWorkers)
	var wg sync.WaitGroup
	for i, p := range peers {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			queryCtx, cancel := context.WithTimeout(ctx, remoteQueryTimeout)
			defer cancel()
			if ann, err := r.discovery.QueryCapability(queryCtx, p); err == nil && ann != nil {
				perPeer[i] = r.offersFrom(p, ann)
			}
		})
	}
	wg.Wait()
	var out []remoteOffer
	for _, offers := range perPeer {
		out = append(out, offers...)
	}
	return out
}

func (r *Router) offersFrom(p peer.ID, ann *capability.Announcement) []remoteOffer {
	var out []remoteOffer
	if ann.Service != nil {
		sub, ok := r.subscriptions[ann.Service.Publisher]
		if !ok || !subscriptionUsable(sub, ann.Service.ID) {
			return nil
		}
		for _, m := range ann.Models {
			if sub.Service.AllowsPeer(p, m.Name) && publisher.Contains(sub.Access.Models, m.Name) {
				out = append(out, remoteOffer{peer: p, model: publisher.Qualified(sub.Service.Publisher, m.Name)})
			}
		}
		return out
	}
	if r.shared {
		return nil
	}
	for _, m := range ann.Models {
		out = append(out, remoteOffer{peer: p, model: m.Name})
	}
	return out
}

func subscriptionUsable(sub publisher.Subscription, serviceID string) bool {
	if sub.Access == nil || sub.Service.ID != serviceID {
		return false
	}
	now := time.Now()
	return sub.Service.Verify(sub.Service.Publisher, "service", now) == nil &&
		sub.Access.Verify(sub.Service.Publisher, "access", now) == nil
}

func (r *Router) DescribeBackends() string {
	parts := make([]string, 0, len(r.backends))
	for _, b := range r.backends {
		parts = append(parts, fmt.Sprintf("%s(%s)", b.Name(), b.Kind()))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}
