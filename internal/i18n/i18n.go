// Package i18n — тексты для пользователя на его языке: каталоги en и ru встроены в бинарник, язык выбирается один раз при старте.
package i18n

import (
	"embed"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

//go:embed locales/*.yaml
var locales embed.FS

// Languages — языки каталогов; первый — по умолчанию.
var Languages = []string{"en", "ru"}

var (
	bundle = newBundle()
	// localizer меняет только SetLanguage, читают все команды — atomic, чтобы
	// параллельные тесты не гонялись с выбором языка.
	localizer atomic.Pointer[goi18n.Localizer]
)

func init() { localizer.Store(goi18n.NewLocalizer(bundle, Languages[0])) }

func newBundle() *goi18n.Bundle {
	b := goi18n.NewBundle(language.English)
	b.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)

	for _, l := range Languages {
		// Каталог встроен при сборке: битый — ошибка сборки, не пользователя.
		if _, err := b.LoadMessageFileFS(locales, "locales/"+l+".yaml"); err != nil {
			panic(err)
		}
	}

	return b
}

// SetLanguage выбирает язык по значению LC_ALL / LC_MESSAGES / LANG:
// «ru_RU.UTF-8» → ru, всё остальное и пустое → en. Вызывать до сборки команд:
// подсказки cobra читают каталог при создании команды.
func SetLanguage(posix string) {
	lang := Languages[0]
	if strings.HasPrefix(strings.ToLower(posix), "ru") {
		lang = "ru"
	}

	localizer.Store(goi18n.NewLocalizer(bundle, lang))
}

func lookup(id string) (string, bool) {
	s, err := localizer.Load().Localize(&goi18n.LocalizeConfig{MessageID: id})

	return s, err == nil
}

// T — текст по id из каталога, всегда через fmt.Sprintf: литеральный % в
// каталоге пишется как %%. Нет id в каталоге — вернёт сам id без подстановки:
// заметно в выводе, а тест каталога ловит такое до мержа.
func T(id string, args ...any) string {
	s, ok := lookup(id)
	if !ok {
		return id
	}

	return fmt.Sprintf(s, args...)
}

// Errorf — fmt.Errorf по тексту из каталога: %w в тексте оборачивает ошибку.
// Нет id — id с обёрнутыми ошибками из args, чтобы errors.Is не терялся.
func Errorf(id string, args ...any) error {
	s, ok := lookup(id)
	if ok {
		return fmt.Errorf(s, args...)
	}

	var errs []error

	for _, a := range args {
		if e, ok := a.(error); ok {
			errs = append(errs, e)
		}
	}

	if len(errs) == 0 {
		return errors.New(id)
	}

	return fmt.Errorf("%s: %w", id, errors.Join(errs...))
}

// Sentinel — ошибка для пакетной var: текст берётся из каталога при каждом
// Error(), то есть уже на выбранном языке. T и Errorf в пакетной var вызвать
// нельзя — они сработают до SetLanguage и навсегда останутся на en.
// Две Sentinel с одним id равны: errors.Is их не различит.
func Sentinel(id string) error { return sentinelError(id) }

type sentinelError string

func (s sentinelError) Error() string { return T(string(s)) }
