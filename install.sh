#!/usr/bin/env bash
# Builds `aisle7` and installs it so you can run it from anywhere.
#   ./install.sh              install to ~/.local/bin
#   INSTALL_DIR=/usr/local/bin ./install.sh   install somewhere else (may need sudo)
#   ./install.sh --uninstall  remove it again
set -euo pipefail

cd "$(dirname "$0")"
dir="${INSTALL_DIR:-$HOME/.local/bin}"
target="$dir/aisle7"

if [[ "${1:-}" == "--uninstall" ]]; then
  rm -f "$target"
  echo "Removed $target"
  exit 0
fi

command -v go >/dev/null || { echo "error: Go is not installed (https://go.dev/dl/)" >&2; exit 1; }

# `aisle7 serve` embeds the React app; without npm the web UI is simply empty.
command -v npm >/dev/null && (cd web && npm install --silent && npm run build --silent)
mkdir -p web/dist && touch web/dist/.keep

mkdir -p "$dir"
go build -trimpath -ldflags "-s -w" -o "$target" .
echo "Installed $target"

case ":$PATH:" in
  *":$dir:"*) echo "Run it with: aisle7" ;;
  *)
    echo
    echo "$dir is not on your PATH. Add this line to ~/.zshrc, then open a new terminal:"
    echo "  export PATH=\"$dir:\$PATH\""
    ;;
esac
