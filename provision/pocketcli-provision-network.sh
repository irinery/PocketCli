#! /usr/bin/env sh

POCKETCLI_DIR="${HOME}/.pocketcli"
. "${POCKETCLI_DIR}/lib/common.sh"

tailscale_status_json | jq -r '.Peer[].HostName' | while IFS= read -r HOST
do

	SAFE_HOST=$(printf "%s" "$HOST" | tr -cd '[:alnum:].-')

	echo "Provisioning $SAFE_HOST"

	sh ~/.pocketcli/repo/provision/pocketcli-provision-host.sh "$SAFE_HOST"

done
