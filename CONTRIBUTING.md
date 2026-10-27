# Как вносить изменения

1. `task check` — чисто (линтер, `govulncheck`, тесты, храповик и пол из `CONSTRAINTS.md`) — то же гоняет CI.
   Стиль — `CODESTYLE.md`.
2. Новая команда или флаг → строка в `coverageRuns`
   (`internal/cmd/coverage_test.go`). Сознательно не поддерживаете часть API →
   `coverageExceptions` с настоящей причиной. Подробно — `docs/api-coverage.md`.
3. Правите скилл (`skills/*/SKILL.md`) — каждая команда в нём должна быть
   проверена запуском; `TestSkillCommandsParse` ловит только несуществующие
   команды и флаги, не смысл. В коммите — трейлер `Doubt-Review:` с разбором
   правки свежим взглядом (не автора): нет ли в скилле утверждений о CLI или API без
   команды и её вывода (хук `.githooks/commit-msg`; включить:
   `git config core.hooksPath .githooks`).
4. Обновилась спецификация API → `scripts/generate.sh --fetch`, затем
   `git diff api/` и `go test ./...` покажут, что добавить.
5. Нашли расхождение спецификации с живым API → строка в таблицу
   «Расхождения» в `docs/api-coverage.md`.

Тексты help и скиллов — на русском.
