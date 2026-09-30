# shellcheck shell=bash
# Supervises the services started with start_service. Sourced by
# berth-entrypoint so that wait sees the services as its children.

BERTH_SIGNAL=""
BERTH_STOP_TIMEOUT=${BERTH_STOP_TIMEOUT:-10}

# shellcheck disable=SC2329,SC2317  # invoked from traps
on_signal() { BERTH_SIGNAL=$1; }

# supervise waits for the first service to exit, stops the rest and returns
# that service's status; on TERM/INT it stops everything and returns 128+n.
supervise() {
  local exited="" status=0
  trap 'on_signal TERM' TERM
  trap 'on_signal INT' INT
  wait -n -p exited "${BERTH_PIDS[@]}" || status=$?
  if [[ -n $BERTH_SIGNAL ]]; then
    log "received SIG$BERTH_SIGNAL; stopping services"
    stop_services ""
    [[ $BERTH_SIGNAL == TERM ]] && return 143
    return 130
  fi
  log "$(pid_name "$exited") (pid $exited) exited with status $status; stopping the other services"
  stop_services "$exited"
  return "$status"
}

# stop_services SKIP_PID sends TERM to every other service (background jobs
# ignore INT in a non-interactive shell), then KILLs whatever is left after
# BERTH_STOP_TIMEOUT seconds.
stop_services() {
  local skip=$1 pid watchdog
  local -a live=()
  for pid in "${BERTH_PIDS[@]}"; do
    [[ $pid == "$skip" ]] || live+=("$pid")
  done
  ((${#live[@]})) || return 0
  kill -s TERM "${live[@]}" 2>/dev/null || true
  (
    sleep "$BERTH_STOP_TIMEOUT"
    kill -s KILL "${live[@]}" 2>/dev/null
  ) &
  watchdog=$!
  wait "${live[@]}" 2>/dev/null || true
  kill "$watchdog" 2>/dev/null || true
}

pid_name() {
  local i
  for i in "${!BERTH_PIDS[@]}"; do
    if [[ ${BERTH_PIDS[i]} == "$1" ]]; then
      echo "${BERTH_PID_NAMES[i]}"
      return
    fi
  done
  echo "service"
}
