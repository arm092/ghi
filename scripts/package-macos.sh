#!/bin/bash
set -euo pipefail

root=$(cd -- "$(dirname -- "$0")/.." && pwd)
version=${1:-0.2.1}
archives=${2:-}
case "$version" in ''|*[!0-9.]* ) echo 'Use a numeric version, e.g. 0.2.1.' >&2; exit 1;; esac
if [ "$(uname -s)" != Darwin ]; then echo 'Build this package on macOS with Xcode Command Line Tools.' >&2; exit 1; fi
for tool in pkgbuild productbuild lipo; do command -v "$tool" >/dev/null; done
work=$(mktemp -d "${TMPDIR:-/tmp}/ghi-pkg.XXXXXXXX")
# Only remove the exact temporary directory allocated above.
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/root/usr/local/lib/ghi/bin" "$work/root/usr/local/bin" "$work/scripts" "$work/resources" "$root/dist"
if [ -z "$archives" ]; then
    archives="$work/downloads"
    mkdir "$archives"
    for file in checksums.txt "ghi_v${version}_darwin_amd64.tar.gz" "ghi_v${version}_darwin_arm64.tar.gz"; do
        curl --fail --location --retry 3 "https://github.com/arm092/ghi/releases/download/v${version}/$file" -o "$archives/$file"
    done
else
    archives=$(cd -- "$archives" && pwd)
fi
for arch in amd64 arm64; do
    archive="ghi_v${version}_darwin_${arch}.tar.gz"
    expected=$(awk -v name="$archive" '$2 == name { print $1 }' "$archives/checksums.txt")
    [ "${#expected}" = 64 ] || { echo "Missing checksum for $archive" >&2; exit 1; }
    actual=$(shasum -a 256 "$archives/$archive" | awk '{print $1}')
    [ "$expected" = "$actual" ] || { echo "$archive checksum mismatch" >&2; exit 1; }
    mkdir "$work/$arch"
    tar -xzf "$archives/$archive" -C "$work/$arch"
    (cd "$work/$arch" && shasum -a 256 -c ghi.sha256 && shasum -a 256 -c mojave.sha256)
done
for name in ghi mojave; do
    binary="$work/root/usr/local/lib/ghi/bin/$name"
    lipo -create "$work/amd64/$name" "$work/arm64/$name" -output "$binary"
    chmod 755 "$binary"
    if [ -n "${GHI_APPLICATION_IDENTITY:-}" ]; then
        codesign --force --options runtime --timestamp --sign "$GHI_APPLICATION_IDENTITY" "$binary"
    else
        codesign --force --sign - "$binary"
    fi
    ln -s "/usr/local/lib/ghi/bin/$name" "$work/root/usr/local/bin/$name"
done
cp "$work/amd64/LICENSE" "$work/amd64/THIRD_PARTY_NOTICES" "$work/root/usr/local/lib/ghi/"
cp "$work/amd64/LICENSE" "$work/resources/LICENSE"
cp "$root/install/macos/preinstall" "$work/scripts/preinstall"
cp "$work/root/usr/local/lib/ghi/bin/ghi" "$work/scripts/ghi"
chmod 755 "$work/scripts/preinstall" "$work/scripts/ghi"
pkgbuild --root "$work/root" --scripts "$work/scripts" --identifier am.ghi.compiler \
    --version "$version" --install-location / --ownership recommended "$work/component.pkg"
cat > "$work/distribution.xml" <<XML
<?xml version="1.0" encoding="utf-8"?>
<installer-gui-script minSpecVersion="2">
  <title>Ghi and Mojave</title>
  <license file="LICENSE" mime-type="text/plain"/>
  <options hostArchitectures="x86_64,arm64" customize="never"/>
  <domains enable_localSystem="true" enable_currentUserHome="false" enable_anywhere="false"/>
  <choices-outline><line choice="ghi"/></choices-outline>
  <choice id="ghi" title="Ghi and Mojave"><pkg-ref id="am.ghi.compiler"/></choice>
  <pkg-ref id="am.ghi.compiler" version="$version">component.pkg</pkg-ref>
</installer-gui-script>
XML
# Bash 3.2 (shipped with macOS) treats empty arrays as unset under nounset.
# Positional arguments are never empty here, including unsigned builds.
set -- --distribution "$work/distribution.xml" --package-path "$work" --resources "$work/resources"
if [ -n "${GHI_INSTALLER_IDENTITY:-}" ]; then set -- "$@" --sign "$GHI_INSTALLER_IDENTITY"; fi
output="$work/ghi_v${version}_macos_universal.pkg"
productbuild "$@" "$output" || exit "$?"
if [ ! -s "$output" ]; then echo 'productbuild did not create a package.' >&2; exit 1; fi
if [ -n "${GHI_NOTARY_PROFILE:-}" ]; then
    [ -n "${GHI_INSTALLER_IDENTITY:-}" ] && [ -n "${GHI_APPLICATION_IDENTITY:-}" ] || {
        echo 'Notarization requires both Developer ID signing identities.' >&2; exit 1;
    }
    xcrun notarytool submit "$output" --keychain-profile "$GHI_NOTARY_PROFILE" --wait
    xcrun stapler staple "$output"
fi
mv "$output" "$root/dist/$(basename "$output")"
output="$root/dist/$(basename "$output")"
(cd "$root/dist" && shasum -a 256 "$(basename "$output")" > "$(basename "$output").sha256")
echo "$output"
