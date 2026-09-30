// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package inference

import (
	"encoding/json"
	"io"

	"github.com/xiayu1987/noobloft/internal/localization"

	"github.com/xiayu1987/noobloft/internal/security/publisher"
	"github.com/xiayu1987/noobloft/internal/serving/backend"
)

const Protocol = "/noobloft/1.0.0/infer"

const MaxRequestBytes = 4 << 20

type Request struct {
	Publisher   string              `json:"publisher,omitempty"`
	Access      *publisher.Document `json:"-"`
	Model       string              `json:"model"`
	Messages    []backend.Message   `json:"messages"`
	Temperature *float64            `json:"temperature,omitempty"`
	MaxTokens   *int                `json:"maxTokens,omitempty"`
}

func (r *Request) Validate() error {
	if r.Model == "" {
		return localization.Errorf("errors.inference.modelEmpty")
	}
	if len(r.Messages) == 0 {
		return localization.Errorf("errors.inference.messagesEmpty")
	}
	for i, m := range r.Messages {
		if m.Role == "" {
			return localization.Errorf("errors.inference.roleEmpty", i)
		}
	}
	return nil
}

type Chunk struct {
	Code             string `json:"code,omitempty"`
	Delta            string `json:"delta,omitempty"`
	Reasoning        string `json:"reasoning,omitempty"`
	Done             bool   `json:"done,omitempty"`
	FinishReason     string `json:"finishReason,omitempty"`
	PromptTokens     int    `json:"promptTokens,omitempty"`
	CompletionTokens int    `json:"completionTokens,omitempty"`
	Error            string `json:"error,omitempty"`
}

func WriteRequest(w io.Writer, req *Request) error {
	raw, err := json.Marshal(req)
	if err != nil {
		return localization.Errorf("errors.inference.encodeRequest", err)
	}
	if len(raw) > MaxRequestBytes {
		return localization.Errorf("errors.inference.tooLarge", len(raw), MaxRequestBytes)
	}
	if _, err := w.Write(append(raw, '\n')); err != nil {
		return localization.Errorf("errors.inference.sendRequest", err)
	}
	return nil
}

func ReadRequest(r io.Reader) (*Request, error) {
	limited := io.LimitReader(r, MaxRequestBytes+1)
	dec := json.NewDecoder(limited)
	var req Request
	if err := dec.Decode(&req); err != nil {
		return nil, localization.Errorf("errors.inference.decodeRequest", err)
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return &req, nil
}

func WriteChunk(w io.Writer, c Chunk) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return localization.Errorf("errors.inference.encodeChunk", err)
	}
	if _, err := w.Write(append(raw, '\n')); err != nil {
		return localization.Errorf("errors.inference.sendChunk", err)
	}
	return nil
}

type ChunkReader struct {
	dec *json.Decoder
}

func NewChunkReader(r io.Reader) *ChunkReader {
	return &ChunkReader{dec: json.NewDecoder(r)}
}

func (cr *ChunkReader) Next() (Chunk, error) {
	var c Chunk
	if err := cr.dec.Decode(&c); err != nil {
		if err == io.EOF {
			return c, io.EOF
		}
		return c, localization.Errorf("errors.inference.decodeChunk", err)
	}
	return c, nil
}
