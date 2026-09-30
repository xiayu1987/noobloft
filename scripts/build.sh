#!/usr/bin/env sh
# Copyright (c) 2026 xiayu
# Contact: 126240622+xiayu1987@users.noreply.github.com
# SPDX-License-Identifier: MIT

set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

cd "$repo_dir/web"
npm ci
npm run build

cd "$repo_dir"
mkdir -p bin
go build -o bin/noobloftd ./cmd/noobloftd
