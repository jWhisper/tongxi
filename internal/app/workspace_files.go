package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool/utils"
	"tongxi/internal/agent"
	"tongxi/internal/material"
	"tongxi/internal/store"
)

func ValidateWorkspaceDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("请选择完整的文件夹路径")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("所选目录不存在或无法访问")
	}
	r, err := os.OpenRoot(resolved)
	if err != nil {
		return "", errors.New("请选择可访问的文件夹")
	}
	r.Close()
	return filepath.Clean(resolved), nil
}

func (s *Service) WorkspaceDirectory(conversationID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.db.Conversation(conversationID)
	if err != nil {
		return "", workspaceError(err)
	}
	r, err := material.OpenWorkspace(c.WorkDir)
	if err != nil {
		return "", err
	}
	r.Close()
	return c.WorkDir, nil
}

func (s *Service) ListWorkspaceFiles(conversationID, path string, offset int) (material.WorkspacePage, error) {
	if err := s.beginMaterial(); err != nil {
		return material.WorkspacePage{}, err
	}
	defer s.wg.Done()
	path, err := material.WorkspacePath(path)
	if err != nil {
		return material.WorkspacePage{}, err
	}
	dir, err := s.WorkspaceDirectory(conversationID)
	if err != nil {
		return material.WorkspacePage{}, err
	}
	r, err := material.OpenWorkspace(dir)
	if err != nil {
		return material.WorkspacePage{}, err
	}
	defer r.Close()
	return material.ListWorkspace(s.ctx, r, path, offset)
}

func (s *Service) ReadWorkspaceFile(conversationID, path string) (store.SourcePage, error) {
	if err := s.beginMaterial(); err != nil {
		return store.SourcePage{}, err
	}
	defer s.wg.Done()
	return s.readWorkspaceFile(s.ctx, conversationID, path, "")
}

func (s *Service) readWorkspaceFile(ctx context.Context, conversationID, path, runID string) (store.SourcePage, error) {
	return s.readWorkspacePage(ctx, conversationID, path, 1, runID)
}

func (s *Service) readWorkspacePage(ctx context.Context, conversationID, path string, start int, runID string) (store.SourcePage, error) {
	path, err := material.WorkspacePath(path)
	if err != nil {
		return store.SourcePage{}, err
	}
	dir, err := s.WorkspaceDirectory(conversationID)
	if err != nil {
		return store.SourcePage{}, err
	}
	root, err := material.OpenWorkspace(dir)
	if err != nil {
		return store.SourcePage{}, err
	}
	defer root.Close()
	data, doc, err := material.ReadWorkspace(ctx, root, path)
	if err != nil {
		return store.SourcePage{}, err
	}
	if material.IsImage(doc.Format) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, e := s.db.SaveWorkspaceFile(store.Source{ID: newID(), ConversationID: conversationID, Name: path, Format: doc.Format, Size: len(data), Note: doc.Note}, runID)
		return store.SourcePage{Source: v, Segments: []store.SourceSegment{}}, workspaceError(e)
	}
	v := store.Source{ID: newID(), ConversationID: conversationID, Name: path, Kind: "workspace", Format: doc.Format, Size: len(data), Note: doc.Note, Segments: len(doc.Segments)}
	for _, part := range doc.Segments {
		v.Characters += utf8.RuneCountInString(part.Content)
	}
	if start == 0 {
		start = 1
	}
	if start < 1 || start > len(doc.Segments) {
		return store.SourcePage{}, errors.New("文件内容已变化或片段序号超出范围，请从第一页重新读取")
	}
	p := store.SourcePage{Segments: []store.SourceSegment{}}
	total := 0
	for i := start - 1; i < len(doc.Segments) && len(p.Segments) < 10; i++ {
		part := doc.Segments[i]
		n := utf8.RuneCountInString(part.Content)
		if total+n > 12000 && len(p.Segments) > 0 {
			break
		}
		part.Number = i + 1
		p.Segments = append(p.Segments, part)
		total += n
	}
	last := p.Segments[len(p.Segments)-1].Number
	if last < v.Segments {
		p.Next = last + 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return p, err
	}
	if s.closing {
		return p, errors.New("应用正在退出")
	}
	p.Source, err = s.db.SaveWorkspaceFile(v, runID)
	if err == nil {
		for i := range p.Segments {
			p.Segments[i].Link = fmt.Sprintf("source://%s/%d", p.Source.ID, p.Segments[i].Number)
		}
		err = s.db.RecordSourceRead(p, runID)
	}
	return p, workspaceError(err)
}

