// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package localization

import (
	"errors"
	"io/fs"
	"testing"
)

func TestLocalizedError(t *testing.T) {
	inner := Errorf("errors.config.read", fs.ErrNotExist)
	outer := Errorf("errors.config.parse", "a.json", inner)
	if got := outer.Error(); got != "failed to parse config a.json: failed to read config: file does not exist" {
		t.Fatalf("english: %q", got)
	}
	if got := Localize(outer, "zh-CN"); got != "解析配置 a.json 失败: 读取配置失败: file does not exist" {
		t.Fatalf("chinese: %q", got)
	}
	if !errors.Is(outer, fs.ErrNotExist) {
		t.Fatal("wrapped error lost")
	}
	var target *Error
	if !errors.As(outer, &target) || target != outer {
		t.Fatal("errors.As failed")
	}
	if got := Localize(errors.New("plain"), "zh-CN"); got != "plain" {
		t.Fatalf("plain: %q", got)
	}
	if got := Errorf("errors.missing.key").Error(); got != "errors.missing.key" {
		t.Fatalf("missing key: %q", got)
	}
	if got := Errorf("errors.missing.key", "a", 1).Error(); got != "errors.missing.key: a, 1" {
		t.Fatalf("missing key args: %q", got)
	}
	if Localize(nil, "en") != "" {
		t.Fatal("nil error")
	}
}
