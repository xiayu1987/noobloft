// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"strings"
	"testing"
)

func TestNewCompletionIDIsUnique(t *testing.T) {
	seen := make(map[string]struct{})
	for range 1000 {
		id := newCompletionID()
		if !strings.HasPrefix(id, "chatcmpl-") || len(id) != len("chatcmpl-")+24 {
			t.Fatalf("unexpected ID format: %q", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate ID: %q", id)
		}
		seen[id] = struct{}{}
	}
}
