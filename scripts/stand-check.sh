#!/usr/bin/env bash
# Проверка Managed ClickHouse/Redis/Kafka на стенде. Слой 1 строго read-only.
# Слой 2 создаёт и удаляет настоящие (платные) кластеры с именами
# "$NAME_PREFIX-<продукт>-…". Слой 1 ищет готовые кластеры "$STAND_PREFIX-<продукт>"
# (ClickHouse — "$STAND_PREFIX-ch"). Журнал — ./stand-check-<время>/log.md, без паролей.
set -uo pipefail

usage() { echo "Usage: $0 layer1|layer2" >&2; exit 2; }
[[ $# == 1 ]] || usage
LAYER=$1
[[ $LAYER == layer1 || $LAYER == layer2 ]] || usage
BIN=${BIN:-bin/cloudru-dbaas}
DRY_RUN=${DRY_RUN:-0}
PRODUCTS=${PRODUCTS:-clickhouse,redis,kafka}
LOG_GROUP_ID=${LOG_GROUP_ID:-}
RESIZE_SIZE=${RESIZE_SIZE:-0}
MULTI_AZ=${MULTI_AZ:-0}
NAME_PREFIX=${NAME_PREFIX:-check}
STAND_PREFIX=${STAND_PREFIX:-stand}
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
RUN_DIR="./stand-check-$STAMP"
mkdir -p "$RUN_DIR"
LOG="$RUN_DIR/log.md"
TMP=$(mktemp -d)
OUT="$TMP/out"
ERR="$TMP/err"
: >"$LOG"
printf '# Журнал проверки %s (%s)\n\n| Шаг | Команда без секретов | Код | stdout/stderr (первые строки) |\n|---|---|---:|---|\n' "$STAMP" "$LAYER" >>"$LOG"

if [[ $DRY_RUN != 1 && (! -x $BIN) ]]; then echo "Бинарник не исполняемый: $BIN" >&2; exit 2; fi
if [[ $DRY_RUN != 1 ]] && ! command -v jq >/dev/null 2>&1; then echo 'Нужен jq' >&2; exit 2; fi

PASSED=0
FAILED=0
SKIPPED=0
FINAL=0
declare -A CLUSTER_ID=() CREATED=() USER_NAME=() TOPIC_NAME=() OP_ID=() MAZ_ID=() MAZ_VERSION=() MAZ_SPEC=() MAZ_STORAGE=()
SUFFIX="$(date -u +%Y%m%d%H%M%S)-$$-${RANDOM}"
PW1="$TMP/password-new"
PW2="$TMP/password-updated"
# Только локальные файлы 0600; пароли никогда не находятся в аргументах или журнале.
umask 077
printf 'Aa1!%s\n' "$SUFFIX-First" >"$PW1"
printf 'Bb2@%s\n' "$SUFFIX-Second" >"$PW2"
chmod 600 "$PW1" "$PW2"

quote_cmd() {
  local a q
  CMD_TEXT=''
  for a in "$@"; do printf -v q '%q' "$a"; CMD_TEXT+="${CMD_TEXT:+ }$q"; done
}
record() {
  local name=$1 command=$2 rc=$3 summary=$4
  summary=${summary//$'\n'/<br>}
  summary=${summary//|/\\|}
  printf '| %s | `%s` | %s | %s |\n' "$name" "$command" "$rc" "$summary" >>"$LOG"
}
skip_step() {
  local name=$1 reason=$2; SKIPPED=$((SKIPPED+1)); printf 'ПРОПУЩЕНО: %s — %s\n' "$name" "$reason"
  record "$name" '—' 'SKIP' "$reason"
}
run_step() {
  local name=$1 rc first
  shift
  # run_step всегда получает бинарник как первый аргумент.
  if [[ $LAYER == layer1 ]]; then
    local a
    for a in "$@"; do
      case "$a" in create|update|resize|delete) echo "Запрещённая команда в layer1: $a" >&2; return 3;; esac
    done
  fi
  quote_cmd "$@"
  if [[ $DRY_RUN == 1 ]]; then
    echo "$CMD_TEXT"
    PASSED=$((PASSED+1)); record "$name" "$CMD_TEXT" 'DRY_RUN' 'команда не запускалась'
    : >"$OUT"; : >"$ERR"; return 0
  fi
  : >"$OUT"; : >"$ERR"
  if "$@" >"$OUT" 2>"$ERR"; then rc=0; PASSED=$((PASSED+1)); else rc=$?; FAILED=$((FAILED+1)); FINAL=1; fi
  first=$( { head -n 2 "$OUT"; head -n 2 "$ERR"; } | paste -sd ' / ' - | cut -c1-300)
  [[ -n $first ]] || first='(пустой вывод)'
  record "$name" "$CMD_TEXT" "$rc" "$first"
  printf '%s: rc=%s\n' "$name" "$rc"
  if [[ $rc != 0 ]]; then sed -n '1,4p' "$ERR" >&2; return "$rc"; fi
}
confirm() {
  local name=$1; shift
  quote_cmd "$@"
  if [[ $DRY_RUN == 1 ]]; then return 0; fi
  printf 'Подтвердите %s: %s\nВведите ровно yes: ' "$name" "$CMD_TEXT" >&2
  local answer; IFS= read -r answer || answer=''
  if [[ $answer != yes ]]; then
    SKIPPED=$((SKIPPED+1)); printf 'ПРОПУЩЕНО: %s — подтверждение не получено\n' "$name"
    record "$name" "$CMD_TEXT" 'SKIP' 'подтверждение не получено'
    return 1
  fi
}
valid_products() {
  local p
  IFS=, read -ra _products <<<"$PRODUCTS"
  for p in "${_products[@]}"; do case "$p" in clickhouse|redis|kafka) ;; *) echo "Неизвестный продукт в PRODUCTS: $p" >&2; exit 2;; esac; done
}
valid_products

# id кластера с точным именем — для уборки, когда create упал, но кластер мог создаться.
adopt_by_name() {
  [[ $DRY_RUN == 1 ]] && return
  local i out
  for i in 1 2 3 4 5; do  # список сам ловит 403 «failed to verify project access»
    if out=$("$BIN" "$1" clusters list --json 2>/dev/null); then
      jq -r --arg n "$2" '[.clusters[]? | select(.name==$n) | .id][0] // empty' <<<"$out"; return
    fi
    sleep 3
  done
}

cleanup() {
  local p id original_rc=$?
  [[ $original_rc == 0 ]] || FINAL=1
  if [[ $LAYER == layer2 ]]; then
    for p in clickhouse redis kafka; do
      id=${CLUSTER_ID[$p]:-}
      if [[ ${CREATED[$p]:-0} == 1 && -n $id ]]; then
        if confirm "удаление временного кластера $p/$id" "$BIN" "$p" clusters delete "$id" --yes --wait --timeout 60m --json; then
          run_step "${p}_cluster_delete" "$BIN" "$p" clusters delete "$id" --yes --wait --timeout 60m --json || true
        else
          FINAL=1
        fi
      fi
      id=${MAZ_ID[$p]:-}
      if [[ -n $id ]]; then
        if confirm "удаление временного multi-AZ кластера $p/$id" "$BIN" "$p" clusters delete "$id" --yes --wait --timeout 60m --json; then
          run_step "${p}_maz_cluster_delete" "$BIN" "$p" clusters delete "$id" --yes --wait --timeout 60m --json || true
        else
          FINAL=1
        fi
      fi
    done
  fi
  printf '\n## Сводка\n\nПрошло: %s; упало: %s; пропущено: %s.\n' "$PASSED" "$FAILED" "$SKIPPED" >>"$LOG"
  printf '\nСводка: прошло %s, упало %s, пропущено %s. Журнал: %s\n' "$PASSED" "$FAILED" "$SKIPPED" "$LOG"
  rm -rf "$TMP"
  exit "$FINAL"
}
trap cleanup EXIT

# Слой 1: все leaf-команды чтения из рекурсивного --help. Для get выбирается
# первый ресурс из соответствующего списка; при пустом списке фиксируется skip.
layer1_product() {
  local p=$1 id name op stand_name has_cluster=1
  stand_name="$STAND_PREFIX-$p"; [[ $p == clickhouse ]] && stand_name=$STAND_PREFIX-ch
  run_step "$p clusters list" "$BIN" "$p" clusters list --json || has_cluster=0
  if [[ $DRY_RUN == 1 ]]; then
    id="DRY-stand-$p"; [[ $p == clickhouse ]] && id=DRY-stand-ch
    CLUSTER_ID[$p]=$id
  else
    id=$(jq -r --arg n "$stand_name" '[.clusters[]? | select(.name==$n) | .id // empty][0] // empty' "$OUT")
    if [[ -n $id ]]; then CLUSTER_ID[$p]=$id; else has_cluster=0; fi
  fi
  run_step "$p versions list" "$BIN" "$p" versions list --json || true
  if [[ $p == clickhouse ]]; then
    run_step "$p specs list" "$BIN" "$p" specs list --json || true
  else
    run_step "$p specs list" "$BIN" "$p" specs list --json || true
  fi
  if [[ $p == clickhouse ]]; then run_step "$p auth check" "$BIN" "$p" auth check --json || true; fi
  if [[ $has_cluster != 1 ]]; then
    skip_step "$p stand cluster" "нет кластера с именем $stand_name; кластерные команды чтения пропущены"
    return 0
  fi
  read_cluster "$p" "${CLUSTER_ID[$p]}"
}
# Чтение по одному кластеру: слой 1 — кластер стенда, слой 2 — временный.
read_cluster() {
  local p=$1 id=$2 name op
  run_step "$p clusters get" "$BIN" "$p" clusters get "$id" --json || true
  if [[ $p != clickhouse ]]; then run_step "$p specs list --cluster-id" "$BIN" "$p" specs list --cluster-id "$id" --json || true; fi
  run_step "$p operations list" "$BIN" "$p" operations list "$id" --json || true
  if [[ $DRY_RUN == 1 ]]; then op="DRY-operation-$p"
  else op=$(jq -r '[.operations[]? | .id // empty][0] // empty' "$OUT"); fi
  if [[ -n $op ]]; then run_step "$p operations get" "$BIN" "$p" operations get "$op" --json || true
  else skip_step "$p operations get" 'список операций пуст'; fi
  run_step "$p roles list" "$BIN" "$p" roles list "$id" --json || true
  run_step "$p users list" "$BIN" "$p" users list "$id" --json || true
  if [[ $DRY_RUN == 1 ]]; then name="DRY-user-$p"
  else name=$(jq -r '[.users[]? | .name // empty][0] // empty' "$OUT"); fi
  if [[ -n $name ]]; then run_step "$p users get" "$BIN" "$p" users get "$id" "$name" --json || true
  else skip_step "$p users get" 'список пользователей пуст'; fi
  if [[ $p == clickhouse ]]; then
    run_step "$p databases list" "$BIN" "$p" databases list "$id" --json || true
    if [[ $DRY_RUN == 1 ]]; then name="DRY-database-$p"
    else name=$(jq -r '[.databases[]? | .name // empty][0] // empty' "$OUT"); fi
    if [[ -n $name ]]; then run_step "$p databases get" "$BIN" "$p" databases get "$id" "$name" --json || true
    else skip_step "$p databases get" 'список баз пуст'; fi
  elif [[ $p == kafka ]]; then
    run_step "$p topics list" "$BIN" "$p" topics list "$id" --json || true
    if [[ $DRY_RUN == 1 ]]; then name="DRY-topic-$p"
    else name=$(jq -r '[.topics[]? | .name // empty][0] // empty' "$OUT"); fi
    if [[ -n $name ]]; then run_step "$p topics get" "$BIN" "$p" topics get "$id" "$name" --json || true
    else skip_step "$p topics get" 'список топиков пуст'; fi
  fi
}
if [[ $LAYER == layer1 ]]; then
  run_step 'zones list' "$BIN" zones list --json || true
  for p in clickhouse redis kafka; do layer1_product "$p"; done
else
  : # layer2 is defined below
fi

# Слой 2: one temporary cluster per selected product, changes only there.
layer2_product() {
  local p=$1 subnet version spec zone storage role uname topic confirm_cmd
  case "$p" in clickhouse) subnet_var=CH_SUBNET_ID;; redis) subnet_var=REDIS_SUBNET_ID;; kafka) subnet_var=KAFKA_SUBNET_ID;; esac
  subnet=${!subnet_var:-}
  [[ $DRY_RUN == 1 && -z $subnet ]] && subnet="DRY-subnet-$p"
  if [[ -z $subnet && $DRY_RUN != 1 ]]; then skip_step "$p create" "не задана переменная $subnet_var (подсеть временного кластера)"; return; fi
  if [[ $DRY_RUN == 1 ]]; then
    version="DRY-version-$p"; spec="DRY-spec-$p"; zone="DRY-zone"; storage=40
  else
    run_step "$p versions list for create" "$BIN" "$p" versions list --json || return
    if [[ $p == clickhouse ]]; then version=$(jq -r '[.versions[]? | .name // empty][0] // empty' "$OUT")
    else version=$(jq -r '[.versions[]? | .id // empty][0] // empty' "$OUT"); fi
    [[ -n $version ]] || { skip_step "$p create" 'API не вернул версию'; return; }
    run_step "$p zones list for create" "$BIN" zones list --json || return
    # zones list отдаёт голый массив; зона должна совпадать с зоной подсети — ZONE_ID.
    zone=${ZONE_ID:-$(jq -r '[(if type=="array" then .[] else .zones[]? end) | .id // empty][0] // empty' "$OUT")}
    [[ -n $zone ]] || { skip_step "$p create" 'API не вернул зону'; return; }
    local spec_args=()
    if [[ $p == clickhouse ]]; then spec_args=("$p" specs list --version "$version" --zone "$zone" --role server --json)
    else spec_args=("$p" specs list --version-id "$version" --zone "$zone" --json); fi
    run_step "$p specs list for create" "$BIN" "${spec_args[@]}" || return
    if [[ $p == clickhouse ]]; then
      spec=$(jq -r '[.specifications[]? | select(.role=="SPECIFICATION_ROLE_CLICKHOUSE_SERVER") | select((.min_shards // 1)<=1 and (.max_shards // 1)>=1 and (.min_replicas // 1)<=1)] | sort_by(.cpu, .memory) | [.[].id // empty][0] // empty' "$OUT")
      storage=$(jq -r --arg id "$spec" '[.specifications[]? | select(.id==$id) | .min_storage_gigs][0] // 40' "$OUT")
    else
      if [[ $p == kafka ]]; then
        spec=$(jq -r '[.specifications[]? | select((.min_brokers // 1)<=1 and (.max_brokers // 1)>=1)] | sort_by(.cpu, .memory) | [.[].id // empty][0] // empty' "$OUT")
        storage=$(jq -r --arg id "$spec" '[.specifications[]? | select(.id==$id) | .min_storage_gigs][0] // 40' "$OUT")
      else
        spec=$(jq -r '[.specifications[]?] | sort_by(.cpu, .memory) | [.[].id // empty][0] // empty' "$OUT")
        storage=$(jq -r --arg id "$spec" '[.specifications[]? | select(.id==$id) | .min_storage_gigs][0] // 40' "$OUT")
      fi
    fi
    [[ -n $spec ]] || { skip_step "$p create" 'не найдена подходящая минимальная спецификация'; return; }
  fi
  local cname="$NAME_PREFIX-$p-$SUFFIX" create_args=()
  case "$p" in
    clickhouse) create_args=("$BIN" "$p" clusters create --name "$cname" --version "$version" --spec "$spec" --shards 1 --replicas 1 --keeper embedded --storage-gib "$storage" --subnet-id "$subnet" --config max_connections=100 --wait --timeout 60m --json);;
    redis) create_args=("$BIN" "$p" clusters create --name "$cname" --version-id "$version" --spec "$spec" --type standalone --replicas 1 --shards 1 --storage-gib "$storage" --subnet-id "$subnet" --param maxmemory-policy=allkeys-lru --wait --timeout 60m --json);;
    kafka) create_args=("$BIN" "$p" clusters create --name "$cname" --version-id "$version" --spec "$spec" --brokers 1 --storage-gib "$storage" --subnet-id "$subnet" --param auto.create.topics.enable=false --wait --timeout 60m --json);;
  esac
  if [[ -n $LOG_GROUP_ID ]]; then create_args+=(--logging --log-group-id "$LOG_GROUP_ID")
  else skip_step "$p logging" 'LOG_GROUP_ID не задан; --logging не отправляется'; fi
  if ! confirm "создание платного кластера $p" "${create_args[@]}"; then return; fi
  CREATED[$p]=1
  MAZ_VERSION[$p]=$version; MAZ_SPEC[$p]=$spec; MAZ_STORAGE[$p]=$storage
  if ! run_step "$p clusters create" "${create_args[@]}"; then
    # 13.10 вживую: create вернул 403, а кластер создался — ищем по имени, чтобы убрать.
    CLUSTER_ID[$p]=$(adopt_by_name "$p" "$cname"); [[ -n ${CLUSTER_ID[$p]} ]] || CREATED[$p]=0
    return
  fi
  if [[ $DRY_RUN == 1 ]]; then CLUSTER_ID[$p]="DRY-created-$p"
  else
    # create --wait без resource_id печатает операцию: её id — не кластер.
    CLUSTER_ID[$p]=$(jq -r 'if has("done") then (.resource_id // empty) else (.id // .cluster.id // empty) end' "$OUT")
    # Без resource_id кластер всё равно создан — ищем по имени, иначе он платно живёт дальше.
    [[ -n ${CLUSTER_ID[$p]} ]] || CLUSTER_ID[$p]=$(adopt_by_name "$p" "$cname")
    [[ -n ${CLUSTER_ID[$p]} ]] || { skip_step "$p resource checks" 'ответ create не содержит id, по имени не найден; уборка по id невозможна'; CREATED[$p]=0; return; }
  fi
  local id=${CLUSTER_ID[$p]}
  # Все изменяющие команды далее адресуют только id, возвращённый нашим create.
  if [[ $p != clickhouse ]]; then run_step "$p specs list --cluster-id" "$BIN" "$p" specs list --cluster-id "$id" --json || true; fi
  if [[ $p == clickhouse ]]; then run_step "$p clusters resize --config" "$BIN" "$p" clusters resize "$id" --config max_connections=101 --wait --timeout 60m --json || true
  else run_step "$p clusters resize --param" "$BIN" "$p" clusters resize "$id" --param "$([[ $p == redis ]] && echo maxmemory-policy=volatile-lru || echo auto.create.topics.enable=true)" --wait --timeout 60m --json || true; fi
  run_step "$p roles list" "$BIN" "$p" roles list "$id" --json || true
  if [[ $DRY_RUN == 1 ]]; then role=cloud_writer
  else role=$(jq -r '[.roles[]? | .name // empty][0] // "cloud_writer"' "$OUT"); fi
  # Kafka: имя пользователя — только строчная латиница и цифры (API 400, 14.10).
  if [[ $p == kafka ]]; then uname="totcheck${SUFFIX//-/}"; else uname="totcheck_${SUFFIX//-/_}"; fi
  USER_NAME[$p]=$uname
  # create/update payloads keep passwords off argv and out of the Markdown journal.
  if [[ $DRY_RUN == 1 ]]; then
    if ! run_step "$p users create" "$BIN" "$p" users create "$id" "$uname" --password-stdin --role "$role" --json; then return; fi
    if [[ $p == clickhouse ]]; then
      run_step "$p users update --password" "$BIN" "$p" users update "$id" "$uname" --password-stdin --json || true
      run_step "$p users update --role" "$BIN" "$p" users update "$id" "$uname" --role "$role" --json || true
    else
      run_step "$p users update" "$BIN" "$p" users update "$id" "$uname" --password-stdin --role "$role" --json || true
    fi
  else
    if ! run_step "$p users create" "$BIN" "$p" users create "$id" "$uname" --password-stdin --role "$role" --json <"$PW1"; then return; fi
    if [[ $p == clickhouse ]]; then
      # ClickHouse: пароль и роли — разными вызовами (API 400, 14.10).
      run_step "$p users update --password" "$BIN" "$p" users update "$id" "$uname" --password-stdin --json <"$PW2" || true
      run_step "$p users update --role" "$BIN" "$p" users update "$id" "$uname" --role "$role" --json || true
    else
      run_step "$p users update" "$BIN" "$p" users update "$id" "$uname" --password-stdin --role "$role" --json <"$PW2" || true
    fi
  fi
  if [[ $p == kafka ]]; then
    topic="${NAME_PREFIX//-/_}-${SUFFIX//-/_}"; TOPIC_NAME[$p]=$topic
    run_step 'kafka topics create' "$BIN" kafka topics create "$id" "$topic" --partitions 1 --replication 1 --wait --json || true
    run_step 'kafka topics update' "$BIN" kafka topics update "$id" "$topic" --partitions 2 --param retention.ms=86400000 --wait --json || true
  fi
  if [[ $p == clickhouse ]]; then
    local db="totcheck_db_${SUFFIX//-/_}"
    run_step 'clickhouse databases create' "$BIN" clickhouse databases create "$id" "$db" --json || true
    run_step 'clickhouse users revoke' "$BIN" clickhouse users revoke "$id" "$uname" "$role" --json || true
    run_step 'clickhouse users grant' "$BIN" clickhouse users grant "$id" "$uname" "$role" --json || true
  fi
  # Чтение по временному кластеру: теперь у users/databases/topics get есть цель.
  read_cluster "$p" "$id"
  if [[ $p == clickhouse ]]; then
    run_step 'clickhouse databases delete' "$BIN" clickhouse databases delete "$id" "$db" --yes --json || true
  fi
  if [[ $p == kafka ]]; then
    run_step 'kafka topics delete' "$BIN" kafka topics delete "$id" "$topic" --yes --wait --json || true
  fi
  run_step "$p users delete" "$BIN" "$p" users delete "$id" "$uname" --yes --json || true
  if [[ $RESIZE_SIZE == 1 ]]; then resize_size "$p" "$id"
  else skip_step "$p clusters resize --spec/--storage-gib" 'RESIZE_SIZE не равен 1'; fi
}
# Resize по размеру: следующая по ресурсам спецификация той же роли и диск +10 ГиБ.
resize_size() {
  local p=$1 id=$2 cur next storage pick min mem
  if [[ $DRY_RUN == 1 ]]; then next="DRY-spec-up-$p"; storage=50
  else
    run_step "$p clusters get for resize" "$BIN" "$p" clusters get "$id" --json || return
    cur=$(jq -r '.specification_id // .cluster.specification_id // empty' "$OUT")
    storage=$(jq -r '((.storage_gib // .cluster.storage_gib // 0) + 10)' "$OUT")
    # version и zone — локальные layer2_product (вызывающей функции); без --zone CH отвечает 400.
    if [[ $p == clickhouse ]]; then run_step "$p specs list for resize" "$BIN" "$p" specs list --version "$version" --zone "$zone" --json || return
    else run_step "$p specs list for resize" "$BIN" "$p" specs list --cluster-id "$id" --json || return; fi
    # specs list --cluster-id (Redis, Kafka) отдаёт только цели resize, без текущей
    # спецификации (прогон 14.10): тогда берём наименьшую из списка.
    pick=$(jq -r --arg cur "$cur" '[.specifications[]?] as $all
      | ([$all[] | select(.id==$cur)] | .[0]) as $c
      | (if $c == null then [$all[] | select(.id!=$cur)]
         else [$all[] | select(.id!=$cur and (.role // "") == ($c.role // "") and (.deployment_mode // "") == ($c.deployment_mode // "") and (.flavor_type // "") == ($c.flavor_type // "") and ((.cpu // .vcpu // 0) > ($c.cpu // $c.vcpu // 0) or (.ram // .memory // 0) > ($c.ram // $c.memory // 0)))] end)
      | sort_by((.cpu // .vcpu // 0), (.ram // .memory // 0)) | .[0] // empty
      | "\(.id) \(.min_storage_gigs // 0) \(.ram // .memory // 0)"' "$OUT")
    read -r next min mem <<<"$pick"
    # Консоль Redis (14.10): диск не меньше 2× RAM целевой спецификации,
    # min_storage_gigs этого не отражает (2vCPU/8GB требует 16 ГиБ).
    [[ $p == redis && -n ${mem:-} && $((mem * 2)) -gt ${min:-0} ]] && min=$((mem * 2))
  fi
  if [[ -n $next ]]; then
    # У целевой спецификации свой минимальный диск — поднимаем его тем же запросом.
    # Диск и спецификация — разными запросами (Redis: 400 conflicting update fields, 14.10):
    # сначала диск до минимума новой спецификации, затем спецификация.
    if [[ -n ${min:-} && ${min:-0} -gt $((storage - 10)) ]]; then
      run_step "$p clusters resize --storage-gib (под спецификацию)" "$BIN" "$p" clusters resize "$id" --storage-gib "$min" --wait --timeout 60m --json || true
      storage=$((min + 10))  # следующий шаг — диск ещё +10 от нового минимума
    fi
    run_step "$p clusters resize --spec" "$BIN" "$p" clusters resize "$id" --spec "$next" --wait --timeout 60m --json || true
  else skip_step "$p clusters resize --spec" 'не найдена спецификация крупнее текущей'; fi
  run_step "$p clusters resize --storage-gib" "$BIN" "$p" clusters resize "$id" --storage-gib "$storage" --wait --timeout 60m --json || true
}
# Multi-AZ: только create → get → delete; подсети через запятую в CH_MAZ_SUBNET_IDS / REDIS_MAZ_SUBNET_IDS.
layer2_multiaz() {
  local p=$1 var subnets version spec storage cname id args=()
  case "$p" in clickhouse) var=CH_MAZ_SUBNET_IDS;; redis) var=REDIS_MAZ_SUBNET_IDS;; *) return;; esac
  subnets=${!var:-}
  [[ $DRY_RUN == 1 && -z $subnets ]] && subnets="DRY-subnet-a,DRY-subnet-b"
  if [[ -z $subnets ]]; then skip_step "$p multi-AZ create" "не задана $var"; return; fi
  if [[ $DRY_RUN == 1 ]]; then version="DRY-version-$p"; spec="DRY-spec-$p"; storage=40
  else
    version=${MAZ_VERSION[$p]:-}; spec=${MAZ_SPEC[$p]:-}; storage=${MAZ_STORAGE[$p]:-}
    [[ -n $version && -n $spec ]] || { skip_step "$p multi-AZ create" 'нет версии/спецификации от sAZ-прохода'; return; }
  fi
  cname="$NAME_PREFIX-maz-$p-$SUFFIX"
  local s; IFS=, read -ra _subs <<<"$subnets"; for s in "${_subs[@]}"; do args+=(--subnet-id "$s"); done
  if [[ $p == clickhouse ]]; then
    # 13.10 вживую: embedded и legacy принимают только 1 подсеть — multi-AZ лишь со standalone Keeper.
    local kspec kstorage
    if [[ $DRY_RUN == 1 ]]; then kspec="DRY-keeper-spec"; kstorage=10
    else
      run_step "$p specs list --role keeper" "$BIN" "$p" specs list --version "$version" --zone "${ZONE_ID:-}" --role keeper --json || return
      kspec=$(jq -r '[.specifications[]? | select(.role=="SPECIFICATION_ROLE_CLICKHOUSE_KEEPER")] | sort_by(.cpu, .memory) | [.[].id // empty][0] // empty' "$OUT")
      kstorage=$(jq -r --arg id "$kspec" '[.specifications[]? | select(.id==$id) | .min_storage_gigs][0] // 10' "$OUT")
      [[ -n $kspec ]] || { skip_step "$p multi-AZ create" 'не найдена спецификация Keeper'; return; }
    fi
    args=("$BIN" "$p" clusters create --name "$cname" --version "$version" --spec "$spec" --shards 1 --replicas "${#_subs[@]}" --keeper standalone --keeper-spec "$kspec" --keeper-storage-gib "$kstorage" --storage-gib "$storage" "${args[@]}" --wait --timeout 60m --json)
  else
    args=("$BIN" "$p" clusters create --name "$cname" --version-id "$version" --spec "$spec" --type master-replica --replicas $(( ${#_subs[@]} - 1 )) --shards 1 --storage-gib "$storage" "${args[@]}" --wait --timeout 60m --json)
  fi
  if ! confirm "создание платного multi-AZ кластера $p" "${args[@]}"; then return; fi
  if ! run_step "$p multi-AZ create" "${args[@]}"; then MAZ_ID[$p]=$(adopt_by_name "$p" "$cname"); return; fi
  if [[ $DRY_RUN == 1 ]]; then id="DRY-maz-$p"; else id=$(jq -r '.id // .cluster.id // empty' "$OUT"); fi
  [[ -n $id ]] || { skip_step "$p multi-AZ checks" 'ответ create не содержит id'; return; }
  MAZ_ID[$p]=$id
  run_step "$p multi-AZ get" "$BIN" "$p" clusters get "$id" --json || true
}
if [[ $LAYER == layer2 ]]; then
  IFS=, read -ra _selected <<<"$PRODUCTS"
  for p in "${_selected[@]}"; do layer2_product "$p"; done
  if [[ $MULTI_AZ == 1 ]]; then for p in "${_selected[@]}"; do layer2_multiaz "$p"; done
  else skip_step 'multi-AZ' 'MULTI_AZ не равен 1'; fi
fi