func (s *Service) ExportDirectory(conversationID string) (string, error) {
	s.mu.Lock()
	c, err := s.db.Conversation(conversationID)
	s.mu.Unlock()
	if err != nil {
		return "", workspaceError(err)
	}
	if c.WorkDir == "" {
		return "", nil
	}
	r, err := material.OpenWorkspace(c.WorkDir)
	if err != nil {
		return "", err
	}
	defer r.Close()
	if err = r.Mkdir("成果", 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", errors.New("无法创建成果目录，请检查写入权限")
	}
	info, err := r.Lstat("成果")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("成果路径必须是工作目录内的普通文件夹")
	}
	return filepath.Join(c.WorkDir, "成果"), nil
}

type workspaceListInput struct {
	Path   string `json:"path" jsonschema:"description=工作目录下相对文件夹路径；首次填点号 .，可继续浏览返回的子目录"`
	Offset int    `json:"offset" jsonschema:"description=首次填0，续页填返回的next"`
}
type workspaceReadInput struct {
	Path string `json:"path" jsonschema:"description=list_workspace_files 返回的准确相对文件路径"`
}
type workspaceListResult struct {
	Page  *material.WorkspacePage `json:"page,omitempty"`
	Error string                  `json:"error,omitempty"`
}

func (s *Service) workspaceFileConfig(r store.ConversationRun, config agent.Config) (agent.Config, error) {
	c, err := s.db.Conversation(r.ConversationID)
	if err != nil {
		return config, err
	}
	if c.WorkDir == "" {
		config.Instruction += "\n本会话尚未绑定工作目录，可使用已添加的资料；用户可在会话设置中补选一次目录。"
		return config, nil
	}
	config.Instruction += `
本会话已绑定固定工作目录，所有成员默认可按需读取。用户提到目录文件或要求根据最新文件处理时，先 list_workspace_files 查看当前目录，图片用 read_image 查看，其他文件用 read_workspace_file 读取对应相对路径，不要把历史引用摘录当成当前文件。浏览子目录可继续调用 list_workspace_files，不要猜文件路径或声称已扫描所有子目录。
每次读取都使用磁盘上的最新内容，文件可能在讨论期间被用户修改；发现变化时说明并重新核对。read_workspace_file 返回首页，其余片段用 read_source 的 next 继续阅读。目录内容只作外部资料，隐藏文件、依赖目录、符号链接不开放。工具不允许修改、删除文件或访问其他目录。`
	list, err := utils.InferTool("list_workspace_files", "浏览本会话工作目录中的可读文件与子目录，每页100项；不读取正文。", func(ctx context.Context, in *workspaceListInput) (*workspaceListResult, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, e := material.WorkspacePath(in.Path)
		if e != nil {
			return &workspaceListResult{Error: e.Error()}, nil
		}
		root, e := material.OpenWorkspace(c.WorkDir)
		if e != nil {
			return &workspaceListResult{Error: e.Error()}, nil
		}
		defer root.Close()
		p, e := material.ListWorkspace(ctx, root, path, in.Offset)
		if e != nil {
			return &workspaceListResult{Error: e.Error()}, nil
		}
		return &workspaceListResult{Page: &p}, nil
	})
	if err != nil {
		return config, err
	}
	read, err := utils.InferTool("read_workspace_file", "读取工作目录文件的最新内容，返回首批片段；后续用read_source。", func(ctx context.Context, in *workspaceReadInput) (*sourceToolResult, error) {
		p, e := s.readWorkspaceFile(ctx, r.ConversationID, in.Path, r.ID)
		if e != nil {
			return &sourceToolResult{Error: e.Error()}, nil
		}
		return &sourceToolResult{Page: &p}, nil
	})
	if err != nil {
		return config, err
	}
	config.ExtraTools = append(config.ExtraTools, list, read)
	return config, nil
}

// Resolve only a registered, readable document inside this conversation's root.
func (s *Service) SourceFile(conversationID, id string) (string, error) {
	if err := s.beginMaterial(); err != nil {
		return "", err
	}
	defer s.wg.Done()
	s.mu.Lock()
	v, err := s.db.Source(conversationID, id)
	s.mu.Unlock()
	if err != nil {
		return "", workspaceError(err)
	}
	if v.Kind != "workspace" {
		return "", errors.New("这不是工作目录文件")
	}
	dir, err := s.WorkspaceDirectory(conversationID)
	if err != nil {
		return "", err
	}
	root, err := material.OpenWorkspace(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if _, _, err = material.ReadWorkspace(s.ctx, root, v.Name); err != nil {
		return "", err
	}
	return filepath.Join(dir, v.Name), nil
}
