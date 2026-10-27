package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// pending — каталоги, ещё не переведённые на i18n: в них кириллица в строках
// кода пока разрешена. Перевели пакет — уберите его отсюда.
var pending = []string{}

const root = "../.."

func catalog(t *testing.T, lang string) map[string]string {
	t.Helper()

	raw, err := locales.ReadFile("locales/" + lang + ".yaml")
	require.NoError(t, err)

	m := map[string]string{}
	require.NoError(t, yaml.Unmarshal(raw, &m))

	return m
}

func TestCatalogsHaveSameKeys(t *testing.T) {
	t.Parallel()

	en := catalog(t, "en")
	for _, l := range Languages[1:] {
		other := catalog(t, l)
		for k := range en {
			assert.Contains(t, other, k, "%s: нет ключа", l)
		}

		for k := range other {
			assert.Contains(t, en, k, "en: нет ключа из %s", l)
		}
	}
}

// verbs — какой глагол fmt получает каждый аргумент (с 1), как считает сам fmt:
// %[n] переставляет счётчик, * берёт аргумент под ширину или точность.
func verbs(s string) map[int]string {
	got := map[int]string{}
	argNum := 0

	index := func(i int) int {
		if i < len(s) && s[i] == '[' {
			if j := strings.IndexByte(s[i:], ']'); j > 0 {
				if n, err := strconv.Atoi(s[i+1 : i+j]); err == nil {
					argNum = n - 1
					return i + j + 1
				}
			}
		}

		return i
	}

	num := func(i int) int {
		if i < len(s) && s[i] == '*' {
			argNum++
			got[argNum] = "*"

			return i + 1
		}

		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}

		return i
	}

	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}

		i++
		for i < len(s) && strings.IndexByte("-+# 0", s[i]) >= 0 {
			i++
		}

		i = num(index(i))
		if i < len(s) && s[i] == '.' {
			i = num(index(i + 1))
		}

		i = index(i)
		if i >= len(s) || s[i] == '%' {
			continue
		}

		argNum++
		got[argNum] = string(s[i])
	}

	return got
}

func TestVerbs(t *testing.T) {
	t.Parallel()

	for s, want := range map[string]map[int]string{
		"100%% %s":       {1: "s"},
		"%s %d %w":       {1: "s", 2: "d", 3: "w"},
		"%[2]s, %[1]d":   {1: "d", 2: "s"},
		"%*d %-8.3f":     {1: "*", 2: "d", 3: "f"},
		"%[3]*.[2]*[1]f": {1: "f", 2: "*", 3: "*"},
	} {
		assert.Equal(t, want, verbs(s), s)
	}
}

// Глаголы fmt в переводе — те же у тех же аргументов: порядок в ru можно менять
// только через %[n], иначе Sprintf подставит аргумент не туда или потеряет %w.
func TestCatalogsHaveSameVerbs(t *testing.T) {
	t.Parallel()

	en := catalog(t, "en")
	for _, l := range Languages[1:] {
		for k, v := range catalog(t, l) {
			assert.Equal(t, verbs(en[k]), verbs(v), "%s: %s", l, k)
		}
	}
}

// go-i18n прогоняет текст через text/template: {{.x}} без данных станет «<no value>».
func TestCatalogsHaveNoTemplates(t *testing.T) {
	t.Parallel()

	for _, l := range Languages {
		for k, v := range catalog(t, l) {
			assert.NotContains(t, v, "{{", "%s: %s", l, k)
		}
	}
}

const importPath = "github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"

// call — вызов i18n.T / i18n.Errorf / i18n.Sentinel в коде.
type call struct {
	fn, id, pos string
	args        int  // аргументов после id
	pkgVar      bool // внутри пакетной var: вычислится до SetLanguage
}

