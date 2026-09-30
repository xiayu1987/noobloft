// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/xiayu1987/noobloft/internal/localization"
)

func init() {
	Register("ollama", func(spec Spec) (Adapter, error) {
		if spec.BaseURL == "" {
			return nil, localization.Errorf("errors.backend.ollamaBaseUrl")
		}
		return &ollamaAdapter{
			name:    spec.Name,
			baseURL: strings.TrimRight(spec.BaseURL, "/"),
			client:  &http.Client{},
		}, nil
	})
}

type ollamaAdapter struct {
	name    string
	baseURL string
	client  *http.Client
}

func (o *ollamaAdapter) Name() string   { return o.name }
func (o *ollamaAdapter) Kind() string   { return "ollama" }
func (o *ollamaAdapter) External() bool { return false }

type ollamaTagsResponse struct {
	Models []struct {
		Name    string `json:"name"`
		Details struct {
			ParameterSize     string `json:"parameter_size"`
			QuantizationLevel string `json:"quantization_level"`
			Family            string `json:"family"`
		} `json:"details"`
	} `json:"models"`
}

func (o *ollamaAdapter) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, localization.Errorf("errors.backend.ollamaRequest", err)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, localization.Errorf("errors.backend.ollamaAccess", o.baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, localization.Errorf("errors.backend.ollamaStatus", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tags ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, localization.Errorf("errors.backend.ollamaModels", err)
	}
	out := make([]ModelInfo, 0, len(tags.Models))
	for _, m := range tags.Models {
		out = append(out, ModelInfo{
			Name:          m.Name,
			ParameterSize: m.Details.ParameterSize,
			Quantization:  m.Details.QuantizationLevel,
			License:       m.Details.Family,
		})
	}
	return out, nil
}

type ollamaChatChunk struct {
	Message struct {
		Content  string `json:"content"`
		Thinking string `json:"thinking"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	Error           string `json:"error"`
}

func (o *ollamaAdapter) Chat(ctx context.Context, req ChatRequest, onChunk func(ChatChunk) error) error {
	payload := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   true,
	}
	opts := map[string]any{}
	if req.Temperature != nil {
		opts["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		opts["num_predict"] = *req.MaxTokens
	}
	if len(opts) > 0 {
		payload["options"] = opts
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return localization.Errorf("errors.backend.ollamaEncode", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return localization.Errorf("errors.backend.ollamaRequest", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(httpReq)
	if err != nil {
		return localization.Errorf("errors.backend.ollamaCall", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return localization.Errorf("errors.backend.ollamaStatus", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var chunk ollamaChatChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return localization.Errorf("errors.backend.ollamaStream", err)
		}
		if chunk.Error != "" {
			return localization.Errorf("errors.backend.ollamaError", chunk.Error)
		}
		out := ChatChunk{
			Delta:            chunk.Message.Content,
			Reasoning:        chunk.Message.Thinking,
			Done:             chunk.Done,
			FinishReason:     chunk.DoneReason,
			PromptTokens:     chunk.PromptEvalCount,
			CompletionTokens: chunk.EvalCount,
		}
		if err := onChunk(out); err != nil {
			return err
		}
		if chunk.Done {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return localization.Errorf("errors.backend.ollamaInterrupted", err)
	}
	return onChunk(ChatChunk{Done: true, FinishReason: "stop"})
}
