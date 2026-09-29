// Package skill reads portable instruction packages; it never executes code.
package skill

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const MaxContent = 64 * 1024
const MaxResources = 512 * 1024

type Resource struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
type Bundle struct {
	Content   string     `json:"content"`
	Resources []Resource `json:"resources"`
	Scripts   []Resource `json:"scripts"`
	Note      string     `json:"note"`
}
type Metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

var validName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func Parse(content string) (Metadata, error) {
	var m Metadata
	if len(content) > MaxContent || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return m, errors.New("SKILL.md 需为 UTF-8 文本，最多64KB")
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimPrefix(content, "\ufeff"), "\r\n", "\n"), "\n")
	if len(lines) < 4 || strings.TrimSpace(lines[0]) != "---" {
		return m, errors.New("SKILL.md 顶部需有 --- 包围的 name 和 description")
	}
	end := 1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "---" {
		end++
	}
	if end == len(lines) || yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &m) != nil {
		return m, errors.New("技能头部格式有误，请检查 name、description 与 ---")
	}
	if !validName.MatchString(m.Name) || len(m.Name) > 64 {
		return m, errors.New("技能名称限64字符，使用小写英文、数字和单个连字符，例如 budget-check")
	}
	if strings.TrimSpace(m.Description) == "" || len([]rune(m.Description)) > 1024 {
		return m, errors.New("请填写1–1024字的技能用途 description")
	}
	if strings.TrimSpace(strings.Join(lines[end+1:], "\n")) == "" {
		return m, errors.New("请在头部之后填写技能的具体步骤和要求")
	}
	return m, nil
}

func ResourcePath(p string) bool {
	if p == "" || p != path.Clean(p) || !filepath.IsLocal(p) || strings.Contains(p, "\\") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") || part == "scripts" || part == "node_modules" || part == "__pycache__" {
			return false
		}
	}
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".txt", ".csv", ".json", ".yaml", ".yml":
		return true
	}
	return false
}

func ScriptPath(p string) bool {
	if !strings.HasPrefix(p, "scripts/") || p != path.Clean(p) || !filepath.IsLocal(p) || strings.Contains(p, "\\") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") || part == "node_modules" || part == "__pycache__" {
			return false
		}
	}
	switch path.Ext(p) {
	case ".py", ".js", ".mjs", ".cjs", ".sh":
		return true
	}
	return false
}

func Validate(p Bundle) (Metadata, error) {
	m, err := Parse(p.Content)
	if err != nil {
		return m, err
	}
	if len(p.Resources)+len(p.Scripts) > 32 || len(p.Note) > 2000 {
		return m, errors.New("每个技能最多32份参考文件与脚本")
	}
	seen := map[string]bool{}
	total := 0
	for i, r := range append(append([]Resource{}, p.Resources...), p.Scripts...) {
		valid := ResourcePath(r.Path)
		if i >= len(p.Resources) {
			valid = ScriptPath(r.Path)
		}
		if !valid || r.Path == "SKILL.md" || seen[r.Path] {
			return m, errors.New("参考文件路径无效或重复")
		}
		seen[r.Path] = true
		total += len(r.Content)
		if len(r.Content) > MaxContent || !utf8.ValidString(r.Content) || strings.ContainsRune(r.Content, 0) {
			return m, errors.New("每份参考文件需为 UTF-8 文本，最多64KB")
		}
	}
	if total > MaxResources {
		return m, errors.New("技能参考文件合计最多512KB")
	}
	return m, nil
}

func Import(directory string) (Bundle, error) {
	p := Bundle{Resources: []Resource{}, Scripts: []Resource{}}
	r, err := os.OpenRoot(directory)
	if err != nil {
		return p, errors.New("无法打开技能目录")
	}
	defer r.Close()
	read := func(name string) (string, error) {
		info, err := r.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("%s 必须是普通文件，不支持符号链接", name)
		}
		f, err := r.Open(name)
		if err != nil {
			return "", err
		}
		defer f.Close()
		opened, err := f.Stat()
		if err != nil || !os.SameFile(info, opened) {
			return "", errors.New("技能文件读取时发生变化，请重试")
		}
		b, err := io.ReadAll(io.LimitReader(f, MaxContent+1))
		if err == nil && len(b) > MaxContent {
			err = fmt.Errorf("%s 超过64KB", name)
		}
		return string(b), err
	}
	p.Content, err = read("SKILL.md")
	if err != nil {
		return p, err
	}
	omitted, visited := 0, 0
	err = fs.WalkDir(r.FS(), ".", func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		visited++
		if visited > 2000 {
			return errors.New("技能目录超过2000项，请仅保留技能所需文件")
		}
		if name == "." || name == "SKILL.md" {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "__pycache__" {
				return fs.SkipDir
			}
			if strings.Count(name, "/") > 4 {
				omitted++
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || (!ResourcePath(name) && !ScriptPath(name)) {
			omitted++
			return nil
		}
		content, e := read(name)
		if e != nil {
			return e
		}
		if ScriptPath(name) {
			p.Scripts = append(p.Scripts, Resource{Path: name, Content: content})
		} else {
			p.Resources = append(p.Resources, Resource{Path: name, Content: content})
		}
		if len(p.Resources)+len(p.Scripts) > 32 {
			return errors.New("每个技能最多32份参考文件与脚本")
		}
		return nil
	})
	if err != nil {
		return p, err
	}
	if omitted > 0 {
		p.Note = "部分文件未导入：仅支持技能说明、文本参考和 Python / Node.js / Shell 脚本，其他文件已跳过。请检查技能步骤是否适用。"
	}
	sort.Slice(p.Scripts, func(i, j int) bool { return p.Scripts[i].Path < p.Scripts[j].Path })
	sort.Slice(p.Resources, func(i, j int) bool { return p.Resources[i].Path < p.Resources[j].Path })
	_, err = Validate(p)
	return p, err
}
