package config

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cloud-ru/evo-dbaas-cli-sdk/internal/i18n"
)

// Change — что записано: файл, профиль и переменная окружения, которая перекрывает
// записанное значение (пусто — не перекрывает).
type Change struct {
	Path, Profile, ShadowedBy string
}

// Set пишет ключ в профиль: --profile > CLOUDRU_PROFILE > profile: в файле > default.
// Профиля нет — создаёт; файла и каталога нет — тоже (0700, 0600).
func Set(o Options, key, value string) (Change, error) { return set(o, os.Environ, key, value) }

func set(o Options, environ func() []string, key, value string) (Change, error) {
	if !slices.Contains(keys, key) {
		return Change{}, i18n.Errorf("config.err.unknown", key, strings.Join(keys, ", "))
	}

	// Тег явно: key_id из цифр или «true» в адресе не должны стать числом и логическим в YAML.
	tag := "!!str"
	if key == "allow_http" {
		if value != "true" && value != "false" {
			return Change{}, i18n.Errorf("config.err.bool", value)
		}

		tag = "!!bool"
	}

	vs := vars(environ())
	c := Change{Profile: o.Profile}

	if c.Profile == "" {
		c.Profile = vs["CLOUDRU_PROFILE"]
	}

	for name, k := range envKeys {
		if k == key && vs[name] != "" {
			c.ShadowedBy = name
		}
	}

	var err error

	c.Path, err = edit(o, vs, func(_ string, top *yaml.Node, _ map[string]map[string]any, current string) error {
		if c.Profile == "" {
			c.Profile = current
		}

		if c.Profile == "" {
			c.Profile = "default"
		}

		scalar(field(field(field(top, "profiles"), c.Profile), key), tag, value)

		return nil
	})

	return c, err
}

// UseProfile пишет profile: в файл. Профиля нет — ошибка со списком, кроме default:
// без профиля default из файла просто ничего не берётся (§8.12).
func UseProfile(o Options, name string) (string, error) { return useProfile(o, os.Environ, name) }

func useProfile(o Options, environ func() []string, name string) (string, error) {
	return edit(o, vars(environ()), func(path string, top *yaml.Node, profiles map[string]map[string]any, _ string) error {
		if _, ok := profiles[name]; !ok && name != "default" {
			return i18n.Errorf("config.err.no_profile", path, name, strings.Join(slices.Sorted(maps.Keys(profiles)), ", "))
		}

		scalar(field(top, "profile"), "!!str", name)

		return nil
	})
}

// edit читает файл, проверяет его так же строго, как Load, меняет и пишет атомарно.
// Кривой файл не переписываем: правка поверх него спрятала бы ошибку пользователя.
// Узлы YAML, а не map: комментарии и порядок ключей пользователя остаются.
func edit(o Options, vs map[string]string, change func(path string, top *yaml.Node, profiles map[string]map[string]any, current string) error) (string, error) {
	path, _ := filePath(o, vs)
	if path == "" {
		return "", i18n.Errorf("config.err.no_path")
	}

	// Ссылку не заменяем файлом: пишем туда, куда она ведёт (конфиг в dotfiles); битую — не трогаем.
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		if path, err = filepath.EvalSymlinks(path); err != nil {
			return "", i18n.Errorf("config.err.read", err)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", i18n.Errorf("config.err.read", err)
	}

	profiles, current, err := parse(path, raw)
	if err != nil {
		return "", err
	}

	// Второй документ (---) и ссылки на якоря (*, <<) при перезаписи пропали бы или разъехались — не правим.
	var doc, next yaml.Node

	dec := yaml.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return "", i18n.Errorf("config.err.parse", path, err)
	}

	if dec.Decode(&next) != io.EOF || hasAliases(&doc) {
		return "", i18n.Errorf("config.err.manual", path)
	}

	// Пустой файл или одни комментарии yaml.v3 читает как ничто: комментарии переносим как есть.
	var out bytes.Buffer
	if doc.Kind != yaml.DocumentNode {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{}}}

		out.Write(bytes.TrimRight(raw, "\n"))

		if out.Len() > 0 {
			out.WriteByte('\n')
		}
	}

	top := doc.Content[0]
	if top.Kind != yaml.MappingNode {
		*top = yaml.Node{Kind: yaml.MappingNode}
	}

	if err := change(path, top, profiles, current); err != nil {
		return "", err
	}

	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)

	if err := enc.Encode(&doc); err != nil {
		return "", i18n.Errorf("config.err.write", path, err)
	}

	return path, write(path, out.Bytes())
}

// field — значение ключа в отображении m; нет — добавляет пустое. Пустой профиль
// (`stand:` без ключей) — null, его превращает в отображение.
func field(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		*m = yaml.Node{Kind: yaml.MappingNode}
	}

	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}

	v := &yaml.Node{Kind: yaml.MappingNode}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)

	return v
}

func hasAliases(n *yaml.Node) bool {
	return n.Kind == yaml.AliasNode || slices.ContainsFunc(n.Content, hasAliases)
}

// scalar меняет значение на месте: комментарий в строке (`key_id: a # ключ`) остаётся.
func scalar(n *yaml.Node, tag, value string) {
	n.Kind, n.Tag, n.Value, n.Style, n.Content = yaml.ScalarNode, tag, value, 0, nil
}

// write — каталог 0700, файл 0600 через временный файл рядом и rename: оборванная запись
// не оставит полфайла, а ключ ни на миг не лежит с правами шире 0600.
func write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return i18n.Errorf("config.err.write", path, err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return i18n.Errorf("config.err.write", path, err)
	}

	defer func() { _ = os.Remove(tmp.Name()) }()

	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}

	if cerr := tmp.Close(); err == nil {
		err = cerr
	}

	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}

	if err != nil {
		return i18n.Errorf("config.err.write", path, err)
	}

	return nil
}
