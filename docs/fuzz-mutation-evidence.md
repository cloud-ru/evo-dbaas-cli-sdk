# Доказательства fuzz-проверок

Все перечисленные изменения временные и восстановлены. Красные прогоны
завершаются из-за проверяемой регрессии, а не ошибки сборки.

## JSON-пробелы

Проверка EOF заменена на `d.InputOffset() != int64(len(raw))`; неиспользуемый io удалён только в мутанте.

Команда: `go test ./internal/cloud -run '^TestPaginationWhitespace$' -count=1`.

Красный прогон, exit 1, дословный хвост:

```text
--- FAIL: TestPaginationWhitespace (0.00s)
    --- FAIL: TestPaginationWhitespace/lf/offset=false (0.00s)
        pages_fuzz_test.go:344: valid whitespace page rejected: calls=1, items=[], err=list not assembled: trailing data in response whitespace
    --- FAIL: TestPaginationWhitespace/lf/offset=true (0.00s)
        pages_fuzz_test.go:344: valid whitespace page rejected: calls=1, items=[], err=list not assembled: trailing data in response whitespace
    --- FAIL: TestPaginationWhitespace/crlf-tab-space/offset=true (0.00s)
        pages_fuzz_test.go:344: valid whitespace page rejected: calls=1, items=[], err=list not assembled: trailing data in response whitespace
    --- FAIL: TestPaginationWhitespace/crlf-tab-space/offset=false (0.00s)
        pages_fuzz_test.go:344: valid whitespace page rejected: calls=1, items=[], err=list not assembled: trailing data in response whitespace
FAIL
FAIL	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud	0.014s
FAIL
```

После восстановления, exit 0, дословный хвост:

```text
ok  	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud	0.018s
```

## Префикс UUID

Перед uuid.Parse добавлено `if len(s) > 36 { s = strings.TrimPrefix(s, " ") }`.

Команда: `go test ./internal/cloud -run '^TestParseUUIDLengthBoundaries$' -count=1`.

Красный прогон, exit 1, дословный хвост:

```text
--- FAIL: TestParseUUIDLengthBoundaries (0.00s)
    --- FAIL: TestParseUUIDLengthBoundaries/36/prefix/"_" (0.00s)
        compute_fuzz_test.go:85: prefixed UUID accepted: " 123e4567-e89b-12d3-a456-426614174000" -> 123e4567-e89b-12d3-a456-426614174000, <nil>
    --- FAIL: TestParseUUIDLengthBoundaries/45/prefix/"_" (0.00s)
        compute_fuzz_test.go:85: prefixed UUID accepted: " urn:uuid:123e4567-e89b-12d3-a456-426614174000" -> 123e4567-e89b-12d3-a456-426614174000, <nil>
    --- FAIL: TestParseUUIDLengthBoundaries/38/prefix/"_" (0.00s)
        compute_fuzz_test.go:85: prefixed UUID accepted: " {123e4567-e89b-12d3-a456-426614174000}" -> 123e4567-e89b-12d3-a456-426614174000, <nil>
FAIL
FAIL	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud	0.013s
FAIL
```

После восстановления, exit 0, дословный хвост:

```text
ok  	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud	0.014s
```

## Shell glob и тильда

В безопасный набор shellValue добавлены `*~`.

Команда: `go test ./internal/cmd/iam -run '^FuzzShellValue$' -count=1`.

Красный прогон, exit 1, дословный хвост:

```text
--- FAIL: FuzzShellValue (0.01s)
    --- FAIL: FuzzShellValue/seed#11 (0.00s)
        accesskey_fuzz_test.go:34: active shell syntax outside quotes: "*"
    --- FAIL: FuzzShellValue/seed#12 (0.00s)
        accesskey_fuzz_test.go:34: active shell syntax outside quotes: "~"
FAIL
FAIL	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/iam	0.027s
FAIL
```

После восстановления, exit 0, дословный хвост:

```text
ok  	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/iam	0.024s
```

## Основание числа

`strconv.ParseInt(v, 10, 64)` заменён на `strconv.ParseInt(v, 0, 64)`.

Команда: `go test ./internal/cloud -run '^FuzzParseClickHouseConfig$' -count=1`.

Красный прогон, exit 1, дословный хвост:

```text
    --- FAIL: FuzzParseClickHouseConfig/seed#118 (0.00s)
        clickhouse_fuzz_test.go:41: invalid config accepted or flag lost: "max_table_size_to_drop"="0x10" -> &{<nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> 0x2f1ca0032838 <nil>}, <nil>
    --- FAIL: FuzzParseClickHouseConfig/seed#119 (0.00s)
        clickhouse_fuzz_test.go:62: config field lost or rounded: {"max_table_size_to_drop":8}; want max_table_size_to_drop=10
    --- FAIL: FuzzParseClickHouseConfig/seed#120 (0.00s)
        clickhouse_fuzz_test.go:41: invalid config accepted or flag lost: "max_table_size_to_drop"="1_0" -> &{<nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> 0x2f1ca00328d8 <nil>}, <nil>
    --- FAIL: FuzzParseClickHouseConfig/seed#121 (0.00s)
        clickhouse_fuzz_test.go:48: valid config rejected: "max_table_size_to_drop"="00000000008": --fuzz-config max_table_size_to_drop="00000000008": an integer is required
    --- FAIL: FuzzParseClickHouseConfig/seed#132 (0.00s)
        clickhouse_fuzz_test.go:41: invalid config accepted or flag lost: "uncompressed_cache_size"="0x10" -> &{<nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> 0x2f1ca0032b50}, <nil>
    --- FAIL: FuzzParseClickHouseConfig/seed#133 (0.00s)
        clickhouse_fuzz_test.go:62: config field lost or rounded: {"uncompressed_cache_size":8}; want uncompressed_cache_size=10
    --- FAIL: FuzzParseClickHouseConfig/seed#134 (0.00s)
        clickhouse_fuzz_test.go:41: invalid config accepted or flag lost: "uncompressed_cache_size"="1_0" -> &{<nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> 0x2f1ca0032bf0}, <nil>
    --- FAIL: FuzzParseClickHouseConfig/seed#135 (0.00s)
        clickhouse_fuzz_test.go:48: valid config rejected: "uncompressed_cache_size"="00000000008": --fuzz-config uncompressed_cache_size="00000000008": an integer is required
FAIL
FAIL	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud	0.047s
FAIL
```

