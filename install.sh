#!/usr/bin/env bash
# blo (billing-on CLI) installer
#
#   curl -fsSL https://raw.githubusercontent.com/kathar0s/blo-cli/main/install.sh | bash
#
# Downloads the latest pre-built binary for your OS/arch from GitHub Releases
# and installs it to ~/.local/bin (override with BLO_INSTALL_DIR).
set -euo pipefail

REPO="kathar0s/blo-cli"
BIN="blo"
INSTALL_DIR="${BLO_INSTALL_DIR:-$HOME/.local/bin}"

err() { echo "오류: $*" >&2; exit 1; }
info() { echo "==> $*"; }

# Detect OS
os="$(uname -s)"
case "$os" in
  Darwin) OS="macOS" ;;
  Linux)  OS="linux" ;;
  *) err "지원하지 않는 OS: $os (수동 설치는 README 참고)" ;;
esac

# Detect arch
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) err "지원하지 않는 아키텍처: $arch" ;;
esac

# Resolve latest version tag
info "최신 릴리스 확인 중..."
TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"\([^"]*\)"$/\1/')"
[ -n "${TAG:-}" ] || err "최신 릴리스를 찾을 수 없습니다. https://github.com/${REPO}/releases 에서 수동 설치하세요."
VERSION="${TAG#v}"

ARCHIVE="blo_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

info "다운로드: ${ARCHIVE}"
curl -fsSL "$URL" -o "$TMP/$ARCHIVE" || err "다운로드 실패: $URL"

info "압축 해제..."
tar -xzf "$TMP/$ARCHIVE" -C "$TMP"

mkdir -p "$INSTALL_DIR"
install -m 0755 "$TMP/$BIN" "$INSTALL_DIR/$BIN"

info "설치 완료: $INSTALL_DIR/$BIN ($TAG)"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo ""; echo "주의: $INSTALL_DIR 가 PATH에 없습니다. 셸 설정에 추가하세요:";
     echo "  export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
echo ""
echo "시작하기:  $BIN auth login"
