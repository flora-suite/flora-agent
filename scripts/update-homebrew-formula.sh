#!/usr/bin/env sh

set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <tap-directory> <version>" >&2
  exit 2
fi

tap_dir="$1"
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

darwin_amd64="flora-agent_${version}_darwin_amd64.tar.gz"
darwin_arm64="flora-agent_${version}_darwin_arm64.tar.gz"
linux_amd64="flora-agent_${version}_linux_amd64.tar.gz"
linux_arm64="flora-agent_${version}_linux_arm64.tar.gz"

for archive in "$darwin_amd64" "$darwin_arm64" "$linux_amd64" "$linux_arm64"; do
  [ -n "$(sha_for "$archive")" ] || { echo "missing checksum for $archive" >&2; exit 1; }
done

mkdir -p "$tap_dir/Formula"
cat > "$tap_dir/Formula/flora-agent.rb" <<EOF
class FloraAgent < Formula
  desc "Edge agent for syncing ROS recording files to Flora"
  homepage "https://github.com/$repository"
  version "$version"
  license "MIT"

  on_macos do
    if Hardware::CPU.intel?
      url "$base_url/$darwin_amd64"
      sha256 "$(sha_for "$darwin_amd64")"
    else
      url "$base_url/$darwin_arm64"
      sha256 "$(sha_for "$darwin_arm64")"
    end
  end

  on_linux do
    if Hardware::CPU.intel?
      url "$base_url/$linux_amd64"
      sha256 "$(sha_for "$linux_amd64")"
    else
      url "$base_url/$linux_arm64"
      sha256 "$(sha_for "$linux_arm64")"
    end
  end

  def install
    bin.install "flora-agent"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/flora-agent version")
  end
end
EOF
