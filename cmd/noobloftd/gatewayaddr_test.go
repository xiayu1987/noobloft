// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package main

import "testing"

func TestLocalGatewayBase(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8080": "http://127.0.0.1:8080",
		"0.0.0.0:8080":   "http://127.0.0.1:8080",
		"[::]:8080":      "http://127.0.0.1:8080",
		":8080":          "http://127.0.0.1:8080",
		"[::1]:9000":     "http://[::1]:9000",
		"localhost:80":   "http://localhost:80",
	}
	for in, want := range cases {
		if got := localGatewayBase(in); got != want {
			t.Errorf("localGatewayBase(%q) = %q, want %q", in, got, want)
		}
	}
}
