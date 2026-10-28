---
name: cloudru-access-setup
description: Доступ агента к Cloud.ru (Evolution) с нуля — сервисный аккаунт, роли в проекте, свой ключ доступа в env-файл, проверка и уборка — через CLI cloudru-dbaas, без браузера и консоли. Используй, когда нужно работать с Managed Redis, Kafka или ClickHouse в Cloud.ru, а ключа нет; когда на запросы по проекту приходит 403 «failed to verify project access»; или когда агенту нужен свой ключ с узкими правами вместо ключа пользователя.
---

# Доступ к Cloud.ru с нуля

Ты заводишь агенту доступ к проекту Cloud.ru через CLI `cloudru-dbaas iam`
(Public API IAM). Цель — **свой ключ с ролями только под задачу**, а не работа
ключом пользователя. Первый ключ всегда приходит от человека: из ничего
аккаунт не создать.

## 0. Что уже есть

```bash
cloudru-dbaas --version || echo "нет CLI"
```

Нет CLI — одна строка, без root, со сверкой sha256:
`curl -fsSL https://raw.githubusercontent.com/cloud-ru/evo-dbaas-cli-sdk/main/install.sh | sh`,
затем `export PATH="$HOME/.local/bin:$PATH"`. Не скачалось — скажи
пользователю, бинарник из других мест не ставь.

Дальше посмотри, что CLI возьмёт и откуда — переменные `CLOUDRU_*` и файл конфига
с профилями (`--profile` > `CLOUDRU_PROFILE` > строка `profile:` в файле > `default`):

```bash
cloudru-dbaas config list --json        # ключ доступа не нужен; key_secret — маской
```

У каждого ключа `source`: `flag`, `env CLOUDRU_…`, `file <профиль>`, `default`, а `none` —
нигде не задан. Ответ `unknown command "config"` — CLI старше v0.2.1: обнови его той же
строкой установки, а не вышло — смотри наличие переменных `CLOUDRU_KEY_ID`,
`CLOUDRU_KEY_SECRET`, `CLOUDRU_PROJECT_ID` и файла конфига сам. Сам файл и переменные не печатай. Ошибка про файл конфига (неизвестный
ключ, нет профиля) — покажи её пользователю, файл не правь. Файл конфига принадлежит
пользователю: `config set` и `config use-profile` — только по его просьбе. Свой ключ из §3
держи в env-файле, а не в профиле: переменные пользователя перекрыли бы профиль. Дальше иди в нужную ветку:

| Что видишь | Куда |
|---|---|
| У `key_id` или `key_secret` в `config list` источник `none` | §1 — попросить ключ у пользователя |
| Ключ есть, запрос по проекту — 403 «failed to verify project access» | §2 |
| Ключ есть, `iam service-accounts list --json` отвечает без 403 — у него право управлять аккаунтами | §3 — выпустить себе свой |
| Ключ с ролями продукта работает | ничего не делай, работай им |

Проверка ключа на продукте задачи — список кластеров, а не только токен:

```bash
cloudru-dbaas redis clusters list --json          # или kafka / clickhouse — тот, что нужен
cloudru-dbaas clickhouse auth check --json        # для ClickHouse: смотри "project_checked": true
```

`auth check` без проекта проверяет только токен и отвечает `"ok": true,
"project_checked": false` — это ещё не доступ к проекту.

## 1. Ключа нет — попросить у пользователя

Ключ в чат не проси: только переменные окружения или файл, который
пользователь подключит сам. Предложи один из двух путей:

1. **Узкий ключ сразу.** Пользователь в консоли создаёт сервисный аккаунт с
   ролями из таблицы ниже, выпускает ключ доступа и выставляет три
   переменные. Ты сразу работаешь, §3 не нужен.
2. **Временный ключ с `platform.project.admin`.** Ты сам выпускаешь себе
   узкий ключ (§3), а пользователь потом отзывает временный. Удобно, когда
   задач несколько или роли заранее не ясны.

