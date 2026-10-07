#!/bin/bash
set -uo pipefail
cd -- "$(dirname -- "$0")" || exit 1
echo 'Building the Ghi and Mojave macOS installer...'
bash scripts/package-macos.sh 1.0.0
status=$?
package=dist/ghi_v1.0.0_macos_universal.pkg
if [ "$status" -ne 0 ] || [ ! -s "$package" ]; then
    echo 'Build failed. No new installer was created. Please copy the error output for diagnosis.'
    read -r -p 'Press Return to close.' || true
    exit 1
fi
echo "Package created: $PWD/$package"
open -R "$package"
read -r -p 'Press Return to close.' || true
