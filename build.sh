#!/bin/bash
sudo rm -rf out

VERSION=v1.29.0

if [ -f ./.config/registry ]; then
	REGISTRY=$(cat ./.config/registry)
else
	REGISTRY=devregistry.aldunelabs.com
fi

make -e REGISTRY=$REGISTRY -e TAG=$VERSION container-push-manifest
