# Мутации в CI — замер 08.11.2026

Обвязка гейта, без изменения поведения CLI. База — `main` на `ed3dada`.
Полный прогон и доказательство сделаны в копии без истории `main`; замер по
PR 83–86 — отдельно, на настоящей истории (раздел ниже). Ручные строки Go: **7902 → 7902**.
Новых тестов нет; существующие проверки, golden, `internal/gen/*`, `go.mod` и
`go.sum` не изменены. Слияние требует решения владельца из-за охраняемых файлов.

## Выбор инструмента и воспроизводимость

Установлен `go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0`.
`go version -m` подтверждает модуль v0.6.0; `gremlins --version` при такой установке
пишет `dev`. Флаги сверены по установленному `gremlins unleash --help`:

```text
  -D, --diff string                 diff branch or commit
      --threshold-efficacy float    threshold for code-efficacy percent
      --timeout-coefficient int     the coefficient by which the timeout is increased
```

На внешнем контрольном модуле из 20 покрытых сравнений (19 KILLED, 1 LIVED,
95% эффективности) обнаружено: **`--threshold-efficacy 100` возвращает 0**.
Viper v1.21.0 отдаёт числовой флаг строкой, а типизированный `configuration.Get`
Gremlins принимает только float64/int, поэтому порог становится нулём.
Через YAML порог работает: `efficacy: 96` на том же модуле даёт код 10,
`efficacy: 94.99999999999999` — код 0.

Запасной `go install github.com/avito-tech/go-mutesting/cmd/go-mutesting@v2.3.1`
не устанавливается; попытка с `/v2` также не проходит. Проверка версии сообщает:

```text
go: loading module retractions for github.com/avito-tech/go-mutesting/v2@v2.3.1: version "v2.3.1" invalid: go.mod has non-.../v2 module path "github.com/avito-tech/go-mutesting" (and .../v2/go.mod does not exist) at revision v2.3.1
```

Поэтому оставлен Gremlins v0.6.0 с работающим типизированным YAML внутри
охраняемого `Taskfile.yml`. Отдельного неохраняемого файла с порогом нет.
Его сравнение строгое (`эффективность <= порог` — ошибка); предыдущий float64
перед 94, `93.99999999999999`, реализует требование **эффективность ≥ 94%**.
N выведен из замера по PR 83–86 (ниже): floor худшего из принятых PR (№83, 94,55%).
Для другого целого N соседнее значение вычисляется `math.Nextafter(float64(N), math.Inf(-1))`.

Есть ещё три подтверждённые особенности v0.6.0:

- Пустой diff означает весь код. `task mutate` сначала проверяет наличие
  изменённых ручных Go-файлов; ошибочная база остаётся ошибкой.
- При двух близких изменениях внутри hunk парсер теряет второе изменение и
  включает неизменённую строку. На внешнем модуле изменены строки 3 и 5:
  обычный diff дал RUNNABLE на 3 и 4, `GIT_DIFF_OPTS=--unified=0` — на 3 и 5.
  Эта переменная задана только для задачи мутаций.
- Ноль выбранных операторов (например, меняется комментарий в Go) оценивается
  как 0% и ошибочно отклоняется. Обёртка разрешает только результат с нулём
  KILLED/LIVED/NOT VIABLE, нулём непокрытых и без таймаутов. Контрольные прогоны:
  комментарий — код 0; один LIVED — код Task 201; один NOT COVERED — код Task 201; один TIMED OUT — код Task 201.
  Остальные ошибки инструмента передаются без изменений.

