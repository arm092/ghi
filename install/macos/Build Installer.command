#!/bin/bash
set -uo pipefail
cd -- "$(dirname -- "$0")" || exit 1
echo 'Building the Ghi and Mojave macOS installer...'
bash scripts/package-macos.sh 0.2.1
status=$?
package=dist/ghi_v0.2.1_macos_universal.pkg
if [ "$status" -ne 0 ] || [ ! -s "$package" ]; then
    echo 'Build failed. No new installer was created. Please copy the error output for diagnosis.'
    read -r -p 'Press Return to close.' || true
    exit 1
fi
echo "Package created: $PWD/$package"
open -R "$package"
echo 'Native installation still needs verification.'
read -r -p 'Press Return to close.' || true
