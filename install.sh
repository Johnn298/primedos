#!/bin/sh
set -eu
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
	echo "нужен Go 1.22+ — https://go.dev/dl/"
	exit 1
fi

go build -trimpath -ldflags "-s -w" -o primedos ./cmd/primedos
mkdir -p "$HOME/.local/bin"
install -m 755 primedos "$HOME/.local/bin/primedos"

echo "установлено: $HOME/.local/bin/primedos"
case ":$PATH:" in
	*":$HOME/.local/bin:"*) ;;
	*) echo "добавь в PATH: export PATH=\"\$HOME/.local/bin:\$PATH\"" ;;
esac
