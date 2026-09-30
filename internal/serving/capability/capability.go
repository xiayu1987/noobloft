// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package capability

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/security/publisher"
	"github.com/xiayu1987/noobloft/internal/serving/backend"
)

const Protocol = "/noobloft/1.0.0/capability"

const MaxAge = 10 * time.Minute

type Model struct {
	Name          string `json:"name"`
	ContextLength int    `json:"contextLength,omitempty"`
	ParameterSize string `json:"parameterSize,omitempty"`
	Quantization  string `json:"quantization,omitempty"`
	License       string `json:"license,omitempty"`
}

type Announcement struct {
	Service       *publisher.Document `json:"service,omitempty"`
	PeerID        string              `json:"peerId"`
	Models        []Model             `json:"models"`
	MaxConcurrent int                 `json:"maxConcurrent"`
	IssuedAt      time.Time           `json:"issuedAt"`
	Signature     []byte              `json:"signature"`
}

func (a *Announcement) signingPayload() ([]byte, error) {
	payload := struct {
		Service       *publisher.Document `json:"service,omitempty"`
		PeerID        string              `json:"peerId"`
		Models        []Model             `json:"models"`
		MaxConcurrent int                 `json:"maxConcurrent"`
		IssuedAt      string              `json:"issuedAt"`
	}{
		Service:       a.Service,
		PeerID:        a.PeerID,
		Models:        a.Models,
		MaxConcurrent: a.MaxConcurrent,
		IssuedAt:      a.IssuedAt.UTC().Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, localization.Errorf("errors.capability.payload", err)
	}
	return raw, nil
}

func FromModelInfos(infos []backend.ModelInfo, allow []string) []Model {
	allowSet := make(map[string]struct{}, len(allow))
	for _, a := range allow {
		allowSet[a] = struct{}{}
	}
	out := make([]Model, 0, len(infos))
	for _, m := range infos {
		if len(allowSet) > 0 {
			if _, ok := allowSet[m.Name]; !ok {
				continue
			}
		}
		out = append(out, Model{
			Name:          m.Name,
			ContextLength: m.ContextLength,
			ParameterSize: m.ParameterSize,
			Quantization:  m.Quantization,
			License:       m.License,
		})
	}
	return out
}

func Sign(priv crypto.PrivKey, self peer.ID, models []Model, maxConcurrent int, service ...*publisher.Document) (*Announcement, error) {
	a := &Announcement{
		PeerID:        self.String(),
		Models:        models,
		MaxConcurrent: maxConcurrent,
		IssuedAt:      time.Now().UTC(),
	}
	if len(service) > 0 {
		a.Service = service[0]
	}
	payload, err := a.signingPayload()
	if err != nil {
		return nil, err
	}
	sig, err := priv.Sign(payload)
	if err != nil {
		return nil, localization.Errorf("errors.capability.sign", err)
	}
	a.Signature = sig
	return a, nil
}

func Verify(a *Announcement, from peer.ID) error {
	if a == nil {
		return localization.Errorf("errors.capability.empty")
	}
	if a.Service != nil {
		if err := a.Service.Verify(a.Service.Publisher, "service", time.Now()); err != nil {
			return err
		}
		for _, m := range a.Models {
			if !a.Service.AllowsPeer(from, m.Name) {
				return fmt.Errorf("model outside service delegation")
			}
		}
	}
	if a.PeerID != from.String() {
		return localization.Errorf("errors.capability.source", a.PeerID, from.String())
	}
	if len(a.Signature) == 0 {
		return localization.Errorf("errors.capability.unsigned")
	}
	age := time.Since(a.IssuedAt)
	if age > MaxAge {
		return localization.Errorf("errors.capability.expired", age.Round(time.Second))
	}
	if age < -time.Minute {
		return localization.Errorf("errors.capability.future")
	}

	pub, err := from.ExtractPublicKey()
	if err != nil {
		return localization.Errorf("errors.capability.publicKey", err)
	}
	payload, err := a.signingPayload()
	if err != nil {
		return err
	}
	ok, err := pub.Verify(payload, a.Signature)
	if err != nil {
		return localization.Errorf("errors.capability.verify", err)
	}
	if !ok {
		return localization.Errorf("errors.capability.signature")
	}
	return nil
}

func (a *Announcement) HasModel(name string) bool {
	for _, m := range a.Models {
		if m.Name == name {
			return true
		}
	}
	return false
}
