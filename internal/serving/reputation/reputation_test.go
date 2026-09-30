// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package reputation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestScoreIsNeutralWithoutSamples(t *testing.T) {
	var r Record
	if got := r.Score(); got != NeutralScore {
		t.Fatalf("zero-sample score = %v, want %v", got, NeutralScore)
	}
	if r.Samples() != 0 {
		t.Fatalf("Samples() = %d, want 0", r.Samples())
	}
	if !r.Trusted() {
		t.Fatal("zero samples should not be untrusted")
	}
}

func TestScoreMovesWithObservedOutcomes(t *testing.T) {
	good := Record{Success: 5}
	if !(good.Score() > NeutralScore) {
		t.Fatalf("all-success score = %v, should exceed neutral %v", good.Score(), NeutralScore)
	}
	if !good.Trusted() {
		t.Fatal("all-success peer should be trusted")
	}

	bad := Record{Failure: 5}
	if !(bad.Score() < NeutralScore) {
		t.Fatalf("all-failure score = %v, should be below neutral %v", bad.Score(), NeutralScore)
	}
}

func TestNoVerdictBelowMinSamples(t *testing.T) {
	for _, r := range []Record{
		{Failure: 1},
		{Failure: 2},
		{ProbesFailed: 1},
	} {
		if r.Samples() >= MinSamples {
			t.Fatalf("case sample count %d should not reach MinSamples=%d", r.Samples(), MinSamples)
		}
		if !r.Trusted() {
			t.Errorf("samples %d below MinSamples should be trusted, score %v", r.Samples(), r.Score())
		}
	}
}

func TestDistrustAfterRepeatedFailures(t *testing.T) {
	r := Record{Failure: MinSamples}
	if r.Score() >= DistrustThreshold {
		t.Fatalf("score %v should be below threshold %v, test case invalid", r.Score(), DistrustThreshold)
	}
	if r.Trusted() {
		t.Fatalf("samples %d with score %v should be untrusted", r.Samples(), r.Score())
	}
}

func TestProbeFailureWeighsMoreThanPlainFailure(t *testing.T) {
	plain := Record{Failure: 3}
	probed := Record{ProbesFailed: 2}
	if !(probed.Score() < plain.Score()) {
		t.Fatalf("2 probe failures (score %v) should be worse than 3 regular failures (score %v)",
			probed.Score(), plain.Score())
	}
	enough := Record{ProbesFailed: MinSamples}
	if enough.Trusted() {
		t.Fatalf("should be untrusted after %d probe failures, score %v", MinSamples, enough.Score())
	}
}

func TestOpenDisabledIsNoop(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, false)
	if err != nil {
		t.Fatalf("Open should not fail when disabled: %v", err)
	}
	if s.Enabled() {
		t.Fatal("Enabled() should be false when enabled=false")
	}
	s.Observe("peer-a", true, 10)
	s.Observe("peer-a", false, 0)
	s.ObserveProbe("peer-a", true)
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush should be a no-op when disabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); !os.IsNotExist(err) {
		t.Fatal("no reputation file should be created when disabled")
	}
	if _, _, ok := s.ScoreOf("peer-a"); ok {
		t.Fatal("no observations expected when disabled")
	}
}

func TestNilStoreIsSafe(t *testing.T) {
	var s *Store
	if s.Enabled() {
		t.Fatal("nil Store Enabled() should be false")
	}
	s.Observe("peer-a", true, 10)
	s.ObserveProbe("peer-a", false)
	if err := s.Flush(); err != nil {
		t.Fatalf("nil Store Flush should be a no-op: %v", err)
	}
	if _, _, ok := s.ScoreOf("peer-a"); ok {
		t.Fatal("nil Store should have no observations")
	}
}

func TestFlushPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, true)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	s.Observe("peer-b", false, 0)
	s.ObserveProbe("peer-b", true)
	s.Observe("peer-a", true, 120)
	s.Observe("peer-a", true, 90)
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, FileName+".tmp")); !os.IsNotExist(err) {
		t.Error("temp file left after Flush, rename incomplete")
	}

	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("read reputation file failed: %v", err)
	}
	var recs []Record
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatalf("reputation file is not valid JSON: %v", err)
	}
	if len(recs) != 2 || recs[0].PeerID != "peer-a" || recs[1].PeerID != "peer-b" {
		t.Fatalf("persisted order or content mismatch: %+v", recs)
	}

	re, err := Open(dir, true)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	score, samples, ok := re.ScoreOf("peer-a")
	if !ok {
		t.Fatal("peer-a observation lost after reload")
	}
	if samples != 2 {
		t.Fatalf("reloaded samples = %d, want 2", samples)
	}
	if want := (Record{Success: 2}).Score(); score != want {
		t.Fatalf("reloaded score = %v, want %v", score, want)
	}
	last, found := re.Record("peer-a")
	if !found || last.LastLatencyMS != 90 {
		t.Fatalf("reloaded latency mismatch: %+v", last)
	}
	if _, found := re.Record("peer-b"); !found {
		t.Fatal("peer-b observation lost after reload")
	}
}

func TestCorruptLedgerDoesNotBlockStartup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{不是合法 JSON"), 0o600); err != nil {
		t.Fatalf("prepare corrupt ledger failed: %v", err)
	}
	s, err := Open(dir, true)
	if err != nil {
		t.Fatalf("corrupt ledger should not block startup: %v", err)
	}
	if _, _, ok := s.ScoreOf("peer-a"); ok {
		t.Fatal("corrupt ledger should be discarded, not partially loaded")
	}
	s.Observe("peer-a", true, 5)
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush after rebuild failed: %v", err)
	}
}

func TestOpenSkipsRecordsWithoutPeerID(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`[{"peerId":"","success":9},{"peerId":"peer-a","success":1}]`)
	if err := os.WriteFile(filepath.Join(dir, FileName), raw, 0o600); err != nil {
		t.Fatalf("prepare ledger failed: %v", err)
	}
	s, err := Open(dir, true)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if _, found := s.Record(""); found {
		t.Fatal("empty PeerID record should be skipped")
	}
	if _, found := s.Record("peer-a"); !found {
		t.Fatal("valid records should not be dropped along with it")
	}
}
