#! /usr/bin/env sh
HOST=$(hostname)

CPU=$(top -bn1 | grep "Cpu" | awk '{print $2+$4}')

MEM=$(free -m | awk '/Mem:/ {print $3}')

DISK=$(df -h / | awk 'NR==2 {print $5}')

UPTIME=$(uptime -p)

mkdir -p ~/.pocketcli/radar

JSON=$(printf '{"host":"%s","cpu":"%s","mem":"%s","disk":"%s","uptime":"%s"}' \
	"$HOST" "$CPU" "$MEM" "$DISK" "$UPTIME")

echo "$JSON" > ~/.pocketcli/radar/status.json

