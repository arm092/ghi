#!/bin/bash
# Tests orchestration with fake Apple tools; this does not validate native PKGs.
set -euo pipefail
repo=$(cd -- "$(dirname -- "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/project/scripts" "$work/project/install/macos" "$work/bin" "$work/archives" "$work/payload"
cp "$repo/scripts/package-macos.sh" "$work/project/scripts/"
cp "$repo/install/macos/preinstall" "$work/project/install/macos/"
for name in ghi mojave LICENSE THIRD_PARTY_NOTICES; do printf 'fixture\n' > "$work/payload/$name"; done
(cd "$work/payload" && sha256sum ghi > ghi.sha256 && sha256sum mojave > mojave.sha256)
for arch in amd64 arm64; do tar -czf "$work/archives/ghi_v0.2.0_darwin_${arch}.tar.gz" -C "$work/payload" .; done
(cd "$work/archives" && sha256sum --text *.tar.gz > checksums.txt)
cat > "$work/bin/uname" <<'SH'
#!/bin/bash
echo Darwin
SH
cat > "$work/bin/shasum" <<'SH'
#!/bin/bash
shift 2
exec sha256sum "$@"
SH
cat > "$work/bin/lipo" <<'SH'
#!/bin/bash
cp "$2" "$5"
SH
cat > "$work/bin/codesign" <<'SH'
#!/bin/bash
exit 0
SH
cat > "$work/bin/ln" <<'SH'
#!/bin/bash
# Avoid Windows Git Bash's symlink emulation in this orchestration test.
printf '%s\n' "$2" > "$3"
SH
cat > "$work/bin/pkgbuild" <<'SH'
#!/bin/bash
for arg do output=$arg; done
printf 'component\n' > "$output"
SH
cat > "$work/bin/productbuild" <<'SH'
#!/bin/bash
printf '%s\n' "$@" > "$ARGUMENT_LOG"
case "${BUILD_MODE:-ok}" in fail) exit 47;; empty) exit 0;; esac
for arg do output=$arg; done
printf 'package fixture\n' > "$output"
SH
chmod +x "$work/bin/"*
export PATH="$work/bin:$PATH"
export ARGUMENT_LOG="$work/arguments"
unset GHI_INSTALLER_IDENTITY GHI_APPLICATION_IDENTITY GHI_NOTARY_PROFILE
output="$work/project/dist/ghi_v0.2.0_macos_universal.pkg"
bash "$work/project/scripts/package-macos.sh" 0.2.0 "$work/archives" >/dev/null
test -s "$output"
if grep -qx -- '--sign' "$ARGUMENT_LOG"; then exit 1; fi
GHI_INSTALLER_IDENTITY='Developer ID Installer: Test User (123)' bash "$work/project/scripts/package-macos.sh" 0.2.0 "$work/archives" >/dev/null
grep -qx 'Developer ID Installer: Test User (123)' "$ARGUMENT_LOG"
before=$(sha256sum "$output")
for mode in fail empty; do
    if BUILD_MODE=$mode bash "$work/project/scripts/package-macos.sh" 0.2.0 "$work/archives" >/dev/null 2>&1; then
        echo "Unexpected success: $mode" >&2; exit 1
    fi
    test "$(sha256sum "$output")" = "$before"
done
# A failed builder must not open an old package or report success.
cp "$repo/install/macos/Build Installer.command" "$work/project/Build Installer.command"
printf '#!/bin/bash\nexit 47\n' > "$work/project/scripts/package-macos.sh"
printf '#!/bin/bash\necho opened > "$OPEN_LOG"\n' > "$work/bin/open"
chmod +x "$work/bin/open"
export OPEN_LOG="$work/open-log"
if bash "$work/project/Build Installer.command" </dev/null > "$work/launcher-log" 2>&1; then exit 1; fi
test ! -e "$OPEN_LOG"
grep -q 'Build failed' "$work/launcher-log"
echo 'Unsigned/signed arguments, build failure, missing output and launcher failure checks passed (mock Apple tools).'
