#!/bin/bash
set -e

REPO="hdck007/yeet"
BINARY_NAME="yeet"
INSTALL_DIR="/usr/local/bin"

echo "🔍 Fetching the latest release version for $REPO..."
VERSION=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$VERSION" ]; then
  echo "❌ Error: Could not determine the latest version. Make sure you have a published release!"
  exit 1
fi

echo "✅ Found latest version: $VERSION"

# We are grabbing the macOS universal binary we built earlier
FILE_NAME="yeet-darwin-universal"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$FILE_NAME"

echo "⬇️  Downloading $FILE_NAME..."
curl -L -o "$BINARY_NAME" "$DOWNLOAD_URL"

echo "🔧 Making binary executable..."
chmod +x "$BINARY_NAME"

echo "🛡️  Bypassing macOS Gatekeeper..."
# We suppress errors here just in case it's run on Linux or the tag isn't there
xattr -d com.apple.quarantine "$BINARY_NAME" 2>/dev/null || true

echo "📦 Installing to $INSTALL_DIR (this might ask for your Mac password)..."
sudo mv "$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"

echo "🎉 Installation complete!"
echo "Run 'yeet' in your terminal to get started."