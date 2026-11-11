#!/bin/sh
# Installs Forgeline on this machine: the binary, a data directory, a token, and (optionally) a
# systemd user service that keeps it running. Works from a clone of the repository or on its own:
#
#   git clone https://github.com/forgelinehq/forgeline && cd forgeline && ./install.sh
#   sh -c "$(curl -fsSL https://raw.githubusercontent.com/forgelinehq/forgeline/main/install.sh)"
#
# From a clone it builds the binary with Go (downloading the Go toolchain the repository asks for, with
# its checksum verified, when none is installed). On its own it downloads the latest release for this
# machine and checks it against the release's SHA256SUMS. It never needs root. Run it again to update.
#
# Options:
#   --dir DIR      where Forgeline lives (default ~/.forgeline: bin/, data/, env)
#   --listen ADDR  address the server listens on (default 127.0.0.1:8080)
#   --release      download the latest release even when run from a clone
#   --version TAG  download that release (for example v0.1.0) instead of the latest
#   --source       build from the clone (the default when run from one)
#   --service      install and start a systemd user service (Linux); otherwise it prints how to start
#   --no-start     only install; print nothing but where things are
#   -h, --help     this text
set -eu

REPO="forgelinehq/forgeline"
DIR="${FORGELINE_HOME:-$HOME/.forgeline}"
LISTEN="127.0.0.1:8080"
MODE=""
VERSION=""
SERVICE=no
START=yes

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --dir) DIR="$2"; shift ;;
    --dir=*) DIR="${1#--dir=}" ;;
    --listen) LISTEN="$2"; shift ;;
    --listen=*) LISTEN="${1#--listen=}" ;;
    --release) MODE=release ;;
    --source) MODE=source ;;
    --version) VERSION="$2"; MODE=release; shift ;;
    --version=*) VERSION="${1#--version=}"; MODE=release ;;
    --service) SERVICE=yes ;;
    --no-start) START=no ;;
    -h|--help) sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option $1 (try --help)" ;;
  esac
  shift
done

# Where the script is, when it is a file inside a clone of the repository.
here=""
case "$0" in
  */*) here=$(cd "$(dirname "$0")" && pwd) ;;
  -*|sh|bash|dash) here="" ;;
  *) here=$(pwd) ;;
esac
if [ -z "$MODE" ]; then
  if [ -n "$here" ] && [ -f "$here/go.mod" ] && [ -d "$here/cmd/forgeline" ]; then MODE=source; else MODE=release; fi
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in linux|darwin) ;; *) die "unsupported operating system $os (Linux and macOS are supported; on Windows use WSL2)" ;; esac
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "unsupported architecture $arch" ;;
esac

need curl
mkdir -p "$DIR/bin" "$DIR/data"
chmod 700 "$DIR/data"
work=$(mktemp -d "${TMPDIR:-/tmp}/forgeline-install.XXXXXX")
trap 'rm -rf "$work"' EXIT INT TERM

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | cut -d' ' -f1
  else die "sha256sum or shasum is required to verify downloads"; fi
}

install_release() {
  if [ -z "$VERSION" ]; then
    VERSION=$(curl -fsSL -H 'Accept: application/vnd.github+json' "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/^ *"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
    [ -n "$VERSION" ] || die "could not find the latest release of $REPO"
  fi
  base="https://github.com/$REPO/releases/download/$VERSION"
  file="forgeline_${VERSION}_${os}_${arch}"
  say "Downloading Forgeline $VERSION for $os/$arch..."
  curl -fsSL -o "$work/$file" "$base/$file" || die "download failed: $base/$file"
  curl -fsSL -o "$work/SHA256SUMS" "$base/SHA256SUMS" || die "download failed: $base/SHA256SUMS"
  want=$(grep " $file\$" "$work/SHA256SUMS" | cut -d' ' -f1)
  [ -n "$want" ] || die "$file is not listed in SHA256SUMS"
  got=$(sha256 "$work/$file")
  [ "$got" = "$want" ] || die "checksum mismatch for $file: expected $want, got $got"
  chmod 755 "$work/$file"
  mv "$work/$file" "$DIR/bin/forgeline.new" && mv "$DIR/bin/forgeline.new" "$DIR/bin/forgeline"
}

# The Go toolchain the repository asks for, from go.dev, checked against the published sha256.
ensure_go() {
  want=$(sed -n 's/^go \([0-9][0-9.]*\).*/\1/p' "$here/go.mod" | head -n 1)
  [ -n "$want" ] || die "could not read the Go version from go.mod"
  if command -v go >/dev/null 2>&1; then
    have=$(go version | sed -n 's/.*go\([0-9][0-9.]*\).*/\1/p')
    if [ "$(printf '%s\n%s\n' "$want" "$have" | sort -V | head -n 1)" = "$want" ]; then GO=go; return; fi
    say "Go $have is installed but the repository needs $want."
  fi
  if [ -x "$DIR/go/bin/go" ]; then
    have=$("$DIR/go/bin/go" version | sed -n 's/.*go\([0-9][0-9.]*\).*/\1/p')
    if [ "$have" = "$want" ]; then GO="$DIR/go/bin/go"; return; fi
  fi
  tgz="go${want}.${os}-${arch}.tar.gz"
  say "Downloading Go $want from go.dev..."
  curl -fsSL -o "$work/$tgz" "https://dl.google.com/go/$tgz" || die "download failed: https://dl.google.com/go/$tgz"
  want_sum=$(curl -fsSL "https://dl.google.com/go/$tgz.sha256" | cut -d' ' -f1)
  got=$(sha256 "$work/$tgz")
  [ "$got" = "$want_sum" ] || die "checksum mismatch for $tgz: expected $want_sum, got $got"
  rm -rf "$DIR/go.new" && mkdir -p "$DIR/go.new"
  tar -C "$DIR/go.new" -xzf "$work/$tgz"
  rm -rf "$DIR/go" && mv "$DIR/go.new/go" "$DIR/go" && rmdir "$DIR/go.new"
  GO="$DIR/go/bin/go"
}

