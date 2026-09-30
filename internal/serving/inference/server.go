// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package inference

import (
	"context"
	"fmt"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"

	"github.com/xiayu1987/noobloft/internal/audit"
	"github.com/xiayu1987/noobloft/internal/serving/backend"
)

const streamTimeout = 10 * time.Minute

type ModelResolver func(ctx context.Context, model string) (backend.Adapter, error)

type Server struct {
	authorization  *Authorization
	host           host.Host
	resolve        ModelResolver
	auditor        *audit.Logger
	sem            chan struct{}
	admissionSlots chan struct{}
}

func NewServer(h host.Host, resolve ModelResolver, auditor *audit.Logger, maxConcurrent int, auth *Authorization) (*Server, error) {
	if maxConcurrent <= 0 {
		return nil, localization.Errorf("errors.inference.maxConcurrent", maxConcurrent)
	}
	s := &Server{
		host:           h,
		resolve:        resolve,
		auditor:        auditor,
		sem:            make(chan struct{}, maxConcurrent),
		admissionSlots: make(chan struct{}, maxConcurrent+32),
	}
	if auth != nil {
		s.authorization = auth
		if err := s.authorization.Service.Verify(s.authorization.Service.Publisher, "service", time.Now()); err != nil {
			return nil, err
		}
		if !s.authorization.Service.AllowsPeer(h.ID(), s.authorization.Service.Models[0]) {
			return nil, fmt.Errorf("service delegation does not authorize this node")
		}
		h.SetStreamHandler(AuthorizedProtocol, s.handle)
	} else {
		h.SetStreamHandler(Protocol, s.handle)
	}
	return s, nil
}

func (s *Server) Close() {
	s.host.RemoveStreamHandler(Protocol)
	s.host.RemoveStreamHandler(AuthorizedProtocol)
}

func (s *Server) handle(stream network.Stream) {
	defer stream.Close()

	remote := stream.Conn().RemotePeer()
	start := time.Now()
	_ = stream.SetDeadline(start.Add(streamTimeout))
	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()
	var header AuthHeader
	if s.authorization != nil {
		select {
		case s.admissionSlots <- struct{}{}:
			defer func() { <-s.admissionSlots }()
		default:
			_ = stream.Reset()
			return
		}
		_ = stream.SetDeadline(start.Add(authTimeout))
		var err error
		header, err = readAuth(stream)
		if err != nil {
			_ = WriteChunk(stream, Chunk{Error: "invalid authorization header", Code: "policy_denied", Done: true})
			return
		}
		release, err := s.authorization.Acquire(remote, header)
		if err != nil {
			_ = WriteChunk(stream, Chunk{Error: err.Error(), Code: "policy_denied", Done: true})
			return
		}
		defer release()
		if _, err := s.resolve(ctx, header.Model); err != nil {
			_ = WriteChunk(stream, Chunk{Error: "model not served", Code: "policy_denied", Done: true})
			return
		}
	}

	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		_ = WriteChunk(stream, Chunk{Error: "provider is at max concurrency; retry later or choose another node", Code: "busy", Done: true})
		return
	}

	if s.authorization != nil {
		if err := WriteChunk(stream, Chunk{Code: "authorized"}); err != nil {
			return
		}
	}
	_ = stream.SetDeadline(time.Now().Add(authTimeout))
	req, err := ReadRequest(stream)
	if err != nil {
		_ = WriteChunk(stream, Chunk{Error: err.Error(), Done: true})
		return
	}

	if s.authorization != nil && (req.Publisher != header.Publisher || req.Model != header.Model || req.MaxTokens == nil || *req.MaxTokens != *header.MaxTokens) {
		_ = WriteChunk(stream, Chunk{Error: "request differs from authorization", Code: "policy_denied", Done: true})
		return
	}
	_ = stream.SetDeadline(start.Add(streamTimeout))
	adapter, err := s.resolve(ctx, req.Model)
	if err != nil {
		_ = WriteChunk(stream, Chunk{Error: err.Error(), Done: true})
		_ = s.log(audit.Event{
			Type:   audit.EventPolicyDenied,
			PeerID: remote.String(),
			Model:  req.Model,
			Reason: err.Error(),
		})
		return
	}

	var bytesOut int64
	var promptTokens, completionTokens int
	writeErr := adapter.Chat(ctx, backend.ChatRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Stream:      true,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}, func(c backend.ChatChunk) error {
		promptTokens = c.PromptTokens
		completionTokens = c.CompletionTokens
		bytesOut += int64(len(c.Delta) + len(c.Reasoning))
		return WriteChunk(stream, Chunk{
			Delta:            c.Delta,
			Reasoning:        c.Reasoning,
			Done:             c.Done,
			FinishReason:     c.FinishReason,
			PromptTokens:     c.PromptTokens,
			CompletionTokens: c.CompletionTokens,
		})
	})
	if writeErr != nil {
		_ = WriteChunk(stream, Chunk{Error: writeErr.Error(), Done: true})
	}

	_ = s.log(audit.Event{
		Type:       audit.EventInferenceServed,
		PeerID:     remote.String(),
		Model:      req.Model,
		Backend:    adapter.Name(),
		BytesOut:   bytesOut,
		DurationMS: time.Since(start).Milliseconds(),
		Reason:     fmt.Sprintf("prompt=%d completion=%d", promptTokens, completionTokens),
	})
}

func (s *Server) log(ev audit.Event) error {
	if s.auditor == nil {
		return nil
	}
	return s.auditor.Log(ev)
}