После восстановления, exit 0, дословный хвост:

```text
ok  	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud	0.048s
```

## Наличие FuzzOneOf

Удалена функция FuzzOneOf, остальные функции пакета оставлены.

Команда: `task fuzz`.

Красный прогон, exit 201, дословный хвост:

```text
fuzz: required target FuzzOneOf is missing from ./internal/cmd/base
task: Failed to run task "fuzz": exit status 1
```

После восстановления, exit 0, дословный хвост:

```text
ok  	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/kafka/user	6.069s
fuzz: elapsed: 0s, gathering baseline coverage: 0/37 completed
fuzz: elapsed: 0s, gathering baseline coverage: 37/37 completed, now fuzzing with 2 workers
fuzz: elapsed: 3s, execs: 15269 (5085/sec), new interesting: 0 (total: 37)
fuzz: elapsed: 6s, execs: 23712 (2816/sec), new interesting: 0 (total: 37)
fuzz: elapsed: 6s, execs: 23712 (0/sec), new interesting: 0 (total: 37)
PASS
ok  	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/iam	6.060s
```

Полный восстановленный task fuzz: 13 PASS, 109.829 с по монотонным часам.

## UUID probe (до фикса)

Вывод ниже — поведение до исправления `parseUUID`; теперь такие обёртки дают `ErrNotUUID`.

Локальный probe выполнял `parseUUID("resource", input)` для двух обёрток,
сверял возвращённый UUID и отсутствие ошибки. Он не включён в постоянные
тесты, чтобы не объявлять принятие мусорных обёрток нормой.
Команда: `go test ./internal/cloud -run '^TestParseUUIDWrapperProbe$' -v -count=1`.
Дословный вывод:

```text
=== RUN   TestParseUUIDWrapperProbe
    uuid_wrapper_probe_test.go:6: parseUUID("a123e4567-e89b-12d3-a456-426614174000b") = 123e4567-e89b-12d3-a456-426614174000, err=<nil>
    uuid_wrapper_probe_test.go:6: parseUUID("{123e4567-e89b-12d3-a456-426614174000}") = 123e4567-e89b-12d3-a456-426614174000, err=<nil>
--- PASS: TestParseUUIDWrapperProbe (0.00s)
PASS
ok  	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud	0.020s
```

Полнота относительно API: изменения не добавляют операций, полей или enum;
результат общей сверки спецификаций приведён в [основном отчёте](fuzz-review.md#полнота-относительно-api).

## Доказательство чувствительности ParseKV

Временно заменено `if !ok || k == ""` на `if !ok`.
Команда обоих прогонов: `go test ./internal/cmd/base -run '^FuzzParseKV$' -count=1`.
Изменение восстановлено. Хвост красного прогона (exit 1):

```text
--- FAIL: FuzzParseKV (0.00s)
    --- FAIL: FuzzParseKV/seed#2 (0.00s)
        flags_fuzz_test.go:35: invalid pair accepted or flag lost: "=" "k=" -> map[: k:], <nil>
FAIL
FAIL	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base	0.018s
FAIL
```

После восстановления (exit 0):

```text
ok  	github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cmd/base	0.048s
```


## Итоговый task check

Команда: `task check BASE=main`.
Описание роста ручных строк передано через RATCHET_PR_BODY.
До floorguard все шаги зелёные. Task завершился с кодом 201, процесс floor — с кодом 1.
Дословный хвост:

```text
Mutation testing completed in 7 seconds 9 milliseconds
Killed: 1, Lived: 0, Not covered: 0
Timed out: 0, Not viable: 0, Skipped: 929
Test efficacy: 100.00%
Mutator coverage: 100.00%
task: [floor] go run ./tools/floorguard -base main
floorguard: нарушений пола — 3:
  [правка-стража-или-гейтов] .github/workflows/ci.yml: tools/, Taskfile.yml, CI, хуки, генератор, .gitignore, go.work, vendor/ — решение человека
  [constraints-изменён] CONSTRAINTS.md: добавлены или удалены строки — решение человека
  [правка-стража-или-гейтов] Taskfile.yml: tools/, Taskfile.yml, CI, хуки, генератор, .gitignore, go.work, vendor/ — решение человека

Каждое опускает планку. Почините код; если ослабление нужно — это решение мейнтейнера, отдельным PR.
exit status 1
task: Failed to run task "check": task: Failed to run task "floor": exit status 1
```
