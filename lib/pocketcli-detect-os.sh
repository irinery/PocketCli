#! /usr/bin/env sh
OS=$(uname)

case "$OS" in

	Linux)
		echo "linux"
		;;

	Darwin)
		echo "macos"
		;;

	*)
		echo "unknown"
		;;

esac

