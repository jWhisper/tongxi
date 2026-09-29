package material

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type WorkspaceFile struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size"`
}
type WorkspacePage struct {
	Path  string          `json:"path"`
	Files []WorkspaceFile `json:"files"`
	Next  int             `json:"next"`
}

func WorkspacePath(path string) (string, error) {
	if path == "" || path == "." {
		return ".", nil
	}
	if !filepath.IsLocal(path) {
		return "", errors.New("只能访问工作目录内的相对路径")
	}
	// Check before cleaning: ignored directories cannot be traversed even with '..'.
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasPrefix(part, ".") || part == "node_modules" || part == "__pycache__" {
			return "", errors.New("隐藏文件、依赖目录和上级路径不向 Agent 开放")
		}
	}
	return filepath.Clean(path), nil
}

func supportedFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".markdown", ".pdf", ".docx", ".xlsx", ".csv":
		return true
	}
	return false
}

func OpenWorkspace(path string) (*os.Root, error) {
	if path == "" {
		return nil, errors.New("请先为此会话选择工作目录")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != filepath.Clean(path) {
		return nil, errors.New("工作目录不可用，请恢复原目录或检查访问权限；此会话不能更换目录")
	}
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.New("工作目录不可用，请恢复原目录或检查访问权限")
	}
	return r, nil
}

// os.Root enforces containment during open, including concurrent symlink changes.
func workspaceInfo(root *os.Root, path string) (os.FileInfo, error) {
	var info os.FileInfo
	current := "."
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		var err error
		info, err = root.Lstat(current)
		if err != nil {
			return nil, errors.New("文件或目录不存在，或没有访问权限")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("工作目录内的符号链接不开放读取")
		}
	}
	return info, nil
}

func ListWorkspace(ctx context.Context, root *os.Root, path string, offset int) (WorkspacePage, error) {
	out := WorkspacePage{Path: path, Files: []WorkspaceFile{}}
	if offset < 0 {
		return out, errors.New("无效的目录分页位置")
	}
	info, err := workspaceInfo(root, path)
	if err != nil {
		return out, err
	}
	if !info.IsDir() {
		return out, errors.New("请选择目录")
	}
	f, err := root.Open(path)
	if err != nil {
		return out, errors.New("无法打开目录")
	}
	defer f.Close()
	entries, err := f.ReadDir(10001)
	if err != nil && err != io.EOF {
		return out, errors.New("无法读取目录")
	}
	if len(entries) > 10000 {
		return out, errors.New("单层目录超过10000项，请按资料类型拆分子目录")
	}
	files := []WorkspaceFile{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		rel := filepath.Join(path, entry.Name())
		if _, e := WorkspacePath(rel); e != nil || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if !entry.IsDir() && !supportedFile(entry.Name()) {
			continue
		}
		info, e := entry.Info()
		if e != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
			continue
		}
		files = append(files, WorkspaceFile{Name: entry.Name(), Path: rel, Directory: info.IsDir(), Size: info.Size()})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Directory != files[j].Directory {
			return files[i].Directory
		}
		return files[i].Name < files[j].Name
	})
	if offset > len(files) {
		return out, errors.New("目录已变化，请刷新后重试")
	}
	end := min(offset+100, len(files))
	out.Files = files[offset:end]
	if end < len(files) {
		out.Next = end
	}
	return out, nil
}

func ReadWorkspace(ctx context.Context, root *os.Root, path string) ([]byte, Document, error) {
	info, err := workspaceInfo(root, path)
	if err != nil {
		return nil, Document{}, err
	}
	if !info.Mode().IsRegular() || !supportedFile(path) {
		return nil, Document{}, errors.New("请选择 TXT、Markdown、PDF、DOCX、XLSX 或 CSV 普通文件")
	}
	if info.Size() > MaxFileBytes {
		return nil, Document{}, errors.New("文件超过20MB，请拆分")
	}
	f, err := root.Open(path)
	if err != nil {
		return nil, Document{}, errors.New("无法读取文件")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, Document{}, errors.New("文件读取时发生变化，请重试")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, Document{}, errors.New("文件读取不完整")
	}
	after, err := f.Stat()
	if err != nil || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return nil, Document{}, errors.New("文件读取时发生变化，请重试")
	}
	doc, err := Parse(ctx, path, data)
	return data, doc, err
}

// Imported originals are only copied on explicit user selection, never overwritten.
func CopyToWorkspace(directory, source string, data []byte) (string, error) {
	root, err := OpenWorkspace(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	name := filepath.Base(source)
	if _, err = WorkspacePath(name); err != nil {
		return "", err
	}
	if !supportedFile(name) {
		return "", errors.New("此文件格式尚不支持")
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err == nil {
		rel, e := filepath.Rel(directory, resolved)
		if e == nil {
			if _, e = WorkspacePath(rel); e == nil {
				if info, e := workspaceInfo(root, rel); e == nil && info.Mode().IsRegular() {
					return rel, nil
				}
			}
		}
	}
	ext := filepath.Ext(name)
	for i := 0; i < 1000; i++ {
		target := name
		if i > 0 {
			target = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), i+1, ext)
		}
		f, e := root.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(e, os.ErrExist) {
			if info, statErr := workspaceInfo(root, target); statErr == nil && info.Mode().IsRegular() && info.Size() == int64(len(data)) {
				if existing, readErr := root.ReadFile(target); readErr == nil && bytes.Equal(existing, data) {
					return target, nil
				}
			}
			continue
		}
		if e != nil {
			return "", errors.New("无法复制文件到工作目录，请检查写入权限")
		}
		_, e = f.Write(data)
		closeErr := f.Close()
		if e != nil || closeErr != nil {
			_ = root.Remove(target)
			return "", errors.New("文件复制失败，请检查磁盘空间")
		}
		return target, nil
	}
	return "", errors.New("同名文件过多，请重命名后添加")
}