install_source() {
  need git
  ensure_go
  say "Building Forgeline from $here with $("$GO" version | cut -d' ' -f3)..."
  ver=$(cd "$here" && git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
  (cd "$here" && "$GO" build -ldflags "-X github.com/forgelinehq/forgeline/internal/version.Version=$ver" -o "$DIR/bin/forgeline.new" ./cmd/forgeline) || die "the build failed"
  mv "$DIR/bin/forgeline.new" "$DIR/bin/forgeline"
}

case "$MODE" in
  release) install_release ;;
  source) install_source ;;
esac
"$DIR/bin/forgeline" version >/dev/null 2>&1 || die "the installed binary does not run"
say "Installed $("$DIR/bin/forgeline" version 2>/dev/null | head -n 1) at $DIR/bin/forgeline"

# A token the server and the command line share, kept across restarts. Made once; never printed to a log.
if [ ! -f "$DIR/env" ]; then
  tok=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
  umask 077
  { echo "FORGELINE_TOKEN=$tok"; echo "FORGELINE_SERVER=http://$LISTEN"; } > "$DIR/env"
  umask 022
fi
TOKEN=$(sed -n 's/^FORGELINE_TOKEN=//p' "$DIR/env")

if [ "$START" = no ]; then
  say "Data directory: $DIR/data. Token and server address: $DIR/env."
  exit 0
fi

if [ "$SERVICE" = yes ]; then
  [ "$os" = linux ] || die "--service needs systemd (Linux); on macOS start the server as printed below"
  command -v systemctl >/dev/null 2>&1 || die "--service needs systemctl"
  unit="$HOME/.config/systemd/user"
  mkdir -p "$unit"
  if [ -f "$unit/forgeline.service" ] && ! grep -q '^# written by forgeline install.sh' "$unit/forgeline.service"; then
    die "$unit/forgeline.service exists and was not written by this script; it is left alone. Remove it or run without --service."
  fi
  cat > "$unit/forgeline.service" <<EOF
# written by forgeline install.sh
[Unit]
Description=Forgeline server
After=network-online.target docker.service

[Service]
EnvironmentFile=$DIR/env
Environment=PATH=$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=$DIR/bin/forgeline server --listen $LISTEN --data-dir $DIR/data
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable --now forgeline.service >/dev/null 2>&1 || systemctl --user restart forgeline.service
  loginctl enable-linger "$USER" >/dev/null 2>&1 || say "Note: 'loginctl enable-linger $USER' did not run, so the service stops when you log out. Run it with sudo once."
  i=0
  while [ $i -lt 30 ]; do
    if curl -fsS -m 2 "http://$LISTEN/healthz" >/dev/null 2>&1; then break; fi
    i=$((i + 1)); sleep 1
  done
  curl -fsS -m 2 "http://$LISTEN/healthz" >/dev/null 2>&1 || die "the service did not answer on http://$LISTEN; see: journalctl --user -u forgeline"
  say ""
  say "Forgeline is running as a systemd user service (journalctl --user -u forgeline)."
else
  say ""
  say "Start it with:"
  say "  set -a; . $DIR/env; set +a; $DIR/bin/forgeline server --listen $LISTEN --data-dir $DIR/data"
  say "On Linux, run install.sh again with --service to keep it running in the background."
fi
say ""
say "Open:  http://$LISTEN/#token=$TOKEN"
say "       The first visit asks you to create an administrator; the token in the link fills itself in."
say "CLI:   set -a; . $DIR/env; set +a; $DIR/bin/forgeline onboard"
say "       (shows what is set up and asks what a terminal can answer)"
say "Update: run this script again."
