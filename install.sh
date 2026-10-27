#!/bin/sh
# Установка cloudru-dbaas из релиза GitHub со сверкой sha256.
#
#   curl -fsSL https://raw.githubusercontent.com/cloud-ru/evo-dbaas-cli-sdk/main/install.sh | sh
#   curl -fsSL …/install.sh | sh -s -- --version v0.1.0 --dir /usr/local/bin
#
# Без root: по умолчанию ставит в ~/.local/bin. Нужны curl или wget и
# sha256sum или shasum. Переменные окружения — те же, что флаги:
# CLOUDRU_DBAAS_VERSION, CLOUDRU_DBAAS_DIR, CLOUDRU_DBAAS_BINARY,
# CLOUDRU_DBAAS_BASE_URL (зеркало релизов: <base>/download/<тег>/<файл>,
# для latest — <base>/latest/download/<файл>).
set -eu

repo=cloud-ru/evo-dbaas-cli-sdk
version=${CLOUDRU_DBAAS_VERSION:-latest}
dir=${CLOUDRU_DBAAS_DIR:-$HOME/.local/bin}
binary=${CLOUDRU_DBAAS_BINARY:-cloudru-dbaas}
base=${CLOUDRU_DBAAS_BASE_URL:-https://github.com/$repo/releases}

die() { echo "install.sh: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
	case $1 in
	--version) version=${2:?}; shift 2 ;;
	--dir) dir=${2:?}; shift 2 ;;
	--binary) binary=${2:?}; shift 2 ;;
	-h | --help) echo "install.sh [--version vX.Y.Z] [--dir каталог] [--binary cloudru-dbaas]"; exit 0 ;;
	*) die "неизвестный аргумент: $1" ;;
	esac
done

case $binary in
cloudru-dbaas) ;;
*) die "--binary или CLOUDRU_DBAAS_BINARY: cloudru-dbaas, а не $binary" ;;
esac

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
MINGW* | MSYS* | CYGWIN*) os=windows ;;
*) die "ОС $(uname -s) не поддержана — соберите из исходников: go install github.com/$repo/cmd/$binary@latest" ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "архитектура $(uname -m) не поддержана" ;;
esac

asset=${binary}_${os}_${arch}
[ "$os" = windows ] && asset=$asset.exe

if [ "$version" = latest ]; then
	url=$base/latest/download
else
	url=$base/download/$version
fi

fetch() { # url файл
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		die "нужен curl или wget"
	fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
trap 'exit 130' INT TERM

fetch "$url/$asset" "$tmp/$asset" || die "не скачался $url/$asset — есть ли такой релиз?"
fetch "$url/checksums.txt" "$tmp/checksums.txt" || die "не скачался $url/checksums.txt"

want=$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "в checksums.txt нет строки для $asset"
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/$asset" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
	got=$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')
else
	die "нужен sha256sum или shasum — без сверки не ставлю"
fi
[ "$got" = "$want" ] || die "sha256 не совпал: ждали $want, получили $got"

target=$binary
[ "$os" = windows ] && target=$target.exe
mkdir -p "$dir"
chmod 0755 "$tmp/$asset"
mv "$tmp/$asset" "$dir/$target"
echo "установлен $dir/$target ($("$dir/$target" --version 2>/dev/null || echo "версия не прочиталась"))"

case :$PATH: in
*:"$dir":*) ;;
*) echo "каталога $dir нет в PATH: export PATH=\"$dir:\$PATH\"" ;;
esac
