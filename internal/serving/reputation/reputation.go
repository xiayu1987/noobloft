// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package reputation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/xiayu1987/noobloft/internal/localization"
)

const FileName = "reputation.json"

const (
	NeutralScore = 0.5

	probeWeight = 2.0

	priorSuccess = 1.0
	priorFailure = 1.0

	MinSamples = 3

	DistrustThreshold = 0.34
)

type Record struct {
	PeerID        string    `json:"peerId"`
	Success       int64     `json:"success"`
	Failure       int64     `json:"failure"`
	ProbesPassed  int64     `json:"probesPassed"`
	ProbesFailed  int64     `json:"probesFailed"`
	LastLatencyMS int64     `json:"lastLatencyMs,omitempty"`
	LastProbedAt  time.Time `json:"lastProbedAt,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (r Record) Samples() int64 {
	return r.Success + r.Failure + r.ProbesPassed + r.ProbesFailed
}

func (r Record) Score() float64 {
	s := float64(r.Success) + probeWeight*float64(r.ProbesPassed) + priorSuccess
	f := float64(r.Failure) + probeWeight*float64(r.ProbesFailed) + priorFailure
	return s / (s + f)
}

func (r Record) Trusted() bool {
	if r.Samples() < MinSamples {
		return true
	}
	return r.Score() >= DistrustThreshold
}

type Store struct {
	mu      sync.Mutex
	path    string
	enabled bool
	records map[string]*Record
}

func Open(dir string, enabled bool) (*Store, error) {
	if !enabled {
		return &Store{enabled: false}, nil
	}
	s := &Store{
		path:    filepath.Join(dir, FileName),
		enabled: true,
		records: map[string]*Record{},
	}

	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, localization.Errorf("errors.reputation.read", err)
	}
	var recs []Record
	if err := json.Unmarshal(raw, &recs); err != nil {
		return s, nil
	}
	for i := range recs {
		r := recs[i]
		if r.PeerID == "" {
			continue
		}
		s.records[r.PeerID] = &r
	}
	return s, nil
}

func (s *Store) Enabled() bool { return s != nil && s.enabled }

func (s *Store) Record(peerID string) (Record, bool) {
	if !s.Enabled() {
		return Record{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[peerID]
	if !ok {
		return Record{}, false
	}
	return *r, true
}

func (s *Store) ScoreOf(peerID string) (score float64, samples int64, ok bool) {
	r, found := s.Record(peerID)
	if !found {
		return NeutralScore, 0, false
	}
	return r.Score(), r.Samples(), true
}

func (s *Store) Observe(peerID string, ok bool, latencyMS int64) {
	if !s.Enabled() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.ensure(peerID)
	if ok {
		r.Success++
		if latencyMS > 0 {
			r.LastLatencyMS = latencyMS
		}
	} else {
		r.Failure++
	}
	r.UpdatedAt = time.Now().UTC()
}

func (s *Store) ObserveProbe(peerID string, passed bool) {
	if !s.Enabled() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.ensure(peerID)
	if passed {
		r.ProbesPassed++
	} else {
		r.ProbesFailed++
	}
	now := time.Now().UTC()
	r.LastProbedAt = now
	r.UpdatedAt = now
}

func (s *Store) NoteLatency(peerID string, latencyMS int64) {
	if !s.Enabled() || latencyMS <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.ensure(peerID)
	r.LastLatencyMS = latencyMS
	r.UpdatedAt = time.Now().UTC()
}

func (s *Store) ensure(peerID string) *Record {
	r, ok := s.records[peerID]
	if !ok {
		r = &Record{PeerID: peerID}
		s.records[peerID] = r
	}
	return r
}

func (s *Store) Flush() error {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	recs := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		recs = append(recs, *r)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].PeerID < recs[j].PeerID })

	raw, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return localization.Errorf("errors.reputation.encode", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return localization.Errorf("errors.reputation.createDir", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return localization.Errorf("errors.reputation.write", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return localization.Errorf("errors.reputation.commit", err)
	}
	return nil
}
