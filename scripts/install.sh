#!/usr/bin/env bash
set -euo pipefail

REPO="${PRE_FLIGHT_REPO:-}"
VERSION="${PRE_FLIGHT_VERSION:-latest}"
DEST_DIR="${PRE_FLIGHT_INSTALL_DIR:-${HOME}/.local/bin}"
BINARY="tf-preflight"

install_from_source() {
  local src_dir="$1"
  echo "Installing ${BINARY} from source in ${src_dir}..."
  cd "${src_dir}"
  if [ ! -f "go.mod" ]; then
    echo "ERROR: not a Go repository (go.mod missing)" >&2
    exit 1
  fi
  if ! command -v go >/dev/null 2>&1; then
    echo "ERROR: Go is required for source installation (go not found)." >&2
    exit 1
  fi
  mkdir -p "${DEST_DIR}"
  local ldflags="${PRE_FLIGHT_LDFLAGS:--s -w}"
  local version="${PRE_FLIGHT_VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
  local commit="${PRE_FLIGHT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
  local build_date="${PRE_FLIGHT_BUILD_DATE:-$(date -u +\"%Y-%m-%dT%H:%M:%SZ\" 2>/dev/null || echo '')}"
  go build -ldflags "${ldflags} -X main.version=${version} -X main.gitCommit=${commit} -X main.buildDate=${build_date}" \
    -o "${DEST_DIR}/${BINARY}" ./cmd/preflight
  chmod +x "${DEST_DIR}/${BINARY}"
  echo "${BINARY} installed at ${DEST_DIR}/${BINARY}"
}

install_from_release() {
  local os arch platform archive_url
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"

  case "${arch}" in
    x86_64|amd64)
      arch="amd64"
      ;;
    arm64|aarch64)
      arch="arm64"
      ;;
    *)
      echo "ERROR: unsupported architecture '${arch}'" >&2
      exit 1
      ;;
  esac

  if [ "${os}" != "linux" ] && [ "${os}" != "darwin" ]; then
    echo "ERROR: unsupported OS '${os}' for release install" >&2
    exit 1
  fi

  platform="${os}-${arch}"
  archive_url="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}-${platform}.tar.gz"
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "${tmp_dir}"' EXIT

  echo "Downloading release from ${archive_url}"
  curl -fsSL "${archive_url}" -o "${tmp_dir}/${BINARY}.tar.gz"
  mkdir -p "${tmp_dir}/bin"
  tar -xzf "${tmp_dir}/${BINARY}.tar.gz" -C "${tmp_dir}/bin"
  mkdir -p "${DEST_DIR}"
  install -m 0755 "${tmp_dir}/bin/${BINARY}" "${DEST_DIR}/${BINARY}"
  echo "${BINARY} installed at ${DEST_DIR}/${BINARY}"
}

if [ -n "${REPO}" ]; then
  install_from_release
  exit 0
fi

if command -v git >/dev/null 2>&1 && git -C "$(pwd)" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  install_from_source "$(pwd)"
  exit 0
fi

if command -v git >/dev/null 2>&1 && [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ] && git -C "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  install_from_source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  exit 0
fi

echo "ERROR: unable to install." >&2
echo "  Run this script from a local clone, or set PRE_FLIGHT_REPO to enable release install." >&2
echo "  Example: PRE_FLIGHT_REPO=<owner>/<repo> bash <(curl -fsSL https://raw.githubusercontent.com/<owner>/<repo>/main/scripts/install.sh)" >&2
exit 1
