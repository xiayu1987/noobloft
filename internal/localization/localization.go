// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package localization

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/xiayu1987/noobloft/locales"
)

var catalogues = load()

func load() map[string]map[string]string {
	result := map[string]map[string]string{}
	for _, lang := range []string{"zh-CN", "en"} {
		b, err := locales.ReadFile(lang + ".json")
		if err != nil {
			panic(err)
		}
		var messages map[string]string
		if err = json.Unmarshal(b, &messages); err != nil {
			panic(err)
		}
		result[lang] = messages
	}
	return result
}
func Normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "en" || strings.HasPrefix(value, "en-") || strings.HasPrefix(value, "en_") || strings.HasPrefix(value, "en.") {
		return "en"
	}
	return "zh-CN"
}
func Environment() string {
	for _, name := range []string{"NOOBLOFT_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := os.Getenv(name); value != "" {
			return Normalize(value)
		}
	}
	return "zh-CN"
}

func FromRequest(r *http.Request) string {
	language, best := "zh-CN", -1.0
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		tag := strings.ToLower(strings.TrimSpace(fields[0]))
		if tag != "en" && !strings.HasPrefix(tag, "en-") && tag != "zh" && !strings.HasPrefix(tag, "zh-") {
			continue
		}
		quality := 1.0
		for _, field := range fields[1:] {
			if q, ok := strings.CutPrefix(strings.TrimSpace(field), "q="); ok {
				value, err := strconv.ParseFloat(q, 64)
				if err != nil || value < 0 || value > 1 {
					quality = 0
				} else {
					quality = value
				}
			}
		}
		if quality > 0 && quality > best {
			language, best = Normalize(tag), quality
		}
	}
	return language
}
func Text(language, key string) string {
	if value, ok := lookup(language, key); ok {
		return value
	}
	return key
}
func lookup(language, key string) (string, bool) {
	if value, ok := catalogues[Normalize(language)][key]; ok {
		return value, true
	}
	value, ok := catalogues["zh-CN"][key]
	return value, ok
}
func Request(r *http.Request, key string) string { return Text(FromRequest(r), key) }
