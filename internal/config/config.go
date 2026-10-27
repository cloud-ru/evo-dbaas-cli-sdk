// Package config собирает настройки CLI в одну структуру: переменные окружения
// поверх значений по умолчанию. Флаги команд берут отсюда свои значения по
// умолчанию, так что порядок — флаг > переменная окружения > значение по умолчанию.
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/v2"
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

	// Lang — язык сообщений по LC_ALL, LC_MESSAGES, LANG (первая непустая, как в POSIX).
	Lang string `koanf:"-"`
}

var defaults = map[string]any{
	"clickhouse_url": "https://clickhouse.api.cloud.ru",
	"redis_url":      "https://redis.api.cloud.ru",
	"kafka_url":      "https://kafka.api.cloud.ru",
	"compute_url":    "https://compute.api.cloud.ru",
	"vpc_url":        "https://vpc.api.cloud.ru",
	"iam_url":        "https://iam.api.cloud.ru",
	"iam_auth_url":   "https://iam.api.cloud.ru/api/v1/auth/token",
}

// envKeys — имена переменных окружения. Исторические CH_CLOUDRU_* остались
// от ch-cloudru: их уже вписали в скрипты пользователей.
var envKeys = map[string]string{
	"CLOUDRU_KEY_ID":          "key_id",
	"CLOUDRU_KEY_SECRET":      "key_secret",
	"CLOUDRU_PROJECT_ID":      "project_id",
	"CH_CLOUDRU_API_URL":      "clickhouse_url",
	"CLOUDRU_REDIS_API_URL":   "redis_url",
	"CLOUDRU_KAFKA_API_URL":   "kafka_url",
	"CLOUDRU_COMPUTE_API_URL": "compute_url",
	"CLOUDRU_VPC_API_URL":     "vpc_url",
	"CLOUDRU_IAM_API_URL":     "iam_url",
	"CH_CLOUDRU_IAM_URL":      "iam_auth_url",
	"CLOUDRU_ALLOW_HTTP":      "allow_http",
}

// Load читает окружение процесса.
func Load() (Config, error) { return load(os.Environ) }

// Lang возвращается и с ошибкой: на её языке и печатается сообщение о ней.
func load(environ func() []string) (Config, error) {
	l := lang(environ())

	k := koanf.New(".")
	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return Config{Lang: l}, fmt.Errorf("config: %w", err)
	}

	if err := k.Load(env.Provider(".", env.Opt{TransformFunc: fromEnv, EnvironFunc: environ}), nil); err != nil {
		return Config{Lang: l}, fmt.Errorf("config: %w", err)
	}

	var c Config
	if err := k.Unmarshal("", &c); err != nil {
		return Config{Lang: l}, fmt.Errorf("config: %w", err)
	}

	c.Lang = l

	return c, nil
}

// lang — первая непустая из LC_ALL, LC_MESSAGES, LANG.
func lang(environ []string) string {
	vars := map[string]string{}

	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}

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
