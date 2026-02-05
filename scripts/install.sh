#!/usr/bin/env sh
set -e

REPO="1to10partners/seven"
BINARY="seven"

if [ -n "$SEVEN_INSTALL_DIR" ]; then
  INSTALL_DIR="$SEVEN_INSTALL_DIR"
elif [ -n "$HOME" ]; then
  INSTALL_DIR="$HOME/.local/bin"
else
  INSTALL_DIR="/usr/local/bin"
fi

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

case "$OS" in
  linux|darwin) ;;
  *)
    echo "Unsupported OS: $OS" >&2
    exit 1
    ;;
esac

VERSION=""
if [ -n "$SEVEN_VERSION" ]; then
  VERSION="$SEVEN_VERSION"
else
  VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": "\(v[^\"]*\)".*/\1/p')
fi

if [ -z "$VERSION" ]; then
  echo "Could not determine latest version" >&2
  exit 1
fi

ARCHIVE="${BINARY}_${VERSION#v}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$VERSION/$ARCHIVE"

mkdir -p "$INSTALL_DIR"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "$URL" -o "$TMP/$ARCHIVE"

tar -xzf "$TMP/$ARCHIVE" -C "$TMP"

install -m 755 "$TMP/$BINARY" "$INSTALL_DIR/$BINARY"

echo "Installed $BINARY to $INSTALL_DIR/$BINARY"
