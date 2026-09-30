// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newOllamaStub(t *testing.T, chatLines []string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, line := range chatLines {
			_, _ = w.Write([]byte(line + "\n"))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newOllamaAdapter(t *testing.T, baseURL string) Adapter {
	t.Helper()
	a, err := New(Spec{Name: "stub", Kind: "ollama", BaseURL: baseURL}, false)
	if err != nil {
		t.Fatalf("create ollama adapter failed: %v", err)
	}
	return a
}

func TestChatCapturesThinkingSeparately(t *testing.T) {
	srv := newOllamaStub(t, []string{
		`{"message":{"role":"assistant","content":"","thinking":"先想一下"},"done":false}`,
		`{"message":{"role":"assistant","content":"","thinking":"再想一下"},"done":false}`,
		`{"message":{"role":"assistant","content":"你好"},"done":false}`,
		`{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":16,"eval_count":24}`,
	})
	a := newOllamaAdapter(t, srv.URL)

	var content, reasoning strings.Builder
	var last ChatChunk
	err := a.Chat(context.Background(), ChatRequest{
		Model:    "qwen3:14b",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(c ChatChunk) error {
		content.WriteString(c.Delta)
		reasoning.WriteString(c.Reasoning)
		last = c
		return nil
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if got := content.String(); got != "你好" {
		t.Errorf("content should contain only the final answer, got %q", got)
	}
	if got := reasoning.String(); got != "先想一下再想一下" {
		t.Errorf("thinking should be fully captured, got %q", got)
	}
	if !last.Done || last.FinishReason != "stop" {
		t.Errorf("last frame should be done=stop, got done=%v reason=%q", last.Done, last.FinishReason)
	}
	if last.PromptTokens != 16 || last.CompletionTokens != 24 {
		t.Errorf("last frame should carry usage, got prompt=%d completion=%d", last.PromptTokens, last.CompletionTokens)
	}
}

func TestChatSyntheticDoneOnTruncatedStream(t *testing.T) {
	srv := newOllamaStub(t, []string{
		`{"message":{"role":"assistant","content":"半句"},"done":false}`,
	})
	a := newOllamaAdapter(t, srv.URL)

	var chunks []ChatChunk
	if err := a.Chat(context.Background(), ChatRequest{Model: "m"}, func(c ChatChunk) error {
		chunks = append(chunks, c)
		return nil
	}); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("want 1 content frame + 1 synthesized done frame, got %d", len(chunks))
	}
	if !chunks[1].Done || chunks[1].FinishReason != "stop" {
		t.Errorf("synthesized frame should be done=stop, got %+v", chunks[1])
	}
}

func TestChatPropagatesUpstreamError(t *testing.T) {
	srv := newOllamaStub(t, []string{`{"error":"model not found"}`})
	a := newOllamaAdapter(t, srv.URL)

	err := a.Chat(context.Background(), ChatRequest{Model: "missing"}, func(ChatChunk) error {
		t.Error("no chunk should be delivered on error")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("upstream error should propagate, got %v", err)
	}
}
