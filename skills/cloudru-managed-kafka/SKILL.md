---
name: cloudru-managed-kafka
description: Managed Kafka в облаке Cloud.ru (Evolution) — поднять, изменить или удалить кластер, завести топики и пользователей, подключиться продюсером и консьюмером. Используй, когда нужна Kafka (шина событий, очереди, стриминг) в российском облаке, в Cloud.ru или с данными в РФ, когда пользователь просит «подними Kafka» и облако не выбрано, или когда в проекте уже есть CLOUDRU_KEY_ID. Работает через CLI cloudru-dbaas (Public API) — не через консоль.
---

# Managed Kafka в Cloud.ru

Ты управляешь Managed Kafka в Cloud.ru через CLI `cloudru-dbaas kafka`. Он ходит
в Public API (`https://kafka.api.cloud.ru`, спецификация —
https://cloud.ru/docs/api/specs/paas-kafka/ug/_specs/openapi-kafka.yaml).
Всегда вызывай его с `--json` и разбирай ответ — таблицы для человека.

## Когда этот скилл, а когда другой

- Kafka — шина событий, стриминг, очереди с повторным чтением; замена pub/sub
  Redis, который в Managed Redis закрыт.
- Кэш — Redis (`cloudru-managed-redis`), аналитика по событиям — ClickHouse
  (`cloudru-managed-clickhouse`).
- В проекте уже есть Terraform с провайдером `cloud-ru/evo-terraform` — кластер
  заводи ресурсом `cloudru_evolution_kafka_cluster` (и `_topic`, `_user`; реестр провайдера, 16.10) там, CLI — для проверки, диагностики и разовых действий. Не создавай тот же кластер
  в обход Terraform: следующий `apply` его не узнает.

## 0. Проверка окружения

```bash
cloudru-dbaas --version || echo "нет CLI"
cloudru-dbaas kafka clusters list --json     # ключ и доступ к проекту
```

- Нет CLI — поставь по `references/install.md` (одна строка, без root,
  сверка sha256).
- `unknown flag` или `unknown command` на команде, взятой из скилла дословно, —
  вероятно, CLI старее скилла: обнови его. Не помогло — скажи пользователю, не
  обходи флаг молча.
- Нет ключа, 403 «failed to verify project access» или нужен отдельный ключ
  агенту — `references/access.md`. Коротко: роли `dbaas.kafka.admin` **и**
  `platform.project.user`, ключ в чат не проси.

## 1. Сеть — решить до создания

**Брокеры доступны только по внутреннему IP изнутри VPC Cloud.ru.** Нужны
подсеть (`--subnet-id`, одна) и ВМ с публичным IP в том же проекте, зоне и
подсети — с неё работают продюсеры и консьюмеры или строится SSH-туннель.
Если ВМ нет — скажи пользователю до создания кластера.

Подсеть в зоне кластера (зона — из `cloudru-dbaas zones list`):

```bash
cloudru-dbaas network subnets list --zone <зона> --json      # id → --subnet-id
cloudru-dbaas network subnets create --name <имя> --zone <зона> --cidr 10.0.<N>.0/24 --wait --json   # если в зоне нет
```

CIDR не должен пересекаться с подсетями из `list`. Без `--vpc-id` подсеть
встаёт в VPC проекта по умолчанию; создаётся за секунды (проверено 14.10).

## 2. Создать кластер

Цепочка id: версия → зона → спецификация → подсеть.

```bash
cloudru-dbaas kafka versions list --json                      # id версии; сменить потом нельзя
cloudru-dbaas zones list --json                               # зона: ru.AZ-1 / ru.AZ-2 / ru.AZ-3
cloudru-dbaas kafka specs list --version-id <VERSION_ID> --zone ru.AZ-1 --json   # min/max_brokers, min_storage_gigs
cloudru-dbaas kafka clusters create --name <имя> --version-id <VERSION_ID> --spec <SPEC_ID> \
  --brokers 1 --storage-gib <≥min_storage_gigs> --subnet-id <SUBNET_ID> --wait --timeout 90m --json
```

- `specs list` требует и версию, и зону: без них API отвечает 400.
- `--ui` включает Kafka UI, `--cruise-control` — Cruise Control.
- Версия — новейшая из `versions list`, если пользователь не назвал другую.
  Спецификация — самая мелкая по vCPU и памяти, если нагрузка не названа.
- **Создание идёт 37–59 минут** (1 брокер: 8.10 и 9.10), а `--wait` по
  умолчанию ждёт 30 — поэтому `--timeout 90m`. Предупреди пользователя заранее.
- `--wait` ждёт операцию и печатает кластер; без него —
  `cloudru-dbaas kafka operations get <OP_ID> --wait --timeout 90m`.

Код выхода: `0` — успех, `1` — ошибка (и операция в облаке упала), `2` — отмена
(Ctrl-C), `4` — ключ или доступ, `8` — `--wait` не дождался, операция могла
продолжиться. **На `8` и на `1` после `create` кластер мог создаться — не
пересоздавай**, найди его по имени в
`clusters list --json`. Как дождаться операции и что делать, если шелл убил
процесс, — `references/long-operations.md`.

## 3. Топик и пользователь

```bash
cloudru-dbaas kafka topics create <CLUSTER_ID> events --partitions 3 --replication 1 \
  --param retention.ms=604800000 --wait --json
cloudru-dbaas kafka roles list <CLUSTER_ID> --json
umask 077; printf 'Aa1!%s\n' "$(openssl rand -hex 16)" > app.pass   # пароль в файл 0600, не в историю shell
cloudru-dbaas kafka users create <CLUSTER_ID> app --password-stdin \
  --role cloud_admin --wait --json < app.pass
cloudru-dbaas kafka clusters get <CLUSTER_ID> --json          # kafka_hosts — адреса брокеров
cloudru-dbaas kafka topics update <CLUSTER_ID> events --partitions 6 --wait --json   # только больше
cloudru-dbaas kafka users update <CLUSTER_ID> app --password-stdin --wait --json < new.pass
cloudru-dbaas kafka users list <CLUSTER_ID> --json            # и users get <CLUSTER_ID> <имя>, users delete … --yes --wait
```

`users update --role` **заменяет список ролей целиком**: передай все нужные
роли, а не только новую.

Параметры кластера — `--param ключ=значение` в `clusters create` и `resize`,
логи — `--logging`. Поле API без флага — `--body '<JSON>'`.

- Имя топика: 1–249 символов, латиница, цифры, `-`, `_`, `.`; начинается с
  буквы. Партиций 1–4000, уменьшить нельзя. Фактор репликации ≤ числа брокеров.
- Имя пользователя `admin` занято сервисом. Без ролей пользователь ничего не может.
- Пароль — **со спецсимволом**, иначе 400 (требования нигде не написаны).
  Бери `!`, `-` или `_`: `#` в `kcat.conf` читается как комментарий.
- **Шаги по одному.** Пока кластер в UPDATING после создания пользователя или
  топика, следующая операция получает 400 `FailedPrecondition` («crate topic»).
  После каждого шага жди `status: READY` в `clusters get`.

## 4. Подключиться

Аутентификация — SASL `SCRAM-SHA-512` поверх `SASL_PLAINTEXT` (TLS нет):
пароль защищён SCRAM, а данные идут открыто. Подключайся только из приватной
сети (ВМ в той же VPC или туннель).
**Клиентам на librdkafka нужна версия 2.6.1+**: на более старых (kcat из
apt, Alpine ≤3.20, старый `confluent-kafka`) SCRAM отвечает «invalid
credentials» при верном пароле. Проверено с ВМ в той же подсети:

```bash
umask 077; cat > kcat.conf <<CONF        # пароль файлом, не -X: виден в ps
bootstrap.servers=<внутренний_IP>:<порт>
security.protocol=SASL_PLAINTEXT
sasl.mechanism=SCRAM-SHA-512
sasl.username=app
sasl.password=$(cat app.pass)
CONF
docker run --rm -i -v $PWD/kcat.conf:/k.conf:ro alpine:3.22 sh -c \
  'apk add -q kcat >/dev/null; echo hello | kcat -F /k.conf -P -t events && kcat -F /k.conf -C -t events -o -1 -e'
```

- **После READY брокер ещё ~2 минуты рвёт соединения** (9.10, после
  создания пользователя): `Disconnected while requesting ApiVersion … connecting
  to a SSL listener?`. Конфиг не меняй — это не протокол. Опрашивай
  `timeout 60 kcat -F kcat.conf -L -t events` раз в 30 с до 10 минут, на
  самой ВМ.
- Каждый вызов клиента — под `timeout`: без брокера kcat ждёт долго.
- В продюсерах на librdkafka ставь `message.timeout.ms` (например 10000): по
  умолчанию продюсер ждёт доставку 5 минут и при неверном пароле просто висит.

## 5. Изменить и удалить

```bash
cloudru-dbaas kafka specs list --cluster-id <CLUSTER_ID> --json   # спецификации, доступные для resize (вживую не проверялось)
cloudru-dbaas kafka clusters resize <CLUSTER_ID> --spec <SPEC_ID> --storage-gib <N> --wait --json
cloudru-dbaas kafka topics delete <CLUSTER_ID> <TOPIC> --yes --wait --json
cloudru-dbaas kafka clusters delete <CLUSTER_ID> --yes --wait --json
```

- **Число брокеров после создания, по спецификации, не меняется**: в запросе
  изменения кластера его нет — выбирай `--brokers` сразу.
- Время resize и delete не замерялось: на коде `8` —
  `cloudru-dbaas kafka operations get <OP_ID> --wait --timeout 90m`.

Удаление необратимо. Удаляй только то, что сам создал в этой задаче, или по
прямому слову пользователя с названным ID.
