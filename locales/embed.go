// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package locales

import "embed"

//go:embed en.json zh-CN.json
var files embed.FS

func ReadFile(name string) ([]byte, error) { return files.ReadFile(name) }
