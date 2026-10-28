package config

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// Setting — значение ключа и откуда оно: flag, env CLOUDRU_…, file <профиль>, default;
// нигде не задан — пустое значение и источник none.
type Setting struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

// Report — то, что показывает config list: файл, профиль и откуда он, все ключи.
type Report struct {
	File          string    `json:"file"`
	FileExists    bool      `json:"file_exists"`
	Profile       string    `json:"profile"`
	ProfileSource string    `json:"profile_source"`
	Settings      []Setting `json:"settings"`
}

// Describe — ключи в порядке полей Config со значением и источником; key_secret — маской.
// Флаги команд (--project) пакет не видит: их источник ставит Override.
func Describe(o Options) (Report, error) { return describe(o, os.Environ) }

func describe(o Options, environ func() []string) (Report, error) {
	values, sources, f, err := resolve(o, environ)
	if err != nil {
		return Report{}, err
	}

	r := Report{File: f.path, FileExists: f.exists, Profile: f.profile, ProfileSource: f.from}

	for _, k := range keys {
		s := Setting{Key: k, Source: "none"}
		if v, ok := values[k]; ok {
			s.Value, s.Source = fmt.Sprint(v), sources[k]
		}

		if k == "key_secret" {
			s.Value = mask(s.Value)
		}

		r.Settings = append(r.Settings, s)
	}

	return r, nil
}

// Override — значение из флага команды: источник flag.
func (r *Report) Override(key, value string) {
	for i := range r.Settings {
		if r.Settings[i].Key == key {
			r.Settings[i].Value, r.Settings[i].Source = value, "flag"
		}
	}
}

// Get — один ключ; неизвестный — ошибка со списком ключей.
func (r Report) Get(key string) (Setting, error) {
	i := slices.IndexFunc(r.Settings, func(s Setting) bool { return s.Key == key })
	if i < 0 {
		return Setting{}, i18n.Errorf("config.err.unknown", key, strings.Join(keys, ", "))
	}

	return r.Settings[i], nil
}

// mask — как aws configure list: видны последние 4 символа, если секрет длинный; короткий — только звёздочки.
func mask(s string) string {
	switch r := []rune(s); {
	case len(r) == 0:
		return ""
	case len(r) < 16:
		return "****"
	default:
		return "****" + string(r[len(r)-4:])
	}
}
