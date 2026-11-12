package cloud

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/operation"
)

func FuzzParseOperation(f *testing.F) {
	addOperationSeeds(f, "resource_id")
	f.Fuzz(func(t *testing.T, raw []byte) {
		checkOperationProjection(t, raw, ParseOperation, true)
	})
}

func addOperationSeeds(f *testing.F, resourceKey string) {
	f.Helper()

	for _, s := range []string{"", "x", "=", " ", "null", "[]", "{}", "{", `{"id":""}`, `{"id":1}`, `{"id":"x","done":null}`, `{"id":"x","done":"true"}`, `{"id":"x","error":[]}`, `{"id":"x","error":{}}`, `{"id":"a","id":"b"}`, `{"id":"x"} {}`, `{"id":"x","extra":999999999999999999999999999999999999999}`, `{"id":"` + strings.Repeat("я", 4096) + `"}`} {
		f.Add([]byte(s))
	}

	f.Add([]byte(`{"id":"\u64cd\u4f5c","` + resourceKey + `":"ресурс","description":"описание","done":true,"error":{"message":"quota"}}`))
}

// The schema oracle uses a wire DTO independent of the production DTO and
// compares the entire projection, including zero fields and the original bytes.
func checkOperationProjection(t *testing.T, raw []byte, parse func([]byte) (operation.Operation, bool), description bool) {
	t.Helper()

	type common struct {
		ID    string `json:"id"`
		Done  bool   `json:"done"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	var shared common

	want := operation.Operation{Raw: raw}

	var decodeErr error
	if description {
		var wire struct {
			common

			ResourceID  string `json:"resource_id"`
			Description string `json:"description"`
		}

		decodeErr = json.Unmarshal(raw, &wire)
		shared = wire.common
		want.ResourceID, want.Description = wire.ResourceID, wire.Description
	} else {
		var wire struct {
			common

			ResourceID string `json:"resourceId"`
		}

		decodeErr = json.Unmarshal(raw, &wire)
		shared = wire.common
		want.ResourceID = wire.ResourceID
	}

	wantOK := decodeErr == nil && shared.ID != ""

	got, ok := parse(raw)
	if ok != wantOK || !bytes.Equal(got.Raw, raw) {
		t.Fatalf("operation recognition/raw changed: %s -> %+v, %t; want ok=%t", raw, got, ok, wantOK)
	}

	if wantOK {
		want.ID, want.Done = shared.ID, shared.Done
		if shared.Error != nil {
			want.Failed, want.Message = true, shared.Error.Message
		}
	} else {
		want = operation.Operation{Raw: raw}
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operation fields changed: got %+v; want %+v", got, want)
	}
}
