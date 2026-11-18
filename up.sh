#!/bin/sh
set -eu
cd "$(dirname "$0")"

SOCK="${XDG_RUNTIME_DIR:-/tmp}/primedos.sock"
mkdir -p logs

if [ ! -f bot.yaml ]; then
	echo "нет bot.yaml: скопируй bot.yaml.example и впиши токен"
	exit 1
fi

if pgrep -f '^\./primedos --dry-run --headless' >/dev/null 2>&1; then
	echo "primedos: уже работает"
else
	setsid ./primedos --dry-run --headless --socket "$SOCK" >>logs/primedos.log 2>&1 &
	echo "primedos: запущен"
fi

if pgrep -x primedos-bot >/dev/null 2>&1; then
	echo "бот: уже работает"
else
	setsid ./primedos-bot --config bot.yaml >>logs/bot.log 2>&1 &
	echo "бот: запущен"
fi

i=0
while [ "$i" -lt 30 ]; do
	if [ -S "$SOCK" ]; then
		echo "сокет: $SOCK"
		exit 0
	fi
	i=$((i + 1))
	sleep 0.2
done

echo "сокет не появился, смотри logs/primedos.log"
exit 1
