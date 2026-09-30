#!/bin/sh
# mise-wrappers OUT_DIR BIN_DIR...: write an exec wrapper in OUT_DIR for every
# executable in the BIN_DIRs. Wrappers, not symlinks, so tools that locate
# their install through $0 keep working. A name provided twice fails.
set -eu
out=$1
shift
mkdir -p "$out"
for dir in "$@"; do
  for f in "$dir"/*; do
    [ -f "$f" ] && [ -x "$f" ] || continue
    name=${f##*/}
    if [ -e "$out/$name" ]; then
      echo "mise-wrappers: $name is provided by more than one tool (second: $f)" >&2
      exit 1
    fi
    printf '#!/bin/sh\nexec "%s" "$@"\n' "$f" >"$out/$name"
    chmod 0755 "$out/$name"
  done
done
