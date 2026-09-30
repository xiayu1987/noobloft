// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

//go:build !windows

package main

import (
	"fmt"
	"os"

	"github.com/xiayu1987/noobloft/internal/localization"
)

func cmdTray(args []string) error { return localization.Errorf("cli.trayUnsupported") }

func trayFatal(err error) { fmt.Fprintf(os.Stderr, tr("cli.error"), trErr(err)) }
