# Ключ доступа и роли — Kafka

Если установлен скилл `cloudru-access-setup` — веди по нему: там то же самое
целиком, с проверкой и уборкой. Ниже — минимум для этого продукта.

- Нет ключа — попроси пользователя создать **сервисный аккаунт** с ролями
  `dbaas.kafka.admin` и `platform.project.user` в нужном проекте (`dbaas.kafka.dba` — без создания и удаления, `dbaas.kafka.readonlyuser` — только просмотр), выпустить
  ключ доступа и выставить `CLOUDRU_KEY_ID`, `CLOUDRU_KEY_SECRET`,
  `CLOUDRU_PROJECT_ID`. Ключ в чат не проси — только переменные окружения или
  файл, который пользователь подключит сам.
- Без `platform.project.user` ключ получает токен, но на всё по проекту API
  отвечает 403 «failed to verify project access» (проверено 14.10). Тот же
  ответ бывает и сбоем облака — отличай по `iam roles list --subject`: есть ли
  там `platform.project.user`.
- У ключа пользователя есть `platform.project.admin` (право управлять
  аккаунтами) — выпусти себе отдельный ключ с ролями продукта тем же CLI и
  работай им; пользователю скажи, что создал. Файл ключа — в `.gitignore`:

```bash
cloudru-dbaas iam service-accounts create --name agent-kafka --json        # id
cloudru-dbaas iam roles grant --subject <SA_ID> --role dbaas.kafka.admin
cloudru-dbaas iam roles grant --subject <SA_ID> --role platform.project.user
cloudru-dbaas iam roles list --subject <SA_ID> --json      # обе роли на месте?
cloudru-dbaas iam access-keys create <SA_ID> --ttl 24h --env-file .cloudru-agent.env
( set -a; . ./.cloudru-agent.env; set +a; cloudru-dbaas kafka clusters list --json )   # узким ключом
```

Узким ключом работай в подшелле (или отдельном терминале), а снаружи оставь
ключ пользователя: удалить аккаунт можно только им — у узкого ключа прав на
`iam` нет.

Секрет CLI пишет только в новый файл (0600) и не печатает. Выдача роли может
упасть EOF и не примениться (14.10) — поэтому `roles list` и повтор
недостающей. Закончил — `iam service-accounts delete <SA_ID> --yes` ключом пользователя
(удаляет и ключи). Что этой парой ролей можно создать кластер, 14.10 не проверялось —
проверено только чтение.
