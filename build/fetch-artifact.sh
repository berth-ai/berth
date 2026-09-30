#!/bin/sh
# fetch-artifact URL SHA256 DEST: download URL with retries and keep it only
# if its sha256 matches. Used at build time only.
set -eu
url=$1 sha=$2 dest=$3
curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 -o "$dest" "$url"
if ! echo "$sha  $dest" | sha256sum -c - >/dev/null 2>&1; then
  echo "fetch-artifact: checksum mismatch for $url (update the SHA256 ARG next to its version)" >&2
  rm -f "$dest"
  exit 1
fi
