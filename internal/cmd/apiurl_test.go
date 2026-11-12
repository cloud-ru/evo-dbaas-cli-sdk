package cmd

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --api-url продукта перекрывает адрес из env и конфига (флаг > env > файл, root.go init).
// Фейковое облако из env отвергает любой запрос API: дошёл туда — флаг потерян.
func TestAPIURLFlagOverridesConfigPerProduct(t *testing.T) {
	for _, product := range []string{"clickhouse", "redis", "kafka", "postgresql"} {
		t.Run(product, func(t *testing.T) {
			f := newFakeCloud(t)

			var (
				mu    sync.Mutex
				paths []string
			)

			override := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()

				paths = append(paths, r.Method+" "+r.URL.Path)

				mu.Unlock()

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"` + testProject + `"}`))
			}))
			t.Cleanup(override.Close)

			_, _, err := execute(NewDBaaSRoot(), "", product, "--api-url", override.URL, "clusters", "get", testProject, "--json")
			require.NoError(t, err)

			mu.Lock()
			defer mu.Unlock()

			assert.NotEmpty(t, paths, "запрос ушёл на адрес из --api-url")
			assert.Empty(t, f.calls, "адрес из env не используется")
		})
	}
}
