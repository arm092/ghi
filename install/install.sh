#!/bin/sh
set -eu

bundle=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
destination=${GHI_INSTALL_DIR:-"$HOME/.local/bin"}
if [ ! -f "$bundle/ghi" ] || [ ! -f "$bundle/ghi.sha256" ]; then
    echo 'Extract the complete Ghi release archive before running this installer.' >&2
    exit 1
fi
(cd "$bundle" && shasum -a 256 -c ghi.sha256)
mkdir -p "$destination"
install -m 755 "$bundle/ghi" "$destination/ghi"
"$destination/ghi" setup
printf 'Ghi installed at %s/ghi\n' "$destination"
case ":$PATH:" in
    *":$destination:"*) ;;
    *) printf 'Add this directory to your shell PATH: %s\n' "$destination" ;;
esac
printf 'Run ghi version or ghi run <project-directory>.\n'
