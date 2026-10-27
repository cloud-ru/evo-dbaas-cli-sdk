#!/usr/bin/env bash
# Перегенерация клиентов Public API из api/openapi-*.yaml.
#
#   scripts/generate.sh           — сгенерировать из спецификаций в api/
#   scripts/generate.sh --fetch   — сначала скачать свежие спецификации
#                                   с cloud.ru/docs и наложить локальные правки
#
# Локальных правок две. compute и iam чинит tools/specfix при каждой генерации.
# api/openapi-clickhouse.patch: поля config в ответе
# приходят строками ("100"), а в спецификации integer, поэтому им задан тип
# FlexInt64 (internal/gen/clickhouse/flexint.go). После --fetch: git diff api/ и go test ./...
set -euo pipefail
cd "$(dirname "$0")/.."

OAPI_CODEGEN=github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
declare -A SPEC_URL=(
  [clickhouse]=https://cloud.ru/docs/api/specs/clickhouse/ug/_specs/openapi-clickhouse.yaml
  [redis]=https://cloud.ru/docs/api/specs/redis/ug/_specs/openapi-redis.yaml
  [kafka]=https://cloud.ru/docs/api/specs/paas-kafka/ug/_specs/openapi-kafka.yaml
)
declare -A PKG=([clickhouse]=gen/clickhouse [redis]=gen/redis [kafka]=gen/kafka [vpc]=gen/vpc [compute]=gen/compute [iam]=gen/iam)

# Скачиваются с User-Agent браузера.
declare -A EXTRA_URL=(
  [compute]=https://cloud.ru/docs/api/specs/virtual-machines/ug/_downloads/43c42ff22a171c77371e690a08181c3f/openapi-v3.yaml
  [vpc]=https://cloud.ru/docs/api/specs/evolution-vpc/ug/_downloads/113b453e0e2aa12a22311dfa29cac9ef/openapi.yaml
  [iam]=https://cloud.ru/docs/api/specs/administration/ug/_specs/iam-gw-public.yaml
)

if [[ ${1:-} == --fetch ]]; then
  for p in "${!SPEC_URL[@]}"; do
    curl -fsSL -o "api/openapi-$p.yaml" "${SPEC_URL[$p]}"
  done
  for p in "${!EXTRA_URL[@]}"; do
    curl -fsSL -A Mozilla/5.0 -o "api/openapi-$p.yaml" "${EXTRA_URL[$p]}"
  done
  git apply -p0 api/openapi-clickhouse.patch
fi

for p in clickhouse redis kafka vpc; do
  go run "$OAPI_CODEGEN" -config "internal/${PKG[$p]}/cfg.yaml" "api/openapi-$p.yaml"
done

# compute и iam в опубликованном виде oapi-codegen не читает (битая ссылка, остатки Swagger 2) —
# сначала tools/specfix, api/ остаётся как скачано.
fixed=$(mktemp -d)
trap 'rm -rf "$fixed"' EXIT
for p in compute iam; do
  go run ./tools/specfix "api/openapi-$p.yaml" "$fixed/$p.yaml"
  go run "$OAPI_CODEGEN" -config "internal/${PKG[$p]}/cfg.yaml" "$fixed/$p.yaml"
done
gofmt -l internal/ | (! grep .) || { echo "gofmt: файлы выше не отформатированы" >&2; exit 1; }
