package snapshot

import (
	"io"
	"strings"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/output"
)

// snapshotRow — поля снапшота для таблицы; живой прогон 17.11: list/get печатали сырой JSON.
type snapshotRow struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Indexes string `json:"indexes_count"`
	Start   string `json:"start_time"`
	End     string `json:"end_time"`
}

func printSnapshots(w io.Writer, raw []byte) error {
	var l struct {
		Snapshots []snapshotRow `json:"snapshots"`
	}
	if err := output.Decode(raw, &l); err != nil {
		return err
	}

	return table(w, l.Snapshots...)
}

func printSnapshot(w io.Writer, raw []byte) error {
	var r snapshotRow
	if err := output.Decode(raw, &r); err != nil {
		return err
	}

	return table(w, r)
}

func table(w io.Writer, rows ...snapshotRow) error {
	t := output.NewTable(w, "NAME", "STATE", "INDEXES", "START", "END")
	for _, r := range rows {
		t.Row(r.Name, strings.TrimPrefix(r.State, "SNAPSHOT_STATE_"), r.Indexes, r.Start, r.End)
	}

	return t.Flush()
}
