---
name: cloudru-managed-redis
description: Managed Redis в облаке Cloud.ru (Evolution) — поднять, изменить или удалить кластер, завести пользователей, подключиться. Используй, когда нужен Redis (кэш, очереди, сессии) в российском облаке, в Cloud.ru или с данными в РФ, когда пользователь просит «подними Redis» и облако не выбрано, или когда в проекте уже есть CLOUDRU_KEY_ID. Работает через CLI cloudru-dbaas (Public API) — не через консоль.
---

# Managed Redis в Cloud.ru

Ты управляешь Managed Redis в Cloud.ru через CLI `cloudru-dbaas redis`. Он ходит
в Public API (`https://redis.api.cloud.ru`, спецификация —
https://cloud.ru/docs/api/specs/redis/ug/_specs/openapi-redis.yaml).
Всегда вызывай его с `--json` и разбирай ответ — таблицы для человека.

## Когда этот скилл, а когда другой

- Redis — кэш, сессии, счётчики, лимиты запросов, очереди на списках.
- Шина событий между сервисами — не сюда: pub/sub в Managed Redis закрыт (§6),
  бери Kafka (скилл `cloudru-managed-kafka`). Аналитика по событиям — ClickHouse
  (`cloudru-managed-clickhouse`).
- В проекте уже есть Terraform с провайдером `cloud-ru/evo-terraform` — кластер
  заводи ресурсом `cloudru_evolution_redis_cluster` там, CLI — для пользователей (в реестре провайдера на 16.10 их нет), проверки и диагностики. Не создавай тот же кластер
  в обход Terraform: следующий `apply` его не узнает.

## 0. Проверка окружения

```bash
cloudru-dbaas --version || echo "нет CLI"
cloudru-dbaas redis clusters list --json     # ключ и доступ к проекту
```

- Нет CLI — поставь по `references/install.md` (одна строка, без root,
  сверка sha256).
- `unknown flag` или `unknown command` на команде, взятой из скилла дословно, —
  вероятно, CLI старее скилла: обнови его. Не помогло — скажи пользователю, не
  обходи флаг молча.
- Нет ключа, 403 «failed to verify project access» или нужен отдельный ключ
  агенту — `references/access.md`. Коротко: роли `dbaas.redis.admin` **и**
  `platform.project.user`, ключ в чат не проси.

## 1. Сеть — решить до создания

**Кластер доступен только по внутреннему IP изнутри VPC Cloud.ru.** Нужны
подсеть (`--subnet-id`) и ВМ с публичным IP в том же проекте, зоне и подсети —
с неё подключаешься (`redis-cli`) или строишь SSH-туннель. Если ВМ нет — скажи
пользователю до создания кластера.

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
cloudru-dbaas redis versions list --json                      # id версии; сменить потом нельзя
cloudru-dbaas zones list --json                               # зона: ru.AZ-1 / ru.AZ-2 / ru.AZ-3
cloudru-dbaas redis specs list --version-id <VERSION_ID> --zone ru.AZ-1 --json
cloudru-dbaas redis clusters create --name <имя> --version-id <VERSION_ID> --spec <SPEC_ID> \
  --type standalone --replicas 1 --storage-gib <≥min_storage_gigs> \
  --subnet-id <SUBNET_ID> --wait --timeout 60m --json
```

- **`--replicas` не меньше 1 даже для `standalone`**: с 0 API отвечает
  `replicas must be positive`.
- `specs list` требует и версию, и зону: без них API отвечает 400.
- Диск — **не меньше 2× RAM спецификации** и не меньше `min_storage_gigs`
  (поле `min_storage_gb` в ответе 0): см. §6.
- **Создание идёт ~23 минуты** (standalone, 9.10, один замер) — близко к 30
  минутам `--wait` по умолчанию, поэтому `--timeout 60m` с запасом. Предупреди пользователя заранее.
- `--wait` ждёт операцию и печатает кластер; без него —
  `cloudru-dbaas redis operations get <OP_ID> --wait`.
- **READY — ещё не готов.** Порт открывается через 5–9 минут после READY
  (8.10 и 9.10), до этого — таймаут или `Connection reset by peer`. Проверяй
  `ACL WHOAMI` (не `PING`, см. §3) раз в 30 с до 10 минут, каждый вызов
  клиента — под `timeout 10`: на закрытом порту `redis-cli` висит. Опрос
  запускай на самой ВМ, а не новым SSH каждые 20 с — частые подключения ВМ
  обрывает.

Код выхода: `0` — успех, `1` — ошибка (и операция в облаке упала), `2` — отмена
(Ctrl-C), `4` — ключ или доступ, `8` — `--wait` не дождался, операция могла
продолжиться. **На `8` и на `1` после `create` кластер мог создаться — не
пересоздавай**, найди его по имени в
`clusters list --json`. Как дождаться операции и что делать, если шелл убил
процесс, — `references/long-operations.md`.

## 3. Пользователь

```bash
cloudru-dbaas redis roles list <CLUSTER_ID> --json
umask 077; printf 'Aa1!%s\n' "$(openssl rand -hex 16)" > app.pass   # пароль в файл 0600, не в историю shell
cloudru-dbaas redis users create <CLUSTER_ID> app --password-stdin \
  --role cloud_admin --role cloud_writer --role cloud_reader --json < app.pass
cloudru-dbaas redis clusters get <CLUSTER_ID> --json          # redis_hosts — адрес для подключения
cloudru-dbaas redis users update <CLUSTER_ID> app --password-stdin --json < new.pass   # смена пароля, роли сохраняются
cloudru-dbaas redis users list <CLUSTER_ID> --json            # и users get <CLUSTER_ID> <имя>, users delete … --yes
```

`users update --role` **заменяет список ролей целиком**: передай все нужные
роли, а не только новую.

Параметры кластера — `--param ключ=значение` в `clusters create` и `resize`,
логи — `--logging`. Поле API без флага — `--body '<JSON>'`.

Имя `default` занято сервисом. Пароль генерируй сам, **со спецсимволом** (без
него API отвечает 400, требования нигде не написаны). Бери `!`, `-` или `_`:
`#`, `$`, кавычки и пробел ломают env-файлы и конфиги клиентов. Передавай первой
строкой stdin из файла с правами 0600 (`--password-stdin`): пароль не попадёт ни
в историю shell, ни в `ps`. `--password-env` — если пароль уже в переменной окружения.

**Чего роли не дают** (`ACL GETUSER`, 5.10 и 9.10): при любом наборе ролей
`PING` запрещён (`-ping`, ответ `NOPERM`), pub/sub закрыт (`resetchannels`,
`PUBLISH` — «No permissions to access a channel»). Хелсчеки на `PING` в
клиентах и библиотеках заменяй на `ACL WHOAMI` или `SET`/`GET`. Чтение и
запись даёт набор `cloud_admin` + `cloud_writer` + `cloud_reader`.

## 4. Подключиться

С ВМ в той же подсети:

```bash
sudo apt-get update && sudo apt-get install -y redis-tools   # без update — 404 на устаревшем индексе
export REDISCLI_AUTH='<пароль>'      # не --pass: пароль виден в ps
timeout 10 redis-cli -h <внутренний_IP> -p <порт> --user app SET hello world | grep -x OK
```

- TLS нет: только порт 6379 без шифрования. Подключайся только из приватной
  сети (ВМ в той же VPC или туннель), порт наружу не открывай.
- **`redis-cli` выходит с кодом 0 и на `NOAUTH`/`WRONGPASS`.** Проверяй ответ
  (`grep -x OK`, `ACL WHOAMI`), а не код выхода.
- В клиентах ограничивай переподключение: ioredis по умолчанию переподключается
  бесконечно и при недоступном хосте висит.

## 5. Изменить и удалить

```bash
cloudru-dbaas redis specs list --cluster-id <CLUSTER_ID> --json   # цели resize
cloudru-dbaas redis clusters resize <CLUSTER_ID> --storage-gib <N> --wait --json   # сначала диск
cloudru-dbaas redis clusters resize <CLUSTER_ID> --spec <SPEC_ID> --wait --json    # затем спецификация
cloudru-dbaas redis clusters delete <CLUSTER_ID> --yes --wait --json
```

- `--spec` и `--storage-gib` — разными вызовами: вместе API отвечает 400
  «conflicting update fields» (стенд 14.10), CLI отказывает до запроса.
- Диск под новую спецификацию — большее из 2× её RAM и `min_storage_gigs` (§6).
  Поднять диск до смены спецификации: в этом порядке resize прошёл (стенд 14.10).

- Тип, число шардов и реплик после создания, по спецификации, не меняются: в
  запросе изменения кластера их нет.
- Время resize и delete не замерялось: на коде `8` —
  `cloudru-dbaas redis operations get <OP_ID> --wait`.

Удаление необратимо. Удаляй только кластер, который сам создал в этой задаче,
или по прямому слову пользователя с названным ID.

## 6. Чего нет в Managed Redis — сказать до того, как строить

**Pub/sub закрыт для всех ролей.** У `cloud_admin`, `cloud_writer` и
`cloud_reader` пустой список каналов (`resetchannels` в `ACL GETUSER`):
`PUBLISH` и `SUBSCRIBE` отвечают «No permissions to access a channel», хотя
сами команды разрешены (Redis 7.2.15: 5.10, 9.10 и на свежем кластере 14.10).

- Открыть каналы себе нельзя: у ролей `-@admin`, а `ACL SETUSER` — команда
  этой категории (вывод по строке ACL, вызов не пробовали). Каналы открыты только
  у `default`, а его пароль клиент не получает (из `ACL GETUSER` 5.10).
- Приложение на pub/sub (уведомления, чат, инвалидация кэша по событию) —
  скажи пользователю **до создания кластера** и предложи Kafka. Просьба
  открыть каналы ушла разработке 14.10; ответа на 16.10 нет — проверь сам
  (пароль — в `REDISCLI_AUTH`, §4):

```bash
timeout 10 redis-cli -h <внутренний_IP> -p <порт> --user app PUBLISH probe 1
```

  Число в ответе — каналы открыли, `NOPERM` — всё ещё закрыто.
- Там же закрыты `PING` (`-ping`, хелсчеки — на `ACL WHOAMI`, §3) и `CONFIG`
  (`-config` в той же строке `ACL GETUSER`, 5.10 и 14.10): уведомления о ключах (`notify-keyspace-events`) клиент не
  включит.

**Диск — не меньше 2× RAM спецификации**, рекомендовано 4×: запас нужен
на перезапись AOF ([документация](https://cloud.ru/docs/redis/ug/topics/concepts__persistence)).
Отклоняет ли API диск меньше 2× RAM, не проверялось — не полагайся на отказ,
считай сам: RAM — из `specs list`, диск — `max(2 × RAM, min_storage_gigs)`.
