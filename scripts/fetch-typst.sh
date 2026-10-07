#!/bin/sh
# Download pinned Typst 0.15.1, MiTeX, Whalogen, CeTZ, CeTZ Plot, and JetBrains Mono.
# The tree stays in third_party/ and is not part of the Go module.
set -eu

TYPST_VERSION=0.15.1
MITEX_VERSION=0.2.7
WHALOGEN_VERSION=0.3.0
XARROW_VERSION=0.3.1
CETZ_VERSION=0.5.2
CETZ_PLOT_VERSION=0.1.4
OXIFMT_VERSION=1.0.0
FONT_ZIP_HASH=6f6376c6ed2960ea8a963cd7387ec9d76e3f629125bc33d1fdcd7eb7012f7bbf
MITEX_HASH=0159e214845e49cbdc332d9d572da112dae5ad248072e0a7680d38c8307c2e15
WHALOGEN_HASH=7bd4acb99a6d2c037a04b9ec4033fdfa764699e81e747d67f3ca7dcc0fc0eb67
XARROW_HASH=371ec408e7cedf29522e47d61da8250da70f1b28ab0089c8a811d3fde3d9484e
CETZ_HASH=77cf8490114ae04c6e665a11efa691d284a0cadb9719771b5708c1197292f23f
CETZ_PLOT_HASH=13170258be0761701e57141081e1f6f029bc8967988f549a3b089289a1347936
OXIFMT_HASH=7d17a1fc8ad01740ec3cb2b03c7360a4225ff9318e5710765fa98ea6fd59594f

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
DEST="$ROOT/third_party/typst"

os=$(uname -s)
arch=$(uname -m)
case "$os-$arch" in
  Darwin-arm64)
    asset=typst-aarch64-apple-darwin.tar.xz
    typst_hash=48f62ed034aa3a7978309579ac6ca00045e2ef0da73114e8af27cfd8e74dc05a
    ;;
  Darwin-x86_64)
    asset=typst-x86_64-apple-darwin.tar.xz
    typst_hash=7f9fdd9584866245de9a79e0add8f9236fae6f40a8a45e2c4771ccc14db4e0fa
    ;;
  Linux-aarch64)
    asset=typst-aarch64-unknown-linux-musl.tar.xz
    typst_hash=5aa8d74a3d906e60ea12a66ac2f37f8eef1b14cbad7182a745e393a10c23dcee
    ;;
  Linux-x86_64)
    asset=typst-x86_64-unknown-linux-musl.tar.xz
    typst_hash=a6d077d0a95eed5a2eba715b2dae06be954f624ccbf85758a03f389ded33118c
    ;;
  *)
    echo "unsupported platform $os-$arch" >&2
    exit 1
    ;;
esac

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

check() {
  printf '%s  %s\n' "$1" "$2" | shasum -a 256 -c -
}

curl -fsSL -o "$tmp/typst.tar.xz" "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/${asset}"
check "$typst_hash" "$tmp/typst.tar.xz"
tar -xf "$tmp/typst.tar.xz" -C "$tmp"

curl -fsSL -o "$tmp/mitex.tar.gz" "https://packages.typst.org/preview/mitex-${MITEX_VERSION}.tar.gz"
check "$MITEX_HASH" "$tmp/mitex.tar.gz"

curl -fsSL -o "$tmp/whalogen.tar.gz" "https://packages.typst.org/preview/whalogen-${WHALOGEN_VERSION}.tar.gz"
check "$WHALOGEN_HASH" "$tmp/whalogen.tar.gz"

# Whalogen loads xarrow. CeTZ loads oxifmt. CeTZ Plot loads CeTZ. A triangle, a formula, or a chart does not compile without them.
curl -fsSL -o "$tmp/xarrow.tar.gz" "https://packages.typst.org/preview/xarrow-${XARROW_VERSION}.tar.gz"
check "$XARROW_HASH" "$tmp/xarrow.tar.gz"

curl -fsSL -o "$tmp/cetz.tar.gz" "https://packages.typst.org/preview/cetz-${CETZ_VERSION}.tar.gz"
check "$CETZ_HASH" "$tmp/cetz.tar.gz"

curl -fsSL -o "$tmp/cetz-plot.tar.gz" "https://packages.typst.org/preview/cetz-plot-${CETZ_PLOT_VERSION}.tar.gz"
check "$CETZ_PLOT_HASH" "$tmp/cetz-plot.tar.gz"

curl -fsSL -o "$tmp/oxifmt.tar.gz" "https://packages.typst.org/preview/oxifmt-${OXIFMT_VERSION}.tar.gz"
check "$OXIFMT_HASH" "$tmp/oxifmt.tar.gz"

curl -fsSL -o "$tmp/jetbrains.zip" "https://github.com/JetBrains/JetBrainsMono/releases/download/v2.304/JetBrainsMono-2.304.zip"
check "$FONT_ZIP_HASH" "$tmp/jetbrains.zip"

rm -rf "$DEST"
mkdir -p \
  "$DEST/packages/preview/mitex/${MITEX_VERSION}" \
  "$DEST/packages/preview/whalogen/${WHALOGEN_VERSION}" \
  "$DEST/packages/preview/xarrow/${XARROW_VERSION}" \
  "$DEST/packages/preview/cetz/${CETZ_VERSION}" \
  "$DEST/packages/preview/cetz-plot/${CETZ_PLOT_VERSION}" \
  "$DEST/packages/preview/oxifmt/${OXIFMT_VERSION}" \
  "$DEST/fonts"
install -m 755 "$tmp"/typst-*/typst "$DEST/typst"
cp "$tmp"/typst-*/LICENSE "$tmp"/typst-*/NOTICE "$DEST/"
tar -xzf "$tmp/mitex.tar.gz" -C "$DEST/packages/preview/mitex/${MITEX_VERSION}"
tar -xzf "$tmp/whalogen.tar.gz" -C "$DEST/packages/preview/whalogen/${WHALOGEN_VERSION}"
tar -xzf "$tmp/xarrow.tar.gz" -C "$DEST/packages/preview/xarrow/${XARROW_VERSION}"
tar -xzf "$tmp/cetz.tar.gz" -C "$DEST/packages/preview/cetz/${CETZ_VERSION}"
tar -xzf "$tmp/cetz-plot.tar.gz" -C "$DEST/packages/preview/cetz-plot/${CETZ_PLOT_VERSION}"
tar -xzf "$tmp/oxifmt.tar.gz" -C "$DEST/packages/preview/oxifmt/${OXIFMT_VERSION}"
unzip -qo -j "$tmp/jetbrains.zip" "fonts/ttf/JetBrainsMono-Regular.ttf" "OFL.txt" -d "$DEST/fonts"

if ! "$DEST/typst" --version | grep -q "$TYPST_VERSION"; then
  echo "typst version is not $TYPST_VERSION" >&2
  exit 1
fi

echo "$DEST/typst"
