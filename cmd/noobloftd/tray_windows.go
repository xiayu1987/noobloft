// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassExW       = user32.NewProc("RegisterClassExW")
	pCreateWindowExW        = user32.NewProc("CreateWindowExW")
	pDefWindowProcW         = user32.NewProc("DefWindowProcW")
	pShowWindow             = user32.NewProc("ShowWindow")
	pGetMessageW            = user32.NewProc("GetMessageW")
	pTranslateMessage       = user32.NewProc("TranslateMessage")
	pDispatchMessageW       = user32.NewProc("DispatchMessageW")
	pIsDialogMessageW       = user32.NewProc("IsDialogMessageW")
	pPostQuitMessage        = user32.NewProc("PostQuitMessage")
	pPostMessageW           = user32.NewProc("PostMessageW")
	pSendMessageW           = user32.NewProc("SendMessageW")
	pDestroyWindow          = user32.NewProc("DestroyWindow")
	pSetWindowTextW         = user32.NewProc("SetWindowTextW")
	pLoadIconW              = user32.NewProc("LoadIconW")
	pLoadCursorW            = user32.NewProc("LoadCursorW")
	pCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	pAppendMenuW            = user32.NewProc("AppendMenuW")
	pTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	pDestroyMenu            = user32.NewProc("DestroyMenu")
	pSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	pGetCursorPos           = user32.NewProc("GetCursorPos")
	pSetTimer               = user32.NewProc("SetTimer")
	pFindWindowW            = user32.NewProc("FindWindowW")
	pMessageBoxW            = user32.NewProc("MessageBoxW")
	pRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	pEnableWindow           = user32.NewProc("EnableWindow")
	pIsIconic               = user32.NewProc("IsIconic")
	pSetProcessDPIAware     = user32.NewProc("SetProcessDPIAware")
	pGetDC                  = user32.NewProc("GetDC")
	pReleaseDC              = user32.NewProc("ReleaseDC")
	pGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	pSetBkColor             = gdi32.NewProc("SetBkColor")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pSetBkMode              = gdi32.NewProc("SetBkMode")
	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pCreateFontW            = gdi32.NewProc("CreateFontW")
	pGetDeviceCaps          = gdi32.NewProc("GetDeviceCaps")
	pShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	pShellExecuteW          = shell32.NewProc("ShellExecuteW")
	pGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmNull           = 0x0000
	wmDestroy        = 0x0002
	wmSize           = 0x0005
	wmClose          = 0x0010
	wmSetFont        = 0x0030
	wmCtlColorEdit   = 0x0133
	wmCtlColorStatic = 0x0138
	wmCommand        = 0x0111
	wmTimer          = 0x0113
	wmLButtonUp      = 0x0202
	wmLButtonDbl     = 0x0203
	wmRButtonUp      = 0x0205
	emGetSel         = 0x00B0
	emSetSel         = 0x00B1
	emLineScroll     = 0x00B6
	emGetFirstLine   = 0x00CE
	emSetMargins     = 0x00D3
	wmApp            = 0x8000
	wmTrayCallback   = wmApp + 1
	wmNodeChanged    = wmApp + 2
	wmShowRequest    = wmApp + 3
	wmNodeStopped    = wmApp + 4
	wmStopRequest    = wmApp + 5

	nimAdd     = 0
	nimModify  = 1
	nimDelete  = 2
	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4
	nifInfo    = 0x10

	swHide    = 0
	swShow    = 5
	swRestore = 9

	idStatus      = 100
	idConsole     = 201
	idDir         = 202
	idHide        = 203
	idQuit        = 204
	idMenuShow    = 301
	idMenuConsole = 302
	idMenuQuit    = 303

	trayClass = "NoobloftTrayWindow"
)

type wndClassEx struct {
	CbSize     uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	Private uint32
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             windows.Handle
	Tip              [128]uint16
	State, StateMask uint32
	Info             [256]uint16
	Version          uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	GUID             windows.GUID
	BalloonIcon      windows.Handle
}

