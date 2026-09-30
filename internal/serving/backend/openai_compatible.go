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
	Register("openai-compatible", func(spec Spec) (Adapter, error) {
		if spec.BaseURL == "" {
			return nil, localization.Errorf("errors.backend.openaiBaseUrl")
		}
		base := strings.TrimRight(spec.BaseURL, "/")
		base = strings.TrimSuffix(base, "/v1")
		return &openAICompatAdapter{
			name:    spec.Name,
			baseURL: base,
			apiKey:  spec.APIKey,
			client:  &http.Client{},
		}, nil
	})
}

type openAICompatAdapter struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
}

func (a *openAICompatAdapter) Name() string { return a.name }
func (a *openAICompatAdapter) Kind() string { return "openai-compatible" }

func (a *openAICompatAdapter) External() bool { return false }

func (a *openAICompatAdapter) setHeaders(r *http.Request) {
	r.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		r.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
}

type openAIModelsResponse struct {
	Data []struct {
		ID         string `json:"id"`
		MaxLen     int    `json:"max_model_len"`
		Permission []any  `json:"permission"`
	} `json:"data"`
}

func (a *openAICompatAdapter) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, localization.Errorf("errors.backend.request", err)
	}
	a.setHeaders(req)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, localization.Errorf("errors.backend.access", a.baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, localization.Errorf("errors.backend.status", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed openAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, localization.Errorf("errors.backend.models", err)
	}
	out := make([]ModelInfo, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		out = append(out, ModelInfo{
			Name:          m.ID,
			ContextLength: m.MaxLen,
		})
	}
	return out, nil
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (a *openAICompatAdapter) Chat(ctx context.Context, req ChatRequest, onChunk func(ChatChunk) error) error {
	payload := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   true,
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		payload["max_tokens"] = *req.MaxTokens
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return localization.Errorf("errors.backend.encode", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.baseURL+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return localization.Errorf("errors.backend.request", err)
	}
	a.setHeaders(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return localization.Errorf("errors.backend.call", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return localization.Errorf("errors.backend.status", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var promptTokens, completionTokens int
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return onChunk(ChatChunk{
				Done:             true,
				FinishReason:     "stop",
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
			})
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return localization.Errorf("errors.backend.sseFrame", err)
		}
		if chunk.Error != nil {
			return localization.Errorf("errors.backend.error", chunk.Error.Message)
		}
		if chunk.Usage != nil {
			promptTokens = chunk.Usage.PromptTokens
			completionTokens = chunk.Usage.CompletionTokens
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		c := chunk.Choices[0]
		out := ChatChunk{
			Delta:            c.Delta.Content,
			Reasoning:        c.Delta.ReasoningContent,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
		}
		if c.FinishReason != nil && *c.FinishReason != "" {
			out.Done = true
			out.FinishReason = *c.FinishReason
		}
		if err := onChunk(out); err != nil {
			return err
		}
		if out.Done {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return localization.Errorf("errors.backend.sseInterrupted", err)
	}
	return onChunk(ChatChunk{
		Done:             true,
		FinishReason:     "stop",
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	})
}
