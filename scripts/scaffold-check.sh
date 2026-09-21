#!/bin/sh
# Prove the default scaffold is valid Podman input, using a Linux host's own
# Podman with no machine involved. CI runs it on Ubuntu; it also works on any
# Linux box with rootless Podman:
#
#   ./scripts/scaffold-check.sh bin/lclaw
#
# It writes the scaffold with `lclaw init` into a temporary directory, checks
# that a second init skips everything, checks that doctor's scaffold and
# topology checks pass, builds every workload image, then plays and tears
# down every Pod file. Pod files reference secrets by name and kube play
# refuses a pod whose secret is missing, so placeholder secrets are created
# first, in the Kubernetes Secret form that secretKeyRef reads.
set -eu

lclaw=${1:?usage: scaffold-check.sh <path-to-lclaw>}

# Workloads whose pod cannot start on a plain runner, one per line as
# "<name> <reason>". Their images are still built; only kube play is skipped.
SKIP='
wireguard needs NET_ADMIN and the wireguard kernel module, which the runner lacks
'

skip_reason() {
  printf '%s\n' "$SKIP" | awk -v w="$1" '$1 == w { $1 = ""; sub(/^ /, ""); print; exit }'
}

tmp=$(mktemp -d)
dir="$tmp/lclaw"
secrets="$tmp/secrets.yaml"
cleanup() {
  if [ -s "$secrets" ]; then
    for name in $(awk '$1 == "name:" { print $2 }' "$secrets"); do
      podman secret rm "$name" >/dev/null 2>&1 || true
    done
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

echo "==> podman version"
podman --version

echo "==> lclaw init"
"$lclaw" init --dir "$dir"

echo "==> lclaw init again (every file skipped, exit 0)"
"$lclaw" --output json init --dir "$dir" > "$tmp/second.json"
if ! grep -q '"written": \[\]' "$tmp/second.json"; then
  echo "second init wrote files:" >&2
  cat "$tmp/second.json" >&2
  exit 1
fi

echo "==> lclaw doctor (scaffold and topology checks pass)"
# doctor exits 1 when the runner's Podman is older than the minimum; only
# the two checks that read the scaffold are asserted here.
"$lclaw" doctor --dir "$dir" > "$tmp/doctor.txt" || true
cat "$tmp/doctor.txt"
for check in scaffold topology; do
  if ! grep -Eq "^PASS  $check " "$tmp/doctor.txt"; then
    echo "doctor: the $check check did not pass" >&2
    exit 1
  fi
done

echo "==> placeholder secrets"
# One Secret document per name referenced by any secretKeyRef, with every
# key it is asked for set to base64("placeholder").
awk '
  /secretKeyRef:/ { want = 1; next }
  want && $1 == "name:" { name = $2; next }
  want && $1 == "key:" {
    want = 0
    if (!((name, $2) in seen)) {
      seen[name, $2] = 1
      keys[name] = keys[name] "  " $2 ": cGxhY2Vob2xkZXI=\n"
    }
  }
  END {
    for (name in keys)
      printf "---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\ndata:\n%s", name, keys[name]
  }
' "$dir"/workloads/*/pod.yaml > "$secrets"
if [ -s "$secrets" ]; then
  podman kube play "$secrets"
fi

for wdir in "$dir"/workloads/*/; do
  name=$(basename "$wdir")
  echo "==> $name: podman build"
  podman build --tag "localhost/lclaw/$name:latest" "$wdir"
  reason=$(skip_reason "$name")
  if [ -n "$reason" ]; then
    echo "==> $name: kube play skipped ($reason)"
    continue
  fi
  echo "==> $name: podman kube play --replace"
  podman kube play --replace "$wdir/pod.yaml"
  echo "==> $name: podman kube down"
  podman kube down "$wdir/pod.yaml"
done

echo "==> ok"
