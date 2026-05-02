#!/usr/bin/env sh
set -eu

REPO="${AGENT_BRAIN_REPO:-NanoCycles/agent-brain}"
VERSION="${AGENT_BRAIN_VERSION:-latest}"
INSTALL_DIR="${AGENT_BRAIN_INSTALL_DIR:-$HOME/.local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$os" in
  linux) os="linux" ;;
  darwin) os="darwin" ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

asset="agent-brain_${os}_${arch}.tar.gz"
if [ "$VERSION" = "latest" ]; then
  url="https://github.com/${REPO}/releases/latest/download/${asset}"
else
  url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$INSTALL_DIR"
echo "Downloading $url"
curl -fsSL "$url" -o "$tmp/$asset"
tar -xzf "$tmp/$asset" -C "$tmp"
install "$tmp/agent-brain" "$INSTALL_DIR/agent-brain"

echo "agent-brain installed at $INSTALL_DIR/agent-brain"
echo "Make sure $INSTALL_DIR is on PATH."
