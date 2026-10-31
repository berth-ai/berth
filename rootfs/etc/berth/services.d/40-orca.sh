# shellcheck shell=bash
# Orca's headless server on 6768, on every interface: the Orca desktop and
# mobile apps dial it directly through the published port, and each device
# needs a pairing link. Runs when frontends.orca.enabled is set. A link is a
# secret, so a normal start offers none; berth orca pair leaves a request in
# the run directory, and the next start sends Orca's output, link included,
# to a readiness file only agent can read instead of to the log.

orca_start() {
  local dir=${BERTH_ORCA_RUN_DIR:-/run/berth/orca} scope address
  local -a args=(serve --port 6768)
  export ORCA_TELEMETRY_DISABLED=1
  # Orca's profile lock lives in the home volume and outlives the container;
  # one Berth container is the only Orca using this home, so a stale lock is
  # safe to clear. Without a lock this exits 4 and does nothing.
  orca-ide serve --reclaim-stale-lock --yes >&2 || true
  if [[ -f $dir/pair-request.json ]]; then
    scope=$(jq -r '.scope // ""' "$dir/pair-request.json" 2>/dev/null) || scope=
    address=$(jq -r '.address // ""' "$dir/pair-request.json" 2>/dev/null) || address=
    rm -f "$dir/pair-request.json"
    if [[ $scope =~ ^(desktop|mobile)$ && -n $address && $address != -* ]]; then
      args+=(--pairing-address "$address" --json)
      [[ $scope == mobile ]] && args+=(--mobile-pairing)
      # Create the file 0600 in a subshell: Orca, and every terminal it
      # starts, keeps the service's own umask.
      rm -f "$dir/readiness.json"
      (umask 077 && : >"$dir/readiness.json")
      exec orca-ide "${args[@]}" >"$dir/readiness.json"
    fi
    echo "berth: ignoring an invalid Orca pairing request" >&2
  fi
  exec orca-ide "${args[@]}" --no-pairing
}

orca_health() { echo 6768; }
