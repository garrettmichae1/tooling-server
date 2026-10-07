#!/bin/sh
# Clone the pinned VectorCraft commit and build vectorcraft-cli.
# The Rust tree stays in third_party/ and is not part of the Go module.
set -eu

PIN=fd99375b1b260ee917f2d0e2bb861582b5baab45
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
DEST="$ROOT/third_party/vectorcraft"
BIN="$DEST/target/release/vectorcraft-cli"

if ! command -v git >/dev/null 2>&1; then
  echo "git is required" >&2
  exit 1
fi
if ! command -v cargo >/dev/null 2>&1; then
  echo "cargo is required to build vectorcraft-cli" >&2
  exit 1
fi

mkdir -p "$ROOT/third_party"
if [ ! -d "$DEST/.git" ]; then
  git clone https://github.com/storytold/vectorcraft.git "$DEST"
fi

git -C "$DEST" fetch --depth 1 origin "$PIN"
git -C "$DEST" checkout --detach "$PIN"

# Keep the binary under third_party even if the shell sets CARGO_TARGET_DIR.
export CARGO_TARGET_DIR="$DEST/target"
cargo build --release --manifest-path "$DEST/Cargo.toml" -p vectorcraft-cli

if [ ! -x "$BIN" ]; then
  echo "build finished but $BIN is missing" >&2
  exit 1
fi

echo "$BIN"
