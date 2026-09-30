#!/usr/bin/env sh
# Copyright (c) 2026 xiayu
# Contact: 126240622+xiayu1987@users.noreply.github.com
# SPDX-License-Identifier: MIT

set -eu
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
node_dir=
while [ $# -gt 0 ]; do
    case $1 in
        --dir) [ $# -ge 2 ] || { echo '--dir requires a path' >&2; exit 2; }; node_dir=$2; shift 2 ;;
        -h|--help) echo 'Usage: sh scripts/stop.sh [--dir PATH] (default: caller home/.noobloft; stop matching nodes and service; requires Python 3.9+ and Linux 5.3+)'; exit 0 ;;
        *) echo "Unknown argument: $1" >&2; exit 2 ;;
    esac
done
[ -n "$node_dir" ] || node_dir=$(getent passwd "${SUDO_USER:-$(id -un)}" | cut -d: -f6)/.noobloft
case $node_dir in /*) ;; *) node_dir=$(pwd)/$node_dir ;; esac
command -v python3 >/dev/null 2>&1 || { echo 'Python 3 is required to safely identify command-line nodes.' >&2; exit 1; }
if [ "$(id -u)" -ne 0 ]; then exec sudo -- sh "$0" --dir "$node_dir"; fi
python3 - "$repo_dir" "$node_dir" <<'PY'
import os
import pathlib
import select
import signal
import shutil
import subprocess
import sys

repo, directory = map(os.path.realpath, sys.argv[1:])
allowed = {os.path.join(repo, 'bin/noobloftd'), '/usr/local/lib/noobloft/noobloftd'}
if not hasattr(os, 'pidfd_open') or not hasattr(signal, 'pidfd_send_signal'):
    sys.exit('Python 3 with Linux pidfd support is required for safe process shutdown.')
probe = os.pidfd_open(os.getpid())
os.close(probe)
service_pid = ''
if shutil.which('systemctl') and os.path.isdir('/run/systemd/system'):
    unit = pathlib.Path('/etc/systemd/system/noobloft.service')
    prefix = 'ExecStart=/usr/local/lib/noobloft/noobloftd run -dir '
    if unit.exists():
        dirs = [line[len(prefix):] for line in unit.read_text().splitlines() if line.startswith(prefix)]
        if len(dirs) == 1 and os.path.realpath(dirs[0]) == directory:
            subprocess.run(['systemctl', 'stop', 'noobloft.service'], check=True)
    service_pid = subprocess.check_output(['systemctl', 'show', '-p', 'MainPID', '--value', 'noobloft.service'], text=True).strip()
for entry in pathlib.Path('/proc').iterdir():
    if not entry.name.isdigit():
        continue
    handle = None
    try:
        executable = os.readlink(entry / 'exe').removesuffix(' (deleted)')
        if executable not in allowed:
            continue
        handle = os.pidfd_open(int(entry.name))
        if os.readlink(entry / 'exe').removesuffix(' (deleted)') not in allowed:
            continue
        argv = (entry / 'cmdline').read_bytes().split(b'\0')[:-1]
        argv = [os.fsdecode(arg) for arg in argv]
        if len(argv) < 2 or argv[1] != 'run':
            continue
        args, target, valid = argv[2:], None, True
        while args:
            arg = args.pop(0)
            if arg in ('-dir', '--dir') and args:
                target = args.pop(0)
            elif arg.startswith(('-dir=', '--dir=')):
                target = arg.split('=', 1)[1]
            else:
                valid = False
                break
        if not valid:
            continue
        if target is None:
            env = (entry / 'environ').read_bytes().split(b'\0')
            home = next((os.fsdecode(x[5:]) for x in env if x.startswith(b'HOME=')), None)
            if not home:
                continue
            target = os.path.join(home, '.noobloft')
        elif not os.path.isabs(target):
            target = os.path.join(os.readlink(entry / 'cwd'), target)
        if os.path.realpath(target) != directory:
            continue
        print(f'Stopping command-line node PID {entry.name}: {directory}', flush=True)
        if entry.name == service_pid:
            subprocess.run(['systemctl', 'stop', 'noobloft.service'], check=True)
            state = subprocess.check_output(['systemctl', 'show', '-p', 'ActiveState', '--value', 'noobloft.service'], text=True).strip()
            if state != 'inactive':
                raise RuntimeError(f'Service did not stop: {state}')
            continue
        signal.pidfd_send_signal(handle, signal.SIGTERM)
        poller = select.poll()
        poller.register(handle, select.POLLIN)
        if not poller.poll(30000):
            print('Shutdown timed out; terminating node (buffered data may be lost).', flush=True)
            signal.pidfd_send_signal(handle, signal.SIGKILL)
            if not poller.poll(5000):
                raise RuntimeError(f'Node PID {entry.name} did not exit')
    except (FileNotFoundError, ProcessLookupError):
        pass
    finally:
        if handle is not None:
            os.close(handle)
print(f'Stopped or already stopped: {directory}')
PY
