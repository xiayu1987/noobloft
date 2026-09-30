// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package node

import (
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

type peerACL []string

func (a peerACL) allowed(p peer.ID) bool {
	for _, s := range a {
		if s == p.String() {
			return true
		}
	}
	return false
}
func (a peerACL) AllowReserve(p peer.ID, _ ma.Multiaddr) bool { return a.allowed(p) }
func (a peerACL) AllowConnect(src peer.ID, _ ma.Multiaddr, dest peer.ID) bool {
	return a.allowed(src) && a.allowed(dest)
}
