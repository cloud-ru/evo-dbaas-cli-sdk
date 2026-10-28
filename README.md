# cloudru-dbaas

CLI для Managed ClickHouse, Redis и Kafka в cloud.ru поверх Public API, плюс
скиллы для агентов, которые учат им пользоваться.

- `cloudru-dbaas` — все три продукта: `cloudru-dbaas clickhouse|redis|kafka …`,
  плюс то, что нужно кластеру вокруг: `zones list`, `network subnets|vpcs`
  (подсети и VPC) и `iam service-accounts|access-keys|roles` (сервисные
  аккаунты, ключи доступа, роли в проекте).

```bash
cloudru-dbaas redis clusters list
cloudru-dbaas kafka clusters get <CLUSTER_ID> --json
cloudru-dbaas clickhouse users create <CLUSTER_ID> app --password-stdin --role cloud_admin < app.pass
```

## Установка

Одной строкой, без root (в `~/.local/bin`, сверка sha256 по `checksums.txt`):

```bash
curl -fsSL https://raw.githubusercontent.com/cloud-ru/evo-dbaas-cli-sdk/main/install.sh | sh
curl -fsSL https://raw.githubusercontent.com/cloud-ru/evo-dbaas-cli-sdk/main/install.sh | sh -s -- --version v0.1.0
```

Или бинарник со страницы релизов: `cloudru-dbaas_<os>_<arch>` (linux, darwin,
windows; amd64, arm64), сверка — `checksums.txt`.

Или из исходников (Go — версия из `go.mod`):

```bash
go install github.com/cloud-ru/evo-dbaas-cli-sdk/cmd/cloudru-dbaas@latest
```

## Доступ

Нужен ключ доступа сервисного аккаунта (или персональный) с ролью на нужный
продукт в проекте, например `dbaas.clickhouse.admin`.

```bash
export CLOUDRU_KEY_ID=…
export CLOUDRU_KEY_SECRET=…
export CLOUDRU_PROJECT_ID=…      # или флаг --project
cloudru-dbaas clickhouse auth check
```

CLI сам обменивает ключ на токен IAM и кеширует токен в пользовательском
каталоге кеша (`cloudru-dbaas/`, права 0600). Пароли пользователей кластеров
передаются только через stdin (`--password-stdin`) или переменную окружения
(`--password-env`) — флага с самим паролем нет.

| Переменная | Зачем |
|---|---|
| `CLOUDRU_KEY_ID`, `CLOUDRU_KEY_SECRET` | ключ доступа |
| `CLOUDRU_PROJECT_ID` | проект по умолчанию |
| `CLOUDRU_CLICKHOUSE_API_URL` | другой адрес Public API ClickHouse (по умолчанию `https://clickhouse.api.cloud.ru`) |
| `CLOUDRU_IAM_AUTH_URL` | другой адрес выдачи токена IAM |
| `CLOUDRU_IAM_API_URL`, `CLOUDRU_VPC_API_URL`, `CLOUDRU_COMPUTE_API_URL` | другие адреса API IAM, VPC и виртуальных машин |

Адрес `http://` не на localhost CLI отвергает: ключ и токен ушли бы без TLS.
Стенд без TLS — `CLOUDRU_ALLOW_HTTP=1`, тогда только предупреждение.

Отдельный ключ для агента можно выпустить из CLI ключом с правом управлять
сервисными аккаунтами проекта:

```bash
cloudru-dbaas iam service-accounts create --name agent-clickhouse
cloudru-dbaas iam roles grant --subject <SA_ID> --role dbaas.clickhouse.admin
cloudru-dbaas iam access-keys create <SA_ID> --env-file agent.env   # секрет — только в файл 0600
```

## Для агентов

- `--json` у любой команды — ответ API как есть, одной строкой.
- Долгие операции (создание, resize, удаление) возвращают операцию; `--wait`
  ждёт её завершения, предел — `--timeout`.
- Коды выхода: `0` успех, `1` ошибка (и операция в облаке упала), `2` отмена
  (Ctrl-C), `4` ключ или доступ, `8` `--wait` не дождался — операция могла
  продолжиться, ресурс не пересоздавать.
- `--body '<JSON>'` у create и resize кластеров — поле API, для которого ещё
  нет флага.
- Скиллы в формате [agentskills.io](https://agentskills.io) — в `skills/`:
  `cloudru-managed-clickhouse`, `cloudru-managed-redis`,
  `cloudru-managed-kafka` и `cloudru-access-setup` (сервисный аккаунт, роли и
  свой ключ агента с нуля, без консоли). Скопируйте каталог целиком, вместе с
  `references/`, туда, где ваш агент ищет скиллы (например, `.claude/skills/`).
  В `SKILL.md` — основной сценарий, в `references/` — установка, ключ и роли,
  коды выхода и долгие операции: агент читает их, когда дошёл до шага.

## Разработка

```bash
go test ./...                 # в том числе сверка CLI со спецификацией OpenAPI
scripts/generate.sh --fetch   # свежие спецификации с cloud.ru/docs и перегенерация клиентов
```

- Клиенты API сгенерированы [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen)
  из `api/openapi-*.yaml` (опубликованы в документации cloud.ru); команды
  написаны руками в `internal/cmd/`.
- `TestAPICoverage` проверяет, что каждая операция, поле и значение enum из
  спецификации доступны из CLI; `TestSkillCommandsParse` — что каждая команда
  из скиллов существует. Подробнее — `docs/api-coverage.md`.
- `tests/e2e/` — сетка e2e на Ginkgo: настоящий бинарник против фейкового облака,
  каждая команда; ожидаемое — в `tests/e2e/testdata/golden/`. Гоняется в `go test ./...`.
- Живой уровень той же сетки — `tests/e2e/live_test.go` за тегом `e2e`: создаёт, смотрит и
  удаляет платный кластер каждого продукта в настоящем cloud.ru. Переменные и команда запуска —
  в начале файла; перед релизом. Прогон — до двух часов.
- `scripts/stand-check.sh` ходит в живой API, слой 2 создаёт платные кластеры и проверяет
  пользователей, топики и resize, которых нет в живом уровне. Описание — в начале скрипта.
- Как вносить изменения — `CONTRIBUTING.md`, стиль — `CODESTYLE.md`, планка качества — `CONSTRAINTS.md`.
- Релиз — тег `vX.Y.Z`, бинарники собирает GoReleaser (`.github/workflows/release.yml`).

---

## Лицензия

MIT, см. [LICENSE](LICENSE).

## English

`cloudru-dbaas` is a command-line client for cloud.ru Managed ClickHouse, Redis
and Kafka, built on the public API, plus agent skills (agentskills.io format)
in `skills/`. Help texts and skills are in Russian.

Install a binary from Releases or `go install
github.com/cloud-ru/evo-dbaas-cli-sdk/cmd/cloudru-dbaas@latest`. Set
`CLOUDRU_KEY_ID`, `CLOUDRU_KEY_SECRET` and `CLOUDRU_PROJECT_ID`, then run
`cloudru-dbaas clickhouse auth check`. Add `--json` to any command for
machine-readable output. Run `go test ./...` before sending changes.

Licensed under MIT, see [LICENSE](LICENSE).
