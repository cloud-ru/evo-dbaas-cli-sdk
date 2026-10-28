// Package config собирает настройки CLI в одну структуру: переменные окружения
// поверх профиля из файла конфига поверх значений по умолчанию. Флаги команд
// перекрывают их при выполнении: флаг > env > файл > значение по умолчанию (DESIGN §8.12).
package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	"gopkg.in/yaml.v3"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// Config — все адреса, ключ и проект в одном месте.
type Config struct {
	KeyID     string `koanf:"key_id"`
	KeySecret string `koanf:"key_secret"`
	ProjectID string `koanf:"project_id"`

	ClickHouseURL string `koanf:"clickhouse_url"`
	RedisURL      string `koanf:"redis_url"`
	KafkaURL      string `koanf:"kafka_url"`
	ComputeURL    string `koanf:"compute_url"`
	VPCURL        string `koanf:"vpc_url"`
	IAMURL        string `koanf:"iam_url"`
	IAMAuthURL    string `koanf:"iam_auth_url"`

	// AllowHTTP разрешает http:// не на локальный адрес — стенд без TLS.
	AllowHTTP bool `koanf:"allow_http"`
}

// Options — путь и профиль из флагов --config и --profile (пусто — не заданы)
// и куда писать предупреждение о правах файла с ключом.
type Options struct {
	Path    string
	Profile string
	Warn    io.Writer
}

var defaults = map[string]any{
	"allow_http":     false,
	"clickhouse_url": "https://clickhouse.api.cloud.ru",
	"redis_url":      "https://redis.api.cloud.ru",
	"kafka_url":      "https://kafka.api.cloud.ru",
	"compute_url":    "https://compute.api.cloud.ru",
	"vpc_url":        "https://vpc.api.cloud.ru",
	"iam_url":        "https://iam.api.cloud.ru",
	"iam_auth_url":   "https://iam.api.cloud.ru/api/v1/auth/token",
}

// keys — ключи в порядке полей Config: так их печатает config list.
var keys = []string{
	"key_id", "key_secret", "project_id", "clickhouse_url", "redis_url", "kafka_url",
	"compute_url", "vpc_url", "iam_url", "iam_auth_url", "allow_http",
}

// envKeys — имена переменных окружения. Все с префиксом CLOUDRU_; старые
// CH_CLOUDRU_* убраны в v0.2.0 без псевдонимов.
var envKeys = map[string]string{
	"CLOUDRU_KEY_ID":             "key_id",
	"CLOUDRU_KEY_SECRET":         "key_secret",
	"CLOUDRU_PROJECT_ID":         "project_id",
	"CLOUDRU_CLICKHOUSE_API_URL": "clickhouse_url",
	"CLOUDRU_REDIS_API_URL":      "redis_url",
	"CLOUDRU_KAFKA_API_URL":      "kafka_url",
	"CLOUDRU_COMPUTE_API_URL":    "compute_url",
	"CLOUDRU_VPC_API_URL":        "vpc_url",
	"CLOUDRU_IAM_API_URL":        "iam_url",
	"CLOUDRU_IAM_AUTH_URL":       "iam_auth_url",
	"CLOUDRU_ALLOW_HTTP":         "allow_http",
}

// Load читает окружение процесса и файл конфига.
func Load(o Options) (Config, error) { return load(o, os.Environ) }

// Lang — язык сообщений по LC_ALL, LC_MESSAGES, LANG (первая непустая, как в POSIX).
// Нужен до разбора флагов — тексты --help, — поэтому отдельно от Load.
func Lang() string { return lang(vars(os.Environ())) }

func load(o Options, environ func() []string) (Config, error) {
	values, _, _, err := resolve(o, environ)
	if err != nil {
		return Config{}, err
	}

	k := koanf.New(".")
	if err := k.Load(confmap.Provider(values, "."), nil); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	var c Config
	if err := k.Unmarshal("", &c); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	return c, nil
}

// resolve — значения ключей и откуда каждое: env > профиль из файла > значение по умолчанию.
// Один порядок на Load и config list: показанный источник — тот, что взял бы Load.
func resolve(o Options, environ func() []string) (map[string]any, map[string]string, file, error) {
	vs := vars(environ())

	f, err := fromFile(o, vs)
	if err != nil {
		return nil, nil, file{}, err
	}

	values, sources := map[string]any{}, map[string]string{}

	for k, v := range defaults {
		values[k], sources[k] = v, "default"
	}

	for k, v := range f.values {
		values[k], sources[k] = v, "file "+f.profile
	}

	for name, value := range vs {
		if key, v := fromEnv(name, value); key != "" {
			values[key], sources[key] = v, "env "+name
		}
	}

	return values, sources, f, nil
}

func vars(environ []string) map[string]string {
	m := map[string]string{}

	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}

	return m
}

// lang — первая непустая из LC_ALL, LC_MESSAGES, LANG.
func lang(vars map[string]string) string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := vars[k]; v != "" {
			return v
		}
	}

	return ""
}

// fromEnv пропускает чужие и пустые переменные: пустая не должна затирать
// значение по умолчанию. CLOUDRU_ALLOW_HTTP включается только значением 1.
func fromEnv(name, value string) (string, any) {
	key, ok := envKeys[name]
	if !ok || value == "" {
		return "", nil
	}

	if key == "allow_http" {
		return key, value == "1"
	}

	return key, value
}

// Env — значение переменной окружения name: пароль из --password-env.
// Окружение читает только этот пакет.
func Env(name string) string { return os.Getenv(name) }

