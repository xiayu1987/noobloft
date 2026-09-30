// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package router

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/network/discovery"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
	"github.com/xiayu1987/noobloft/internal/serving/capability"
)

func (r *Router) ConfigurePublishers(self peer.ID, shared bool, subs []publisher.Subscription) error {
	index := map[string]publisher.Subscription{}
	for _, s := range subs {
		if e := s.Validate(self); e != nil {
			return e
		}
		if _, ok := index[s.Service.Publisher]; ok {
			return fmt.Errorf("duplicate publisher subscription")
		}
		index[s.Service.Publisher] = s
	}
	r.subscriptions = index
	r.shared = shared
	return nil
}
func (r *Router) routePublisher(ctx context.Context, pub, model string) (Decision, error) {
	s, ok := r.subscriptions[pub]
	if !ok || !subscriptionUsable(s, s.Service.ID) {
		return Decision{}, fmt.Errorf("publisher not subscribed or credential invalid")
	}
	if !publisher.Contains(s.Service.Models, model) || !publisher.Contains(s.Access.Models, model) {
		return Decision{}, fmt.Errorf("model not authorized")
	}
	if r.discovery == nil || r.client == nil {
		return Decision{}, fmt.Errorf("P2P unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	found, e := r.discovery.FindProviders(ctx, publisher.Qualified(pub, model), 32)
	if e != nil {
		return Decision{}, e
	}
	var allowed []discovery.Provider
	for _, p := range found {
		if !s.Service.AllowsPeer(p.AddrInfo.ID, model) || p.Announcement == nil || p.Announcement.Service == nil || p.Announcement.Service.ID != s.Service.ID {
			continue
		}
		if capability.Verify(p.Announcement, p.AddrInfo.ID) != nil || !p.Announcement.HasModel(model) {
			continue
		}
		allowed = append(allowed, p)
	}
	if len(allowed) == 0 {
		return Decision{}, fmt.Errorf("publisher has no authorized online provider for model %q", model)
	}
	chosen := selectProvider(allowed, r.reputation, r.draw())
	return Decision{Remote: chosen.AddrInfo.ID, RemoteValid: true}, nil
}
