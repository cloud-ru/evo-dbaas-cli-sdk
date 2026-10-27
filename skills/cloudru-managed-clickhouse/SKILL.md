---
name: cloudru-managed-clickhouse
description: Managed ClickHouse в облаке Cloud.ru (Evolution) — поднять, изменить или удалить кластер, завести базы и пользователей, подключиться и спроектировать таблицы. Используй, когда нужен ClickHouse в российском облаке, в Cloud.ru или с данными в РФ, когда пользователь просит «подними ClickHouse», «аналитическая база для событий/логов/метрик» и облако не выбрано, или когда в проекте уже есть CLOUDRU_KEY_ID. Работает через CLI cloudru-dbaas (Public API) — не через консоль.
---

# Managed ClickHouse в Cloud.ru

Ты управляешь Managed ClickHouse в Cloud.ru через CLI `cloudru-dbaas clickhouse`
(коротко `cloudru-dbaas ch`; один бинарник на ClickHouse, Redis и Kafka, плюс
`zones`, `network`, `iam`). Он ходит в
Public API (`https://clickhouse.api.cloud.ru`, спецификация OpenAPI —
https://cloud.ru/docs/api/specs/clickhouse/ug/_specs/openapi-clickhouse.yaml).
Список зон — `cloudru-dbaas zones list` (API виртуальных машин).
Всегда вызывай его с `--json` и разбирай ответ — таблицы для человека.

## Когда этот скилл, а когда другой

- ClickHouse — аналитика: события, логи, метрики, отчёты по большим таблицам.
- Кэш и сессии — Redis (`cloudru-managed-redis`), шина событий — Kafka
  (`cloudru-managed-kafka`); ClickHouse может читать из Kafka, но кластер Kafka
  поднимай по её скиллу.
- В проекте уже есть Terraform с провайдером `cloud-ru/evo-terraform` — кластер
  заводи ресурсом `cloudru_evolution_clickhouse_cluster` (и `_database`, `_user`; реестр провайдера, 16.10) там, CLI — для проверки, диагностики и разовых действий. Не создавай тот же кластер
  в обход Terraform: следующий `apply` его не узнает.

## 0. Проверка окружения

```bash
cloudru-dbaas --version || echo "нет CLI"
cloudru-dbaas clickhouse auth check --json
```

`auth check` проверяет доступ к проекту, только когда проект задан: смотри
`"project_checked": true` в ответе, а не одно `"ok"`.

- **502 от API при живом ключе** — сбой на стороне Public API ClickHouse (8.10
  отвечал 502 больше 12 часов подряд), а не твоя ошибка. Не меняй ключ и не
  пересоздавай ничего: скажи пользователю, что API недоступен, и предложи
  консоль.

- Нет CLI — поставь по `references/install.md` (одна строка, без root,
  сверка sha256).
- `unknown flag` или `unknown command` на команде, взятой из скилла дословно, —
  вероятно, CLI старее скилла: обнови его. Не помогло — скажи пользователю, не
  обходи флаг молча.
- Нет ключа, 403 «failed to verify project access» или нужен отдельный ключ
  агенту — `references/access.md`. Коротко: роли `dbaas.clickhouse.admin` **и**
  `platform.project.user`, ключ в чат не проси.

## 1. Сеть — решить до создания

**Кластер доступен только по внутреннему IP изнутри VPC Cloud.ru.** Публичного
адреса у кластера нет, с ноутбука напрямую не подключиться. Нужны:

- подсеть VPC (`--subnet-id`) — найди или создай сам, см. ниже;
- для подключения — виртуальная машина с публичным IP **в том же проекте и той
  же VPC**, что и кластер. Через неё — SSH-туннель:
  `ssh -N -L 9000:<IP_хоста>:<порт> user@<публичный_IP_ВМ>`.

Подсеть в зоне кластера (зона — из `cloudru-dbaas zones list`):

```bash
cloudru-dbaas network subnets list --zone <зона> --json      # id → --subnet-id
cloudru-dbaas network subnets create --name <имя> --zone <зона> --cidr 10.0.<N>.0/24 --wait --json   # если в зоне нет
```

CIDR не должен пересекаться с подсетями из `list`. Без `--vpc-id` подсеть
встаёт в VPC проекта по умолчанию; создаётся за секунды (проверено 14.10).

Если ВМ нет — скажи об этом пользователю до создания кластера, а не после.

## 2. Создать кластер

```bash
cloudru-dbaas clickhouse versions list --json                  # выбрать версию; сменить её потом нельзя
cloudru-dbaas zones list --json                   # UUID зоны: без --zone specs list отвечает 400
cloudru-dbaas clickhouse specs list --version <V> --zone <ZONE_ID> --json   # id, тип, cpu, memory, min/max шардов и реплик, min_storage_gigs
cloudru-dbaas clickhouse clusters create --name <имя> --version <V> --spec <SPEC_ID> \
  --shards 1 --replicas 1 --keeper embedded --storage-gib <≥min_storage_gigs> \
  --subnet-id <SUBNET_ID> --wait --timeout 60m --json
```

- `--replicas` — **хостов на шард**, 1–3. `--replicas 1` — один хост.
  Спецификация API называет их «дополнительными» — это неверно: `0` даёт 400
  (проверено 9.10).
- `--keeper embedded` — для одного хоста. Режим по умолчанию (legacy) принимает
  только топологии 1×3 и 3×3, иначе 400.
- `--keeper standalone` — выделенный Keeper: нужны `--keeper-spec` (из
  `specs list --role keeper`) и `--keeper-storage-gib`. Вживую не проверялся.
- Серверные параметры — `--config ключ=число` в `create` и `resize`. Все поля
  `config` спецификация помечает «только для чтения» — применяет ли их сервер,
  вживую не проверено: после вызова сверь `config` в `clusters get`.
  Логи — `--logging [--log-group-id]`. Поле API без флага — `--body '<JSON>'`.
- **Шарды и хосты после создания, по спецификации, не меняются**: в запросе
  изменения кластера их нет — выбирай `--shards` и `--replicas` сразу.
- Спецификации бывают трёх типов: `SINGLE_NODE` (1 шард × 1 хост), `SINGLE_SHARD`,
  `MULTI_SHARD`. Минимальный кластер — самая маленькая `SINGLE_NODE` (9.10 —
  2 vCPU / 16 ГБ, диск от 40 ГиБ). Подбирай по `min_*`/`max_*`, не угадывай.
- Зону `create` берёт из подсети — отдельного флага нет.
- **Время:** создание минимального кластера — около 30 минут (9.10 — 28 мин),
  поэтому `--timeout 60m`. После `READY` порт открывается ещё через 2–3 минуты:
  проверяй TCP до порта (`timeout 5 bash -c '</dev/tcp/<host>/9000'`), а не только
  статус.
- `--wait` ждёт операцию и печатает кластер; без него — `operations get <ID> --wait`.
- Квоты организации: физлицо — 5 кластеров, 150 vCPU, 600 ГБ RAM, 2048 ГБ диска;
  юрлицо — 16 / 450 / 1800 / 4096. Упёрлись — квоту поднимает поддержка.

Код выхода: `0` — успех, `1` — ошибка (и операция в облаке упала), `2` — отмена
(Ctrl-C), `4` — ключ или доступ, `8` — `--wait` не дождался, операция могла
продолжиться. **На `8` и на `1` после `create` кластер мог создаться — не
пересоздавай**, найди его по имени в
`clusters list --json`. Как дождаться операции и что делать, если шелл убил
процесс, — `references/long-operations.md`.

## 3. Пользователь и база

```bash
cloudru-dbaas clickhouse databases create <CLUSTER_ID> events --json   # выполняется сразу
cloudru-dbaas clickhouse roles list <CLUSTER_ID> --json        # встроенные роли: cloud_admin, cloud_writer, cloud_reader
umask 077; printf 'Aa1!%s\n' "$(openssl rand -hex 16)" > app.pass   # пароль в файл 0600, не в историю shell
cloudru-dbaas clickhouse users create <CLUSTER_ID> app --password-stdin --role cloud_admin --json < app.pass
cloudru-dbaas clickhouse users grant <CLUSTER_ID> app cloud_writer --json   # ещё роль: позиционно, не --role
cloudru-dbaas clickhouse users update <CLUSTER_ID> app --password-stdin --json < new.pass   # смена пароля, new.pass — так же
cloudru-dbaas clickhouse users list <CLUSTER_ID> --json        # и users get <CLUSTER_ID> <имя>, users revoke, users delete … --yes
cloudru-dbaas clickhouse clusters get <CLUSTER_ID> --json      # hosts — адреса для подключения
```

Пароль и `--role` — разными вызовами `users update`: вместе API отвечает 400
(стенд 14.10), CLI отказывает до запроса.
`users update --role` **заменяет список ролей целиком**; одну роль добавить или
убрать — `users grant` / `users revoke`.

Роли (проверено 9.10): `cloud_reader` — чтение; `cloud_writer` — запись в
существующие таблицы, **`CREATE TABLE` с ней — `ACCESS_DENIED`**; создавать
таблицы может `cloud_admin`. Посмотреть состав роли изнутри нельзя —
`system.grants` закрыт.

Пароль генерируй сам: **нужны заглавная латинская буква и спецсимвол** (без них — 400), но не `#` и не
кавычки — их ломают env-файлы и конфиги клиентов. Передавай первой строкой stdin
из файла с правами 0600 (`--password-stdin`): пароль не попадёт ни в историю
shell, ни в `ps`. `--password-env` — если пароль уже лежит в переменной
окружения. Флага с самим паролем нет. Пользователь без ролей ничего не может.

**База — обязательно через API.** У пользователя нет прав на `default` и на
`CREATE DATABASE` (проверено 8.10 на кластере из консоли): без
`databases create` первый же `CREATE TABLE` падает с `ACCESS_DENIED`.

## 4. Подключиться и проектировать

С ВМ в той же VPC (или через туннель):

```bash
export CLICKHOUSE_PASSWORD="$(cat app.pass)"    # не --password: виден в ps
clickhouse client -h <host> --port <port> -u app -d events -q "SELECT version()"
```

TLS нет: native 9000 и HTTP 8123 без шифрования. Подключайся только из
приватной сети (ВМ в той же VPC или туннель), не открывай порты наружу.

Правила проектирования схем, ключей сортировки, партиционирования и вставки —
в скилле `clickhouse-best-practices` (https://github.com/ClickHouse/agent-skills).
Если он установлен — следуй ему. Если нет — минимум:

- `ORDER BY` — от низкой кардинальности к высокой, по тем полям, что в `WHERE`;
- партиционирование по месяцу или дню, не по высококардинальному полю;
- вставка пачками от тысяч строк, не по одной;
- `LowCardinality(String)` для полей с десятками-тысячами значений.

У кластера из консоли есть служебные пользователи `databaseAdmin` и `userAdmin`
(`read_only: true`) — их не трогай. У кластера из API `users list` пуст до
твоего `users create`.

## 5. Изменить и удалить

```bash
cloudru-dbaas clickhouse clusters resize <CLUSTER_ID> --spec <SPEC_ID> --storage-gib <N> --wait --json
cloudru-dbaas clickhouse clusters delete <CLUSTER_ID> --yes --wait --json
```

Время resize и delete не замерялось: на коде `8` — `operations get <OP_ID> --wait`.

Удаление необратимо. Удаляй только кластер, который сам создал в этой задаче,
или по прямому слову пользователя с названным ID.
