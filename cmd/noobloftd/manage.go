// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiayu1987/noobloft/internal/config"
)

const managementHelp = "cli.manage.help"

func cmdManage(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(tr(managementHelp))
		return nil
	}
	endpoint := args[0]
	allowed := map[string]bool{"state": true, "resources": true, "issuer": true, "authorization": true, "peers": true, "models": true, "publisher": true, "publisher/export": true, "config": true, "subscription": true, "chat": true, "restart": true, "reset": true}
	if !allowed[endpoint] {
		return fmt.Errorf("%s", tr("cli.manage.unknown"))
	}
	fs := flag.NewFlagSet("manage "+endpoint, flag.ContinueOnError)
	method := fs.String("method", "GET", tr("cli.manage.method"))
	file := fs.String("file", "", tr("cli.manage.file"))
	confirm := fs.String("confirm", "", tr("cli.manage.confirm"))
	expectedRevision := fs.String("revision", "", tr("cli.manage.revision"))
	backup := fs.String("backup", "", tr("cli.manage.backup"))
	dir, e := resolveDir(fs, args[1:])
	if e != nil {
		return e
	}
	c, e := config.Load(dir)
	if e != nil {
		return e
	}
	if _, _, e := net.SplitHostPort(c.Gateway.BindAddr); e != nil {
		return e
	}
	base := localGatewayBase(c.Gateway.BindAddr)
	if !c.Gateway.Enabled {
		return fmt.Errorf("%s", tr("cli.manage.gatewayDisabled"))
	}
	token, e := os.ReadFile(filepath.Join(dir, "admin.token"))
	if e != nil {
		return e
	}
	client := &http.Client{Timeout: 30 * time.Second}
	call := func(path, verb string, body []byte, rev string) ([]byte, error) {
		req, e := http.NewRequest(verb, base+"/manage/"+path, bytes.NewReader(body))
		if e != nil {
			return nil, e
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", cliLanguage)
		req.Header.Set("If-Match", rev)
		resp, e := client.Do(req)
		if e != nil {
			return nil, e
		}
		defer resp.Body.Close()
		b, e := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if e != nil {
			return nil, e
		}
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("%s: %s", resp.Status, b)
		}
		return b, nil
	}
	var body []byte
	isReset := endpoint == "reset" && *confirm != ""
	if isReset && *expectedRevision == "" {
		return fmt.Errorf("%s", tr("cli.manage.previewRequired"))
	}
	if endpoint == "reset" && *confirm == "" {
		endpoint = "resources"
		*method = "GET"
	}
	if endpoint == "restart" || endpoint == "reset" {
		body, _ = json.Marshal(map[string]string{"operation": endpoint, "confirm": *confirm, "backup": *backup})
		endpoint = "lifecycle"
		*method = "POST"
	} else if *method != "GET" {
		if *file == "" {
			return fmt.Errorf("%s", tr("cli.manage.fileRequired"))
		}
		body, e = os.ReadFile(*file)
		if e != nil {
			return e
		}
	}
	rev := ""
	if *method != "GET" {
		b, e := call("state", "GET", nil, "")
		if e != nil {
			return e
		}
		var s struct {
			Revision string `json:"revision"`
		}
		if e = json.Unmarshal(b, &s); e != nil {
			return e
		}
		rev = s.Revision
	}
	if isReset {
		rev = *expectedRevision
	}
	b, e := call(endpoint, *method, body, rev)
	if e != nil {
		return e
	}
	fmt.Println(string(b))
	if isReset {
		var op config.Recovery
		if e = json.Unmarshal(b, &op); e != nil {
			return e
		}
		for i := 0; i < 45; i++ {
			time.Sleep(time.Second)
			b, e = call("resources", "GET", nil, "")
			if e != nil {
				continue
			}
			var result struct {
				Recovery *config.Recovery `json:"recovery"`
			}
			if json.Unmarshal(b, &result) != nil || result.Recovery == nil {
				continue
			}
			s := result.Recovery
			if s.ID == op.ID && s.Status != "pending" {
				fmt.Printf(tr("cli.manage.recoveryResult"), s.Status, s.Backup)
				if s.Status != "completed" {
					return fmt.Errorf("%s", s.Error)
				}
				return nil
			}
		}
		return fmt.Errorf("%s", tr("cli.manage.unconfirmed"))
	}
	return nil
}
