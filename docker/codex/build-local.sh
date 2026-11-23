#!/usr/bin/env bash
#
# Build the app-server binary from the pinned Codex source tree into this directory, so the
# image can be built offline.
#
#   CODEX_SRC=~/src/codex/codex-rs ./build-local.sh && docker build -t infiniflow/codex:local .
#
# CODEX_SRC must be the codex-rs workspace (the directory holding Cargo.toml and the
# codex-app-server package). CARGO_TARGET_DIR defaults to that workspace's target/ and is
# honoured when set, so an existing build cache is reused.
#
# The app-server PACKAGE is built, not the `codex` CLI: its binary takes --listen/--ws-auth
# directly, which is what entrypoint.sh invokes, and it does not drag in the TUI.
#
# Packaging knobs, in the order they matter for a Codex-sized build:
#   - stable rustc    : the tree pins an older channel in rust-toolchain.toml; packaging uses
#                       the installed stable toolchain unless RUSTUP_TOOLCHAIN is set.
#   - no LTO          : the repo's release profile enables thin LTO, which dominates the
#                       final link (tens of minutes, several GiB, enough to trip an
#                       out-of-memory watchdog).
#   - no debuginfo    : an image never symbolicates; line tables only bloat the artifact.
#   - mold / lld      : linking a multi-hundred-MiB binary with GNU ld is the other slow
#                       step. mold is fastest, lld a good fallback.
#   - CARGO_BUILD_JOBS passes through, so callers can cap parallelism when memory is tight.
set -euo pipefail

: "${CODEX_SRC:?set CODEX_SRC to the codex-rs workspace directory}"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
target_dir="${CARGO_TARGET_DIR:-$CODEX_SRC/target}"

# Prefer mold, then lld. Both take the same rustc flag.
if command -v mold >/dev/null 2>&1; then
    export RUSTFLAGS="${RUSTFLAGS:-} -C link-arg=-fuse-ld=mold"
elif command -v ld.lld >/dev/null 2>&1; then
    export RUSTFLAGS="${RUSTFLAGS:-} -C link-arg=-fuse-ld=lld"
fi

echo ">>> building codex from $CODEX_SRC (target dir: $target_dir, toolchain: ${RUSTUP_TOOLCHAIN:-stable})"
(cd "$CODEX_SRC" && CARGO_TARGET_DIR="$target_dir" \
    RUSTUP_TOOLCHAIN="${RUSTUP_TOOLCHAIN:-stable}" \
    CARGO_PROFILE_RELEASE_LTO=false \
    CARGO_PROFILE_RELEASE_DEBUG=0 \
    CARGO_PROFILE_RELEASE_STRIP=symbols \
    cargo build --release -p codex-app-server --bin codex-app-server)

# Strip on the host: a RUN strip in the image would store the unstripped binary again as a
# second layer. The packaging profile already strips, so this only trims what is left.
install -m 0755 "$target_dir/release/codex-app-server" "$here/codex-app-server"
strip --strip-all "$here/codex-app-server"

echo ">>> wrote $here/codex-app-server ($(du -h "$here/codex-app-server" | cut -f1))"
