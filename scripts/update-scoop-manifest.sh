#!/usr/bin/env sh

set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <bucket-directory> <version>" >&2
  exit 2
fi

bucket_dir="$1"
version="$2"
repository="flora-suite/flora-agent"
tag="v$version"
base_url="https://github.com/$repository/releases/download/$tag"
checksums="$(mktemp)"
trap 'rm -f "$checksums"' EXIT HUP INT TERM

curl -fsSL "$base_url/checksums.txt" -o "$checksums"

sha_for() {
  awk -v name="$1" '$2 == name { print $1 }' "$checksums"
}

amd64="flora-agent_${version}_windows_amd64.zip"
arm64="flora-agent_${version}_windows_arm64.zip"
[ -n "$(sha_for "$amd64")" ] || { echo "missing checksum for $amd64" >&2; exit 1; }
[ -n "$(sha_for "$arm64")" ] || { echo "missing checksum for $arm64" >&2; exit 1; }

mkdir -p "$bucket_dir/bucket"
cat > "$bucket_dir/bucket/flora-agent.json" <<EOF
{
  "version": "$version",
  "description": "Edge agent for syncing ROS recording files to Flora",
  "homepage": "https://github.com/$repository",
  "license": "MIT",
  "architecture": {
    "64bit": {
      "url": "$base_url/$amd64",
      "hash": "$(sha_for "$amd64")"
    },
    "arm64": {
      "url": "$base_url/$arm64",
      "hash": "$(sha_for "$arm64")"
    }
  },
  "bin": "flora-agent.exe"
}
EOF
