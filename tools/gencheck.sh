#!/usr/bin/env bash
# Сгенерированным считается только то, что воспроизводит генератор. Заголовок «Code generated … DO NOT EDIT.»
# выводит файл из-под линтера и храповика, поэтому в копии рабочего дерева файлы с заголовком удаляются,
# генераторы (scripts/generate.sh, mockery по .mockery.yml) запускаются заново, и каждый файл с заголовком
# должен вернуться байт в байт. Ручной код под заголовком или ручная правка внутри *.gen.go — красное.
set -euo pipefail
cd "$(dirname "$0")/.."

MOCKERY=github.com/vektra/mockery/v3@v3.8.1
header='^// Code generated .* DO NOT EDIT\.$'

# CR в .go: сканер Go отрезает \r у комментария, и линтер видит заголовок, а grep здесь и регэксп храповика — нет.
if crlf=$(git ls-files -co --exclude-standard -- '*.go' | xargs -r grep -l $'\r'); then
  echo "gencheck: CR в Go-файлах — определения «сгенерированного» у линтера и храповика расходятся:" >&2
  echo "$crlf" >&2
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

git ls-files -co --exclude-standard -z | xargs -0 cp --parents -t "$tmp" --
mapfile -t want < <(cd "$tmp" && grep -rlE --include='*.go' "$header" . | sort)

(
  cd "$tmp"
  rm -f -- "${want[@]}"
  scripts/generate.sh >/dev/null
  if [[ -f .mockery.yml ]]; then
    MOCKERY_LOG_LEVEL=error go run "$MOCKERY" >/dev/null
  fi
)

mapfile -t got < <(cd "$tmp" && grep -rlE --include='*.go' "$header" . | sort)

fail=0
for f in "${want[@]}"; do
  if [[ ! -f "$tmp/$f" ]]; then
    echo "gencheck: ${f#./} — заголовок генератора, но генератор этот файл не создаёт" >&2
    fail=1
  elif ! cmp -s "$f" "$tmp/$f"; then
    echo "gencheck: ${f#./} — отличается от выхода генератора (ручная правка?)" >&2
    fail=1
  fi
done
for f in "${got[@]}"; do
  if [[ ! -f "$f" ]]; then
    echo "gencheck: ${f#./} — генератор создаёт, в репо нет (task gen)" >&2
    fail=1
  fi
done

[[ $fail == 0 ]] && echo "gencheck: сгенерированное воспроизводится (${#want[@]} файлов)"
exit $fail
