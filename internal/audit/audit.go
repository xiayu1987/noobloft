// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"
)

type EventType string

const (
	EventNodeStart          EventType = "node_start"
	EventNodeStop           EventType = "node_stop"
	EventPeerConnected      EventType = "peer_connected"
	EventPeerDisconnected   EventType = "peer_disconnected"
	EventInferenceServed    EventType = "inference_served"
	EventInferenceRequested EventType = "inference_requested"
	EventRelayCircuit       EventType = "relay_circuit"
	EventPolicyDenied       EventType = "policy_denied"
	EventPeerBlocked        EventType = "peer_blocked"
)

type Event struct {
	Time       time.Time `json:"time"`
	Type       EventType `json:"type"`
	PeerID     string    `json:"peerId,omitempty"`
	Model      string    `json:"model,omitempty"`
	Backend    string    `json:"backend,omitempty"`
	BytesIn    int64     `json:"bytesIn,omitempty"`
	BytesOut   int64     `json:"bytesOut,omitempty"`
	DurationMS int64     `json:"durationMs,omitempty"`
	Reason     string    `json:"reason,omitempty"`
}

type Logger struct {
	mu      sync.Mutex
	file    *os.File
	enabled bool
}

func Open(dir string, enabled bool) (*Logger, error) {
	if !enabled {
		return &Logger{enabled: false}, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, localization.Errorf("errors.audit.createDir", err)
	}
	path := filepath.Join(dir, "audit.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, localization.Errorf("errors.audit.open", err)
	}
	return &Logger{file: f, enabled: true}, nil
}

func (l *Logger) Log(ev Event) error {
	if l == nil || !l.enabled {
		return nil
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return localization.Errorf("errors.audit.encode", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.file.Write(append(raw, '\n')); err != nil {
		return localization.Errorf("errors.audit.write", err)
	}
	return nil
}

func (l *Logger) Close() error {
	if l == nil || !l.enabled {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}
