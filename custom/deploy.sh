#!/usr/bin/env bash
#
# Build and deploy the custom firmware, then make sure the USB gadget comes
# back. If it does not, restore the stock app so the host keeps its keyboard
# and mouse.
#
#   custom/deploy.sh 10.1.1.150      test run (jetkvm_app_debug, gone after a reboot)
#   custom/deploy.sh -i 10.1.1.150   install permanently (replaces jetkvm_app, reboots)
#   custom/deploy.sh --recover 10.1.1.150        stop a test run and restart the installed app
#   custom/deploy.sh --restore-stock 10.1.1.150  put back the app saved before the first install
#
# The installed build reports the version "<upstream>+custom.<commit>"; semver
# ignores the "+..." part, so update checks treat it like the upstream version.
#
# Needs: docker (or podman), ssh access with developer mode enabled.

set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

INSTALL=false
RECOVER_ONLY=false
RESTORE_STOCK=false
while [[ $# -gt 1 ]]; do
    case "$1" in
        -i|--install) INSTALL=true ;;
        --recover) RECOVER_ONLY=true ;;
        --restore-stock) RESTORE_STOCK=true ;;
        *) echo "unknown option: $1"; exit 1 ;;
    esac
    shift
done
HOST="${1:?usage: custom/deploy.sh [-i|--recover|--restore-stock] <device-ip>}"

LOG="$(mktemp -t jetkvm-deploy.XXXXXX.log)"
SSH=(ssh -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "root@$HOST")

info() { echo -e "\033[1;34m▶ $*\033[0m"; }
ok()   { echo -e "\033[1;32m✓ $*\033[0m"; }
warn() { echo -e "\033[1;33m! $*\033[0m"; }

usb_state() { "${SSH[@]}" 'cat /sys/class/udc/*/state 2>/dev/null' 2>/dev/null || true; }

recover() {
    warn "restoring the stock app and USB gadget"
    "${SSH[@]}" 'G=/sys/kernel/config/usb_gadget/jetkvm
kill -9 $(pgrep jetkvm_app_debu) 2>/dev/null; sleep 2
rm -f $G/configs/c.1/ffs.touchscreen
umount /run/jetkvm/ffs-touchscreen 2>/dev/null
rmdir $G/functions/ffs.touchscreen 2>/dev/null
[ -e /sys/class/udc/ffb00000.usb ] || echo ffb00000.usb > /sys/bus/platform/drivers/dwc3/bind
pgrep -x jetkvm_app >/dev/null || {
  export LD_LIBRARY_PATH=/oem/usr/lib:$LD_LIBRARY_PATH
  setsid /userdata/jetkvm/bin/jetkvm_app > /userdata/jetkvm/last.log 2>&1 < /dev/null &
}
sleep 15
echo "app procs: $(pgrep jetkvm_app | wc -l), usb: $(cat /sys/class/udc/*/state)"'
}

restore_stock() {
    warn "restoring the app saved before the first custom install"
    "${SSH[@]}" 'B=/userdata/jetkvm/bin
[ -f $B/jetkvm_app.stock ] || { echo "no backup at $B/jetkvm_app.stock"; exit 1; }
rm -f /userdata/jetkvm/jetkvm_app.update
cp $B/jetkvm_app.stock /userdata/jetkvm/jetkvm_app.update
sync; reboot' || true
    echo "device is rebooting into the restored app"
}

# Run a command with the project toolchain (mise) and docker access (the
# docker group only applies after a re-login, so fall back to sg).
run() {
    local cmd=("$@")
    if command -v mise >/dev/null; then cmd=(mise exec -- "${cmd[@]}"); fi
    if docker info >/dev/null 2>&1; then
        "${cmd[@]}"
    else
        sg docker -c "$(printf '%q ' "${cmd[@]}")"
    fi
}

