#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yaml"
ENV_FILE=${GATELENS_ENV_FILE:-"$SCRIPT_DIR/docker.env"}
ACTION=${1:-up}
PROJECT_NAME=${GATELENS_COMPOSE_PROJECT:-gatelens}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

detect_compose() {
  command -v docker >/dev/null 2>&1 || fail "docker is not installed or is not in PATH"
  if docker compose version >/dev/null 2>&1; then
    COMPOSE_STYLE=plugin
    return
  fi
  if command -v docker-compose >/dev/null 2>&1 && docker-compose version >/dev/null 2>&1; then
    COMPOSE_STYLE=standalone
    printf 'Warning: using legacy docker-compose; install the Compose v2 plugin when possible.\n' >&2
    return
  fi
  fail "Docker Compose is not available; install docker-compose-plugin or the legacy docker-compose binary"
}

generate_token() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
    return
  fi
  if command -v od >/dev/null 2>&1 && [ -r /dev/urandom ]; then
    od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
    return
  fi
  fail "cannot generate an agent token; install openssl or create $ENV_FILE manually"
}

ensure_env_file() {
  if [ -f "$ENV_FILE" ]; then
    return
  fi

  umask 077
  token=$(generate_token)
  {
    printf 'GATELENS_AGENT_TOKEN=%s\n' "$token"
    printf 'GATELENS_API_IMAGE=ghcr.io/gatelens/gatelens-api:dev\n'
    printf 'GATELENS_WEB_IMAGE=ghcr.io/gatelens/gatelens-web:dev\n'
    printf 'GATELENS_BIND_ADDRESS=0.0.0.0\n'
    printf 'GATELENS_HTTP_PORT=8080\n'
    printf 'GATELENS_SERVER_CLUSTER_ID=federation\n'
    printf 'GATELENS_STALE_AFTER=2m\n'
  } >"$ENV_FILE"
  printf 'Created %s with a new Agent token.\n' "$ENV_FILE"
}

compose() {
  if [ "$COMPOSE_STYLE" = plugin ]; then
    docker compose \
      --project-name "$PROJECT_NAME" \
      --env-file "$ENV_FILE" \
      --file "$COMPOSE_FILE" \
      "$@"
  else
    docker-compose \
      --project-name "$PROJECT_NAME" \
      --env-file "$ENV_FILE" \
      --file "$COMPOSE_FILE" \
      "$@"
  fi
}

detect_compose
ensure_env_file

case "$ACTION" in
  up | update)
    if [ "$ACTION" = update ]; then
      compose pull
    fi
    compose up --detach --remove-orphans
    published_address=$(compose port gatelens-web 8080 2>/dev/null || true)
    printf 'GateLens is running at http://%s\n' "${published_address:-localhost:8080}"
    printf 'Use the token in %s as GATELENS_AGENT_TOKEN for every cluster Agent.\n' "$ENV_FILE"
    ;;
  down)
    compose down
    ;;
  restart)
    compose restart
    ;;
  status)
    compose ps
    ;;
  logs)
    shift || true
    compose logs --follow --tail=200 "$@"
    ;;
  *)
    fail "usage: $0 [up|update|down|restart|status|logs [service]]"
    ;;
esac