func u16(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func copyU16(dst []uint16, s string) {
	src, _ := windows.UTF16FromString(s)
	if len(src) > len(dst) {
		src = append(src[:len(dst)-1], 0)
	}
	copy(dst, src)
}

type trayApp struct {
	dir, title, logPath     string
	lastStatus              string
	hwnd, status            uintptr
	phaseLabel              uintptr
	canvasBrush, whiteBrush uintptr
	font, headingFont       uintptr
	btnConsole              uintptr
	icon                    windows.Handle
	taskbarCreated          uint32
	cancel                  context.CancelFunc
	done                    chan struct{}
	quitting                bool
	consoleURL              string

	mu      sync.Mutex
	phase   string
	nodeErr error
	rt      *runtime
	started time.Time
}

var app *trayApp

func trayFatal(err error) {
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(u16(tr("tray.startFailed")+trErr(err)))), uintptr(unsafe.Pointer(u16("Noobloft"))), 0x10|0x10000)
}

func cmdTray(args []string) error {
	fs := flag.NewFlagSet("tray", flag.ContinueOnError)
	dir, err := resolveDir(fs, args)
	if err != nil {
		return err
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return err
	}
	goruntime.LockOSThread()
	title := "Noobloft Node - " + dir

	sum := sha256.Sum256([]byte(strings.ToLower(dir)))
	mutex, err := windows.CreateMutex(nil, false, u16(`Local\NoobloftTray-`+hex.EncodeToString(sum[:8])))
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(u16(trayClass))), uintptr(unsafe.Pointer(u16(title)))); h != 0 {
			pPostMessageW.Call(h, wmShowRequest, 0, 0)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf(tr("tray.lockFailed"), err)
	}
	defer windows.CloseHandle(mutex)

	logPath := filepath.Join(dir, "node.log")
	_ = os.Rename(logPath, logPath+".1")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	if trayBuild != "1" {
		kernel32.NewProc("FreeConsole").Call()
	}
	os.Stdout, os.Stderr = logFile, logFile
	log.SetOutput(logFile)
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(logFile.Fd()))
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(logFile.Fd()))

	app = &trayApp{dir: dir, title: title, logPath: logPath, phase: "starting", done: make(chan struct{})}
	if err := app.createWindow(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	app.cancel = cancel
	go func() {
		defer close(app.done)
		err := runNode(ctx, dir, logFile, nodeHooks{
			Ready: func(rt *runtime) {
				app.mu.Lock()
				app.rt, app.phase, app.started = rt, "running", time.Now()
				app.mu.Unlock()
				pPostMessageW.Call(app.hwnd, wmNodeChanged, 0, 0)
			},
			Stopping: func() {
				app.mu.Lock()
				app.rt = nil
				if app.phase != "stopping" {
					app.phase = "starting"
				}
				app.mu.Unlock()
				pPostMessageW.Call(app.hwnd, wmNodeChanged, 0, 0)
			},
		})
		app.mu.Lock()
		app.rt, app.phase, app.nodeErr = nil, "stopped", err
		app.mu.Unlock()
		if err != nil {
			fmt.Fprintf(logFile, tr("tray.nodeExited"), err)
		}
		pPostMessageW.Call(app.hwnd, wmNodeChanged, 0, 0)
	}()

	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if d, _, _ := pIsDialogMessageW.Call(app.hwnd, uintptr(unsafe.Pointer(&m))); d != 0 {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	cancel()
	if !app.quitting {
		select {
		case <-app.done:
		case <-time.After(15 * time.Second):
		}
	}
	return nil
}

func (a *trayApp) createWindow() error {
	pSetProcessDPIAware.Call()
	hdc, _, _ := pGetDC.Call(0)
	dpi, _, _ := pGetDeviceCaps.Call(hdc, 90)
	pReleaseDC.Call(0, hdc)
	if dpi == 0 {
		dpi = 96
	}
	px := func(v int) uintptr { return uintptr(v * int(dpi) / 96) }

	instance, _, _ := pGetModuleHandleW.Call(0)
	inst := windows.Handle(instance)
	icon, _, _ := pLoadIconW.Call(0, 32512)
	cursor, _, _ := pLoadCursorW.Call(0, 32512)
	a.icon = windows.Handle(icon)
	a.canvasBrush, _, _ = pCreateSolidBrush.Call(0x00F5F4F2)
	a.whiteBrush, _, _ = pCreateSolidBrush.Call(0x00FFFFFF)
	wc := wndClassEx{WndProc: windows.NewCallback(wndProc), Instance: inst, Icon: a.icon, IconSm: a.icon, Cursor: windows.Handle(cursor), Background: windows.Handle(a.canvasBrush), ClassName: u16(trayClass)}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf(tr("tray.registerFailed"), err)
	}
	tc, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(u16("TaskbarCreated"))))
	a.taskbarCreated = uint32(tc)

	const style = 0x00C00000 | 0x00080000 | 0x00020000
	w, h := px(696), px(500)
	sw, _, _ := pGetSystemMetrics.Call(0)
	sh, _, _ := pGetSystemMetrics.Call(1)
	hwnd, _, err := pCreateWindowExW.Call(0x00010000, uintptr(unsafe.Pointer(u16(trayClass))), uintptr(unsafe.Pointer(u16(a.title))), style,
		(sw-w)/2, (sh-h)/2, w, h, 0, 0, uintptr(inst), 0)
	if hwnd == 0 {
		return fmt.Errorf(tr("tray.createFailed"), err)
	}
	a.hwnd = hwnd
	height := -int32(px(15))
	a.font, _, _ = pCreateFontW.Call(uintptr(height), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16("Microsoft YaHei UI"))))
	a.headingFont, _, _ = pCreateFontW.Call(uintptr(-int32(px(24))), 0, 0, 0, 600, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16("Microsoft YaHei UI"))))
	child := func(class, text string, style, x, y, cw, ch uintptr, id int) uintptr {
		c, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))), 0x40000000|0x10000000|style, x, y, cw, ch, hwnd, uintptr(id), uintptr(inst), 0)
		pSendMessageW.Call(c, wmSetFont, a.font, 1)
		return c
	}
	heading := child("STATIC", "Noobloft", 0, px(24), px(20), px(400), px(36), 0)
	pSendMessageW.Call(heading, wmSetFont, a.headingFont, 1)
	a.phaseLabel = child("STATIC", "", 0, px(24), px(64), px(632), px(27), 0)
	a.status = child("EDIT", "", 0x4|0x40|0x800|0x00200000|0x00800000, px(24), px(108), px(632), px(282), idStatus)
	pSendMessageW.Call(a.status, emSetMargins, 3, px(12)|(px(12)<<16))
	const tab = 0x00010000
	a.btnConsole = child("BUTTON", tr("tray.openConsole"), tab|1, px(24), px(410), px(146), px(40), idConsole)
	child("BUTTON", tr("tray.openDirectory"), tab, px(180), px(410), px(146), px(40), idDir)
	child("BUTTON", tr("tray.hide"), tab, px(336), px(410), px(146), px(40), idHide)
	child("BUTTON", tr("tray.stopExit"), tab, px(502), px(410), px(154), px(40), idQuit)
	a.addIcon(true)
	a.refresh()
	pSetTimer.Call(hwnd, 1, 2000, 0)
	pShowWindow.Call(hwnd, swHide)
	return nil
}

