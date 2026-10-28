package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func environ(vars ...string) func() []string { return func() []string { return vars } }

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	c, err := load(Options{}, environ())
	require.NoError(t, err)

	assert.Equal(t, Config{
		ClickHouseURL: "https://clickhouse.api.cloud.ru",
		RedisURL:      "https://redis.api.cloud.ru",
		KafkaURL:      "https://kafka.api.cloud.ru",
		ComputeURL:    "https://compute.api.cloud.ru",
		VPCURL:        "https://vpc.api.cloud.ru",
		IAMURL:        "https://iam.api.cloud.ru",
		IAMAuthURL:    "https://iam.api.cloud.ru/api/v1/auth/token",
	}, c)
}

func TestLoadEnvOverridesDefaults(t *testing.T) {
	t.Parallel()

	c, err := load(Options{}, environ(
		"CLOUDRU_KEY_ID=kid", "CLOUDRU_KEY_SECRET=sec", "CLOUDRU_PROJECT_ID=p1",
		"CLOUDRU_CLICKHOUSE_API_URL=http://ch", "CLOUDRU_REDIS_API_URL=http://redis", "CLOUDRU_KAFKA_API_URL=http://kafka",
		"CLOUDRU_COMPUTE_API_URL=http://compute", "CLOUDRU_VPC_API_URL=http://vpc", "CLOUDRU_IAM_API_URL=http://iam",
		"CLOUDRU_IAM_AUTH_URL=http://token", "CLOUDRU_ALLOW_HTTP=1",
	))
	require.NoError(t, err)

	assert.Equal(t, Config{
		KeyID: "kid", KeySecret: "sec", ProjectID: "p1",
		ClickHouseURL: "http://ch", RedisURL: "http://redis", KafkaURL: "http://kafka",
		ComputeURL: "http://compute", VPCURL: "http://vpc", IAMURL: "http://iam", IAMAuthURL: "http://token",
		AllowHTTP: true,
	}, c)
}

func TestLoadEmptyEnvKeepsDefault(t *testing.T) {
	t.Parallel()

	c, err := load(Options{}, environ("CLOUDRU_VPC_API_URL=", "CLOUDRU_PROJECT_ID="))
	require.NoError(t, err)

	assert.Equal(t, "https://vpc.api.cloud.ru", c.VPCURL)
	assert.Empty(t, c.ProjectID)
}

func TestLoadAllowHTTPOnlyOne(t *testing.T) {
	t.Parallel()

	for _, v := range []string{"true", "yes", "0", "01"} {
		c, err := load(Options{}, environ("CLOUDRU_ALLOW_HTTP="+v))
		require.NoError(t, err)
		assert.False(t, c.AllowHTTP, v)
	}
}

func TestLoadIgnoresForeignVars(t *testing.T) {
	t.Parallel()

	c, err := load(Options{}, environ("HOME="+t.TempDir(), "CLOUDRU_UNKNOWN=x", "key_id=leak"))
	require.NoError(t, err)

	assert.Empty(t, c.KeyID)
}

// Регрессия: CH_CLOUDRU_* убраны в v0.2.0 без псевдонимов — старое имя
// не должно тихо перенаправлять запросы.
func TestLoadIgnoresOldCHCloudruVars(t *testing.T) {
	t.Parallel()

	c, err := load(Options{}, environ("CH_CLOUDRU_API_URL=http://old", "CH_CLOUDRU_IAM_URL=http://old"))
	require.NoError(t, err)

	assert.Equal(t, "https://clickhouse.api.cloud.ru", c.ClickHouseURL)
	assert.Equal(t, "https://iam.api.cloud.ru/api/v1/auth/token", c.IAMAuthURL)
}

func TestLoadLangPOSIXOrder(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		env  []string
		want string
	}{
		{nil, ""},
		{[]string{"LANG=ru_RU.UTF-8"}, "ru_RU.UTF-8"},
		{[]string{"LANG=ru_RU.UTF-8", "LC_MESSAGES=en_US.UTF-8"}, "en_US.UTF-8"},
		{[]string{"LANG=en_US.UTF-8", "LC_MESSAGES=en_US.UTF-8", "LC_ALL=ru_RU.UTF-8"}, "ru_RU.UTF-8"},
		{[]string{"LC_ALL=", "LANG=ru_RU"}, "ru_RU"},
	} {
		assert.Equal(t, tc.want, lang(vars(tc.env)), tc.env)
	}
}