// calls — вызовы i18n в коде. Пакет импортируется только под своим именем:
// по алиасу или через точку тест вызовы бы не нашёл. id обязан быть литералом,
// иначе тест не может проверить его наличие в каталоге.
func calls(t *testing.T) []call {
	t.Helper()

	var found []call

	walkGo(t, func(path string, f *ast.File, fset *token.FileSet) {
		imported := false

		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == importPath {
				imported = true

				assert.Nil(t, imp.Name, "%s: i18n импортируется без алиаса", fset.Position(imp.Pos()))
			}
		}

		if !imported {
			return
		}

		for _, decl := range f.Decls {
			gd, isVar := decl.(*ast.GenDecl)
			fd, isFunc := decl.(*ast.FuncDecl)
			pkgVar := isVar && gd.Tok == token.VAR || isFunc && fd.Recv == nil && fd.Name.Name == "init"

			ast.Inspect(decl, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				sel, ok := ce.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}

				if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "i18n" ||
					!slices.Contains([]string{"T", "Errorf", "Sentinel"}, sel.Sel.Name) {
					return true
				}

				pos := fset.Position(ce.Pos()).String()

				lit, ok := ce.Args[0].(*ast.BasicLit)
				if !assert.True(t, ok && lit.Kind == token.STRING, "%s: id не литерал", pos) {
					return true
				}

				id, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)

				found = append(found, call{fn: sel.Sel.Name, id: id, pos: pos, args: len(ce.Args) - 1, pkgVar: pkgVar})

				return true
			})
		}
	})

	return found
}

func TestUsedIDsExist(t *testing.T) {
	t.Parallel()

	en := catalog(t, "en")
	for _, c := range calls(t) {
		assert.Contains(t, en, c.id, "%s: нет в каталоге", c.pos)
	}
}

func TestNoUnusedIDs(t *testing.T) {
	t.Parallel()

	used := map[string]bool{}
	for _, c := range calls(t) {
		used[c.id] = true
	}

	for id := range catalog(t, "en") {
		assert.True(t, used[id], "%s: ключ каталога не используется в коде", id)
	}
}

// Число аргументов вызова — по глаголам текста: лишний даст %!(EXTRA …),
// недостающий — %!s(MISSING). %w — только в Errorf; у Sentinel аргументов нет.
func TestCallsMatchVerbs(t *testing.T) {
	t.Parallel()

	en := catalog(t, "en")
	for _, c := range calls(t) {
		vs := verbs(en[c.id])
		assert.Len(t, vs, c.args, "%s: %s — аргументов не столько, сколько глаголов", c.pos, c.id)

		for i := 1; i <= len(vs); i++ {
			assert.Contains(t, vs, i, "%s: %s — %%[n] мимо аргументов 1..%d", c.pos, c.id, len(vs))
		}

		assert.NotRegexp(t, `(^|[^%])(%%)*%$`, en[c.id], "%s: %s — одинокий %% в конце", c.pos, c.id)

		if c.fn != "Errorf" {
			assert.NotContains(t, slices.Collect(maps.Values(vs)), "w", "%s: %%w — только в i18n.Errorf", c.pos)
		}
	}
}

// T и Errorf в пакетной var вычисляются при загрузке пакета, до SetLanguage:
// текст навсегда останется на en. Для ошибок — i18n.Sentinel.
func TestNoTranslationInPackageVars(t *testing.T) {
	t.Parallel()

	for _, c := range calls(t) {
		assert.False(t, c.pkgVar && c.fn != "Sentinel", "%s: i18n.%s в пакетной var — нужен i18n.Sentinel", c.pos, c.fn)
	}
}

// Переведённые пакеты не держат русский текст в строках кода: всё, что видит
// пользователь, — из каталога. Комментарии и тесты не в счёт.
func TestNoCyrillicOutsidePending(t *testing.T) {
	t.Parallel()

	walkGo(t, func(path string, f *ast.File, fset *token.FileSet) {
		if slices.Contains(pending, filepath.Dir(path)) {
			return
		}

		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && strings.ContainsFunc(lit.Value, isCyrillic) {
				assert.Fail(t, "кириллица в строке кода", "%s: %s", fset.Position(lit.Pos()), lit.Value)
			}

			return true
		})
	})
}

// Нет кириллицы — ещё не значит «из каталога»: в переведённых пакетах текст
// подсказок, ошибок, флагов и сообщений Printer не пишется литералом и по-английски. Разрешены
// форматы ошибок без слов («%s: %w») и с префиксом пакета («config: %w»).
var (
	userFields = []string{"Short", "Long", "Example", "Msg"}
	flagMethod = regexp.MustCompile(`^(Bool|Count|Duration|Float64|Int|Int64|IntSlice|String|StringArray|StringSlice|StringToString|Uint)(Var)?P?$`)
	verbOnly   = regexp.MustCompile(`%(\[\d+\])?[-+# 0]*[0-9*]*(\.[0-9*]+)?[a-zA-Z]`)
	hasLetter  = regexp.MustCompile(`\pL`)
)