func (a *trayApp) iconData() *notifyIconData {
	d := &notifyIconData{HWnd: a.hwnd, ID: 1, Flags: nifMessage | nifIcon | nifTip, CallbackMessage: wmTrayCallback, Icon: a.icon}
	d.CbSize = uint32(unsafe.Sizeof(*d))
	a.mu.Lock()
	copyU16(d.Tip[:], tr("tray.tooltipPrefix")+tr("tray.phase."+a.phase))
	a.mu.Unlock()
	return d
}

func (a *trayApp) addIcon(balloon bool) {
	d := a.iconData()
	if balloon {
		d.Flags |= nifInfo
		d.InfoFlags = 1
		copyU16(d.InfoTitle[:], tr("tray.backgroundTitle"))
		copyU16(d.Info[:], tr("tray.backgroundHint"))
	}
	pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(d)))
}

func (a *trayApp) balloon(title, text string) {
	d := a.iconData()
	d.Flags |= nifInfo
	d.InfoFlags = 2
	copyU16(d.InfoTitle[:], title)
	copyU16(d.Info[:], text)
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(d)))
}

func consoleURL(bind string) string {
	return localGatewayBase(bind) + "/"
}

func onOff(v bool) string {
	if v {
		return tr("tray.on")
	}
	return tr("common.close")
}

