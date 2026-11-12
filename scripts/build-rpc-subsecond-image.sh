#!/usr/bin/env bash
# Build a candidate on the node host. This script has no service activation.
set -euo pipefail
umask 077

if [[ "${1:-}" == --help ]]; then
  echo "Usage: bash build-rpc-subsecond-image.sh /data/nitro /data/rpc-subsecond-build.XXXXXX"
  echo "Resume verification: bash build-rpc-subsecond-image.sh --verify-only /data/rpc-subsecond-build.XXXXXX"
  exit 0
fi
[[ $# == 2 && $(uname -s) == Linux && $EUID == 0 ]] || {
  echo "Run as root on the Linux build host, with repository and fresh build-directory arguments." >&2
  exit 1
}
VERIFY_ONLY=0
REPO=
if [[ "$1" == --verify-only ]]; then VERIFY_ONLY=1; else REPO=$(realpath "$1"); fi
BUILD=$(realpath "$2")
[[ "$BUILD" == /data/rpc-subsecond-build.* && -d "$BUILD" ]] || {
  echo "Use a fresh /data/rpc-subsecond-build.* directory." >&2
  exit 1
}
if [[ $VERIFY_ONLY == 0 && -e "$BUILD/source" ]]; then
  echo "Build source already exists; use --verify-only for an assembled candidate." >&2
  exit 1
fi
exec 9>/run/lock/nitro-rpc-subsecond-build.lock
flock -n 9 || { echo "An RPC subsecond build is already running." >&2; exit 1; }

SOURCE_REV=b7df6d1a17d33f304bee1ac553e00e3e35a27b93
GETH_REV=bf399f01dffc34785f6f1653a3ddaeed30d9d5fb
EXPECTED_BASE=sha256:e27ef7fd4d2df39cd402c56d68d5a5d1010f2bd3954e994ece396bddf8d676af
SOURCE="$BUILD/source"
SUFFIX=${BUILD##*.}
WORKER="rpc-subsecond-worker-$SUFFIX"
BUILDER="rpc-subsecond-$SUFFIX"
BASE_TAG="deriw-rpc-subsecond-base:$SUFFIX"
IMAGE="deriw-rpc-subsecond:b7df6d1a1-$SUFFIX"
STAGE=preflight
WORKER_CREATED=0
BUILDER_CREATED=0

status() {
  python3 -B - "$BUILD" "$1" "$STAGE" "$IMAGE" "$SOURCE_REV" "$GETH_REV" "$EXPECTED_BASE" "$$" <<'PY'
from datetime import datetime, timezone
from pathlib import Path
import json, sys
root, state, stage, image, nitro, geth, base, pid = sys.argv[1:]
path = Path(root) / "status.json"
data = {"time_utc": datetime.now(timezone.utc).isoformat(), "state": state,
        "stage": stage, "pid": int(pid), "image": image, "source_revision": nitro,
        "geth_revision": geth, "base_image": base, "activated": False}
temp = path.with_suffix(".tmp")
temp.write_text(json.dumps(data, indent=2) + "\n")
temp.replace(path)
PY
}
verify_image() {
  STAGE=image_verification
  status running
  local candidate_id native_hash image_hash
  candidate_id=$(docker image inspect --format '{{.Id}}' "$IMAGE")
  grep -qxF 'RPC stage diagnostics including subsecond boundaries: PASS' "$BUILD/payload/checks.txt"
  timeout 30s docker run --rm --network none --cpus 1 --memory 1g \
    --entrypoint /usr/local/bin/nitro "$candidate_id" --version > "$BUILD/version.txt" 2>&1
  grep -F 'rpc-subsecond-b7df6d1a1' "$BUILD/version.txt"
  timeout 30s docker run --rm --network none --cpus 1 --memory 1g \
    --entrypoint /usr/local/bin/nitro "$candidate_id" --help > "$BUILD/help.txt" 2>&1
  for flag in persistent.pebble.compaction-output-bytes-per-second persistent.pebble.max-concurrent-compactions rpc.stage-diagnostics-threshold; do
    grep -qF -- "--$flag" "$BUILD/help.txt"
  done
  python3 -B - "$BASE" "$candidate_id" <<'PY'
import json, subprocess, sys
base, candidate = json.loads(subprocess.check_output(["docker", "image", "inspect", *sys.argv[1:]], text=True))
for key in ("Entrypoint", "Cmd", "Env", "User", "WorkingDir", "ExposedPorts", "Volumes", "StopSignal", "Healthcheck", "Shell"):
    if base["Config"].get(key) != candidate["Config"].get(key):
        raise SystemExit("Runtime image configuration differs: " + key)
old, new = base["RootFS"]["Layers"], candidate["RootFS"]["Layers"]
if new[:len(old)] != old or len(new) != len(old) + 1:
    raise SystemExit("Expected the original runtime layers plus one native-binary layer")
print("PASS: inherited runtime configuration and base layers")
PY
  native_hash=$(sha256sum "$BUILD/payload/nitro" | awk '{print $1}')
  image_hash=$(timeout 30s docker run --rm --network none --cpus 1 --memory 1g \
    --entrypoint /usr/bin/sha256sum "$candidate_id" /usr/local/bin/nitro | awk '{print $1}')
  [[ "$native_hash" == "$image_hash" ]] || { echo "Candidate binary differs from tested payload." >&2; return 1; }
  python3 -B "$SOURCE/scripts/check-main-runtime.py" --compactions 1 --baseline "$BUILD/runtime-before.json"
  [[ $(docker image inspect --format '{{.Id}}' "$IMAGE") == "$candidate_id" ]] || {
    echo "Candidate tag changed during verification." >&2; return 1;
  }
  sha256sum "$BUILD/payload/nitro" > "$BUILD/native.sha256"
  printf 'IMAGE_ID=%s\n' "$candidate_id" | tee "$BUILD/image-id.txt"
  printf '%s\n' "$IMAGE" > "$BUILD/image-tag.txt"
  status assembled_and_verified_not_deployed
  echo "IMAGE=$IMAGE"
  echo "BUILD=$BUILD"
  echo "STATE=assembled_and_verified_not_deployed"
}
cleanup() {
  result=$?
  trap - EXIT
  set +e
  if [[ $BUILDER_CREATED == 1 ]]; then docker buildx rm "$BUILDER"; fi
  if [[ $WORKER_CREATED == 1 ]]; then docker stop --time 30 "$WORKER"; fi
  if [[ $result != 0 ]]; then
    status failed
    echo "BUILD_FAILED stage=$STAGE log=$BUILD/build.log" >&2
  fi
  exit "$result"
}
if [[ $VERIFY_ONLY == 1 ]]; then
  # Preserve the first attempt's status and log before retrying only verification.
  python3 -B - "$BUILD" "$IMAGE" "$SOURCE_REV" "$GETH_REV" "$EXPECTED_BASE" <<'PY'
from pathlib import Path
import json, shutil, sys, time
root, image, nitro, geth, base = sys.argv[1:]
root = Path(root)
record = json.loads((root / "status.json").read_text())
if (record.get("stage") != "image_verification"
        or record.get("state") not in ("failed", "assembled_and_verified_not_deployed")
        or any(record.get(k) != v for k, v in
               (("image", image), ("source_revision", nitro), ("geth_revision", geth), ("base_image", base)))):
    raise SystemExit("Saved verification identity/stage differs; retain this directory for review.")
for name in ("runtime-before.json", "payload/nitro", "payload/checks.txt", "source/scripts/check-main-runtime.py"):
    if not (root / name).is_file():
        raise SystemExit("Missing verification input: " + name)
backup = root / ("status.before-verify-" + str(time.time_ns()) + ".json")
shutil.copyfile(root / "status.json", backup)
print("Previous status retained:", backup)
PY
  BASE=$EXPECTED_BASE
  trap cleanup EXIT
  verify_image
  exit 0
fi
trap cleanup EXIT
status running

BASE=$(docker inspect --format '{{.Image}}' main-nitro-1)
[[ "$BASE" == "$EXPECTED_BASE" ]] || { echo "Main image changed; review the new base before building." >&2; exit 1; }
[[ $(docker image inspect --format '{{.Architecture}}' "$BASE") == amd64 ]] || {
  echo "This build recipe requires linux/amd64." >&2; exit 1;
}
python3 -B - "$BUILD" <<'PY'
from pathlib import Path
import shutil, sys
available = next(int(s.split()[1]) * 1024 for s in Path("/proc/meminfo").read_text().splitlines()
                 if s.startswith("MemAvailable:"))
if available < 12 * 1024**3 or shutil.disk_usage(sys.argv[1]).free < 50 * 1024**3:
    raise SystemExit("Build requires 12 GiB available memory and 50 GiB free in the build directory.")
PY

STAGE=source_checkout
status running
git -C "$REPO" cat-file -e "$SOURCE_REV^{commit}"
git clone --shared --no-checkout "$REPO" "$SOURCE"
git -C "$SOURCE" remote set-url origin https://github.com/weequan93/nitro.git
git -C "$SOURCE" checkout --detach "$SOURCE_REV"
git -C "$SOURCE" submodule update --init --recursive --jobs 2
[[ $(git -C "$SOURCE/go-ethereum" rev-parse HEAD) == "$GETH_REV" ]]
python3 -B "$SOURCE/scripts/check-main-runtime.py" --compactions 1 --save "$BUILD/runtime-before.json"

cat "$SOURCE/Dockerfile" > "$BUILD/Dockerfile.native"
cat >> "$BUILD/Dockerfile.native" <<'DOCKER'

FROM node-builder-base AS rpc-subsecond-checked
COPY --from=prover-export / target/
ENV GOMAXPROCS=2 GOFLAGS=-p=2
RUN NITRO_BUILD_IGNORE_TIMESTAMPS=1 make target/bin/nitro
RUN cd /workspace/go-ethereum && GOMAXPROCS=2 go test -p 2 -parallel 2 -mod=readonly -race -count=1 ./rpc -run '^TestStageDiagnostics' && printf '%s\n' 'RPC stage diagnostics including subsecond boundaries: PASS' > /workspace/rpc-subsecond-checks.txt
FROM scratch AS rpc-subsecond-export
COPY --from=rpc-subsecond-checked /workspace/target/bin/nitro /nitro
COPY --from=rpc-subsecond-checked /workspace/rpc-subsecond-checks.txt /checks.txt
DOCKER
mkdir "$BUILD/buildkit" "$BUILD/socket" "$BUILD/payload"
printf '[worker.oci]\n  max-parallelism = 1\n' > "$BUILD/buildkitd.toml"

STAGE=native_build_and_tests
status running
# Keep the large build cache on /data and cap the worker at 2 CPUs / 10 GiB.
docker run -d --privileged --name "$WORKER" --cpus 2 --memory 10g --memory-swap 10g \
  --mount "type=bind,src=$BUILD/buildkit,dst=/var/lib/buildkit" \
  --mount "type=bind,src=$BUILD/socket,dst=/run/buildkit" \
  --mount "type=bind,src=$BUILD/buildkitd.toml,dst=/etc/buildkit/buildkitd.toml,readonly" \
  moby/buildkit:v0.24.0 --config /etc/buildkit/buildkitd.toml
WORKER_CREATED=1
for attempt in $(seq 1 60); do
  [[ -S "$BUILD/socket/buildkitd.sock" ]] && break
  [[ $(docker inspect --format '{{.State.Running}}' "$WORKER") == true ]] || {
    docker logs --tail 30 "$WORKER"; exit 1;
  }
  sleep 1
done
[[ -S "$BUILD/socket/buildkitd.sock" ]]
docker buildx create --name "$BUILDER" --driver remote "unix://$BUILD/socket/buildkitd.sock"
BUILDER_CREATED=1
docker buildx inspect --bootstrap "$BUILDER"
docker buildx build --builder "$BUILDER" --platform linux/amd64 --progress plain \
  --provenance=false --target rpc-subsecond-export \
  --build-arg version=rpc-subsecond-b7df6d1a1 --build-arg modified=false \
  --build-arg "datetime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -f "$BUILD/Dockerfile.native" --output "type=local,dest=$BUILD/payload" "$SOURCE"
docker buildx rm "$BUILDER"
BUILDER_CREATED=0
docker stop --time 30 "$WORKER"
WORKER_CREATED=0

STAGE=image_assembly
status running
docker image tag "$BASE" "$BASE_TAG"
cat > "$BUILD/payload/Dockerfile" <<'DOCKER'
ARG RUNTIME_BASE
FROM ${RUNTIME_BASE}
COPY --chmod=755 nitro /usr/local/bin/nitro
LABEL org.opencontainers.image.revision="b7df6d1a17d33f304bee1ac553e00e3e35a27b93"
DOCKER
printf '*\n!Dockerfile\n!nitro\n' > "$BUILD/payload/.dockerignore"
docker buildx build --builder default --load --provenance=false --pull=false --network=none --build-arg "RUNTIME_BASE=$BASE_TAG" \
  -t "$IMAGE" "$BUILD/payload"

verify_image