// filePath — путь к файлу конфига и задан ли он явно (флагом или CLOUDRU_CONFIG).
// По умолчанию — XDG и на macOS, как у gh и kubectl; без HOME файла нет.
func filePath(o Options, vars map[string]string) (string, bool) {
	switch {
	case o.Path != "":
		return o.Path, true
	case vars["CLOUDRU_CONFIG"] != "":
		return vars["CLOUDRU_CONFIG"], true
	}

	dir := vars["XDG_CONFIG_HOME"]

	switch {
	case runtime.GOOS == "windows":
		dir = vars["APPDATA"]
	case dir == "" && vars["HOME"] != "":
		dir = filepath.Join(vars["HOME"], ".config")
	}

	if dir == "" {
		return "", false
	}

	return filepath.Join(dir, "cloudru-dbaas", "config.yaml"), false
}

// file — выбранный профиль: путь к файлу, есть ли файл, имя профиля и откуда оно, значения.
type file struct {
	path, profile, from string
	exists              bool
	values              map[string]any
}

// fromFile — значения выбранного профиля. Профиль: --profile > CLOUDRU_PROFILE >
// profile: в файле > default; названного нет — ошибка, default может не быть.
func fromFile(o Options, vars map[string]string) (file, error) {
	name, from := o.Profile, "flag"
	if name == "" && vars["CLOUDRU_PROFILE"] != "" {
		name, from = vars["CLOUDRU_PROFILE"], "env CLOUDRU_PROFILE"
	}

	path, explicit := filePath(o, vars)

	raw, err := []byte(nil), fs.ErrNotExist
	if path != "" {
		raw, err = os.ReadFile(path)
	}

	if err != nil && (explicit || !errors.Is(err, fs.ErrNotExist)) {
		return file{}, i18n.Errorf("config.err.read", err)
	}

	profiles, current, err := parse(path, raw)
	if err != nil {
		return file{}, err
	}

	if name == "" && current != "" {
		name, from = current, "file"
	}

	named := name != ""
	if !named {
		name, from = "default", "default"
	}

	p, ok := profiles[name]

	switch {
	case !ok && named && path == "":
		return file{}, i18n.Errorf("config.err.no_home", name)
	case !ok && named && raw == nil:
		return file{}, i18n.Errorf("config.err.no_file", name, path)
	case !ok && named:
		return file{}, i18n.Errorf("config.err.no_profile", path, name, strings.Join(slices.Sorted(maps.Keys(profiles)), ", "))
	}

	// Секрет в любом профиле: права файла не зависят от выбранного.
	for _, other := range profiles {
		if _, secret := other["key_secret"]; secret {
			warnMode(o.Warn, path)

			break
		}
	}

	return file{path: path, profile: name, from: from, exists: raw != nil, values: p}, nil
}

// parse разбирает файл строго: неизвестный ключ или чужой тип — ошибка с путём
// файла и ключом, иначе опечатка молча уведёт на адрес по умолчанию (урок v0.2.0).
func parse(path string, raw []byte) (map[string]map[string]any, string, error) {
	var top map[string]any
	if err := yaml.Unmarshal(raw, &top); err != nil {
		return nil, "", i18n.Errorf("config.err.parse", path, err)
	}

	profiles := map[string]map[string]any{}

	var current string

	// Ключи по порядку: при нескольких ошибках в файле сообщение одно и то же от запуска к запуску.
	for _, key := range slices.Sorted(maps.Keys(top)) {
		v := top[key]

		var ok bool

		switch key {
		case "profile":
			current, ok = v.(string)
		case "profiles":
			var all map[string]any
			if all, ok = v.(map[string]any); ok {
				for _, name := range slices.Sorted(maps.Keys(all)) {
					p, err := profile(path, name, all[name])
					if err != nil {
						return nil, "", err
					}

					profiles[name] = p
				}
			}
		default:
			return nil, "", i18n.Errorf("config.err.key", path, key)
		}

		if !ok {
			return nil, "", i18n.Errorf("config.err.type", path, key)
		}
	}

	return profiles, current, nil
}

// profile проверяет ключи и типы профиля. Пустое значение (`vpc_url: ""` или `vpc_url:`) —
// как не заданный ключ, так же, как пустая переменная окружения: не затирает умолчание.
func profile(path, name string, v any) (map[string]any, error) {
	in, ok := v.(map[string]any)
	if !ok && v != nil {
		return nil, i18n.Errorf("config.err.type", path, "profiles."+name)
	}

	p := map[string]any{}

	for _, key := range slices.Sorted(maps.Keys(in)) {
		val, at := in[key], "profiles."+name+"."+key
		if !slices.Contains(keys, key) {
			return nil, i18n.Errorf("config.err.key", path, at)
		}

		if val == nil || val == "" {
			continue
		}

		_, isBool := val.(bool)
		_, isString := val.(string)

		if key == "allow_http" && !isBool || key != "allow_http" && !isString {
			return nil, i18n.Errorf("config.err.type", path, at)
		}

		p[key] = val
	}

	return p, nil
}

// warnMode — как ssh: файл с key_secret читают другие (на Unix права шире 0600) — предупреждение, не отказ.
func warnMode(w io.Writer, path string) {
	if w == nil || runtime.GOOS == "windows" {
		return
	}

	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0o077 != 0 {
		_, _ = fmt.Fprintf(w, "%s\n", i18n.T("config.warn.mode", path, fi.Mode().Perm(), path))
	}
}
