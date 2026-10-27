// Package output — всё, что видит пользователь: результат команды (JSON как
// есть или таблица для человека) в stdout, сообщения и прогресс в stderr,
// тексты ошибок нижних слоёв. Команды не пишут в потоки сами.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// Printer печатает результат команды в Out, сообщения — в Err.
type Printer struct {
	Out  io.Writer
	Err  io.Writer
	JSON bool // --json: результат — JSON, сообщения о ходе работы молчат
}

// Object печатает результат: в режиме JSON (или без human) — v как JSON,
// иначе — то, что human пишет в Out. []byte — тело API, печатается без
// изменений (только сжатие пробелов); остальное — через json.Marshal.
func (p *Printer) Object(v any, human func(w io.Writer) error) error {
	if !p.JSON && human != nil {
		return human(p.Out)
	}

	var buf bytes.Buffer

	switch b := v.(type) {
	case []byte:
		if json.Compact(&buf, b) != nil {
			buf.Reset()
			buf.Write(b)
		}
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return i18n.Errorf("output.err.encode_json", err)
		}

		buf.Write(raw)
	}

	buf.WriteByte('\n')

	if _, err := p.Out.Write(buf.Bytes()); err != nil {
		return i18n.Errorf("output.err.write_output", err)
	}

	return nil
}

// Messagef пишет сообщение для человека в Err в любом режиме. Подтверждение
// мутации команда пишет из human-колбэка Object — при --json его заменяет JSON.
func (p *Printer) Messagef(format string, args ...any) {
	fmt.Fprintf(p.Err, format+"\n", args...)
}

// Progressf пишет строку о ходе долгой операции в Err; в режиме JSON молчит.
func (p *Printer) Progressf(format string, args ...any) {
	if p.JSON {
		return
	}

	p.Messagef(format, args...)
}

// Table — колонки, выровненные пробелами (text/tabwriter).
type Table struct {
	tw *tabwriter.Writer
}

// NewTable начинает таблицу в w; header — первая строка, пустой — без неё.
func NewTable(w io.Writer, header ...string) *Table {
	t := &Table{tw: tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)}
	if len(header) > 0 {
		t.Row(header...)
	}

	return t
}

// Row добавляет строку.
func (t *Table) Row(cols ...string) { fmt.Fprintln(t.tw, strings.Join(cols, "\t")) }

// Field добавляет строку «ключ: значение» — карточка одного объекта.
func (t *Table) Field(k, v string) { fmt.Fprintf(t.tw, "%s:\t%s\n", k, v) }

// Flush выравнивает и печатает накопленное.
func (t *Table) Flush() error {
	if err := t.tw.Flush(); err != nil {
		return i18n.Errorf("output.err.write_table", err)
	}

	return nil
}

// Decode разбирает сырой ответ API для таблицы: команда объявляет структуру
// с нужными колонками, encoding/json ей запрещён.
func Decode(raw []byte, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return i18n.Errorf("output.err.decode", err)
	}

	return nil
}
