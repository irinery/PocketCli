#! /usr/bin/env sh

tailscale status --json | jq -r '.Peer[].HostName' | while read HOST
do

	SAFE_HOST=$(printf "%s" "$HOST" | tr -cd '[:alnum:].-')

	echo "Provisioning $SAFE_HOST"

	sh ~/.pocketcli/repo/provision/pocketcli-provision-host.sh "$SAFE_HOST"

done
