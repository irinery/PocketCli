#!/usr/bin/env sh

set -e

echo "Installing PocketCli..."

install_pkg() {

if command -v apt >/dev/null 2>&1
then
sudo apt update
sudo apt install -y "$@"

elif command -v apk >/dev/null 2>&1
then
sudo apk add "$@"

elif command -v brew >/dev/null 2>&1
then
brew install "$@"

elif command -v dnf >/dev/null 2>&1
then
sudo dnf install -y "$@"

else
echo "unsupported package manager"
exit 1
fi
}

install_pkg git jq fzf openssh-client

mkdir -p ~/.pocketcli

if [ ! -d ~/.pocketcli/repo ]
then
git clone https://github.com/YOURORG/pocketcli ~/.pocketcli/repo
else
cd ~/.pocketcli/repo
git pull
fi

mkdir -p ~/.local/bin

ln -sf ~/.pocketcli/repo/bin/pocketcli ~/.local/bin/pocketcli

chmod +x ~/.pocketcli/repo/bin/pocketcli

echo ""
echo "PocketCli installed"
echo "Run:"
echo "pocketcli"
