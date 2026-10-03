#!/usr/bin/env bash
#
# Re-apply the custom changes on top of a new upstream JetKVM version.
#
#   custom/update.sh                 rebase onto the latest stable release tag (release/X.Y.Z)
#   custom/update.sh dev             rebase onto the latest upstream dev branch
#   custom/update.sh release/0.6.0   rebase onto a specific tag or commit
#   custom/update.sh --continue      after resolving conflicts: continue and verify
#   custom/update.sh --abort         give up and return to the previous state
#   custom/update.sh --check         only run the checks on the current tree
#
# The upstream commit the custom commits sit on is tracked by the tag
# "custom/base", so only our own commits are replayed.

set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

STATE_FILE="$(git rev-parse --git-dir)/custom-update-target"
BASE_TAG="custom/base"

info() { echo -e "\033[1;34m▶ $*\033[0m"; }
ok()   { echo -e "\033[1;32m✓ $*\033[0m"; }
warn() { echo -e "\033[1;33m! $*\033[0m"; }
die()  { echo -e "\033[1;31m✗ $*\033[0m"; exit 1; }

run() {
    if command -v mise >/dev/null; then mise exec -- "$@"; else "$@"; fi
}

checks() {
    info "UI: dependencies, i18n, typecheck, lint"
    (
        cd ui
        if [ ! -d node_modules ] || [ package-lock.json -nt node_modules ]; then
            HUSKY=0 run npm ci --no-audit --no-fund
        fi
        run npm run i18n:compile >/dev/null
        run npx tsc --noEmit
        run npx oxlint src
    ) || die "UI checks failed"

    info "Go: vet and tests"
    if [ ! -f static/index.html ]; then
        # web.go embeds static/; build the UI once so the root package compiles
        (cd ui && run npm run build:device >/dev/null)
    fi
    run go vet . ./internal/usbgadget ./internal/hidrpc ./internal/edidfit || die "go vet failed"
    run go test ./internal/usbgadget ./internal/hidrpc ./internal/edidfit || die "go test failed"

    ok "all checks passed"
}

finish() {
    local target
    target="$(cat "$STATE_FILE")"
    git tag -f "$BASE_TAG" "$target" >/dev/null
    rm -f "$STATE_FILE"
    ok "custom commits now sit on $(git log -1 --format='%h %s' "$target")"
    git log --oneline "$BASE_TAG..HEAD"
    checks
    echo
    echo "Next: custom/deploy.sh <device-ip>  (test)   or   custom/deploy.sh -i <device-ip>  (install)"
}

case "${1:-}" in
    --check)
        checks
        exit 0
        ;;
    --abort)
        git rebase --abort 2>/dev/null || true
        rm -f "$STATE_FILE"
        ok "update aborted"
        exit 0
        ;;
    --continue)
        [ -f "$STATE_FILE" ] || die "no update in progress"
        if [ -d "$(git rev-parse --git-path rebase-merge)" ] || [ -d "$(git rev-parse --git-path rebase-apply)" ]; then
            GIT_EDITOR=true git rebase --continue || die "rebase still has conflicts; resolve them, git add, then run --continue again"
        fi
        finish
        exit 0
        ;;
esac

git diff --quiet && git diff --cached --quiet || die "commit or stash local changes first"
git rev-parse -q --verify "refs/tags/$BASE_TAG" >/dev/null || die "tag $BASE_TAG is missing"
[ "$(git rev-parse --abbrev-ref HEAD)" = "custom" ] || die "check out the custom branch first"

info "fetching upstream"
git fetch origin --tags --prune --force

REF="${1:-}"
if [ -z "$REF" ]; then
    REF="$(git tag -l 'release/*' --sort=-v:refname | grep -v -- '-dev' | head -1)"
    [ -n "$REF" ] || die "no release tag found"
fi
case "$REF" in
    dev|main) REF="origin/$REF" ;;
esac
TARGET="$(git rev-parse --verify -q "$REF^{commit}")" || die "unknown ref: $REF"

if [ "$TARGET" = "$(git rev-parse "$BASE_TAG^{commit}")" ]; then
    ok "already based on $REF"
    exit 0
fi

info "rebasing $(git rev-list --count "$BASE_TAG..HEAD") custom commits onto $REF"
git branch -f custom-backup HEAD
echo "$TARGET" > "$STATE_FILE"

if ! git rebase --onto "$TARGET" "$BASE_TAG" custom; then
    warn "conflicts. The previous state is kept in the branch custom-backup."
    echo
    git status --short | grep -E '^(UU|AA|DU|UD|AU|UA|DD) ' || true
    echo
    echo "1. Fix the files above (see custom/README.md, 'conflict hotspots')"
    echo "2. git add <files>"
    echo "3. custom/update.sh --continue        (or --abort to give up)"
    exit 1
fi

finish
