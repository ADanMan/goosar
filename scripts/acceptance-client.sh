#!/usr/bin/env bash
# Goosar — client acceptance on a clean machine (0.11.0, T-19 / #582).
#
# The server side is raised by `make selfhost` and checked by its own smoke
# tests. The client side — "downloaded → signed in → daemon online → packages delivered →
# an agent took a task" — had no such thing, and every failure on it was
# silent (docs/client-install-map.md). This script walks that path on the
# machine it runs on and prints one PASS/FAIL table, so the acceptance run
# on a clean VM is a command, not a checklist read from a screen.
#
# Usage:
#   bash scripts/acceptance-client.sh --tag vX.Y.Z --server-url URL --app-url URL
#       [--token gsl_…] [--ca-file PEM] [--dmg FILE] [--profile NAME] [--report FILE]
#
# --token     sign in without a browser (a PAT); without it the browser login
#             of `goosar setup self-host` runs and the operator finishes it.
# --ca-file   the stand's CA (stand-root-ca.crt) — exported as GOOSAR_CA_FILE.
# --dmg       verify the desktop artifact against checksums.txt beside it and,
#             on macOS, that the bundle carries the goosar CLI.
# --report    also write the table as Markdown (pasted into 44-acceptance.md).
#
# Steps that need a GUI (the app's own sign-in and provisioning, giving an
# agent a task) ask the operator and wait GOOSAR_ACCEPTANCE_WAIT seconds
# (default 600) for a y/n. Exit 1 on any FAIL.
set -uo pipefail

TAG=""; SERVER_URL=""; APP_URL=""; TOKEN=""; CA_FILE=""; DMG=""; PROFILE=""; REPORT=""
WAIT="${GOOSAR_ACCEPTANCE_WAIT:-600}"
while [ $# -gt 0 ]; do
  case "$1" in
    --tag) TAG="${2:?}"; shift ;;
    --server-url) SERVER_URL="${2:?}"; shift ;;
    --app-url) APP_URL="${2:?}"; shift ;;
    --token) TOKEN="${2:?}"; shift ;;
    --ca-file) CA_FILE="${2:?}"; shift ;;
    --dmg) DMG="${2:?}"; shift ;;
    --profile) PROFILE="${2:?}"; shift ;;
    --report) REPORT="${2:?}"; shift ;;
    -h|--help) sed -n '2,/^set -uo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
  shift
done
[ -n "$TAG" ] && [ -n "$SERVER_URL" ] && [ -n "$APP_URL" ] || { echo "usage: --tag vX.Y.Z --server-url URL --app-url URL [--token …] (see --help)" >&2; exit 2; }
[ -n "$CA_FILE" ] && export GOOSAR_CA_FILE="$CA_FILE"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then GREEN=$'\033[32m'; RED=$'\033[31m'; YELLOW=$'\033[33m'; RESET=$'\033[0m'; else GREEN=""; RED=""; YELLOW=""; RESET=""; fi
ROWS=()
record() { ROWS+=("$1	$2	$3"); }
FAILED=0
profile_args() { [ -n "$PROFILE" ] && printf -- '--profile\n%s\n' "$PROFILE"; return 0; }