wait_usb() {
    for _ in $(seq 1 "$1"); do
        if [ "$(usb_state)" = "configured" ]; then
            sleep 5
            [ "$(usb_state)" = "configured" ] && return 0
        fi
        sleep 2
    done
    return 1
}

if $RECOVER_ONLY; then recover; exit 0; fi
if $RESTORE_STOCK; then restore_stock; exit 0; fi

if $INSTALL; then
    VERSION="$(sed -n 's/^VERSION := //p' Makefile)+custom.$(git rev-parse --short HEAD)"
    git diff --quiet HEAD || VERSION="$VERSION.dirty"

    info "building UI"
    run make frontend SKIP_UI_BUILD=0 >"$LOG" 2>&1 || { tail -20 "$LOG"; exit 1; }
    info "building release $VERSION (log: $LOG)"
    run make build_release VERSION="$VERSION" >>"$LOG" 2>&1 || { tail -20 "$LOG"; exit 1; }

    info "stopping any test run, backing up the stock app (first install only)"
    "${SSH[@]}" 'B=/userdata/jetkvm/bin
killall jetkvm_app_debug 2>/dev/null
[ -f $B/jetkvm_app.stock ] || cp $B/jetkvm_app $B/jetkvm_app.stock'
    info "installing and rebooting"
    "${SSH[@]}" "cat > /userdata/jetkvm/jetkvm_app.update" < bin/jetkvm_app
    BOOT_ID="$("${SSH[@]}" 'cat /proc/sys/kernel/random/boot_id')"
    "${SSH[@]}" 'sync; reboot' || true

    # reboot returns before the device goes down; wait for a new boot
    until NEW_BOOT_ID="$("${SSH[@]}" 'cat /proc/sys/kernel/random/boot_id' 2>/dev/null)" &&
        [ -n "$NEW_BOOT_ID" ] && [ "$NEW_BOOT_ID" != "$BOOT_ID" ]; do
        sleep 3
    done
    info "device rebooted"
    if wait_usb 60; then
        installed="$("${SSH[@]}" 'grep -a -o -m1 "[0-9.]*+custom\.[0-9a-f.dirty]*" /userdata/jetkvm/bin/jetkvm_app')"
        [ "$installed" = "$VERSION" ] || warn "installed binary reports '$installed', expected $VERSION"
        ok "installed $installed"
        ok "USB configured: $("${SSH[@]}" 'ls /sys/kernel/config/usb_gadget/jetkvm/configs/c.1 | grep -v -e MaxPower -e bmAttributes -e strings | tr "\n" " "')"
        exit 0
    fi
    warn "USB not configured after the install"
    restore_stock
    exit 2
fi

# docker group membership only applies after a re-login; fall back to sg
DEPLOY=(./dev_deploy.sh -r "$HOST")
if command -v mise >/dev/null; then DEPLOY=(mise exec -- "${DEPLOY[@]}"); fi
if ! docker info >/dev/null 2>&1 && command -v podman >/dev/null; then
    export CONTAINER_CMD=podman
fi

info "building and deploying (log: $LOG)"
if docker info >/dev/null 2>&1 || [ "${CONTAINER_CMD:-}" = podman ]; then
    "${DEPLOY[@]}" >"$LOG" 2>&1 &
else
    sg docker -c "$(printf '%q ' "${DEPLOY[@]}")" >"$LOG" 2>&1 &
fi
DEPLOY_PID=$!

until grep -q "JetKVM Starting Up" "$LOG" 2>/dev/null; do
    if ! kill -0 "$DEPLOY_PID" 2>/dev/null; then
        tail -20 "$LOG"; echo; warn "deploy failed before the app started"; exit 1
    fi
    sleep 2
done
info "app started"

if wait_usb 45; then
    ok "USB configured"
    echo "The test build runs while this script's deploy process lives (pid $DEPLOY_PID); stop it or reboot the device to go back."
    wait "$DEPLOY_PID"
    exit 0
fi

warn "USB not configured after 90s"
recover
exit 2
