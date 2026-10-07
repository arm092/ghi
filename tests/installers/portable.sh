#!/bin/sh
# Run in a disposable Linux environment with no Go on PATH.
set -eu
if [ "$(uname -s)" != Linux ]; then echo 'This cache-isolation test requires Linux; test macOS in a fresh user account.' >&2; exit 1; fi
bundle=$(CDPATH= cd -- "$1" && pwd)
version=${2:-1.0.0}
mojave_version=${3:-0.1.0}
if command -v go >/dev/null 2>&1; then echo 'Go must not be on PATH.' >&2; exit 1; fi
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
export XDG_CACHE_HOME="$work/cache" XDG_CONFIG_HOME="$work/config"
export GOPATH="$work/gopath" GOCACHE="$work/go-build" GOENV=off
unset GHI_GO GOROOT GOTOOLCHAIN GOWORK
cp -R "$bundle" "$work/bundle"
export GHI_INSTALL_DIR="$work/installed bin" GHI_NO_PATH=1
sh "$work/bundle/install.sh"
ghi="$GHI_INSTALL_DIR/ghi"
mojave="$GHI_INSTALL_DIR/mojave"
test "$("$ghi" -v)" = "ghi v$version"
test "$("$mojave" -v)" = "mojave v$mojave_version"
setup_output=$("$ghi" setup)
printf '%s\n' "$setup_output" | grep -F "$XDG_CACHE_HOME/ghi/toolchains/"
sh "$work/bundle/install.sh"
guard="$work/protected destination"
mkdir "$guard"
for name in ghi mojave; do printf 'existing %s installation sentinel\n' "$name" > "$guard/$name"; done
cp "$guard/ghi" "$work/ghi.expected"
cp "$guard/mojave" "$work/mojave.expected"
for name in ghi mojave; do
    printf '%064d  %s\n' 0 "$name" > "$work/bundle/$name.sha256"
    if GHI_INSTALL_DIR="$guard" sh "$work/bundle/install.sh" > "$work/corrupt.log" 2>&1; then echo "Corrupt $name bundle accepted." >&2; exit 1; fi
    grep -i 'checksum\|FAILED' "$work/corrupt.log"
    cmp "$guard/ghi" "$work/ghi.expected"
    cmp "$guard/mojave" "$work/mojave.expected"
    cp "$bundle/$name.sha256" "$work/bundle/$name.sha256"
done
project="$work/new project"
"$ghi" init "$project"
test -f "$project/mojave.json"
test -d "$project/tests"
(cd "$project" && "$mojave" install)
"$ghi" check "$project"
test "$("$ghi" run "$project")" = 'Hello from Ghi!'
"$ghi" build -o "$work/hello" "$project"
test "$("$work/hello")" = 'Hello from Ghi!'
echo "PASS: $(uname -s) portable $version, no system Go, fresh caches, reinstall, checksum rejection, init, Mojave install, check, run, build and standalone execution."
