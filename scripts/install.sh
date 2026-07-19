#!/usr/bin/env sh

set -eu

REPOSITORY="flora-suite/flora-agent"
BINARY_NAME="flora-agent"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

fail() {
  printf '%s\n' "error: $*" >&2
  exit 1
}

detect_platform() {
  case "$(uname -s)" in
    Linux) os="linux" ;;
    Darwin) os="darwin" ;;
    *) fail "unsupported operating system: $(uname -s)" ;;
  esac

  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
}

verify_checksum() {
  expected="$1"
  file="$2"
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$file" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$file" | awk '{print $1}')"
  else
    fail "sha256sum or shasum is required to verify the download"
  fi
  [ "$expected" = "$actual" ] || fail "checksum verification failed"
}

detect_platform
command -v curl >/dev/null 2>&1 || fail "curl is required"

api_url="https://api.github.com/repos/$REPOSITORY/releases/latest"
tag="$(curl -fsSL "$api_url" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
[ -n "$tag" ] || fail "could not determine the latest release"

version="${tag#v}"
archive="${BINARY_NAME}_${version}_${os}_${arch}.tar.gz"
base_url="https://github.com/$REPOSITORY/releases/download/$tag"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM

printf 'Installing %s %s for %s/%s...\n' "$BINARY_NAME" "$tag" "$os" "$arch"
curl -fsSL "$base_url/$archive" -o "$tmpdir/$archive"
curl -fsSL "$base_url/checksums.txt" -o "$tmpdir/checksums.txt"

expected="$(awk -v name="$archive" '$2 == name { print $1 }' "$tmpdir/checksums.txt")"
[ -n "$expected" ] || fail "checksum for $archive is missing"
verify_checksum "$expected" "$tmpdir/$archive"

tar -xzf "$tmpdir/$archive" -C "$tmpdir"
mkdir -p "$INSTALL_DIR"
install -m 755 "$tmpdir/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"

printf 'Installed %s to %s/%s\n' "$BINARY_NAME" "$INSTALL_DIR" "$BINARY_NAME"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) printf 'Add %s to PATH to run it from any shell.\n' "$INSTALL_DIR" ;;
esac
