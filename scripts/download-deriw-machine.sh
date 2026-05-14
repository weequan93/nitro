#!/usr/bin/env bash
set -e

mkdir "$2"
ln -sfT "$2" latest
cd "$2"
echo "$2" > module-root.txt
url_base="https://arbitrum-nitro.s3.ap-southeast-1.amazonaws.com/$2"
wget "$url_base/machine.wavm.br"

status_code="$(curl -LI "$url_base/replay.wasm" -so /dev/null -w '%{http_code}')"
if [ "$status_code" -ne 404 ]; then
	wget "$url_base/replay.wasm"
fi
