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
docker_installer=$(mktemp) || exit 1
if curl -fsSL -o "$docker_installer" https://get.docker.com
then
sh "$docker_installer"
else
rm -f "$docker_installer"
exit 1
fi
rm -f "$docker_installer"
fi

if ! command -v tailscale >/dev/null 2>&1
then
tailscale_installer=$(mktemp) || exit 1
if curl -fsSL -o "$tailscale_installer" https://tailscale.com/install.sh
then
sh "$tailscale_installer"
else
rm -f "$tailscale_installer"
exit 1
fi
rm -f "$tailscale_installer"
fi

mkdir -p ~/.pocketcli

EOF
