#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
version=${1:-dev}
case "$version" in *[!a-zA-Z0-9._-]*|'') echo 'Invalid version' >&2; exit 1;; esac
mojave_version=$(go list -m -f '{{.Version}}' github.com/arm092/mojave)
mkdir -p dist
for target in windows/amd64 windows/arm64 darwin/amd64 darwin/arm64; do
    os=${target%/*}
    arch=${target#*/}
    bundle=$(mktemp -d)
    suffix=''
    if [ "$os" = windows ]; then suffix='.exe'; fi
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags="-X main.version=$version" -o "$bundle/ghi$suffix" ./cmd/ghi
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags="-X main.version=$mojave_version" -o "$bundle/mojave$suffix" github.com/arm092/mojave/cmd/mojave
    (cd "$bundle" && shasum -a 256 "ghi$suffix" > ghi.sha256)
    (cd "$bundle" && shasum -a 256 "mojave$suffix" > mojave.sha256)
    if [ "$os" = windows ]; then
    cp install/install.ps1 "$bundle/"
        cp LICENSE THIRD_PARTY_NOTICES "$bundle/"
        (cd "$bundle" && zip -q "$root/dist/ghi_${version}_${os}_${arch}.zip" "ghi$suffix" "mojave$suffix" ghi.sha256 mojave.sha256 install.ps1 LICENSE THIRD_PARTY_NOTICES)
    else
        cp install/install.sh "$bundle/"
        cp LICENSE THIRD_PARTY_NOTICES "$bundle/"
        tar -czf "$root/dist/ghi_${version}_${os}_${arch}.tar.gz" -C "$bundle" ghi mojave ghi.sha256 mojave.sha256 install.sh LICENSE THIRD_PARTY_NOTICES
    fi
    # bundle is the exact directory returned by mktemp in this shell.
    rm -rf -- "$bundle"
done
(cd dist && shasum -a 256 ghi_"${version}"_* > checksums.txt)
