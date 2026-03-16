#! /usr/bin/env sh
BASE="$HOME/.pocketcli/repo"

tailscale status --json | jq -r '.Peer[].HostName' | while read HOST
do

	SAFE_HOST=$(printf "%s" "$HOST" | tr -cd '[:alnum:].-')

	echo "Checking $SAFE_HOST"

	if ssh -o ConnectTimeout=3 "$SAFE_HOST" "echo ok" >/dev/null 2>&1
	then

		echo "Installing PocketCli agent on $SAFE_HOST"

		scp "$BASE/radar/pocketcli-agent.sh" \
			"$SAFE_HOST:~/.pocketcli-agent.sh"

		ssh "$SAFE_HOST" "chmod +x ~/.pocketcli-agent.sh"

		ssh "$SAFE_HOST" \
			'(crontab -l 2>/dev/null; echo "*/2 * * * * ~/.pocketcli-agent.sh") | crontab -'

fi
done
