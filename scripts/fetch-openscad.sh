#!/bin/sh
# Download a pinned official macOS OpenSCAD snapshot.
# The app bundle stays intact under third_party/openscad/. A wrapper named
# openscad execs the binary inside the bundle, because the app only runs
# from that layout. The tree is not part of the Go module.
set -eu

OPENSCAD_DMG=OpenSCAD-2026.10.03.dmg
OPENSCAD_URL="https://files.openscad.org/snapshots/${OPENSCAD_DMG}"
OPENSCAD_HASH=59372133bd070a76760a86f3ae6720636d7991aa5d7febcb68798fbcafcb7fc2

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
DEST="$ROOT/third_party/openscad"

os=$(uname -s)
if [ "$os" != "Darwin" ]; then
  echo "OpenSCAD fetch supports macOS only" >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1 || ! command -v hdiutil >/dev/null 2>&1; then
  echo "curl and hdiutil are required" >&2
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL -o "$tmp/openscad.dmg" "$OPENSCAD_URL"
printf '%s  %s\n' "$OPENSCAD_HASH" "$tmp/openscad.dmg" | shasum -a 256 -c -

mount="$tmp/mnt"
mkdir -p "$mount"
hdiutil attach -nobrowse -readonly -mountpoint "$mount" "$tmp/openscad.dmg"
cleanup_mount() {
  hdiutil detach "$mount" >/dev/null 2>&1 || true
}
trap 'cleanup_mount; rm -rf "$tmp"' EXIT

if [ ! -d "$mount/OpenSCAD.app/Contents/MacOS" ]; then
  echo "OpenSCAD.app is missing from the disk image" >&2
  exit 1
fi

rm -rf "$DEST"
mkdir -p "$DEST"
cp -R "$mount/OpenSCAD.app" "$DEST/OpenSCAD.app"
xattr -dr com.apple.quarantine "$DEST/OpenSCAD.app" 2>/dev/null || true
cat > "$DEST/openscad" <<'EOF'
#!/bin/sh
HERE=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
exec "$HERE/OpenSCAD.app/Contents/MacOS/OpenSCAD" "$@"
EOF
chmod 755 "$DEST/openscad"

if ! "$DEST/openscad" --version >/dev/null 2>&1; then
  echo "openscad did not start" >&2
  exit 1
fi

echo "$DEST/openscad"
