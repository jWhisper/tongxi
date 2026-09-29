package app

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"tongxi/internal/material"
	"tongxi/internal/store"
)

type ExportFile struct {
	Name string
	Data []byte
}

var inlineSourceLink = regexp.MustCompile(`\[([^\]\n]*)\]\((source://[a-f0-9]{32}/[1-9][0-9]*)\)`)

func (s *Service) ExportVersion(conversationID, versionID, format string) (ExportFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if format != "md" && format != "docx" {
		return ExportFile{}, errors.New("请选择 Markdown 或 Word 格式")
	}
	versions, err := s.db.WorkVersions(conversationID)
	if err != nil {
		return ExportFile{}, workspaceError(err)
	}
	var version *store.WorkVersion
	for i := range versions {
		if versions[i].ID == versionID {
			version = &versions[i]
			break
		}
	}
	if version == nil {
		return ExportFile{}, errors.New("本会话中不存在这个正式成果版本")
	}
	tasks, err := s.db.WorkTasks(conversationID)
	if err != nil {
		return ExportFile{}, workspaceError(err)
	}
	title := "成果"
	for _, t := range tasks {
		if t.ID == version.TaskID {
			title = t.Title
			break
		}
	}
	refs := map[string]int{}
	var appendix strings.Builder
	if len(version.Step.Citations) > 0 {
		appendix.WriteString("\n\n## 资料来源\n\n以下为该版本引用时的原文摘录；引用存在不等于结论已被独立核实。\n")
	}
	for i, c := range version.Step.Citations {
		refs[fmt.Sprintf("source://%s/%d", c.SourceID, c.Segment)] = i + 1
		fmt.Fprintf(&appendix, "\n<a id=\"source-%d\"></a>\n\n### 来源 %d\n\n%s · %s\n\n", i+1, i+1, plainMarkdown(c.Name), plainMarkdown(c.Location))
		if source, e := s.db.Source(conversationID, c.SourceID); e == nil && source.URL != "" {
			fmt.Fprintf(&appendix, "原网页：%s\n\n", source.URL)
		}
		appendix.WriteString("> " + strings.ReplaceAll(plainMarkdown(c.Quote), "\n", "\n> ") + "\n")
	}
	if len(version.Step.Files) > 0 {
		appendix.WriteString("\n\n## 随本版交付的文件\n\n文件快照可在同席中打开本成果版本查看。\n")
		for _, file := range version.Step.Files {
			a, e := s.db.Artifact(conversationID, file.ArtifactID)
			if e != nil {
				return ExportFile{}, workspaceError(e)
			}
			fmt.Fprintf(&appendix, "\n- %s（%s，%d字节）：%s\n", plainMarkdown(a.Name), a.Format, a.Size, plainMarkdown(file.Evidence))
		}
	}
	body := inlineSourceLink.ReplaceAllStringFunc(version.Step.Result, func(link string) string {
		parts := inlineSourceLink.FindStringSubmatch(link)
		if number, ok := refs[parts[2]]; ok {
			return fmt.Sprintf("[%s · %d](#source-%d)", parts[1], number, number)
		}
		return link
	})
	markdown := fmt.Sprintf("# %s · V%d\n\n交付时间：%s\n\n", plainMarkdown(title), version.Number, version.CreatedAt) + body + appendix.String() + "\n"
	data := []byte(markdown)
	if format == "docx" {
		data, err = material.DOCX(markdown)
		if err != nil {
			return ExportFile{}, errors.New("Word 文档生成失败")
		}
	}
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '-'
		}
		return r
	}, title)
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	return ExportFile{Name: fmt.Sprintf("%s-V%d.%s", name, version.Number, format), Data: data}, nil
}
func plainMarkdown(s string) string {
	return strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "<", "&lt;", ">", "&gt;", "#", "\\#", "`", "\\`").Replace(s)
}