Исходники особенностей: [типизированный Get](https://github.com/go-gremlins/gremlins/blob/v0.6.0/internal/configuration/configuration.go),
[порог и формула](https://github.com/go-gremlins/gremlins/blob/v0.6.0/internal/report/report.go),
[парсер diff](https://github.com/go-gremlins/gremlins/blob/v0.6.0/internal/diff/diff.go),
[go.mod запасного инструмента](https://github.com/avito-tech/go-mutesting/blob/v2.3.1/go.mod).

## Полный прогон

```sh
gremlins unleash --exclude-files '(^|/)(internal/gen|tools)/|_test\.go$' --workers 4 --timeout-coefficient 3 --output /tmp/cloudru-mutations/full.json
```

Без diff и без отсечения по порогу; стандартные операторы v0.6.0.
Go 1.27.3, Linux amd64, 4 CPU. Мутации запускают тесты своего пакета;
`-race` отдельно проверяет `task check`. Первый сбор покрытия занял 58.145 с.
Таймаут на мутанта — 3 × время сбора покрытия, в этом прогоне 174.435 с;
v0.6.0 не предоставляет флага абсолютного таймаута. Три зацикливания пагинации
завершились таймаутом, остальные мутанты получили результат без него.

| Область | Всего | Убито | Выжило | Не покрыто | Таймаут | Эффективность | Время |
|---|---:|---:|---:|---:|---:|---:|---:|
| Весь ручной код | 926 | 760 | 40 | 123 | 3 | 95,00% | 720,206 с |

Нежизнеспособных и пропущенных в полном прогоне — 0. Всего посчитано по всем
статусам, а не по `mutants_total` JSON: поле инструмента не включает непокрытые
и таймауты. Эффективность = KILLED / (KILLED + LIVED); непокрытые и таймауты
не входят в знаменатель. Подтверждённые таймауты: `internal/cloud/pages.go:65`
(INCREMENT_DECREMENT), `:66` (CONDITIONALS_NEGATION), `:122` (INCREMENT_DECREMENT).

Полный прогон короче 15 минут, поэтому `task mutate` включён в `task check`
после `ratchet`, перед `floor`. Установка закреплена в шаге `tools` CI.
GitHub Actions в этой копии не запускался: 720,206 с — локальный замер,
а не измеренное время runner CI. Малый diff из доказательства ниже занял
74,763 с и 78,371 с с учётом полного предварительного сбора покрытия.

## По пакетам

Числа — группировка единого полного прогона по каталогам. Для времени каждый
пакет повторно прогнан из своего каталога с исключением подкаталогов
(`--exclude-files '/|_test\.go$'`, `--workers 4`). Все статусы повторов сверены
с полным прогоном и совпали. Время повторов включает собственный сбор покрытия
и запуск Gremlins; складывать его с 720,206 с нельзя. Повторы частично
совпали по времени с другими локальными проверками и делили с ними CPU;
это измеренные локальные времена, не изолированный прогноз CI.

В отдельных повторах коэффициент таймаута увеличен до 100 для `cmd/cloudru-dbaas`,
`internal/auth`, `internal/cloud`, `internal/cmd`, `internal/cmd/base`,
`internal/cmd/clickhouse/database`, и до 1000 для остальных: тёплый сбор покрытия
короткого пакета занимает доли секунды, а компиляция мутанта дольше. Пробные
повторы с ложными таймаутами отброшены и заменены завершёнными повторами.
Эти коэффициенты относятся только к измерению времени пакетов, не к гейту.

| Пакет | Всего | Убито | Выжило | Не покрыто | Таймаут | Эффективность | Время повтора, с |
|---|---:|---:|---:|---:|---:|---:|---:|
| `cmd/cloudru-dbaas` | 1 | 0 | 0 | 1 | 0 | — | 31.395 |
| `internal/auth` | 45 | 43 | 1 | 1 | 0 | 97.73% | 31.442 |
| `internal/cloud` | 202 | 189 | 9 | 1 | 3 | 95.45% | 203.260 |
| `internal/cmd` | 28 | 18 | 9 | 1 | 0 | 66.67% | 44.840 |
| `internal/cmd/base` | 32 | 28 | 0 | 4 | 0 | 100.00% | 26.674 |
| `internal/cmd/clickhouse/auth` | 6 | 4 | 0 | 2 | 0 | 100.00% | 19.240 |
| `internal/cmd/clickhouse/cluster` | 70 | 35 | 0 | 35 | 0 | 100.00% | 33.208 |
| `internal/cmd/clickhouse/database` | 4 | 4 | 0 | 0 | 0 | 100.00% | 19.501 |
| `internal/cmd/clickhouse/operation` | 5 | 5 | 0 | 0 | 0 | 100.00% | 17.955 |
| `internal/cmd/clickhouse/role` | 1 | 1 | 0 | 0 | 0 | 100.00% | 6.305 |
| `internal/cmd/clickhouse/spec` | 13 | 13 | 0 | 0 | 0 | 100.00% | 21.560 |
| `internal/cmd/clickhouse/user` | 15 | 12 | 0 | 3 | 0 | 100.00% | 23.013 |
| `internal/cmd/clickhouse/version` | 1 | 1 | 0 | 0 | 0 | 100.00% | 5.602 |
| `internal/cmd/iam` | 26 | 25 | 1 | 0 | 0 | 96.15% | 26.404 |
| `internal/cmd/kafka/cluster` | 50 | 31 | 0 | 19 | 0 | 100.00% | 25.902 |
| `internal/cmd/kafka/operation` | 5 | 5 | 0 | 0 | 0 | 100.00% | 15.535 |
| `internal/cmd/kafka/role` | 1 | 1 | 0 | 0 | 0 | 100.00% | 4.909 |
| `internal/cmd/kafka/spec` | 10 | 10 | 0 | 0 | 0 | 100.00% | 17.508 |
| `internal/cmd/kafka/topic` | 9 | 9 | 0 | 0 | 0 | 100.00% | 17.124 |
| `internal/cmd/kafka/user` | 18 | 15 | 3 | 0 | 0 | 83.33% | 22.466 |
| `internal/cmd/kafka/version` | 1 | 1 | 0 | 0 | 0 | 100.00% | 4.901 |
| `internal/cmd/network` | 25 | 25 | 0 | 0 | 0 | 100.00% | 27.540 |
| `internal/cmd/postgresql/backup` | 1 | 1 | 0 | 0 | 0 | 100.00% | 4.799 |
| `internal/cmd/postgresql/cluster` | 87 | 69 | 3 | 15 | 0 | 95.83% | 54.553 |
| `internal/cmd/postgresql/database` | 5 | 5 | 0 | 0 | 0 | 100.00% | 18.050 |
| `internal/cmd/postgresql/operation` | 5 | 5 | 0 | 0 | 0 | 100.00% | 17.245 |
| `internal/cmd/postgresql/spec` | 3 | 3 | 0 | 0 | 0 | 100.00% | 13.005 |
| `internal/cmd/postgresql/user` | 10 | 10 | 0 | 0 | 0 | 100.00% | 21.107 |
| `internal/cmd/postgresql/version` | 1 | 1 | 0 | 0 | 0 | 100.00% | 5.028 |
| `internal/cmd/redis/cluster` | 26 | 22 | 4 | 0 | 0 | 84.62% | 28.304 |
| `internal/cmd/redis/operation` | 5 | 5 | 0 | 0 | 0 | 100.00% | 17.468 |
| `internal/cmd/redis/role` | 1 | 1 | 0 | 0 | 0 | 100.00% | 4.789 |
| `internal/cmd/redis/spec` | 10 | 10 | 0 | 0 | 0 | 100.00% | 18.978 |
| `internal/cmd/redis/user` | 13 | 13 | 0 | 0 | 0 | 100.00% | 19.290 |
| `internal/cmd/redis/version` | 1 | 1 | 0 | 0 | 0 | 100.00% | 4.809 |
| `internal/cmd/zones` | 3 | 3 | 0 | 0 | 0 | 100.00% | 13.354 |
| `internal/config` | 91 | 76 | 5 | 10 | 0 | 93.83% | 30.612 |
| `internal/i18n` | 5 | 5 | 0 | 0 | 0 | 100.00% | 2.990 |
| `internal/operation` | 26 | 10 | 4 | 12 | 0 | 71.43% | 6.276 |
| `internal/output` | 16 | 11 | 0 | 5 | 0 | 100.00% | 4.336 |
| `internal/state` | 18 | 9 | 1 | 8 | 0 | 90.00% | 19.572 |
| `internal/transport` | 31 | 25 | 0 | 6 | 0 | 100.00% | 53.198 |

Пакеты `internal/cmd/{clickhouse,kafka,postgresql,redis}` не содержат операторов,
для которых стандартный набор создаёт мутанты: по 0, эффективность не определена.

## Замер по PR 83–86

Каждый PR переигран в отдельной копии на его коммите в `main` (№86 — на голове
ветки PR), база — родитель (№86 — `merge-base` с `main`). Флаги те же, что у
`task mutate`, порог выключен. Локально, 8.11.

| PR | Всего | Убито | Выжило | Не покрыто | Эффективность | Время |
|---|---:|---:|---:|---:|---:|---:|
| №83 PostgreSQL, ч. 1 | 122 | 104 | 6 | 12 | 94,55% | 130 с |
| №84 PostgreSQL, ч. 2 | 22 | 19 | 0 | 3 | 100% | 80 с |
| №85 page_size и specs | 2 | 2 | 0 | 0 | 100% | 63 с |
| №86 бэкапы по умолчанию | 3 | 3 | 0 | 0 | 100% | 67 с |

Порог 94 % = floor худшего из принятых (№83): все четыре PR прошли бы гейт.
Выжившие №83:

- `internal/cmd/root.go:143` — `product == "postgresql" → !=`: нет теста, что
  `--api-url` подменяет адрес PostgreSQL. Похоже на настоящий пробел (на всём
  коде так же выживают строки 140–142 для других продуктов).
- `internal/cloud/postgresql.go:224`, `:243` (`WALGiB > 0`),
  `internal/cmd/postgresql/cluster/cluster.go:96` (`len(cl.Zones) > 0`),
  `topology.go:53` (`sp.MaxHosts > 0`), `update.go:78` (`DataGiB > 0`) —
  граница `> 0 → >= 0`. Похоже на эквивалентные мутанты (при нуле ветка
  ничего не меняет), по одному не разобраны.

Риск порога: на маленьком диффе один выживший роняет процент (2 мутанта — 50 %).
Красный от эквивалентного мутанта — разбирать выжившего, а не снижать порог.

## Выжившие: 30 кандидатов из 40

Первыми — условия отказа, выбор конфигурации и обработка ошибок, далее —
границы и значения времени. Это кандидаты на неполные проверки; эквивалентность
мутантов и необходимость новых тестов отдельно не разбирались.

- `internal/cmd/redis/cluster/resize.go:24` — `==` → `!=`, столбец 14; `if u.Spec == "" && u.StorageGiB == 0 && len(params) == 0 && body == "" {`
- `internal/cmd/redis/cluster/resize.go:24` — `==` → `!=`, столбец 56; `if u.Spec == "" && u.StorageGiB == 0 && len(params) == 0 && body == "" {`
- `internal/cmd/redis/cluster/resize.go:24` — `==` → `!=`, столбец 69; `if u.Spec == "" && u.StorageGiB == 0 && len(params) == 0 && body == "" {`
- `internal/cmd/root.go:140` — `==` → `!=`, столбец 52; `{&a.st.ClickHouseURL, cfg.ClickHouseURL, product == "clickhouse" && cmd.Flags().Changed("api-url")},`
- `internal/cmd/root.go:141` — `==` → `!=`, столбец 42; `{&a.st.RedisURL, cfg.RedisURL, product == "redis" && cmd.Flags().Changed("api-url")},`
- `internal/cmd/root.go:142` — `==` → `!=`, столбец 42; `{&a.st.KafkaURL, cfg.KafkaURL, product == "kafka" && cmd.Flags().Changed("api-url")},`
- `internal/cmd/root.go:143` — `==` → `!=`, столбец 52; `{&a.st.PostgreSQLURL, cfg.PostgreSQLURL, product == "postgresql" && cmd.Flags().Changed("api-url")},`
- `internal/cmd/root.go:150` — `==` → `!=`, столбец 74; `if cmd.Flags().Changed("api-url") && cmd.Flag("api-url").Value.String() == "" {`
- `internal/config/write.go:205` — `==` → `!=`, столбец 9; `if err == nil {`
- `internal/config/write.go:209` — `==` → `!=`, столбец 30; `if cerr := tmp.Close(); err == nil {`
- `internal/cmd/iam/accesskey.go:90` — `==` → `!=`, столбец 32; `if cerr := fh.Close(); werr == nil {`
- `internal/operation/operation.go:186` — `==` → `!=`, столбец 15; `if failures == 0 && delay < 4*opts.Delay {`
- `internal/operation/operation.go:186` — `<` → `<=`, столбец 29; `if failures == 0 && delay < 4*opts.Delay {`
- `internal/operation/operation.go:186` — `<` → `>=`, столбец 29; `if failures == 0 && delay < 4*opts.Delay {`
- `internal/operation/operation.go:186` — `*` → `/`, столбец 32; `if failures == 0 && delay < 4*opts.Delay {`
- `internal/cmd/kafka/user/create.go:57` — `>` → `>=`, столбец 20; `if (c < 'a' || c > 'z') && (c < '0' || c > '9') {`
- `internal/cmd/kafka/user/create.go:57` — `<` → `<=`, столбец 33; `if (c < 'a' || c > 'z') && (c < '0' || c > '9') {`
- `internal/cmd/kafka/user/create.go:57` — `>` → `>=`, столбец 44; `if (c < 'a' || c > 'z') && (c < '0' || c > '9') {`
- `internal/cmd/postgresql/cluster/topology.go:53` — `<` → `<=`, столбец 36; `if sp.MaxHosts > 0 && sp.MaxHosts < maxHosts {`
- `internal/cmd/postgresql/cluster/update.go:92` — `>` → `>=`, столбец 81; `if u.DataGiB > 0 && !cmd.Flags().Changed("wal-gib") && current.Storage.WALGiB > 0 {`
- `internal/cloud/postgresql.go:247` — `>` → `>=`, столбец 14; `if c.WALGiB > 0 {`
- `internal/cloud/postgresql.go:266` — `>` → `>=`, столбец 15; `if u.WALGiB > 0 {`
- `internal/cmd/redis/cluster/resize.go:28` — `>` → `>=`, столбец 36; `if u.Spec != "" && u.StorageGiB > 0 {`
- `internal/cloud/cloud.go:86` — `>` → `>=`, столбец 46; `if resp.StatusCode < 200 || resp.StatusCode > 299 {`
- `internal/cmd/root.go:75` — `*` → `/`, столбец 62; `root.PersistentFlags().DurationVar(&a.timeout, "timeout", 30*time.Minute, i18n.T("cmd.flag.timeout"))`
- `internal/state/connect.go:79` — `*` → `/`, столбец 33; `hc := &http.Client{Timeout: 60 * time.Second, Transport: transport.New(http.DefaultTransport, transport.Options{`
- `internal/cloud/pages.go:128` — `<` → `<=`, столбец 25; `if !changed && (total < 0 || len(all) == total) {`
- `internal/cloud/pages.go:157` — `>=` → `>`, столбец 32; `changed = changed || (total >= 0 && t != total)`
- `internal/cloud/pages.go:161` — `<` → `<=`, столбец 48; `items, _, err := pageItems(page, path, total < 0 || offset >= total, "items")`
- `internal/cloud/pages.go:169` — `>=` → `>`, столбец 32; `if len(items) == 0 || (total >= 0 && offset >= total) || (total < 0 && len(items) < offsetPageLimit) {`

## Доказательство на настоящем тесте backup

Настройки и CI закоммичены до опыта (первые три коммита этой ветки).
В этой копии нет названного теста умолчаний PostgreSQL create. Использована
существующая матрица `TestClusterBackupFields` в
`internal/cmd/postgresql/cluster/backup_test.go`, проверяющая сохранение полей
backup при update. Временно удалён `assert.Equal(t, want, body.Backup)`.

В `update.go:68` временно записано `if nil == u.Backup.Schedule` вместо
`if u.Backup.Schedule == nil`: поведение сохранено, строка появилась в diff.
Настоящую поломку `==` → `!=` затем вносит Gremlins. Так обычные тесты до мутаций
проходят и до, и после восстановления assert, а сравнивается один и тот же мутант.
`--diff HEAD` работает; в обоих прогонах ровно один мутант проверен,
925 вне diff пропущены. Затем оба исходных файла восстановлены дословно.

Без assert, `task mutate BASE=HEAD`, код Task 201 (Gremlins 10), хвост дословно:

```text
     SKIPPED CONDITIONALS_NEGATION at internal/transport/transport.go:214:9
       LIVED CONDITIONALS_NEGATION at internal/cmd/postgresql/cluster/update.go:68:11

Mutation testing completed in 9 seconds 934 milliseconds
Killed: 0, Lived: 1, Not covered: 0
Timed out: 0, Not viable: 0, Skipped: 925
Test efficacy: 0.00%
Mutator coverage: 100.00%
ERROR: below efficacy-threshold
task: Failed to run task "mutate": exit status 10
```

После восстановления assert, `task mutate BASE=HEAD`, код 0, хвост дословно:

```text
     SKIPPED CONDITIONALS_BOUNDARY at internal/transport/transport.go:214:9
     SKIPPED CONDITIONALS_NEGATION at internal/transport/transport.go:216:11
      KILLED CONDITIONALS_NEGATION at internal/cmd/postgresql/cluster/update.go:68:11

Mutation testing completed in 18 seconds 912 milliseconds
Killed: 1, Lived: 0, Not covered: 0
Timed out: 0, Not viable: 0, Skipped: 925
Test efficacy: 100.00%
Mutator coverage: 100.00%
```

## Финальный гейт

```sh
PATH=/home/tot/.local/toolchains/go1.27.3/go/bin:/home/tot/go/bin:$PATH task check BASE=main
```

Первый локальный запуск потребовал исправить PATH: `gofmt` установлен в каталоге
Go, которого не было в исходном PATH. В финальном прогоне формат, линтер,
уязвимости, воспроизводимость 8 генерируемых файлов, тесты с `-race`, храповик
и мутации зелёные. Новых ручных Go-строк в этом PR нет, поэтому мутационная задача
на финальном diff не запускает анализ; её красный и зелёный пути проверены выше.
Пройдено 1773 теста; покрытие 96,2% при записанных 96,5% и допуске 0,5.
Единственная ошибка — ожидаемый floorguard по трём согласованно изменённым файлам.
Код Task 201, хвост дословно:

```text
mutate: нет изменённых строк ручного Go-кода
task: [floor] go run ./tools/floorguard -base main
floorguard: нарушений пола — 3:
  [правка-стража-или-гейтов] .github/workflows/ci.yml: tools/, Taskfile.yml, CI, хуки, генератор, .gitignore, go.work, vendor/ — решение человека
  [constraints-изменён] CONSTRAINTS.md: добавлены или удалены строки — решение человека
  [правка-стража-или-гейтов] Taskfile.yml: tools/, Taskfile.yml, CI, хуки, генератор, .gitignore, go.work, vendor/ — решение человека

Каждое опускает планку. Почините код; если ослабление нужно — это решение мейнтейнера, отдельным PR.
exit status 1
task: Failed to run task "check": task: Failed to run task "floor": exit status 1
```

Полнота относительно API: **103 операции, 218 полей тела, 46 query-параметров,
24 значения enum**; `TestAPICoverage` и `TestSkillCommandsParse` прошли.
В неизменённых исключениях — 37 записей: 6 операций, 9 полей, 20 query-параметров,
2 флага. Причины: пагинация/ожидание проверяются своим уровнем, неподдерживаемые
сценарии сети/IAM и реально отвергаемые API поля; полный список —
[`coverage-exceptions.yaml`](../internal/cmd/testdata/coverage-exceptions.yaml).
Живых вызовов не было; перечень непроверенных вживую возможностей сохраняется
в [`docs/api-coverage.md`](api-coverage.md).

Не проверены запуск и длительность в GitHub Actions (локально по диффу PR —
63–130 с), живой API. Ревью свежим контекстом пройдено 8.11 (гейт ok). Выжившие не исправлялись в этом PR: это замер
и новый гейт, не пересмотр поведения CLI или набора тестов.
