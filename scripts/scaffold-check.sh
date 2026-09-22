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
  # podman wait/podman logs on the wire probe (below) are unguarded simple
  # commands: under set -e a non-zero exit there jumps straight here before
  # the pod is torn down. Tear it down first, using the wire.yaml manifest
  # while it still exists (rm -rf "$tmp" below would otherwise delete it out
  # from under a later `kube down`), then force-remove the pod by its fixed
  # name as a belt-and-braces step for a kube play that failed half-way and
  # left a pod `kube down` cannot map from a partial or missing manifest.
  if [ -f "$tmp/wire.yaml" ]; then
    podman kube down "$tmp/wire.yaml" >/dev/null 2>&1 || true
  fi
  podman pod rm --force lclaw-wire-test >/dev/null 2>&1 || true
  if [ -s "$secrets" ]; then
    for name in $(awk '$1 == "name:" { print $2 }' "$secrets"); do
      podman secret rm "$name" >/dev/null 2>&1 || true
    done
  fi
  podman secret rm lclaw-wire-test >/dev/null 2>&1 || true
  podman volume rm --force lclaw-wire-test >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

echo "==> podman version"
podman --version

# lclaw init's keychain step shells out to macOS's /usr/bin/security to
# create the keychain named in lclaw.toml. This runner has no `security`,
# so the keychain state comes back "failed" and init exits 1 even though
# every file was written. Both calls are run with --output json and
# checked instead of trusted to exit 0: no file may fail, and a non-zero
# exit must be explained by exactly that keychain failure, nothing else.
check_init_json() {
  json=$1
  code=$2
  want_files=$3

  if [ ! -s "$json" ]; then
    echo "lclaw init produced no output" >&2
    exit 1
  fi

  if ! grep -q '"failed": \[\]' "$json"; then
    echo "lclaw init reported a failed file:" >&2
    cat "$json" >&2
    exit 1
  fi

  if [ "$want_files" = yes ]; then
    if grep -q '"written": \[\]' "$json"; then
      echo "lclaw init wrote no files:" >&2
      cat "$json" >&2
      exit 1
    fi
  elif ! grep -q '"written": \[\]' "$json"; then
    echo "lclaw init wrote files it should have skipped:" >&2
    cat "$json" >&2
    exit 1
  fi

  if [ "$code" -ne 0 ] && ! grep -q '"state": "failed"' "$json"; then
    echo "lclaw init exited $code for a reason other than the keychain step:" >&2
    cat "$json" >&2
    exit 1
  fi
}

echo "==> lclaw init (files written; the keychain step needs macOS's security tool and is expected to fail here)"
code=0
"$lclaw" --output json init --dir "$dir" > "$tmp/first.json" || code=$?
check_init_json "$tmp/first.json" "$code" yes

echo "==> lclaw init again (every file skipped; the keychain step is expected to fail here too)"
code=0
"$lclaw" --output json init --dir "$dir" > "$tmp/second.json" || code=$?
check_init_json "$tmp/second.json" "$code" no

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

echo "==> secret wire contract"
# lclaw stores each value as a Kubernetes Secret object through
# `podman secret create`, because kube play unmarshals a secret as JSON and
# then YAML and reads the requested key from its data map. This proves the
# exact body lclaw sends is consumable both as an environment variable and
# as a file, and that a secret volume is a named volume carrying the
# secret's name, which is what `lclaw down` removes.
wire=lclaw-wire-test
# Remove any leftover first rather than passing --replace: on Podman 4.9,
# which is what the Ubuntu runner ships, --replace deletes unconditionally
# and fails with "deleting secret : : no secret data with ID" when the
# secret is not already there.
podman secret rm "$wire" >/dev/null 2>&1 || true
printf '{"apiVersion":"v1","kind":"Secret","metadata":{"name":"%s"},"type":"Opaque","data":{"value":"%s"}}' \
  "$wire" "$(printf 'wire-contract-ok' | base64)" |
  podman secret create --label app.kubernetes.io/part-of=localclaw "$wire" -

cat > "$tmp/wire.yaml" <<YAML
apiVersion: v1
kind: Pod
metadata:
  name: $wire
spec:
  restartPolicy: Never
  containers:
    - name: probe
      # busybox:1.38.0, resolved with skopeo (no podman machine) on
      # 2026-09-21; the manifest list covers linux/amd64 and linux/arm64,
      # among other architectures.
      image: docker.io/library/busybox:1.38.0@sha256:dc2d74b28e4cf8984fa52af1f39bc7c3d9c73760b41a74d629f5d11b1ab28616
      command: ["sh", "-c", "printf '%s|%s\n' \"\$WIRE_ENV\" \"\$(cat /run/secrets/$wire/value)\""]
      env:
        - name: WIRE_ENV
          valueFrom:
            secretKeyRef:
              name: $wire
              key: value
      volumeMounts:
        - name: wire
          mountPath: /run/secrets/$wire
  volumes:
    - name: wire
      secret:
        secretName: $wire
YAML

podman kube play "$tmp/wire.yaml"
# Wait for the one-shot container to finish, then read what it printed.
podman wait "$wire-probe" >/dev/null
got=$(podman logs "$wire-probe" 2>/dev/null)
want='wire-contract-ok|wire-contract-ok'
if [ "$got" != "$want" ]; then
  echo "secret did not reach the container both ways: got '$got', want '$want'" >&2
  podman kube down "$tmp/wire.yaml" >/dev/null 2>&1 || true
  podman secret rm "$wire" >/dev/null 2>&1 || true
  exit 1
fi
echo "    env and file both read back the value"

# kube play turns a secret volume into a named volume with the secret's
# name. lclaw down relies on that when it removes the volume alongside the
# secret; if Podman ever changes it, this fails here rather than leaving a
# plaintext copy on a machine's disk.
if ! podman volume exists "$wire"; then
  echo "no volume named $wire: kube play no longer names a secret volume after its secret" >&2
  podman kube down "$tmp/wire.yaml" >/dev/null 2>&1 || true
  podman secret rm "$wire" >/dev/null 2>&1 || true
  exit 1
fi
echo "    a volume named after the secret exists"

podman kube down "$tmp/wire.yaml"
podman volume rm --force "$wire" >/dev/null 2>&1 || true
podman secret rm "$wire"
if podman secret exists "$wire"; then
  echo "secret $wire survived removal" >&2
  exit 1
fi
echo "    secret and volume removed"

echo "==> ok"
