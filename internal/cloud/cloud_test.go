package cloud

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return srv.URL
}

// noAuth — свой транспорт: httptest.Server.Close закрывает простаивающие
// соединения http.DefaultTransport и рвёт запросы параллельных тестов.
var noAuth = Options{HTTPClient: &http.Client{Transport: &http.Transport{}}, Auth: func(_ context.Context, _ *http.Request) error { return nil }}

func TestReadErrorMessageFromBody(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, body, want string }{
		{"message из тела", `{"code":5,"message":"vpc not found"}`, "API 404: vpc not found"},
		{"тело не JSON", "  not found\n", "API 404: not found"},
		{"без message", `{"code":5}`, `API 404: {"code":5}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := NewVPC(serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(tc.body))
			}), noAuth)
			require.NoError(t, err)

			_, err = v.Get(t.Context(), "v1")
			require.EqualError(t, err, tc.want)
			assert.True(t, IsNotFound(err))
		})
	}
}

// Успех — только 2xx: 300 с телом-объектом не выдаётся за ответ ресурса.
func TestRead3xxIsAPIError(t *testing.T) {
	t.Parallel()

	v, err := NewVPC(serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMultipleChoices)
		_, _ = w.Write([]byte(`{"message":"выбор"}`))
	}), noAuth)
	require.NoError(t, err)

	raw, err := v.Get(t.Context(), "v1")
	require.EqualError(t, err, "API 300: выбор")
	assert.Nil(t, raw)
}

func TestReadEmptyBodyIsEmptyObject(t *testing.T) {
	t.Parallel()

	v, err := NewVPC(serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), noAuth)
	require.NoError(t, err)

	raw, err := v.Delete(t.Context(), "v1")
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(raw))
}

func TestIsNotFoundOnlyFor404(t *testing.T) {
	t.Parallel()

	assert.False(t, IsNotFound(&APIError{Status: http.StatusForbidden}))
	assert.False(t, IsNotFound(errors.New("сеть")))
}

// IAM не раскодирует «+» в фильтре: пробел уходит как %20, настоящий «+» — как %2B.
func TestIAMQuerySpaceAsPercent20(t *testing.T) {
	t.Parallel()

	var query string

	s, err := NewIAM(serve(t, func(_ http.ResponseWriter, r *http.Request) { query = r.URL.RawQuery }), noAuth)
	require.NoError(t, err)

	_, err = s.Permissions(t.Context(), "resource_id=p AND subject_id=a+b")
	require.NoError(t, err)
	assert.Contains(t, query, "filter=resource_id%3Dp%20AND%20subject_id%3Da%2Bb")
	assert.NotContains(t, query, "+")
}

func TestGrantRejectsBadExpiresBeforeRequest(t *testing.T) {
	t.Parallel()

	s, err := NewIAM(serve(t, func(_ http.ResponseWriter, r *http.Request) { t.Errorf("запрос: %s", r.URL) }), noAuth)
	require.NoError(t, err)

	_, err = s.Grant(t.Context(), "p", "sa", "r", "2026-12-31")
	require.ErrorIs(t, err, ErrBadExpires)
}

func TestComputeRejectsNonUUIDBeforeRequest(t *testing.T) {
	t.Parallel()

	c, err := NewCompute(serve(t, func(_ http.ResponseWriter, r *http.Request) { t.Errorf("запрос: %s", r.URL) }), noAuth)
	require.NoError(t, err)

	const id = "11111111-1111-4111-8111-111111111111"

	for name, call := range map[string]func() error{
		"get":           func() error { _, err := c.Subnet(t.Context(), "sn1"); return err },
		"delete":        func() error { _, err := c.DeleteSubnet(t.Context(), "sn1"); return err },
		"list проект":   func() error { _, err := c.Subnets(t.Context(), SubnetFilter{Project: "p1"}); return err },
		"list зона":     func() error { _, err := c.Subnets(t.Context(), SubnetFilter{Project: id, Zone: "z"}); return err },
		"list VPC":      func() error { _, err := c.Subnets(t.Context(), SubnetFilter{Project: id, VPC: "v"}); return err },
		"create проект": func() error { _, err := c.CreateSubnet(t.Context(), SubnetCreate{Project: "p1"}); return err },
	} {
		t.Run(name, func(t *testing.T) { require.ErrorIs(t, call(), ErrNotUUID) })
	}
}

// Каждый метод уходит в свою операцию спецификации: метод HTTP, путь и тело.
func TestMethodsHitTheirOperations(t *testing.T) {
	t.Parallel()

	const (
		id   = "11111111-1111-4111-8111-111111111111"
		zone = "33333333-3333-4333-8333-333333333333"
	)

	var got string

	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = r.Method + " " + r.URL.Path + "?" + r.URL.RawQuery + " " + string(bytes.TrimSpace(body))
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	v, err := NewVPC(url, noAuth)
	require.NoError(t, err)
	s, err := NewIAM(url, noAuth)
	require.NoError(t, err)
	c, err := NewCompute(url, noAuth)
	require.NoError(t, err)

	// Списки отдают все страницы: поле списка появляется и пустым.
	listOut := map[string]string{
		"GET /v1/vpcs?projectId=p&pageSize=1000 ":                                                                            `{"ok":true,"vpcs":[]}`,
		"GET /api/v2/serviceAccounts?target.projectId=p&pageSize=1000 ":                                                      `{"ok":true,"service_accounts":[]}`,
		"GET /api/v3/permissions?filter=f&pageSize=1000 ":                                                                    `{"ok":true,"permissions":[]}`,
		"GET /api/v1/subnets?project_id=" + id + "&availability_zone_ids=" + zone + "&vpc_id=" + id + "&offset=0&limit=100 ": `{"items":[],"total":0}`,
	}

	ctx := t.Context()
	for _, tc := range []struct {
		call func() ([]byte, error)
		want string
	}{
		{func() ([]byte, error) { return v.List(ctx, "p") }, "GET /v1/vpcs?projectId=p&pageSize=1000 "},
		{func() ([]byte, error) { return v.Get(ctx, "v1") }, "GET /v1/vpcs/v1? "},
		{func() ([]byte, error) { return v.Create(ctx, VPCCreate{Project: "p", Name: "n"}) }, `POST /v1/vpcs? {"name":"n","projectId":"p"}`},
		{func() ([]byte, error) { return v.Delete(ctx, "v1") }, "DELETE /v1/vpcs/v1? "},
		{func() ([]byte, error) { return v.Operation(ctx, "op1") }, "GET /v1/vpcs/operations/op1? "},
		{func() ([]byte, error) { return s.ServiceAccounts(ctx, "p") }, "GET /api/v2/serviceAccounts?target.projectId=p&pageSize=1000 "},
		{func() ([]byte, error) { return s.Permissions(ctx, "f") }, "GET /api/v3/permissions?filter=f&pageSize=1000 "},
		{func() ([]byte, error) { return s.ServiceAccount(ctx, "sa") }, "GET /api/v2/serviceAccounts/sa? "},
		{func() ([]byte, error) { return s.CreateServiceAccount(ctx, "p", "n", "") }, `POST /api/v2/serviceAccounts? {"name":"n","target":{"projectId":"p"}}`},
		{func() ([]byte, error) { return s.DeleteServiceAccount(ctx, "sa") }, "DELETE /api/v2/serviceAccounts/sa? "},
		{func() ([]byte, error) { return s.AccessKeys(ctx, "sa") }, "GET /api/v2/serviceAccounts/sa/accessKeys? "},
		{func() ([]byte, error) { return s.AccessKey(ctx, "sa", "ak") }, "GET /api/v2/serviceAccounts/sa/accessKeys/ak? "},
		{func() ([]byte, error) { return s.CreateAccessKey(ctx, "sa", "", "") }, "POST /api/v2/serviceAccounts/sa/accessKeys? {}"},
		{func() ([]byte, error) { return s.DeleteAccessKey(ctx, "sa", "ak") }, "DELETE /api/v2/serviceAccounts/sa/accessKeys/ak? "},
		{func() ([]byte, error) { return s.Permission(ctx, "pm") }, "GET /api/v3/permissions/pm? "},
		{func() ([]byte, error) { return s.Grant(ctx, "p", "sa", "r", "") }, `POST /api/v3/permissions? {"resourceId":"p","role":"r","subjectId":"sa"}`},
		{func() ([]byte, error) { return s.Revoke(ctx, "pm") }, "DELETE /api/v3/permissions/pm? "},
		{func() ([]byte, error) { return c.Subnets(ctx, SubnetFilter{Project: id, Zone: zone, VPC: id}) }, "GET /api/v1/subnets?project_id=" + id + "&availability_zone_ids=" + zone + "&vpc_id=" + id + "&offset=0&limit=100 "},
		{func() ([]byte, error) { return c.Subnet(ctx, id) }, "GET /api/v1/subnets/" + id + "? "},
		{func() ([]byte, error) {
			return c.CreateSubnet(ctx, SubnetCreate{Project: id, Name: "n", Zone: zone, CIDR: "10.0.9.0/24", DNS: []string{"8.8.8.8"}, Isolated: true})
		}, `POST /api/v1/subnets? {"availability_zone_id":"` + zone + `","dns_servers":["8.8.8.8"],"name":"n","project_id":"` + id + `","routed_network":false,"subnet_address":"10.0.9.0/24"}`},
		{func() ([]byte, error) { return c.DeleteSubnet(ctx, id) }, "DELETE /api/v1/subnets/" + id + "? "},
		{func() ([]byte, error) { return c.Zones(ctx) }, "GET /api/v1/availability-zones? "},
	} {
		t.Run(tc.want, func(t *testing.T) {
			raw, err := tc.call()
			require.NoError(t, err)

			out, ok := listOut[tc.want]
			if !ok {
				out = `{"ok":true}`
			}

			assert.JSONEq(t, out, string(raw))
			assert.Equal(t, tc.want, got)
		})
	}
}

// «.» и «..» сгенерированный клиент схлопнул бы в пути: DELETE ушёл бы на
// родительский ресурс. Пустой id — на список.
func TestPathIDRejectedBeforeRequest(t *testing.T) {
	t.Parallel()

	url := serve(t, func(_ http.ResponseWriter, r *http.Request) { t.Errorf("запрос: %s %s", r.Method, r.URL) })
	v, err := NewVPC(url, noAuth)
	require.NoError(t, err)
	s, err := NewIAM(url, noAuth)
	require.NoError(t, err)

	ctx := t.Context()
	for _, id := range []string{"", ".", ".."} {
		for name, call := range map[string]func() ([]byte, error){
			"vpc get":            func() ([]byte, error) { return v.Get(ctx, id) },
			"vpc delete":         func() ([]byte, error) { return v.Delete(ctx, id) },
			"vpc operation":      func() ([]byte, error) { return v.Operation(ctx, id) },
			"sa get":             func() ([]byte, error) { return s.ServiceAccount(ctx, id) },
			"sa delete":          func() ([]byte, error) { return s.DeleteServiceAccount(ctx, id) },
			"keys list":          func() ([]byte, error) { return s.AccessKeys(ctx, id) },
			"key get":            func() ([]byte, error) { return s.AccessKey(ctx, "sa", id) },
			"key get аккаунт":    func() ([]byte, error) { return s.AccessKey(ctx, id, "ak") },
			"key create":         func() ([]byte, error) { return s.CreateAccessKey(ctx, id, "", "") },
			"key delete":         func() ([]byte, error) { return s.DeleteAccessKey(ctx, "sa", id) },
			"key delete аккаунт": func() ([]byte, error) { return s.DeleteAccessKey(ctx, id, "ak") },
			"role get":           func() ([]byte, error) { return s.Permission(ctx, id) },
			"role revoke":        func() ([]byte, error) { return s.Revoke(ctx, id) },
		} {
			_, err := call()
			require.ErrorIs(t, err, ErrBadID, "%s %q", name, id)
		}
	}
}

// Пробел в query у всех трёх API — %20, как до сгенерированных клиентов.
func TestComputeAndVPCQuerySpaceAsPercent20(t *testing.T) {
	t.Parallel()

	var query string

	url := serve(t, func(_ http.ResponseWriter, r *http.Request) { query = r.URL.RawQuery })
	c, err := NewCompute(url, noAuth)
	require.NoError(t, err)

	_, err = c.Subnets(t.Context(), SubnetFilter{Project: "11111111-1111-4111-8111-111111111111", Name: "a b+c"})
	require.NoError(t, err)
	assert.Contains(t, query, "name=a%20b%2Bc")

	v, err := NewVPC(serve(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = w.Write([]byte(`{"vpcs":[],"nextPageToken":"a+b /="}`))
		}
	}), noAuth)
	require.NoError(t, err)

	_, err = v.List(t.Context(), "p")
	require.NoError(t, err)
	assert.Contains(t, query, "pageToken=a%2Bb%20%2F%3D")
}

// Списки VPC и IAM ходят за следующей страницей с токеном из ответа: без
// него вторая страница — снова первая, и повтор токена обрывает обход.
func TestTokenListsSendNextPageToken(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, key string
		list      func(t *testing.T, url string) ([]byte, error)
	}{
		{"vpcs", "vpcs", func(t *testing.T, url string) ([]byte, error) {
			v, err := NewVPC(url, noAuth)
			require.NoError(t, err)

			return v.List(t.Context(), "p")
		}},
		{"service accounts", "service_accounts", func(t *testing.T, url string) ([]byte, error) {
			s, err := NewIAM(url, noAuth)
			require.NoError(t, err)

			return s.ServiceAccounts(t.Context(), "p")
		}},
		{"permissions", "permissions", func(t *testing.T, url string) ([]byte, error) {
			s, err := NewIAM(url, noAuth)
			require.NoError(t, err)

			return s.Permissions(t.Context(), "f")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var tokens []string

			raw, err := tc.list(t, serve(t, func(w http.ResponseWriter, r *http.Request) {
				token := r.URL.Query().Get("pageToken")
				tokens = append(tokens, token)

				if token == "" {
					_, _ = fmt.Fprintf(w, `{"%s":[{"id":"a"}],"next_page_token":"t2"}`, tc.key)
					return
				}

				_, _ = fmt.Fprintf(w, `{"%s":[{"id":"b"}]}`, tc.key)
			}))
			require.NoError(t, err)
			assert.Equal(t, []string{"", "t2"}, tokens)
			assert.JSONEq(t, fmt.Sprintf(`{"%s":[{"id":"a"},{"id":"b"}]}`, tc.key), string(raw))
		})
	}
}

// testTransport — для обёрток транспорта в тестах, по той же причине.
var testTransport = &http.Transport{}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) {
	return 0, errors.New("тело больше предела")
}
func (failingBody) Close() error { return nil }

func TestReadBodyErrorPassedThrough(t *testing.T) {
	t.Parallel()

	rt := redisRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: failingBody{}, Header: http.Header{}, Request: r}, nil
	})

	v, err := NewVPC("http://vpc.test", Options{HTTPClient: &http.Client{Transport: rt}, Auth: noAuth.Auth})
	require.NoError(t, err)

	_, err = v.Get(t.Context(), "v1")
	require.EqualError(t, err, "тело больше предела")
}