func TestTranslatedPackagesTakeTextFromCatalog(t *testing.T) {
	t.Parallel()

	walkGo(t, func(path string, f *ast.File, fset *token.FileSet) {
		dir := filepath.Dir(path)
		if slices.Contains(pending, dir) || dir == "internal/i18n" {
			return
		}

		// Формат ошибки без слов («%s: %w») или с префиксом своего пакета («config: %w»).
		wrap := func(s string) bool {
			rest := strings.TrimPrefix(verbOnly.ReplaceAllString(s, ""), f.Name.Name+": ")

			return !hasLetter.MatchString(rest)
		}
		noWords := func(s string) bool { return !hasLetter.MatchString(verbOnly.ReplaceAllString(s, "")) }

		// bad проверяет литерал, склейку литералов («"x " + id») и формат fmt.Sprintf.
		var bad func(e ast.Expr, ok func(string) bool, what string)

		bad = func(e ast.Expr, ok func(string) bool, what string) {
			switch v := e.(type) {
			case *ast.BinaryExpr:
				bad(v.X, noWords, what)
				bad(v.Y, noWords, what)

				return
			case *ast.CallExpr:
				if sel, isSel := v.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == "Sprintf" && len(v.Args) > 0 {
					bad(v.Args[0], ok, what)
				}

				return
			}

			lit, isLit := e.(*ast.BasicLit)
			if !isLit || lit.Kind != token.STRING {
				return
			}

			s, err := strconv.Unquote(lit.Value)
			require.NoError(t, err)

			if hasLetter.MatchString(s) && !ok(s) {
				assert.Fail(t, what+" — литералом, не из каталога", "%s: %s", fset.Position(lit.Pos()), lit.Value)
			}
		}

		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.KeyValueExpr:
				if k, ok := n.Key.(*ast.Ident); ok && slices.Contains(userFields, k.Name) {
					bad(n.Value, noWords, k.Name)
				}
			case *ast.AssignStmt:
				for i, l := range n.Lhs {
					if sel, ok := l.(*ast.SelectorExpr); ok && slices.Contains(userFields, sel.Sel.Name) && i < len(n.Rhs) {
						bad(n.Rhs[i], noWords, sel.Sel.Name)
					}
				}
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || len(n.Args) == 0 {
					return true
				}

				x, _ := sel.X.(*ast.Ident)
				switch {
				case x != nil && (x.Name == "fmt" && sel.Sel.Name == "Errorf" || x.Name == "errors" && sel.Sel.Name == "New"):
					bad(n.Args[0], wrap, "текст ошибки")
				case sel.Sel.Name == "Messagef" || sel.Sel.Name == "Progressf":
					bad(n.Args[0], noWords, "вывод пользователю")
				case x != nil && x.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Fprint") && len(n.Args) > 1:
					for _, a := range n.Args[1:] {
						bad(a, noWords, "вывод пользователю")
					}
				case flagMethod.MatchString(sel.Sel.Name) && len(n.Args) >= 3:
					bad(n.Args[len(n.Args)-1], noWords, "описание флага")
				}
			}

			return true
		})
	})
}

func isCyrillic(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }

// walkGo обходит не тестовые и не сгенерированные .go файлы под cmd/ и internal/;
// path — от корня репозитория.
func walkGo(t *testing.T, fn func(path string, f *ast.File, fset *token.FileSet)) {
	t.Helper()

	fset := token.NewFileSet()
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			require.NoError(t, err)

			if d.IsDir() && d.Name() == "gen" {
				return filepath.SkipDir
			}

			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}

			f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
			require.NoError(t, err)

			if !ast.IsGenerated(f) {
				fn(strings.TrimPrefix(filepath.ToSlash(p), root+"/"), f, fset)
			}

			return nil
		})
		require.NoError(t, err)
	}
}
