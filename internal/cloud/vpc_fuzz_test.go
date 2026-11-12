package cloud

import "testing"

func FuzzParseVPCOperation(f *testing.F) {
	addOperationSeeds(f, "resourceId")
	f.Fuzz(func(t *testing.T, raw []byte) {
		checkOperationProjection(t, raw, ParseVPCOperation, false)
	})
}
