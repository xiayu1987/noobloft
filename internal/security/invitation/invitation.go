// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package invitation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

const MaxSize = 64 << 10
const domain = "noobloft:invitation:v1:"

type Invitation struct {
	Version         int      `json:"version"`
	NetworkID       string   `json:"networkId"`
	IssuerPeerID    string   `json:"issuerPeerId"`
	IssuerPublicKey []byte   `json:"issuerPublicKey"`
	BootstrapPeers  []string `json:"bootstrapPeers"`
	RelayPeers      []string `json:"relayPeers"`
	IssuedAt        int64    `json:"issuedAt"`
	ExpiresAt       int64    `json:"expiresAt"`
	Signature       []byte   `json:"signature,omitempty"`
}

func NetworkID(psk []byte) string {
	sum := sha256.Sum256(append([]byte("noobloft:network:v1:"), psk...))
	return hex.EncodeToString(sum[:])
}

func (v Invitation) payload() ([]byte, error) {
	v.Signature = nil
	raw, err := json.Marshal(v)
	return append([]byte(domain), raw...), err
}

func Sign(key crypto.PrivKey, psk []byte, bootstrap, relays []string, now time.Time, ttl time.Duration) (*Invitation, error) {
	if len(psk) != 32 || ttl < time.Second || ttl > 30*24*time.Hour {
		return nil, localization.Errorf("errors.invitation.params")
	}
	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		return nil, err
	}
	pub, err := crypto.MarshalPublicKey(key.GetPublic())
	if err != nil {
		return nil, err
	}
	v := &Invitation{Version: 1, NetworkID: NetworkID(psk), IssuerPeerID: id.String(), IssuerPublicKey: pub, BootstrapPeers: bootstrap, RelayPeers: relays, IssuedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix()}
	payload, err := v.payload()
	if err != nil {
		return nil, err
	}
	v.Signature, err = key.Sign(payload)
	if err != nil {
		return nil, err
	}
	if err := v.Verify(id.String(), psk, now); err != nil {
		return nil, err
	}
	return v, nil
}

func Decode(r io.Reader) (*Invitation, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxSize {
		return nil, localization.Errorf("errors.invitation.tooLarge", MaxSize)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var v Invitation
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, localization.Errorf("errors.invitation.trailing")
	}
	return &v, nil
}

func (v *Invitation) Verify(trustedIssuer string, psk []byte, now time.Time) error {
	if trustedIssuer == "" || v.IssuerPeerID != trustedIssuer {
		return localization.Errorf("errors.invitation.untrusted")
	}
	if v.Version != 1 {
		return localization.Errorf("errors.invitation.version")
	}
	if len(psk) != 32 || v.NetworkID != NetworkID(psk) {
		return localization.Errorf("errors.invitation.network")
	}
	if v.IssuedAt > now.Unix()+60 || v.ExpiresAt <= now.Unix() || v.ExpiresAt <= v.IssuedAt || v.ExpiresAt-v.IssuedAt > 30*24*3600 {
		return localization.Errorf("errors.invitation.expired")
	}
	pub, err := crypto.UnmarshalPublicKey(v.IssuerPublicKey)
	if err != nil {
		return err
	}
	id, err := peer.IDFromPublicKey(pub)
	if err != nil {
		return err
	}
	if id.String() != v.IssuerPeerID {
		return localization.Errorf("errors.invitation.publicKey")
	}
	payload, err := v.payload()
	if err != nil {
		return err
	}
	ok, err := pub.Verify(payload, v.Signature)
	if err != nil || !ok {
		return localization.Errorf("errors.invitation.signature")
	}
	if len(v.BootstrapPeers) == 0 || len(v.BootstrapPeers)+len(v.RelayPeers) > 64 {
		return localization.Errorf("errors.invitation.anchors")
	}
	for _, s := range append(append([]string{}, v.BootstrapPeers...), v.RelayPeers...) {
		a, err := ma.NewMultiaddr(s)
		if err != nil {
			return err
		}
		pi, err := peer.AddrInfoFromP2pAddr(a)
		if err != nil || len(pi.Addrs) == 0 {
			return localization.Errorf("errors.invitation.addressIncomplete", s)
		}
		if _, err := a.ValueForProtocol(ma.P_TCP); err != nil {
			return localization.Errorf("errors.invitation.addressTCP", s)
		}
	}
	return nil
}
