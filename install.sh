#!/bin/sh
# ccpane installer for macOS / Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/hassan-alachek/ccpane/main/install.sh | sh
#
# Env overrides:
#   CCPANE_REPO         owner/repo            (default: hassan-alachek/ccpane)
#   CCPANE_VERSION      vX.Y.Z               (default: latest release)
#   CCPANE_INSTALL_DIR  install directory    (default: /usr/local/bin or ~/.local/bin)
set -e

REPO="${CCPANE_REPO:-hassan-alachek/ccpane}"
BIN="ccpane"

info() { printf '\033[36m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[33mwarn:\033[0m %s\n' "$1" >&2; }
err()  { printf '\033[31merror:\033[0m %s\n' "$1" >&2; exit 1; }

# --- detect platform ---
os=$(uname -s 2>/dev/null || echo unknown)
arch=$(uname -m 2>/dev/null || echo unknown)
case "$os" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  *) err "unsupported OS '$os' — on Windows use install.ps1" ;;
esac
case "$arch" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) err "unsupported architecture: $arch" ;;
esac

asset="${BIN}_${os}_${arch}.tar.gz"
releases="https://github.com/${REPO}/releases"
if [ -n "${CCPANE_VERSION:-}" ]; then
  path="download/${CCPANE_VERSION}"
else
  path="latest/download"
fi
url="${releases}/${path}/${asset}"

# --- downloader (curl or wget) ---
dl() { # <url> <out>
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else err "need curl or wget installed"; fi
}

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t ccpane)
trap 'rm -rf "$tmp"' EXIT INT TERM

info "downloading ${asset}"
dl "$url" "$tmp/$asset" || err "download failed: $url"

# --- verify checksum (best effort) ---
if dl "${releases}/${path}/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
  want=$(grep " ${asset}$" "$tmp/checksums.txt" 2>/dev/null | awk '{print $1}' | head -1)
  if [ -n "$want" ]; then
    if command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$tmp/$asset" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then got=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
    else got=""; fi
    if [ -n "$got" ] && [ "$got" != "$want" ]; then err "checksum mismatch (expected $want, got $got)"; fi
    [ -n "$got" ] && info "checksum verified"
  fi
fi

# --- extract ---
tar -xzf "$tmp/$asset" -C "$tmp" || err "failed to extract archive"
[ -f "$tmp/$BIN" ] || err "binary '$BIN' not found in archive"
chmod +x "$tmp/$BIN"

# --- choose install dir ---
if [ -n "${CCPANE_INSTALL_DIR:-}" ]; then dir="$CCPANE_INSTALL_DIR"
elif [ "$(id -u 2>/dev/null)" = "0" ]; then dir="/usr/local/bin"
elif [ -w /usr/local/bin ]; then dir="/usr/local/bin"
else dir="$HOME/.local/bin"; fi
mkdir -p "$dir" || err "cannot create $dir"

info "installing to $dir/$BIN"
if command -v install >/dev/null 2>&1; then
  install -m 0755 "$tmp/$BIN" "$dir/$BIN" || err "install failed (try a writable CCPANE_INSTALL_DIR)"
else
  cp "$tmp/$BIN" "$dir/$BIN" && chmod 0755 "$dir/$BIN" || err "copy failed"
fi

# --- PATH hint ---
case ":$PATH:" in
  *":$dir:"*) : ;;
  *) warn "$dir is not on your PATH. Add this to your shell profile:"
     printf '    export PATH="%s:$PATH"\n' "$dir" ;;
esac

info "installed $("$dir/$BIN" -version 2>/dev/null || echo "$BIN")"
printf '\nRun \033[36mccpane\033[0m (live pane) or \033[36mccpane -b\033[0m (browse all sessions).\n'
