// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package localization

import (
	"net/http/httptest"
	"regexp"
	"slices"
	"testing"
)

func TestCatalogues(t *testing.T) {
	if len(catalogues["en"]) != len(catalogues["zh-CN"]) {
		t.Fatal("catalogue key sets differ")
	}
	placeholders := regexp.MustCompile(`%(?:\[[0-9]+\])?[-+# 0]*(?:[0-9]+|\*)?(?:\.[0-9]+)?[a-zA-Z%]`)
	semantic := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*(\.[a-zA-Z][a-zA-Z0-9]*)+$`)
	for key, chinese := range catalogues["zh-CN"] {
		english, ok := catalogues["en"][key]
		if !ok || chinese == "" || english == "" {
			t.Errorf("missing text: %s", key)
		}
		if !semantic.MatchString(key) {
			t.Errorf("non-semantic key: %s", key)
		}
		if !slices.Equal(placeholders.FindAllString(chinese, -1), placeholders.FindAllString(english, -1)) {
			t.Errorf("format placeholders differ: %s", key)
		}
	}
}
func TestLanguageSelection(t *testing.T) {
	for _, tc := range []struct{ header, want string }{{"", "zh-CN"}, {"en-US,en;q=0.9", "en"}, {"zh-CN;q=0.9,en;q=0.4", "zh-CN"}, {"fr,en;q=0.8", "en"}, {"en;q=0", "zh-CN"}, {"en;q=broken", "zh-CN"}} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Language", tc.header)
		if got := FromRequest(r); got != tc.want {
			t.Errorf("%q: %s, want %s", tc.header, got, tc.want)
		}
	}
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("NOOBLOFT_LANG", "")
	if Environment() != "en" {
		t.Fatal("LANG ignored")
	}
	t.Setenv("NOOBLOFT_LANG", "zh-CN")
	if Environment() != "zh-CN" {
		t.Fatal("override ignored")
	}
}
