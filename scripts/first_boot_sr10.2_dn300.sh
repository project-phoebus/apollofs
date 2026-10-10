#!/usr/bin/env bash
# First boot of the SR10.2 DN300 volume built by build_sr10.2_dn300.sh: creates
# the node's registry, location-broker database and daemon markers with the
# SR10.2 tools, on the node itself, under MAME (the original SR10.2 kernel
# booted by the kernel repo's harness, domain_os/tools/mame_boot_direct.py).
#
# The SR10.2 tapes ship no registry: rgy_create(8) builds one on a running
# node ("It ordinarily should be run only once at a site"), and the daemons
# /etc/rc starts are enabled by stub files in /etc/daemons (`node_data/etc/
# daemons/README: llbd, glbd - "at least one node per network", rgyd -
# "registry sites only.  Requires llbd").  At the Phase II `)' prompt this
# script types:
#
#   sh                                    single-process shell; it asks to log in
#   user.none.none, empty password        the only login without a registry
#                                         (SR10.1 changes 5.2.5, case 3)
#   /install/tools/rgy_create             master registry in /sys/registry/rgy_data
#   crf /etc/daemons/llbd                 /etc/rc starts llbd, glbd, rgyd
#   crf /etc/daemons/glbd
#   crf /etc/daemons/rgyd
#   /etc/server /etc/ncs/llbd             glbd needs a running llbd
#   /etc/ncs/glbd -create -first          first GLB replica: `node_data/glb.e,
#   ^C (after ~40 s)                      glb.p; it then serves, so quit it
#   ^D                                    leave the shell
#   shut                                  Phase II: flush and dismount cleanly
#
# Every reserved account rgy_create makes (root, user, admin, ...) has the
# password -apollo-.  The next boot (`go') starts llbd, glbd and rgyd.
#
# usage: first_boot_sr10.2_dn300.sh <image.awd> [work dir]
#   The image is modified in place: the kernel writes it, so afterwards it IS
#   the finished volume.  Needs the kernel repo (DOMAIN_OS, default
#   ~/src/domainos-archeology/source/domain_os) for the harness and MAME; the
#   SR10.2 kernel and link map come from SR102 (default
#   ~/src/domainos-archeology/sr10.2-install) and MAP.  Takes ~8 minutes.
set -euo pipefail
IMG="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
WORK="${2:-$(dirname "$IMG")/first_boot}"
DOMAIN_OS="${DOMAIN_OS:-$HOME/src/domainos-archeology/source/domain_os}"
SR102="${SR102:-$HOME/src/domainos-archeology/sr10.2-install}"
MAP="${MAP:-$HOME/src/domainos-archeology/sau2-maps/domain_os.10.2.map}"
mkdir -p "$WORK"
WORK="$(cd "$WORK" && pwd)"

cd "$DOMAIN_OS"
DOS_ROM_FRAMES=100000 python3 tools/mame_boot_direct.py \
    --image "$SR102/install/ri.apollo.os.v.10.2/sau2/domain_os" --map "$MAP" \
    --disk "$IMG" --work-dir "$WORK" --clean-volume --last-valid 0x4F900000 \
    --frames 30000 --stuck-frames 3000 --type-inject --finish-on 'Shutdown successful' \
    --expect ')'          --type 'sh\r' \
    --expect 'login: '    --type 'user.none.none\r' \
    --expect 'Password: ' --type '\r' \
    --expect '$ '         --type '/install/tools/rgy_create\r' \
    --expect '$ '         --type 'crf /etc/daemons/llbd\r' \
    --expect '$ '         --type 'crf /etc/daemons/glbd\r' \
    --expect '$ '         --type 'crf /etc/daemons/rgyd\r' \
    --expect '$ '         --type '/etc/server /etc/ncs/llbd\r' \
    --expect '$ '         --type '/etc/ncs/glbd -create -first\r' \
    --wait 2400           --type '\x03' \
    --expect '$ '         --type 'catf /sys/node_data/system_logs/glb_log\r' \
    --expect '$ '         --type '\x04' \
    --expect ')'          --type 'shut\r' \
    > "$WORK/harness.out" 2>&1 || true

# the console: the PROM output routine's characters (d1 at its breakpoint)
python3 - "$WORK/error.log" > "$WORK/console.txt" <<'PY'
import re, sys
s = ''
for l in open(sys.argv[1], errors='replace'):
    if 'EXC handler=00000818' in l:
        s += chr(int(re.search(r'd1=([0-9A-F]+)', l).group(1), 16) & 0x7f)
print(s)
PY
sed -n '/^)sh/,$p' "$WORK/console.txt"
grep -q 'object glb created' "$WORK/console.txt" || { echo "first boot: glbd did not create its database" >&2; exit 1; }
grep -q 'Shutdown successful' "$WORK/console.txt" || { echo "first boot: no clean shutdown" >&2; exit 1; }
echo "first boot done: $IMG"
