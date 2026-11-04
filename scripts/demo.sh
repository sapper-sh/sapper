#!/usr/bin/env bash
# Recorded with: asciinema rec --cols 82 --rows 60 -c scripts/demo.sh demo.cast
# Rendered with: python3 scripts/termshot.py gif demo.cast docs/img/demo.gif (needs pyte and Pillow)
# Paths below point at a scratch fortress outside the repository; adjust before running.
unset NO_COLOR; export PATH=/tmp/sapperbin:$PATH SAPPER_STATE_DIR=/workspace/sapper-demo/state TERM=xterm-256color SAPPER_NO_ANIM=1
cd /workspace/sapper-demo/fort
p() { printf '\033[38;2;120;225;255m$\033[0m %s\n' "$*"; sleep 0.6; eval "$@"; sleep 1.5; }
p 'export SAPPER_RESTIC_PASSWORD=demo-only'
p sapper backup
p sapper breach
sleep 2
printf '\033[2m# simulate silent corruption: change one recorded checksum\033[0m\n'; sleep 0.8
python3 - <<'PY'
import glob,json
f=sorted(glob.glob('/workspace/sapper-demo/state/fortresses/*/manifests/*.json'))[-1]; m=json.load(open(f))
e=next(v for k,v in sorted(m['entries'].items()) if k.endswith('db.sqlite3')); e['sha256']='0'*64; json.dump(m,open(f,'w'))
PY
p 'sapper breach; echo "exit code: $?"'
sleep 2
p 'sapper breach --brief'
p sapper reports
p sapper reports verify
sleep 2
