package clickhouse

import (
	"encoding/json"
	"testing"
)

func TestClusterConfigAcceptsStringAndNumber(t *testing.T) {
	for _, body := range []string{`{"max_connections":"100"}`, `{"max_connections":100}`} {
		var c CloudruDbaasClickhouseV1ClusterConfig
		if err := json.Unmarshal([]byte(body), &c); err != nil {
			t.Fatalf("%s: %v", body, err)
		}

		if c.MaxConnections == nil || *c.MaxConnections != 100 {
			t.Fatalf("%s: max_connections = %v", body, c.MaxConnections)
		}
	}

	b, _ := json.Marshal(CloudruDbaasClickhouseV1ClusterConfig{MaxConnections: ptr(FlexInt64(100))})
	if string(b) != `{"max_connections":100}` {
		t.Fatalf("marshal: %s", b)
	}
}

func ptr[T any](v T) *T { return &v }
