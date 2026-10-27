# Установка CLI cloudru-dbaas

Одной строкой, без root — в `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/cloud-ru/evo-dbaas-cli-sdk/main/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
cloudru-dbaas --version
```

- Скрипт берёт бинарник `cloudru-dbaas_<os>_<arch>` из релиза GitHub и сверяет
  sha256 с `checksums.txt` того же релиза. Не совпало — отказ: не обходи его
  и не скачивай бинарник руками, скажи пользователю.
- Версию закрепляй, когда важна повторяемость (CI, чужая песочница):
  `curl -fsSL …/install.sh | sh -s -- --version vX.Y.Z`. Каталог — `--dir`.
- Нужны `curl` или `wget` и `sha256sum` или `shasum`. Нет — или есть Go:
  `go install github.com/cloud-ru/evo-dbaas-cli-sdk/cmd/cloudru-dbaas@latest`.
- 404 на `install.sh` или на файле релиза — у песочницы нет доступа к
  github.com или нужного релиза нет. Скажи пользователю; бинарник из других
  источников не ставь.
- Песочница пересоздаётся при каждом запуске — ставь в начале задачи, а не
  один раз «навсегда»; проверка — `cloudru-dbaas --version`.
