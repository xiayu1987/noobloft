// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/xiayu1987/noobloft/internal/localization"
)

var cliLanguage = localization.Environment()

func tr(key string) string { return localization.Text(cliLanguage, key) }

func trErr(err error) string { return localization.Localize(err, cliLanguage) }

func languageArgs(args []string) ([]string, string, error) {
	language := localization.Environment()
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			out = append(out, args[i:]...)
			break
		}
		if arg == "--lang" || arg == "-lang" || strings.HasPrefix(arg, "--lang=") || strings.HasPrefix(arg, "-lang=") {
			_, value, inline := strings.Cut(arg, "=")
			if !inline {
				i++
				if i >= len(args) {
					return nil, "", fmt.Errorf("--lang requires zh-CN or en / --lang 必须指定 zh-CN 或 en")
				}
				value = args[i]
			}
			if value != "zh-CN" && value != "en" {
				return nil, "", fmt.Errorf("unsupported language %q; use zh-CN or en / 不支持该语言", value)
			}
			language = value
			continue
		}
		out = append(out, arg)
		if strings.HasPrefix(arg, "-") && !strings.Contains(arg, "=") && arg != "-h" && arg != "--help" && arg != "-help" && i+1 < len(args) {
			i++
			out = append(out, args[i])
		}
	}
	return out, language, nil
}
