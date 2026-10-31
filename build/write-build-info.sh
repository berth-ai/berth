#!/bin/sh
# write-build-info OUT VERSION COMMIT ARCH BASE_DIGEST TTYD_VERSION CODE_SERVER_VERSION SEED_LOCK ORCA_VERSION
# ORCA_VERSION is "none" in an image without Orca.
set -eu
seed_hash=$(sha256sum "$8" | cut -c1-12)
jq -n --arg version "$2" --arg commit "$3" --arg arch "$4" --arg base_digest "$5" \
  --arg ttyd_version "$6" --arg code_server_version "$7" --arg orca_version "$9" --arg seed_hash "$seed_hash" \
  '$ARGS.named' >"$1"
