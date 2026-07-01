#!/usr/bin/env bash
# Tefnut one-click setup: configure + launch via Docker Compose.
set -euo pipefail
cd "$(dirname "$0")"

ENV_FILE=".env"
DEF_PORT="8086"

err()  { printf '\033[31m%s\033[0m\n' "$*" >&2; }
info() { printf '\033[36m%s\033[0m\n' "$*"; }

# --- preflight ---
if ! command -v docker >/dev/null 2>&1; then
  err "未找到 docker。请先安装 Docker：https://docs.docker.com/get-docker/"
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  err "未找到 docker compose (v2)。请升级 Docker 到包含 Compose V2 的版本。"
  exit 1
fi

# --- read existing .env values as defaults (grep/cut, not sourcing — safe for spaces) ---
envval() { [ -f "$ENV_FILE" ] && grep -E "^$1=" "$ENV_FILE" | head -1 | cut -d= -f2- || true; }
CUR_COMICS="$(envval COMICS_PATH)"
CUR_PORT="$(envval TEFNUT_PORT)"; CUR_PORT="${CUR_PORT:-$DEF_PORT}"
CUR_TZ="$(envval TZ)"

detect_tz() {
  local tz=""
  if [ -L /etc/localtime ]; then tz="$(readlink /etc/localtime | sed 's#.*/zoneinfo/##')"; fi
  [ -n "$tz" ] && printf '%s' "$tz" || printf 'Asia/Shanghai'
}
DEF_TZ="${CUR_TZ:-$(detect_tz)}"

# --- prompt: comic library path (required, must exist) ---
while :; do
  if [ -n "$CUR_COMICS" ]; then printf '漫画库目录绝对路径 [%s]: ' "$CUR_COMICS"; else printf '漫画库目录绝对路径: '; fi
  read -r COMICS_PATH || true
  [ -z "$COMICS_PATH" ] && COMICS_PATH="$CUR_COMICS"
  case "$COMICS_PATH" in "~"*) COMICS_PATH="${HOME}${COMICS_PATH#\~}";; esac
  if [ -z "$COMICS_PATH" ]; then err "库路径必填。"; continue; fi
  if [ ! -d "$COMICS_PATH" ]; then err "目录不存在: $COMICS_PATH"; continue; fi
  break
done

# --- prompt: port ---
while :; do
  printf 'Web 端口 [%s]: ' "$CUR_PORT"
  read -r TEFNUT_PORT || true
  [ -z "$TEFNUT_PORT" ] && TEFNUT_PORT="$CUR_PORT"
  if printf '%s' "$TEFNUT_PORT" | grep -qE '^[0-9]+$' && [ "$TEFNUT_PORT" -ge 1 ] && [ "$TEFNUT_PORT" -le 65535 ]; then break; fi
  err "端口需为 1-65535 的数字。"
done

# --- prompt: timezone ---
printf '时区 [%s]: ' "$DEF_TZ"
read -r TZ_IN || true
TZ_VAL="${TZ_IN:-$DEF_TZ}"

# --- write .env ---
{
  printf 'COMICS_PATH=%s\n' "$COMICS_PATH"
  printf 'TEFNUT_PORT=%s\n' "$TEFNUT_PORT"
  printf 'TZ=%s\n' "$TZ_VAL"
} > "$ENV_FILE"
info "已写入 $ENV_FILE"

# --- launch ---
info "拉取镜像…"
if ! docker compose pull; then
  err "拉取镜像失败。请确认镜像已由 CI 发布到 GHCR、且 GHCR 包可访问（或先 'docker login ghcr.io'）。"
  err "也可从源码构建: 在 docker-compose.yml 注释 image、启用 'build: .'，再 'docker compose up -d --build'。"
  exit 1
fi
info "启动容器…"
docker compose up -d

# --- report ---
info "等待服务就绪…"
if command -v curl >/dev/null 2>&1; then
  for _ in $(seq 1 30); do
    curl -fsS -o /dev/null "http://127.0.0.1:${TEFNUT_PORT}/healthz" 2>/dev/null && break
    sleep 1
  done
fi
docker compose ps
info "打开: http://localhost:${TEFNUT_PORT}"
info "（首次启动会在后台扫描漫画库并生成封面，大库可能需要几分钟。）"
