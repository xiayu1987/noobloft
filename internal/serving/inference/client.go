// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/xiayu1987/noobloft/internal/audit"
)

type Client struct {
	host    host.Host
	auditor *audit.Logger
}

func NewClient(h host.Host, auditor *audit.Logger) *Client {
	return &Client{host: h, auditor: auditor}
}

type ErrRemote struct {
	Code   string
	PeerID string
	Detail string
}

func (e *ErrRemote) Error() string {
	return e.Message("en")
}

func (e *ErrRemote) Message(language string) string {
	detail := e.Detail
	if e.Code == "busy" {
		detail = localization.Text(language, "errors.inference.busy")
	}
	return fmt.Sprintf(localization.Text(language, "errors.inference.remote"), e.PeerID, detail)
}

func (c *Client) Chat(ctx context.Context, target peer.ID, req *Request, onChunk func(Chunk) error) error {
	if err := req.Validate(); err != nil {
		return err
	}
	start := time.Now()

	proto := Protocol
	if req.Publisher != "" {
		proto = AuthorizedProtocol
	}
	stream, err := c.host.NewStream(network.WithAllowLimitedConn(ctx, "inference-relay-fallback"), target, protocol.ID(proto))
	if err != nil {
		return localization.Errorf("errors.inference.connect", target, err)
	}
	defer stream.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = stream.Reset() })
	defer stopCancel()
	_ = stream.SetDeadline(start.Add(streamTimeout))

	if req.Publisher != "" {
		_ = stream.SetDeadline(time.Now().Add(authTimeout))
		if err := json.NewEncoder(stream).Encode(AuthHeader{Publisher: req.Publisher, Model: req.Model, MaxTokens: req.MaxTokens, Access: req.Access}); err != nil {
			return err
		}
		ack, err := NewChunkReader(stream).Next()
		if err != nil {
			return err
		}
		if ack.Error != "" {
			return &ErrRemote{PeerID: target.String(), Detail: ack.Error, Code: ack.Code}
		}
		if ack.Code != "authorized" {
			return fmt.Errorf("missing authorization acknowledgement")
		}
		_ = stream.SetDeadline(start.Add(streamTimeout))
	}
	if err := WriteRequest(stream, req); err != nil {
		_ = stream.Reset()
		return err
	}
	if err := stream.CloseWrite(); err != nil {
		_ = stream.Reset()
		return localization.Errorf("errors.inference.closeWrite", err)
	}

	var bytesIn int64
	reader := NewChunkReader(stream)
	for {
		select {
		case <-ctx.Done():
			_ = stream.Reset()
			return ctx.Err()
		default:
		}

		chunk, err := reader.Next()
		if errors.Is(err, io.EOF) {
			_ = c.log(audit.Event{
				Type:       audit.EventInferenceRequested,
				PeerID:     target.String(),
				Model:      req.Model,
				BytesIn:    bytesIn,
				DurationMS: time.Since(start).Milliseconds(),
				Reason:     "peer closed without a final frame",
			})
			return onChunk(Chunk{Done: true, FinishReason: "stop"})
		}
		if err != nil {
			_ = stream.Reset()
			return err
		}
		if chunk.Error != "" {
			return &ErrRemote{PeerID: target.String(), Detail: chunk.Error, Code: chunk.Code}
		}

		bytesIn += int64(len(chunk.Delta) + len(chunk.Reasoning))
		if err := onChunk(chunk); err != nil {
			_ = stream.Reset()
			return err
		}
		if chunk.Done {
			_ = c.log(audit.Event{
				Type:       audit.EventInferenceRequested,
				PeerID:     target.String(),
				Model:      req.Model,
				BytesIn:    bytesIn,
				DurationMS: time.Since(start).Milliseconds(),
			})
			return nil
		}
	}
}

func (c *Client) log(ev audit.Event) error {
	if c.auditor == nil {
		return nil
	}
	return c.auditor.Log(ev)
}
