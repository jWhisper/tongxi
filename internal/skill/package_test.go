package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const example = "---\nname: budget-check\ndescription: 核对活动预算\n---\n请按 references/check.md 的检查项核对。"

func TestValidateSkillAndResourceBoundaries(t *testing.T) {
	if _, err := Validate(Bundle{Content: example}); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"# no frontmatter", strings.Replace(example, "budget-check", "bad/name", 1), strings.Replace(example, "核对活动预算", "", 1), "---\nname: budget-check\ndescription: test\n---\n", example + strings.Repeat("x", MaxContent)} {
		if _, err := Parse(content); err == nil {
			t.Fatalf("accepted invalid skill %q", content[:min(len(content), 90)])
		}
	}
	for _, name := range []string{"../secret.md", "/secret.md", ".env", "references/../secret.md", "scripts/run.py", "a\\b.txt", "file.pdf"} {
		if _, err := Validate(Bundle{Content: example, Resources: []Resource{{Path: name, Content: "data"}}}); err == nil {
			t.Fatal("invalid resource", name)
		}
	}
	if _, err := Validate(Bundle{Content: example, Resources: []Resource{{Path: "a.md", Content: "x"}, {Path: "a.md", Content: "y"}}}); err == nil {
		t.Fatal("duplicate resource")
	}
}

func TestImportCopiesOnlyBoundedTextWithoutFollowingLinks(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"references", "scripts"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{"SKILL.md": example, "references/check.md": "核对合计与余额", "scripts/run.sh": "exit 1", ".secret.txt": "hidden"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "references/check.md"), filepath.Join(dir, "linked.md")); err != nil {
		t.Fatal(err)
	}
	p, err := Import(dir)
	if err != nil || len(p.Resources) != 1 || len(p.Scripts) != 1 || p.Scripts[0].Path != "scripts/run.sh" || p.Resources[0].Content != "核对合计与余额" || p.Note == "" {
		t.Fatal(p, err)
	}
	if err = os.WriteFile(filepath.Join(dir, "references/check.md"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if p.Resources[0].Content != "核对合计与余额" {
		t.Fatal("import did not retain content")
	}
	if err = os.Remove(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(dir, "references/check.md"), filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err = Import(dir); err == nil {
		t.Fatal("symlink skill allowed")
	}
}

func TestScriptPathsAreConfinedToSupportedTextFiles(t *testing.T) {
	for _, p := range []string{"scripts/a.py", "scripts/a.js", "scripts/a.mjs", "scripts/a.cjs", "scripts/a.sh"} {
		if _, err := Validate(Bundle{Content: example, Scripts: []Resource{{Path: p, Content: "test"}}}); err != nil {
			t.Fatal(p, err)
		}
	}
	for _, p := range []string{"../scripts/a.sh", "/scripts/a.py", "scripts/../a.py", "scripts/.env.py", "scripts/a.exe", "run.py", "scripts/node_modules/a.js"} {
		if _, err := Validate(Bundle{Content: example, Scripts: []Resource{{Path: p, Content: "test"}}}); err == nil {
			t.Fatal(p)
		}
	}
}
