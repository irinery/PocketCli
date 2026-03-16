#! /usr/bin/env sh
echo ""
echo "PocketCli Network Scan"
echo ""

tailscale status --json | jq -r '.Peer[].HostName' | while read HOST
do

	SAFE_HOST=$(printf "%s" "$HOST" | tr -cd '[:alnum:].-')

	echo "Host: $SAFE_HOST"

	if ssh -o ConnectTimeout=3 "$SAFE_HOST" "command -v docker" >/dev/null 2>&1
	then

		echo "  docker containers:"

		ssh "$SAFE_HOST" "docker ps --format '{{.Names}}'"

	fi

	echo ""

done
