#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  fio-pathdb-disk-test.sh TEST_DIRECTORY

Example:
  SIZE_GIB=64 RUNTIME=45 ./fio-pathdb-disk-test.sh /data/fio-test

The script creates and removes one temporary test file. It never writes to a
raw block device. Stop pathdb-migrate and other disk-heavy jobs before running.
EOF
}

if [[ $# -ne 1 || "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  [[ $# -eq 1 ]] && exit 0
  exit 2
fi

for command_name in fio jq df awk nproc mktemp pgrep readlink; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "Missing command: $command_name" >&2
    echo "On Ubuntu: sudo apt-get install -y fio jq" >&2
    exit 1
  fi
done

if [[ "$(fio --version 2>/dev/null || true)" != fio-* ]]; then
  echo "The installed 'fio' command is not Flexible I/O Tester." >&2
  exit 1
fi

TEST_DIR="$(readlink -f "$1")"
SIZE_GIB="${SIZE_GIB:-64}"
RUNTIME="${RUNTIME:-45}"
MIGRATION_JOBS="${MIGRATION_JOBS:-3}"
MAX_JOBS="${MAX_JOBS:-8}"
MAX_IODEPTH="${MAX_IODEPTH:-32}"

for value_name in SIZE_GIB RUNTIME MIGRATION_JOBS MAX_JOBS MAX_IODEPTH; do
  value="${!value_name}"
  if [[ ! "$value" =~ ^[1-9][0-9]*$ ]]; then
    echo "$value_name must be a positive integer, got: $value" >&2
    exit 2
  fi
done

if [[ ! -d "$TEST_DIR" || ! -w "$TEST_DIR" ]]; then
  echo "Test directory must exist and be writable: $TEST_DIR" >&2
  exit 1
fi

if pgrep -f '[p]athdb-migrate' >/dev/null 2>&1; then
  echo "pathdb-migrate is running. Stop it before benchmarking." >&2
  exit 1
fi

cpu_count="$(nproc)"
if (( MAX_JOBS > cpu_count )); then
  MAX_JOBS="$cpu_count"
fi

available_bytes="$(df -PB1 "$TEST_DIR" | awk 'NR == 2 {print $4}')"
required_bytes=$(( (SIZE_GIB + 2) * 1024 * 1024 * 1024 ))
if (( available_bytes < required_bytes )); then
  echo "Not enough free space. Need at least $((SIZE_GIB + 2)) GiB." >&2
  exit 1
fi

TEST_FILE="$(mktemp "$TEST_DIR/.fio-pathdb-test.XXXXXX")"
RESULT_DIR="$(mktemp -d /tmp/fio-pathdb-results.XXXXXX)"

cleanup() {
  rm -f -- "$TEST_FILE"
  rm -rf -- "$RESULT_DIR"
}
trap cleanup EXIT INT TERM

echo "Target filesystem:"
df -hT "$TEST_DIR"
echo
echo "This writes ${SIZE_GIB} GiB to a temporary file and then saturates reads."
echo "AutoPL burst I/O may incur charges and other workloads may become slow."
read -r -p "Type YES to continue: " answer
if [[ "$answer" != "YES" ]]; then
  echo "Cancelled."
  exit 1
fi

echo
echo "Preparing a real ${SIZE_GIB} GiB test file..."
fio \
  --name=prepare \
  --filename="$TEST_FILE" \
  --ioengine=libaio \
  --direct=1 \
  --rw=write \
  --bs=1M \
  --size="${SIZE_GIB}G" \
  --iodepth=32 \
  --numjobs=1 \
  --refill_buffers=1 \
  --end_fsync=1 \
  --group_reporting \
  --output="$RESULT_DIR/prepare.txt"

sync

summarize() {
  local name="$1"
  local result_file="$2"

  jq -r --arg name "$name" '
    ([.jobs[].read.iops] | add) as $iops |
    ([.jobs[].read.bw_bytes] | add) as $bytes |
    ([.jobs[].read.clat_ns.mean] | add / length) as $mean_ns |
    ([.jobs[].read.clat_ns.percentile["99.000000"]] | max) as $p99_ns |
    "\($name): IOPS=\($iops | floor) MB/s=\((($bytes / 1048576) * 10 | round) / 10) mean_us=\($mean_ns / 1000 | floor) p99_us=\($p99_ns / 1000 | floor)"
  ' "$result_file"
}

run_read_test() {
  local name="$1"
  local pattern="$2"
  local block_size="$3"
  local jobs="$4"
  local depth="$5"
  local result_file="$RESULT_DIR/$name.json"

  fio \
    --name="$name" \
    --filename="$TEST_FILE" \
    --readonly \
    --ioengine=libaio \
    --direct=1 \
    --rw="$pattern" \
    --bs="$block_size" \
    --size="${SIZE_GIB}G" \
    --numjobs="$jobs" \
    --iodepth="$depth" \
    --time_based \
    --runtime="$RUNTIME" \
    --ramp_time=5 \
    --randrepeat=0 \
    --norandommap=1 \
    --clat_percentiles=1 \
    --percentile_list=99 \
    --group_reporting \
    --output-format=json \
    --output="$result_file"

  summarize "$name" "$result_file"
}

echo
echo "Running read-only tests..."
run_read_test "migration_like_8k" randread 8k "$MIGRATION_JOBS" 1
run_read_test "max_random_8k" randread 8k "$MAX_JOBS" "$MAX_IODEPTH"
run_read_test "max_sequential_1m" read 1M 1 32

echo
echo "Interpretation:"
echo "- migration_like_8k approximates ${MIGRATION_JOBS} blocking migration workers."
echo "- max_random_8k estimates the disk/instance random-read ceiling."
echo "- max_sequential_1m estimates the throughput ceiling."
echo "- A large gap between migration_like and max_random means migration concurrency,"
echo "  memory, or its dependency chain is limiting speed rather than AutoPL."
