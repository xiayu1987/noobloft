// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/xiayu1987/noobloft/internal/localization"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
}

type ChatChunk struct {
	Delta            string
	Reasoning        string
	Done             bool
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
}

type ModelInfo struct {
	Name          string `json:"name"`
	ContextLength int    `json:"contextLength,omitempty"`
	ParameterSize string `json:"parameterSize,omitempty"`
	Quantization  string `json:"quantization,omitempty"`
	License       string `json:"license,omitempty"`
}

type Adapter interface {
	Name() string
	Kind() string
	External() bool
	ListModels(ctx context.Context) ([]ModelInfo, error)
	Chat(ctx context.Context, req ChatRequest, onChunk func(ChatChunk) error) error
}

type Factory func(cfg Spec) (Adapter, error)

type Spec struct {
	Name    string
	Kind    string
	BaseURL string
	APIKey  string
}

var (
	registryMu sync.RWMutex
	factories  = map[string]Factory{}
)

func Register(kind string, f Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := factories[kind]; dup {
		panic(fmt.Sprintf("backend: duplicate registration kind=%s", kind))
	}
	factories[kind] = f
}

func Kinds() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(factories))
	for k := range factories {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var ErrExternalForwardingDisabled = localization.Errorf("errors.backend.externalForwarding")

func New(spec Spec, allowExternal bool) (Adapter, error) {
	registryMu.RLock()
	f, ok := factories[spec.Kind]
	registryMu.RUnlock()
	if !ok {
		return nil, localization.Errorf("errors.backend.unknownKind", spec.Kind, Kinds())
	}
	a, err := f(spec)
	if err != nil {
		return nil, localization.Errorf("errors.backend.create", spec.Name, err)
	}
	if a.External() && !allowExternal {
		return nil, localization.Errorf("errors.backend.denied", spec.Name, spec.Kind, ErrExternalForwardingDisabled)
	}
	return a, nil
}