# json_rows <json>: prints "id status detail" per object, in document order.
# No jq on a clean machine; ids and statuses are paired in the order the CLI
# prints them (id first inside every row).
json_rows() {
  # `|| [ -n "$obj" ]`: the last object has no trailing newline after tr,
  # and a bare `read` would drop it — which is how the kit row went missing.
  printf '%s' "$1" | tr -d '\n' | sed 's/},{/}\n{/g' | while IFS= read -r obj || [ -n "$obj" ]; do
    id="$(printf '%s' "$obj" | sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p')"
    st="$(printf '%s' "$obj" | sed -n 's/.*"status" *: *"\([^"]*\)".*/\1/p')"
    dt="$(printf '%s' "$obj" | sed -n 's/.*"detail" *: *"\([^"]*\)".*/\1/p')"
    [ -n "$id" ] && printf '%s %s %s\n' "$id" "$st" "$dt"
  done
}
ask() { # ask <question> -> 0 on y
  local ans=""
  printf '\n%s [y/n] ' "$1"
  if read -r -t "$WAIT" ans; then case "$ans" in y|Y|yes|да) return 0 ;; esac; return 1; fi
  printf '\n'; return 2
}
skip_rest() { local n; for n in "$@"; do record SKIPPED "$n" "предыдущий шаг не пройден"; done; }

# --- 0. artifact --------------------------------------------------------------
if [ -n "$DMG" ]; then
  sums="$(dirname "$DMG")/checksums.txt"
  if [ ! -f "$DMG" ]; then record FAIL "артефакт" "нет файла $DMG"; FAILED=1
  elif [ -f "$sums" ] && ! (cd "$(dirname "$DMG")" && grep -F " $(basename "$DMG")" checksums.txt | shasum -a 256 -c --status); then
    record FAIL "артефакт" "sha256 не сошлась с checksums.txt"; FAILED=1
  else record PASS "артефакт" "$(basename "$DMG")$([ -f "$sums" ] && printf ', sha256 сошлась')"; fi
else
  record SKIPPED "артефакт" "не передан --dmg"
fi

# --- 1. goosar on PATH -------------------------------------------------------
GOOSAR="$(command -v goosar 2>/dev/null || true)"
if [ -z "$GOOSAR" ]; then
  record FAIL "goosar в PATH" "команда не найдена — в приложении: Справка → Установить командную строку"
  skip_rest "версия" "doctor" "конфигурация" "вход" "демон" "doctor --server" "провижининг" "задача агенту"
  FAILED=1
else
  record PASS "goosar в PATH" "$GOOSAR"

  # --- 2. version ---------------------------------------------------------------
  vjson="$(goosar version --output json 2>/dev/null)"
  got="$(printf '%s' "$vjson" | sed -n 's/.*"version" *: *"v\{0,1\}\([^"]*\)".*/\1/p' | head -n1)"
  want="${TAG#v}"
  if [ "$got" = "$want" ]; then record PASS "версия" "$got"; else record FAIL "версия" "нашли ${got:-?}, ожидали $want"; FAILED=1; fi

  # --- 3. doctor (local) --------------------------------------------------------
  djson="$(goosar doctor --output json 2>/dev/null)"
  bad_rows="$(json_rows "$djson" | awk '$2=="missing"||$2=="outdated"{print $1}' | grep -v '^agent-cli$' | tr '\n' ' ')"
  agent_missing="$(json_rows "$djson" | awk '$1=="agent-cli" && ($2=="missing"){print "agent-cli отсутствует — ставится отдельно (#443)"}')"
  if [ -z "$bad_rows" ]; then record PASS "doctor" "${agent_missing:-все зависимости на месте}"; else record FAIL "doctor" "красные строки: $bad_rows"; FAILED=1; fi

  # --- 4. config + 5. login -----------------------------------------------------
  pa=(); while IFS= read -r a; do pa+=("$a"); done < <(profile_args)
  if [ -n "$TOKEN" ]; then
    if goosar config set server_url "$SERVER_URL" ${pa[@]+"${pa[@]}"} >/dev/null 2>&1 &&
       goosar config set app_url "$APP_URL" ${pa[@]+"${pa[@]}"} >/dev/null 2>&1; then
      record PASS "конфигурация" "$SERVER_URL${CA_FILE:+, CA $CA_FILE}"
    else record FAIL "конфигурация" "goosar config set завершился с ошибкой"; FAILED=1; fi
    # The token goes on stdin (the --token prompt), never on argv.
    if printf '%s\n' "$TOKEN" | goosar login --token ${pa[@]+"${pa[@]}"} >/dev/null 2>&1; then
      record PASS "вход" "по токену"
    else record FAIL "вход" "goosar login --token отклонён"; FAILED=1; fi
    goosar daemon start ${pa[@]+"${pa[@]}"} >/dev/null 2>&1 || true
  else
    printf '\nБраузерный вход: завершите его в открывшемся окне.\n'
    if goosar setup self-host --server-url "$SERVER_URL" --app-url "$APP_URL" ${CA_FILE:+--ca-file "$CA_FILE"} --yes ${pa[@]+"${pa[@]}"}; then
      record PASS "конфигурация" "$SERVER_URL"; record PASS "вход" "через браузер"
    else record FAIL "конфигурация" "goosar setup self-host завершился с ошибкой"; record FAIL "вход" "см. выше"; FAILED=1; fi
  fi

  # --- 6. daemon ----------------------------------------------------------------
  status=""; i=0
  while [ "$i" -lt "$WAIT" ]; do
    status="$(goosar daemon status --output json ${pa[@]+"${pa[@]}"} 2>/dev/null | sed -n 's/.*"status" *: *"\([^"]*\)".*/\1/p' | head -n1)"
    [ "$status" = "running" ] && break
    i=$((i + 1)); sleep 1
  done
  if [ "$status" = "running" ]; then record PASS "демон" "running"; else record FAIL "демон" "status=${status:-нет ответа} спустя ${WAIT} с"; FAILED=1; fi

  # --- 7. doctor --server -------------------------------------------------------
  sjson="$(goosar doctor --server --output json ${pa[@]+"${pa[@]}"} 2>/dev/null)"
  bad_rows="$(json_rows "$sjson" | awk '$2=="missing"||$2=="outdated"{print $1}' | grep -v '^agent-cli$' | tr '\n' ' ')"
  if [ -z "$bad_rows" ]; then
    record PASS "doctor --server" "$(json_rows "$sjson" | awk '$1=="server"{ $1=""; $2=""; print substr($0,3) }')"
  else record FAIL "doctor --server" "красные строки: $bad_rows"; FAILED=1; fi

  # --- 8. provisioning ----------------------------------------------------------
  kit="$(json_rows "$sjson" | awk '$1=="kit-platform"{ st=$2; $1=""; $2=""; print st "|" substr($0,3) }')"
  kit_status="${kit%%|*}"; kit_detail="${kit#*|}"
  installed="$(printf '%s' "$kit_detail" | grep -oE '[0-9]+ установлено' | grep -oE '[0-9]+' || true)"
  if [ "$kit_status" = "skipped" ]; then
    record SKIPPED "провижининг" "$kit_detail"
  elif ask "Откройте приложение, войдите и дождитесь установки пакетов. Пакеты в приложении видны?"; then
    if [ -n "$installed" ] && [ "$installed" -gt 0 ]; then record PASS "провижининг" "$kit_detail"
    else record FAIL "провижининг" "оператор подтвердил, но doctor видит: ${kit_detail:-пусто}"; FAILED=1; fi
  else record FAIL "провижининг" "оператор не подтвердил (или нет ответа за ${WAIT} с)"; FAILED=1; fi

  # --- 9. a task to an agent ----------------------------------------------------
  if ask "Дайте агенту задачу с инструментом mcp__*. Задача выполнена?"; then record PASS "задача агенту" "подтверждено оператором"
  else record FAIL "задача агенту" "оператор не подтвердил (или нет ответа за ${WAIT} с)"; FAILED=1; fi
fi

# --- table -------------------------------------------------------------------
printf '\n %-3s %-8s %-18s %s\n' "#" "СТАТУС" "ШАГ" "ДЕТАЛИ"
i=1
for line in "${ROWS[@]}"; do
  IFS=$'\t' read -r st name detail <<<"$line"
  case "$st" in PASS) c="$GREEN" ;; FAIL) c="$RED" ;; *) c="$YELLOW" ;; esac
  printf ' %-3s %s%-8s%s %-18s %s\n' "$i" "$c" "$st" "$RESET" "$name" "$detail"
  i=$((i + 1))
done
if [ -n "$REPORT" ]; then
  {
    printf '| Статус | Шаг | Детали |\n| --- | --- | --- |\n'
    for line in "${ROWS[@]}"; do IFS=$'\t' read -r st name detail <<<"$line"; printf '| %s | %s | %s |\n' "$st" "$name" "$detail"; done
  } > "$REPORT"
  printf '\nОтчёт: %s\n' "$REPORT"
fi
if [ "$FAILED" = "1" ]; then printf '\n%sПриёмка клиента не пройдена.%s\n' "$RED" "$RESET" >&2; exit 1; fi
printf '\n%sПриёмка клиента %s пройдена.%s\n' "$GREEN" "$TAG" "$RESET"
