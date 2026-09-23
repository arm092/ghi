#!/bin/sh
set -eu

bundle=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
destination=${GHI_INSTALL_DIR:-"$HOME/.local/bin"}
if [ ! -f "$bundle/ghi" ] || [ ! -f "$bundle/ghi.sha256" ]; then
    echo 'Extract the complete Ghi release archive before running this installer.' >&2
    exit 1
fi
(cd "$bundle" && shasum -a 256 -c ghi.sha256)
"$bundle/ghi" setup
mkdir -p "$destination"
destination=$(CDPATH= cd -- "$destination" && pwd)
install -m 755 "$bundle/ghi" "$destination/ghi"
printf 'Ghi installed at %s/ghi\n' "$destination"
if [ "${GHI_NO_PATH:-0}" != 1 ]; then
    config="${XDG_CONFIG_HOME:-$HOME/.config}/ghi"
    mkdir -p "$config"
    printf '%s\n' "$destination" > "$config/path"
    cat > "$config/env.sh" <<'ENV'
ghi_bin=$(cat "${XDG_CONFIG_HOME:-$HOME/.config}/ghi/path")
case ":$PATH:" in
    *":$ghi_bin:"*) ;;
    *) PATH="$ghi_bin:$PATH"; export PATH ;;
esac
unset ghi_bin
ENV
    profile_line='[ ! -r "${XDG_CONFIG_HOME:-$HOME/.config}/ghi/env.sh" ] || . "${XDG_CONFIG_HOME:-$HOME/.config}/ghi/env.sh"'
    configure_profile() {
        if [ ! -f "$1" ] || ! grep -Fqx "$profile_line" "$1"; then
            printf '\n# Ghi compiler\n%s\n' "$profile_line" >> "$1"
        fi
    }
    shell=${SHELL:-/bin/zsh}
    case "${shell##*/}" in
        zsh) configure_profile "$HOME/.zprofile"; configure_profile "$HOME/.zshrc" ;;
        bash)
            configure_profile "$HOME/.bashrc"
            if [ -f "$HOME/.bash_profile" ]; then configure_profile "$HOME/.bash_profile"
            elif [ -f "$HOME/.bash_login" ]; then configure_profile "$HOME/.bash_login"
            else configure_profile "$HOME/.profile"; fi
            ;;
        *) configure_profile "$HOME/.profile" ;;
    esac
fi
printf 'Open a new terminal, then run ghi version or ghi run <project-directory>.\n'
