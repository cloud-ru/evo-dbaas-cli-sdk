package state

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/auth"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/cloud"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/config"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/transport"
)

// Conn — из чего собрать клиенты API: конфиг, версия для User-Agent и
// настройки транспорта (тесты уменьшают паузы и предел тела).
type Conn struct {
	Config      config.Config
	Version     string
	RetryDelays []time.Duration
	MaxBody     int64
}

// Connect собирает ключ, кэш токена и транспорт и заполняет клиенты API.
// Клиенты строятся при первом вызове; адреса продуктов берутся из полей
// *URL уже после разбора флагов --api-url.
func (s *State) Connect(c Conn) error {
	cfg := c.Config

	cacheDir := ""
	if d, err := os.UserCacheDir(); err == nil {
		cacheDir = filepath.Join(d, "ch-cloudru")
	}

	iam := &auth.IAMSource{
		KeyID:  cfg.KeyID,
		Secret: cfg.KeySecret,
		URL:    cfg.IAMAuthURL,
	}
	src := &auth.CachedSource{Fetch: iam.Fetch, Key: auth.CacheKey(iam.KeyID, iam.Secret, iam.Endpoint())}

	if cacheDir != "" {
		src.Store = auth.FileStore{Dir: cacheDir}
	}

	refresh := func(ctx context.Context) (string, error) {
		tok, err := src.Refresh(ctx)

		return tok, output.AuthError(err)
	}
	bearer := func(ctx context.Context, req *http.Request) error {
		tok, err := src.Token(ctx)
		if err != nil {
			return output.AuthError(err)
		}

		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("User-Agent", "ch-cloudru/"+c.Version)

		return nil
	}

	for _, u := range []string{s.ClickHouseURL, s.RedisURL, s.KafkaURL, iam.URL, cfg.ComputeURL, cfg.VPCURL, cfg.IAMURL} {
		if err := checkInsecureURL(s.Printer.Err, u, cfg.AllowHTTP); err != nil {
			return err
		}
	}

	hc := &http.Client{Timeout: 60 * time.Second, Transport: transport.New(http.DefaultTransport, transport.Options{
		Refresh: refresh, RetryDelays: c.RetryDelays, MaxBody: c.MaxBody, EmptyBodyHint: emptyBodyHint,
	})}

	o := cloud.Options{HTTPClient: hc, Auth: bearer}

	s.ClickHouse = sync.OnceValues(func() (*cloud.ClickHouse, error) { return cloud.NewClickHouse(s.ClickHouseURL, o) })
	s.Redis = sync.OnceValues(func() (*cloud.Redis, error) { return cloud.NewRedis(s.RedisURL, o) })
	s.Kafka = sync.OnceValues(func() (*cloud.Kafka, error) { return cloud.NewKafka(s.KafkaURL, o) })
	s.Compute = sync.OnceValues(func() (*cloud.Compute, error) { return cloud.NewCompute(cfg.ComputeURL, o) })
	s.VPC = sync.OnceValues(func() (*cloud.VPC, error) { return cloud.NewVPC(cfg.VPCURL, o) })
	s.IAM = sync.OnceValues(func() (*cloud.IAM, error) { return cloud.NewIAM(cfg.IAMURL, o) })

	return nil
}

// checkInsecureURL отказывает, если ключ или токен ушли бы по http:// не на
// локальный адрес: опечатка в env стоила бы ключа. Осознанный стенд без TLS —
// CLOUDRU_ALLOW_HTTP=1, тогда только предупреждение.
func checkInsecureURL(w io.Writer, raw string, allowHTTP bool) error {
	u, err := url.Parse(raw) // схему url.Parse приводит к нижнему регистру
	if err != nil || u.Scheme != "http" {
		return nil
	}

	if isLoopback(u.Hostname()) {
		return nil
	}

	if allowHTTP {
		fmt.Fprintf(w, "%s\n", i18n.T("state.warning.insecure_url", raw))
		return nil
	}

	return errors.New(i18n.T("state.err.insecure_url", raw))
}

// isLoopback: localhost в любом регистре и любой адрес 127.0.0.0/8 или ::1 —
// трафик не покидает машину.
func isLoopback(host string) bool {
	host = strings.TrimSuffix(host, ".")
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// emptyBodyHint — подсказка по коду, когда шлюз не прислал причину.
func emptyBodyHint(code int) string {
	switch {
	case code == http.StatusBadRequest:
		return i18n.T("state.empty_body.bad_request")
	case code == http.StatusUnauthorized:
		return i18n.T("state.empty_body.unauthorized")
	case code == http.StatusForbidden:
		return i18n.T("state.empty_body.forbidden")
	case code == http.StatusNotFound:
		return i18n.T("state.empty_body.not_found")
	case code == http.StatusTooManyRequests:
		return i18n.T("state.empty_body.rate_limited")
	case code >= 500:
		return i18n.T("state.empty_body.server_error")
	}

	return i18n.T("state.empty_body.other")
}