func (a *trayApp) statusText() (string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var b strings.Builder
	url := ""
	switch {
	case a.rt != nil:
		fmt.Fprintf(&b, tr("tray.status.nodeId"), a.rt.nd.ID())
		cfg := a.rt.cfg
		if cfg.Gateway.Enabled {
			url = consoleURL(cfg.Gateway.BindAddr)
			fmt.Fprintf(&b, tr("tray.status.console"), url)
		} else {
			b.WriteString(tr("tray.status.gatewayDisabled"))
		}
		fmt.Fprintf(&b, tr("tray.status.peers"), len(a.rt.nd.Host().Network().Peers()))
		fmt.Fprintf(&b, tr("tray.status.services"), cfg.Network.Mode, onOff(cfg.Provider.Enabled), onOff(cfg.Relay.Enabled))
		for _, addr := range a.rt.nd.Addrs() {
			fmt.Fprintf(&b, tr("tray.status.listening"), addr)
		}
	case a.phase == "stopped":
		b.WriteString(tr("tray.status.stopped"))
		if a.nodeErr != nil {
			fmt.Fprintf(&b, tr("tray.status.reason"), a.nodeErr)
		}
		b.WriteString(tr("tray.status.fixHint"))
	default:
		fmt.Fprintf(&b, tr("tray.status.phase"), tr("tray.phase."+a.phase))
	}
	fmt.Fprintf(&b, tr("tray.status.paths"), a.dir, a.logPath)
	b.WriteString(tr("tray.status.closeHint"))
	return b.String(), url
}

func (a *trayApp) refresh() {
	text, url := a.statusText()
	a.mu.Lock()
	header := tr("tray.phase." + a.phase)
	if a.phase == "running" {
		header = strings.TrimSpace(fmt.Sprintf(tr("tray.status.running"), time.Since(a.started).Round(time.Second)))
	}
	a.mu.Unlock()
	pSetWindowTextW.Call(a.phaseLabel, uintptr(unsafe.Pointer(u16(header))))
	a.consoleURL = url
	enabled := uintptr(0)
	if url != "" {
		enabled = 1
	}
	pEnableWindow.Call(a.btnConsole, enabled)
	if a.lastStatus != text {
		var selectionStart, selectionEnd uint32
		pSendMessageW.Call(a.status, emGetSel, uintptr(unsafe.Pointer(&selectionStart)), uintptr(unsafe.Pointer(&selectionEnd)))
		firstLine, _, _ := pSendMessageW.Call(a.status, emGetFirstLine, 0, 0)
		pSetWindowTextW.Call(a.status, uintptr(unsafe.Pointer(u16(text))))
		a.lastStatus = text
		pSendMessageW.Call(a.status, emSetSel, uintptr(selectionStart), uintptr(selectionEnd))
		currentLine, _, _ := pSendMessageW.Call(a.status, emGetFirstLine, 0, 0)
		pSendMessageW.Call(a.status, emLineScroll, 0, uintptr(int32(firstLine)-int32(currentLine)))
	}
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(a.iconData())))
}

func (a *trayApp) show() {
	if r, _, _ := pIsIconic.Call(a.hwnd); r != 0 {
		pShowWindow.Call(a.hwnd, swRestore)
	}
	pShowWindow.Call(a.hwnd, swShow)
	pSetForegroundWindow.Call(a.hwnd)
	a.refresh()
}

func shellOpen(target string) {
	if target != "" {
		pShellExecuteW.Call(0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(target))), 0, 0, 1)
	}
}

func (a *trayApp) quit(confirm bool) {
	if a.quitting {
		return
	}
	a.mu.Lock()
	running := a.phase != "stopped"
	a.mu.Unlock()
	if running && confirm {
		r, _, _ := pMessageBoxW.Call(a.hwnd, uintptr(unsafe.Pointer(u16(tr("tray.quitConfirm")))), uintptr(unsafe.Pointer(u16("Noobloft"))), 0x1|0x20|0x10000)
		if r != 1 {
			return
		}
	}
	a.quitting = true
	a.mu.Lock()
	if a.phase != "stopped" {
		a.phase = "stopping"
	}
	a.mu.Unlock()
	a.refresh()
	a.cancel()
	go func() {
		select {
		case <-a.done:
		case <-time.After(15 * time.Second):
		}
		pPostMessageW.Call(a.hwnd, wmNodeStopped, 0, 0)
	}()
}

