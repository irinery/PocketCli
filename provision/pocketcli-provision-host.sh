#! /usr/bin/env sh
HOST="$1"

if [ -z "$HOST" ]
then
	echo "usage: pocketcli-provision-host <host>"
	exit 1
fi

echo "Provisioning $HOST"

ssh "$HOST" 'sh -s' <<'EOF'

if ! command -v docker >/dev/null 2>&1
then
curl -fsSL https://get.docker.com | sh
fi

if ! command -v tailscale >/dev/null 2>&1
then
curl -fsSL https://tailscale.com/install.sh | sh
fi

mkdir -p ~/.pocketcli

EOF

