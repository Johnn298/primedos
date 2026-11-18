#!/bin/sh
cd "$(dirname "$0")"

for p in $(pgrep -x primedos-bot); do
	kill "$p" 2>/dev/null || true
done
for p in $(pgrep -x primedos); do
	kill "$p" 2>/dev/null || true
done

i=0
while [ "$i" -lt 30 ]; do
	if ! pgrep -x primedos-bot >/dev/null 2>&1 && ! pgrep -x primedos >/dev/null 2>&1; then
		break
	fi
	i=$((i + 1))
	sleep 0.2
done

pkill -9 -x primedos-bot 2>/dev/null || true
pkill -9 -x primedos 2>/dev/null || true

rm -f "${XDG_RUNTIME_DIR:-/tmp}/primedos.sock"
echo "остановлено"
