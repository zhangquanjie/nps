#!/usr/bin/env bash
set -euo pipefail

group=${1:?Usage: package-binaries.sh PLATFORM_GROUP OUTPUT_DIR}
output=${2:?Usage: package-binaries.sh PLATFORM_GROUP OUTPUT_DIR}
os=${group%%-*}

case "$group" in
    windows) arches=(386 amd64 arm64) ;;
    linux) arches=(386 amd64 arm64 arm_v5 arm_v6 arm_v7) ;;
    linux-mips) arches=(mips mipsle mips64 mips64le) ;;
    linux-extra) arches=(riscv64 loong64 ppc64 ppc64le s390x) ;;
    darwin) arches=(amd64 arm64) ;;
    freebsd) arches=(386 amd64 arm arm64) ;;
    *) echo "Unsupported platform group: $group" >&2; exit 1 ;;
esac

mkdir -p "$output"
output=$(cd "$output" && pwd)
cd "$(dirname "$0")/.."
source_dir=$(pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

export GOOS="$os" CGO_ENABLED=0
suffix=
if [[ "$os" == windows ]]; then
    suffix=.exe
fi

for platform in "${arches[@]}"; do
    export GOARCH="${platform%%_*}"
    unset GOARM
    if [[ "$platform" == arm_v* ]]; then
        export GOARM="${platform#arm_v}"
    fi
    echo "Building $os/$platform"
    go build -mod=readonly -trimpath -ldflags '-s -w' -o "$work/nps$suffix" ./cmd/nps
    go build -mod=readonly -trimpath -ldflags '-s -w' -o "$work/npc$suffix" ./cmd/npc

    # Database files are empty on new installs.
    mkdir -p "$work/conf"
    touch "$work/conf/"{clients,hosts,tasks}.json
    tar -czf "$output/${os}_${platform}_server.tar.gz" \
        -C "$work" "nps$suffix" conf/clients.json conf/hosts.json conf/tasks.json \
        -C "$source_dir" conf/nps.conf conf/server.pem conf/server.key web/views web/static LICENSE README_zh.md
    tar -czf "$output/${os}_${platform}_client.tar.gz" \
        -C "$work" "npc$suffix" \
        -C "$source_dir" conf/npc.conf conf/multi_account.conf LICENSE README_zh.md
done
