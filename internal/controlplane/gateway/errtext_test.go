// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xiayu1987/noobloft/internal/localization"
	"github.com/xiayu1987/noobloft/internal/serving/inference"
)

func TestErrTextFollowsAcceptLanguage(t *testing.T) {
	cases := []struct {
		name string
		err  error
		zh   string
		en   string
	}{
		{"localized", localization.Errorf("errors.inference.modelEmpty"), localization.Text("zh-CN", "errors.inference.modelEmpty"), localization.Text("en", "errors.inference.modelEmpty")},
		{"busy", &inference.ErrRemote{Code: "busy", PeerID: "peer-a", Detail: "ignored"}, "并发已满", "max concurrency"},
		{"wrapped", localization.Errorf("errors.backend.denied", "b", "openai", localization.Errorf("errors.backend.externalForwarding")), "策略禁止", "policy forbids"},
		{"plain", fmt.Errorf("plain failure"), "plain failure", "plain failure"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, pair := range [][2]string{{"zh-CN", tc.zh}, {"en", tc.en}} {
				lang, want := pair[0], pair[1]
				r := httptest.NewRequest("GET", "/", nil)
				r.Header.Set("Accept-Language", lang)
				if got := errText(r, tc.err); !strings.Contains(got, want) {
					t.Fatalf("lang %s: got %q, want substring %q", lang, got, want)
				}
			}
		})
	}
}