Роли в проекте (`--role`):

| Продукт | Создать, изменить, удалить | Без создания и удаления | Только просмотр |
|---|---|---|---|
| Redis | `dbaas.redis.admin` | `dbaas.redis.dba` | `dbaas.redis.readonlyuser` |
| Kafka | `dbaas.kafka.admin` | `dbaas.kafka.dba` | `dbaas.kafka.readonlyuser` |
| ClickHouse | `dbaas.clickhouse.admin` | не проверялось | не проверялось |

**К любой из них — `platform.project.user`.** Без неё ключ получает токен и
`versions list`, а на всё по проекту — 403 (проверено 14.10). Что пара
`dbaas.*.admin` + `platform.project.user` даёт создать кластер, не проверялось
— проверено только чтение. Права на подсети и VPC (`network …`) у этих ролей не
проверялись: получил 403 на `network` — скажи пользователю.

## 2. 403 «failed to verify project access»

Чаще всего у аккаунта нет `platform.project.user`. Но тот же ответ бывал и
сбоем облака (13.10 и 14.10 ночью) — не гадай, проверь роли:

```bash
cloudru-dbaas iam roles list --subject <SA_ID> --json
```

`roles list` требует права на аккаунты проекта. Его нет или id аккаунта
неизвестен — попроси пользователя посмотреть роли в консоли и добавить
`platform.project.user`. Роль на месте, а 403 держится — это облако: подожди
и повтори, ключ не перевыпускай.

## 3. Выпустить себе свой ключ

Ключом с `platform.project.admin`. Один аккаунт на задачу, имя — с задачей и
суффиксом, чтобы потом найти и удалить:

```bash
cloudru-dbaas iam service-accounts create --name agent-redis-<суффикс> \
  --description "агент: <задача>, удалить после" --json      # id → SA_ID
cloudru-dbaas iam roles grant --subject <SA_ID> --role dbaas.redis.admin
cloudru-dbaas iam roles grant --subject <SA_ID> --role platform.project.user
cloudru-dbaas iam roles list --subject <SA_ID> --json        # обе роли на месте?
cloudru-dbaas iam access-keys create <SA_ID> --ttl 24h --env-file .cloudru-agent.env
( set -a; . ./.cloudru-agent.env; set +a; cloudru-dbaas redis clusters list --json )   # узким ключом
```

Узким ключом работай в подшелле (или отдельном терминале), а снаружи оставь
ключ пользователя: удалить аккаунт можно только им — у узкого ключа прав на
`iam` нет.

- **Выдача роли может упасть EOF и не примениться** (14.10, на
  `platform.project.user`): поэтому `roles list` после выдачи и повтор
  недостающей.
- Секрет CLI пишет только в **новый** файл с правами 0600 и не печатает;
  существующий файл не перезаписывает. Добавь файл в `.gitignore` до
  `create`.
- `--ttl` — коротко, под задачу. Формат `1h` проверен вживую 14.10, `24h` — тот же.
- `platform.project.admin` своему аккаунту не выдавай: агенту нужен только
  продукт.
- В env-файле три переменные: `CLOUDRU_KEY_ID`, `CLOUDRU_KEY_SECRET` и
  `CLOUDRU_PROJECT_ID` того проекта, в котором выпущен ключ.
- Пользователю скажи, что создал: имя и id аккаунта, роли, срок ключа.

## 4. Убрать за собой

Задача закончена — удали аккаунт **ключом пользователя** (не узким); ключи
удалятся вместе с ним:

```bash
cloudru-dbaas iam service-accounts delete <SA_ID> --yes
cloudru-dbaas iam service-accounts list --json             # аккаунта больше нет
rm -f .cloudru-agent.env
```

Удаляй только аккаунт, который создал сам в этой задаче. Чужие аккаунты в
`service-accounts list` (например, `…-sa` с описанием «auto-generated service-account» —
такой создал сервис ML Finetuning, видно 16.10) не трогай.
