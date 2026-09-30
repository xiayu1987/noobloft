// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xiayu1987/noobloft/internal/audit"
	"github.com/xiayu1987/noobloft/internal/config"
	"github.com/xiayu1987/noobloft/internal/controlplane/gateway"
	"github.com/xiayu1987/noobloft/internal/network/discovery"
	"github.com/xiayu1987/noobloft/internal/network/node"
	"github.com/xiayu1987/noobloft/internal/security/identity"
	"github.com/xiayu1987/noobloft/internal/security/publisher"
	"github.com/xiayu1987/noobloft/internal/serving/backend"
	"github.com/xiayu1987/noobloft/internal/serving/capability"
	"github.com/xiayu1987/noobloft/internal/serving/inference"
	"github.com/xiayu1987/noobloft/internal/serving/reputation"
	"github.com/xiayu1987/noobloft/internal/serving/router"
)

var trayBuild string

func main() {
	args, language, languageErr := languageArgs(os.Args[1:])
	if languageErr != nil {
		if trayBuild == "1" {
			trayFatal(languageErr)
		} else {
			fmt.Fprintln(os.Stderr, languageErr)
		}
		os.Exit(2)
	}
	cliLanguage = language
	os.Args = append([]string{os.Args[0]}, args...)
	if trayBuild == "1" {
		args := os.Args[1:]
		if len(args) > 0 && args[0] == "tray" {
			args = args[1:]
		}
		if err := cmdTray(args); err != nil {
			trayFatal(err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "manage":
		err = cmdManage(os.Args[2:])
	case "publisher":
		err = cmdPublisher(os.Args[2:])
	case "init":
		err = cmdInit(os.Args[2:])
	case "invite":
		err = cmdInvite(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "tray":
		err = cmdTray(os.Args[2:])
	case "info":
		err = cmdInfo(os.Args[2:])
	case "peers":
		err = cmdPeers(os.Args[2:])
	case "models":
		err = cmdModels(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, tr("cli.unknownCommand"), os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, tr("cli.error"), trErr(err))
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(tr("cli.usage"))
}

func resolveDir(fs *flag.FlagSet, args []string) (string, error) {
	dir := fs.String("dir", "", tr("cli.directory"))
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if *dir != "" {
		return *dir, nil
	}
	return config.DefaultDir()
}

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	mode := fs.String("mode", "private", tr("cli.init.mode"))
	joinKey := fs.String("join", "", tr("cli.init.join"))
	profile := fs.String("profile", "lan", tr("cli.init.profile"))
	announce := fs.String("announce", "", tr("cli.init.announce"))
	dir, err := resolveDir(fs, args)
	if err != nil {
		return err
	}

	if err := checkInitTarget(dir); err != nil {
		return err
	}

	cfg := config.Default()
	cfg.Network.Mode = *mode
	if *mode == "shared" && *joinKey != "" {
		return fmt.Errorf("shared mode does not accept -join")
	}
	if err := applyProfile(cfg, *profile, *announce); err != nil {
		return err
	}
	tokenRaw := make([]byte, 32)
	if _, err := rand.Read(tokenRaw); err != nil {
		return fmt.Errorf(tr("cli.init.tokenFailed"), err)
	}
	cfg.Gateway.AuthToken = base64.RawURLEncoding.EncodeToString(tokenRaw)
	if err := cfg.Validate(); err != nil {
		return err
	}

	if err := config.Save(dir, cfg); err != nil {
		return err
	}

	priv, err := identity.LoadOrCreateIdentity(dir)
	if err != nil {
		return err
	}

	if *mode == "shared" {
		fmt.Println(tr("cli.init.shared"))
	} else if *joinKey != "" {
		raw, err := os.ReadFile(*joinKey)
		if err != nil {
			return fmt.Errorf(tr("cli.readFailed"), *joinKey, err)
		}
		target := filepath.Join(dir, config.SwarmKeyFileName)
		if err := os.WriteFile(target, raw, 0o600); err != nil {
			return fmt.Errorf(tr("cli.init.writeKeyFailed"), err)
		}
		if _, err := identity.LoadSwarmKey(dir); err != nil {
			return fmt.Errorf(tr("cli.init.invalidKey"), err)
		}
		fmt.Printf(tr("cli.init.keyImported"), target)
	} else {
		path, err := identity.GenerateSwarmKey(dir)
		if err != nil {
			return err
		}
		fmt.Printf(tr("cli.init.keyCreated"), path)
		fmt.Println(tr("cli.init.copyKey"))
		fmt.Println(tr("cli.init.joinExample"))
	}

	pid, err := peerIDFromPriv(priv)
	if err != nil {
		return err
	}
	fmt.Printf(tr("cli.init.directory"), dir)
	fmt.Printf(tr("cli.init.nodeId"), pid)
	fmt.Printf(tr("cli.init.gateway"), cfg.Gateway.BindAddr)
	fmt.Printf(tr("cli.init.token"), cfg.Gateway.AuthToken)
	fmt.Println(tr("cli.init.next"))
	fmt.Println(tr("cli.init.startHint"))
	return nil
}

type runtime struct {
	cfg        *config.Config
	dir        string
	nd         *node.Node
	disc       *discovery.Service
	rtr        *router.Router
	auditor    *audit.Logger
	reputation *reputation.Store
	srv        *inference.Server
}

func buildRuntime(ctx context.Context, dir string) (*runtime, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}
	priv, err := identity.LoadOrCreateIdentity(dir)
	if err != nil {
		return nil, err
	}
	var psk []byte
	if cfg.Network.Mode == "private" {
		psk, err = identity.LoadSwarmKey(dir)
		if err != nil {
			return nil, err
		}
	}
	rt := &runtime{cfg: cfg, dir: dir}
	fail := func(err error) (*runtime, error) {
		_ = rt.close()
		return nil, err
	}
	auditor, err := audit.Open(dir, cfg.Policy.AuditEnabled)
	if err != nil {
		return nil, err
	}
	rt.auditor = auditor
	rep, err := reputation.Open(dir, cfg.Reputation.Enabled)
	if err != nil {
		return fail(err)
	}
	rt.reputation = rep

	adapters := make([]backend.Adapter, 0, len(cfg.Backends))
	for _, b := range cfg.Backends {
		if !b.Enabled {
			continue
		}
		a, err := backend.New(backend.Spec{
			Name:    b.Name,
			Kind:    b.Kind,
			BaseURL: b.BaseURL,
			APIKey:  b.APIKey,
		}, cfg.Policy.AllowExternalAPIForwarding)
		if err != nil {
			_ = auditor.Log(audit.Event{
				Type:    audit.EventPolicyDenied,
				Backend: b.Name,
				Reason:  err.Error(),
			})
			return fail(err)
		}
		adapters = append(adapters, a)
	}

	nd, err := node.New(ctx, node.Options{
		Cfg:     cfg,
		PrivKey: priv,
		PSK:     psk,
		Auditor: auditor,
	})
	if err != nil {
		return fail(err)
	}
	rt.nd = nd

	var contentRouter discovery.ContentRouter
	if d := nd.DHT(); d != nil {
		contentRouter = d
	}
	disc := discovery.New(nd.Host(), contentRouter)
	client := inference.NewClient(nd.Host(), auditor)
	rtr := router.New(adapters, disc, client, rep)
	rt.disc, rt.rtr = disc, rtr
	if err := rtr.ConfigurePublishers(nd.ID(), cfg.Network.Mode == "shared", cfg.Subscriptions); err != nil {
		return fail(err)
	}

	if cfg.Provider.Enabled {
		var auth *inference.Authorization
		if svc := cfg.Provider.Service; svc != nil {
			if err := svc.Verify(svc.Publisher, "service", time.Now()); err != nil {
				rt.close()
				return nil, err
			}
			path := cfg.Provider.RevocationsFile
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			rev, err := publisher.Load(path)
			if err != nil {
				rt.close()
				return nil, err
			}
			if err = rev.Verify(svc.Publisher, "revocations", time.Now()); err != nil {
				rt.close()
				return nil, err
			}
			auth = &inference.Authorization{Service: *svc, RevocationsFile: path}
			disc.PublisherService = svc
		}
		resolve := func(ctx context.Context, model string) (backend.Adapter, error) {
			if !providerAllows(cfg, model) {
				return nil, fmt.Errorf("model not permitted for remote serving")
			}
			return rtr.ResolveLocal(ctx, model)
		}
		srv, err := inference.NewServer(nd.Host(), resolve, auditor, cfg.Provider.MaxConcurrent, auth)
		if err != nil {
			_ = rt.close()
			return nil, err
		}
		rt.srv = srv

		disc.EnableSelfAnnouncement(priv, cfg.Provider.MaxConcurrent, func() []capability.Model {
			var infos []backend.ModelInfo
			for _, m := range rtr.LocalModelInfos(ctx) {
				if providerAllows(cfg, m.Name) {
					infos = append(infos, m)
				}
			}
			return capability.FromModelInfos(infos, nil)
		})
		if err := disc.RefreshAnnouncement(); err != nil {
			_ = rt.close()
			return nil, err
		}
	}

	return rt, nil
}

func (r *runtime) close() error {
	if r.srv != nil {
		r.srv.Close()
	}
	var firstErr error
	if r.reputation != nil {
		if err := r.reputation.Flush(); err != nil {
			firstErr = err
		}
	}
	if r.nd != nil {
		if err := r.nd.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if r.auditor != nil {
		if err := r.auditor.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	dir, err := resolveDir(fs, args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runNode(ctx, dir, os.Stdout, nodeHooks{})
}

type nodeHooks struct {
	Ready    func(*runtime)
	Stopping func()
}

func runNode(ctx context.Context, dir string, out io.Writer, hooks nodeHooks) error {
	for {
		restart := make(chan struct{}, 1)
		child, cancel := context.WithCancel(ctx)
		err := runNodeOnce(child, dir, out, nodeHooks{
			Ready: func(rt *runtime) {
				if e := config.CompleteRecovery(dir, rt.cfg); e != nil {
					fmt.Fprintf(out, tr("cli.recovery.recordFailed"), e)
				}
				if hooks.Ready != nil {
					hooks.Ready(rt)
				}
			},
			Stopping: hooks.Stopping,
		}, restart, cancel)
		cancel()
		if err != nil && ctx.Err() == nil {
			s, e := config.RecoveryState(dir)
			if e == nil && s != nil && s.Status == "pending" {
				if e = config.RollbackRecovery(dir, err); e != nil {
					return fmt.Errorf(tr("cli.recovery.rollbackFailed"), err, e)
				}
				fmt.Fprintf(out, tr("cli.recovery.rolledBack"), err)
				continue
			}
		}
		if err != nil || ctx.Err() != nil {
			return err
		}
		select {
		case <-restart:
			continue
		default:
			return nil
		}
	}
}

func runNodeOnce(ctx context.Context, dir string, out io.Writer, hooks nodeHooks, restart chan struct{}, cancel context.CancelFunc) error {
	rt, err := buildRuntime(ctx, dir)
	if err != nil {
		return err
	}
	defer func() {
		if hooks.Stopping != nil {
			hooks.Stopping()
		}
		_ = rt.close()
	}()

	fmt.Fprintf(out, tr("cli.run.started"), rt.nd.ID())
	for _, a := range rt.nd.Addrs() {
		fmt.Fprintf(out, tr("cli.run.listening"), a)
	}
	backends := rt.rtr.DescribeBackends()
	if backends == "" {
		backends = tr("common.none")
	}
	fmt.Fprintf(out, tr("cli.run.backend"), backends)
	fmt.Fprintf(out, "  provider: %v  relay: %v  DHT: %v  mDNS: %v\n",
		rt.cfg.Provider.Enabled, rt.cfg.Relay.Enabled,
		rt.cfg.Network.EnableDHT, rt.cfg.Network.EnableMDNS)

	if rt.cfg.Provider.Enabled {
		rt.disc.StartMaintenanceLoop(ctx, func() []string {
			var names []string
			if ann := rt.disc.Announcement(); ann != nil {
				for _, m := range ann.Models {
					names = append(names, m.Name)
				}
			}
			return names
		})
	}
	if rt.cfg.Reputation.ProbeEnabled {
		rt.rtr.StartProbeLoop(ctx,
			time.Duration(rt.cfg.Reputation.ProbeIntervalSec)*time.Second,
			time.Duration(rt.cfg.Reputation.ProbeCooldownSec)*time.Second,
		)
	}
	if rt.reputation.Enabled() {
		go flushReputationLoop(ctx, rt.reputation, 30*time.Second)
	}

	if rt.cfg.Gateway.Enabled {
		adminToken, err := gateway.ManagementToken(rt.dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, tr("cli.run.console"), rt.cfg.Gateway.BindAddr)
		gw, err := gateway.New(gateway.Options{
			Management: &gateway.ManagementOptions{Dir: rt.dir, Self: rt.nd.Host().ID().String(), Token: adminToken, Running: rt.cfg, Restart: func() bool {
				select {
				case restart <- struct{}{}:
					time.AfterFunc(300*time.Millisecond, cancel)
					return true
				default:
					return false
				}
			}},
			BindAddr:   rt.cfg.Gateway.BindAddr,
			Token:      rt.cfg.Gateway.AuthToken,
			Timeout:    time.Duration(rt.cfg.Gateway.RequestTimeoutSec) * time.Second,
			Router:     rt.rtr,
			Reputation: rt.reputation,
			Peers:      rt.disc,
		})
		if err != nil {
			return err
		}
		if err := gw.Start(); err != nil {
			return err
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = gw.Close(shutdownCtx)
		}()

		fmt.Fprintf(out, tr("cli.run.gateway"), rt.cfg.Gateway.BindAddr)
		if !gateway.IsLoopback(rt.cfg.Gateway.BindAddr) {
			fmt.Fprintln(out, tr("cli.run.exposedWarning"))
			fmt.Fprintln(out, tr("cli.run.exposedHint"))
		}
	}

	if hooks.Ready != nil {
		hooks.Ready(rt)
	}
	fmt.Fprintln(out, tr("cli.run.stopHint"))
	<-ctx.Done()
	fmt.Fprintln(out, tr("cli.run.stopping"))
	return nil
}

func flushReputationLoop(ctx context.Context, rep *reputation.Store, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := rep.Flush(); err != nil {
				fmt.Printf(tr("cli.run.ledgerFailed"), err)
			}
		}
	}
}

func cmdInfo(args []string) error {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	dir, err := resolveDir(fs, args)
	if err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	priv, err := identity.LoadOrCreateIdentity(dir)
	if err != nil {
		return err
	}
	if cfg.Network.Mode == "private" {
		if _, err := identity.LoadSwarmKey(dir); err != nil {
			return err
		}
	}
	pid, err := peerIDFromPriv(priv)
	if err != nil {
		return err
	}

	fmt.Printf(tr("cli.info.directory"), dir)
	fmt.Printf(tr("cli.info.nodeId"), pid)
	fmt.Printf(tr("cli.info.gateway"), cfg.Gateway.BindAddr)
	fmt.Printf("provider  : %v\n", cfg.Provider.Enabled)
	fmt.Printf("relay     : %v\n", cfg.Relay.Enabled)
	fmt.Printf(tr("cli.info.staticPeers"), cfg.Network.StaticPeers)
	fmt.Printf(tr("cli.info.backendTypes"), backend.Kinds())
	fmt.Println(tr("cli.info.listenHint"))
	fmt.Println(tr("cli.info.connectHint"))
	return nil
}

func cmdPeers(args []string) error {
	fs := flag.NewFlagSet("peers", flag.ContinueOnError)
	dir, err := resolveDir(fs, args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var resp gateway.AdminPeersResponse
	if err := adminGet(ctx, dir, "/admin/peers", &resp); err != nil {
		return err
	}

	if len(resp.Peers) == 0 {
		fmt.Println(tr("cli.peers.empty"))
		fmt.Println(tr("cli.peers.troubleshoot"))
		return nil
	}
	for _, p := range resp.Peers {
		for _, path := range p.ConnectionPaths {
			fmt.Printf(tr("cli.peers.connection"), p.ID, path)
		}
		if p.Error != "" {
			fmt.Printf(tr("cli.peers.queryFailed"), p.ID, p.Error)
			continue
		}
		fmt.Printf("- %s", p.ID)
		if p.PublisherID != "" {
			fmt.Printf(tr("cli.peers.publisher"), p.PublisherID, p.ServiceID)
		}
		if p.Reputation != nil {
			fmt.Printf(tr("cli.peers.reputation"),
				p.Reputation.Score, p.Reputation.Samples, p.Reputation.Trusted)
			if p.Reputation.LastLatencyMS > 0 {
				fmt.Printf(tr("cli.peers.latency"), p.Reputation.LastLatencyMS)
			}
		}
		if len(p.Models) == 0 {
			fmt.Println(tr("cli.peers.notServing"))
			continue
		}
		fmt.Printf(tr("cli.peers.concurrency"), p.MaxConcurrent)
		for _, m := range p.Models {
			fmt.Printf("    %s", m.Name)
			if m.ParameterSize != "" {
				fmt.Printf(tr("cli.models.parameters"), m.ParameterSize)
			}
			if m.Quantization != "" {
				fmt.Printf(tr("cli.models.quantization"), m.Quantization)
			}
			if m.License != "" {
				fmt.Printf(tr("cli.models.license"), m.License)
			}
			fmt.Println()
		}
	}
	return nil
}

func cmdModels(args []string) error {
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	dir, err := resolveDir(fs, args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var resp struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := adminGet(ctx, dir, "/v1/models", &resp); err != nil {
		return err
	}

	var local, remote []string
	for _, it := range resp.Data {
		if it.OwnedBy == "local" {
			local = append(local, it.ID)
			continue
		}
		remote = append(remote, it.ID)
	}

	fmt.Printf(tr("cli.models.local"), len(local))
	if len(local) == 0 {
		fmt.Println(tr("cli.models.noLocal"))
	}
	for _, m := range local {
		fmt.Printf("  %s\n", m)
	}

	fmt.Printf(tr("cli.models.remote"), len(remote))
	if len(remote) == 0 {
		fmt.Println(tr("cli.models.noRemote"))
	}
	for _, m := range remote {
		fmt.Printf("  %s\n", m)
	}
	return nil
}

func adminGet(ctx context.Context, dir, path string, out any) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if !cfg.Gateway.Enabled {
		return fmt.Errorf("%s", tr("cli.gatewayDisabled"))
	}

	url := localGatewayBase(cfg.Gateway.BindAddr) + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf(tr("cli.requestFailed"), err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Gateway.AuthToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf(tr("cli.connectFailed"), cfg.Gateway.BindAddr, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf(tr("cli.daemonError"), resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf(tr("cli.decodeFailed"), err)
	}
	return nil
}

func peerIDFromPriv(priv libp2pcrypto.PrivKey) (string, error) {
	id, err := peer.IDFromPublicKey(priv.GetPublic())
	if err != nil {
		return "", fmt.Errorf(tr("cli.peerIdFailed"), err)
	}
	return id.String(), nil
}
