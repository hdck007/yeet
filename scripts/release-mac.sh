#!/usr/bin/env bash
set -euo pipefail

VERSION="v$(date +'%Y.%m.%d')-$(git rev-parse --short HEAD)"
BRANCH="release/$VERSION"
DIST="dist/mac"

echo "==> Building yeet $VERSION for macOS"
mkdir -p "$DIST"

echo "  -> darwin/amd64"
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -ldflags "-X github.com/hdck007/yeet/internal/cli.Version=$VERSION" -o "$DIST/yeet-darwin-amd64" ./cmd/yeet/

echo "  -> darwin/arm64"
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags "-X github.com/hdck007/yeet/internal/cli.Version=$VERSION" -o "$DIST/yeet-darwin-arm64" ./cmd/yeet/

echo "==> Creating universal binary"
lipo -create -output "$DIST/yeet-darwin-universal" "$DIST/yeet-darwin-amd64" "$DIST/yeet-darwin-arm64"

echo "==> Done"
ls -lh "$DIST"
echo ""
echo "Binaries in $DIST/"
echo "  yeet-darwin-amd64      (Intel)"
echo "  yeet-darwin-arm64      (Apple Silicon)"
echo "  yeet-darwin-universal  (Universal)"

# Check if release branch already exists on remote
if git ls-remote --heads origin | grep -q "refs/heads/$BRANCH$"; then
  echo "Branch $BRANCH already exists on remote — skipping."
  exit 0
fi

read -rp "
Create and push release branch $BRANCH? [y/N] " confirm
if [[ "$confirm" =~ ^[Yy]$ ]]; then
  git checkout -b "$BRANCH"
  git push origin "$BRANCH"
  git checkout -
  echo ""
  echo "Branch $BRANCH pushed."
  echo "Merge it into main — CI will tag and publish the release automatically."
fi