func (a *trayApp) menu() {
	m, _, _ := pCreatePopupMenu.Call()
	pAppendMenuW.Call(m, 0, idMenuShow, uintptr(unsafe.Pointer(u16(tr("tray.showStatus")))))
	flags := uintptr(0)
	if a.consoleURL == "" {
		flags = 0x1
	}
	pAppendMenuW.Call(m, flags, idMenuConsole, uintptr(unsafe.Pointer(u16(tr("tray.openConsole")))))
	pAppendMenuW.Call(m, 0x800, 0, 0)
	pAppendMenuW.Call(m, 0, idMenuQuit, uintptr(unsafe.Pointer(u16(tr("tray.exit")))))
	var pt struct{ X, Y int32 }
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(a.hwnd)
	pTrackPopupMenu.Call(m, 0x0020, uintptr(pt.X), uintptr(pt.Y), 0, a.hwnd, 0)
	pPostMessageW.Call(a.hwnd, wmNull, 0, 0)
	pDestroyMenu.Call(m)
}

func wndProc(hwnd, message, wparam, lparam uintptr) uintptr {
	a := app
	if a == nil || a.hwnd == 0 {
		r, _, _ := pDefWindowProcW.Call(hwnd, message, wparam, lparam)
		return r
	}
	switch uint32(message) {
	case wmCtlColorStatic, wmCtlColorEdit:
		pSetTextColor.Call(wparam, 0x002D2925)
		if lparam == a.status {
			pSetBkMode.Call(wparam, 2)
			pSetBkColor.Call(wparam, 0x00FFFFFF)
			return a.whiteBrush
		}
		pSetBkMode.Call(wparam, 1)
		pSetBkColor.Call(wparam, 0x00F5F4F2)
		if lparam == a.phaseLabel {
			a.mu.Lock()
			phase := a.phase
			a.mu.Unlock()
			color := uintptr(0x000078A0)
			switch phase {
			case "running":
				color = 0x00497227
			case "stopped":
				color = 0x00394AC4
			}
			pSetTextColor.Call(wparam, color)
		}
		return a.canvasBrush
	case wmClose:
		pShowWindow.Call(hwnd, swHide)
		return 0
	case wmSize:
		if wparam == 1 {
			pShowWindow.Call(hwnd, swHide)
		}
	case wmTimer:
		a.refresh()
		return 0
	case wmTrayCallback:
		switch uint32(lparam & 0xFFFF) {
		case wmLButtonUp, wmLButtonDbl:
			a.show()
		case wmRButtonUp:
			a.menu()
		}
		return 0
	case wmNodeChanged:
		a.refresh()
		a.mu.Lock()
		failed := a.phase == "stopped" && a.nodeErr != nil && !a.quitting
		a.mu.Unlock()
		if failed {
			a.show()
			a.balloon(tr("tray.stoppedTitle"), tr("tray.stoppedHint"))
		}
		return 0
	case wmShowRequest:
		a.show()
		return 0
	case wmNodeStopped:
		pDestroyWindow.Call(hwnd)
		return 0
	case wmStopRequest:
		a.quit(false)
		return 0
	case wmCommand:
		switch int(wparam & 0xFFFF) {
		case idConsole, idMenuConsole:
			shellOpen(a.consoleURL)
		case idDir:
			shellOpen(a.dir)
		case idHide:
			pShowWindow.Call(hwnd, swHide)
		case idQuit, idMenuQuit:
			a.quit(true)
		case idMenuShow:
			a.show()
		}
		return 0
	case wmDestroy:
		pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(a.iconData())))
		for _, object := range []uintptr{a.font, a.headingFont, a.whiteBrush, a.canvasBrush} {
			if object != 0 {
				pDeleteObject.Call(object)
			}
		}
		pPostQuitMessage.Call(0)
		return 0
	default:
		if a.taskbarCreated != 0 && uint32(message) == a.taskbarCreated {
			a.addIcon(false)
			return 0
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, message, wparam, lparam)
	return r
}
