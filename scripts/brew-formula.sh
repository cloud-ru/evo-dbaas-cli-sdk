#!/usr/bin/env bash
# Формула Homebrew для cloudru-dbaas из checksums.txt релиза — в stdout.
# Релиз — голые бинарники (archives: formats [binary]), поэтому формула
# качает бинарник под ОС и архитектуру и ставит его под именем cloudru-dbaas.
# Вызывает release.yml после GoReleaser; руками:
#   scripts/brew-formula.sh v0.6.0 checksums.txt > Formula/cloudru-dbaas.rb
set -euo pipefail

usage() { echo "Usage: $0 vX.Y.Z checksums.txt" >&2; exit 2; }
[[ $# == 2 ]] || usage
TAG=$1
SUMS=$2
[[ $TAG =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage
REPO=${REPO:-cloud-ru/evo-dbaas-cli-sdk}
BASE="https://github.com/$REPO/releases/download/$TAG"

sum() {
  local s
  s=$(awk -v f="cloudru-dbaas_$1" '$2 == f { print $1 }' "$SUMS")
  [[ $s =~ ^[0-9a-f]{64}$ ]] || { echo "нет sha256 для cloudru-dbaas_$1 в $SUMS" >&2; exit 1; }
  echo "$s"
}

block() {
  cat <<EOF
      url "$BASE/cloudru-dbaas_$1"
      sha256 "$(sum "$1")"
EOF
}

cat <<EOF
# Файл генерирует scripts/brew-formula.sh при каждом релизе — руками не править.
class CloudruDbaas < Formula
  desc "CLI для Managed ClickHouse, Redis, Kafka, PostgreSQL и OpenSearch в cloud.ru"
  homepage "https://github.com/$REPO"
  version "${TAG#v}"
  license "MIT"

  on_macos do
    on_arm do
$(block darwin_arm64)
    end
    on_intel do
$(block darwin_amd64)
    end
  end

  on_linux do
    on_arm do
$(block linux_arm64)
    end
    on_intel do
$(block linux_amd64)
    end
  end

  def install
    bin.install Dir["cloudru-dbaas_*"].first => "cloudru-dbaas"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/cloudru-dbaas version")
  end
end
EOF
