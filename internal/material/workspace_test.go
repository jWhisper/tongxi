package material

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceContainmentListingAndCopy(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"资料", ".hidden", "node_modules"} {
		if err := os.Mkdir(filepath.Join(dir, folder), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"资料/预算.csv", ".env.txt", ".hidden/password.txt", "node_modules/file.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("项目,金额\n茶点,96"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(dir, "外部")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("资料", filepath.Join(dir, "内部链接")); err != nil {
		t.Fatal(err)
	}
	root, err := OpenWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	p, err := ListWorkspace(context.Background(), root, ".", 0)
	if err != nil || len(p.Files) != 1 || p.Files[0].Name != "资料" {
		t.Fatal(p, err)
	}
	for _, bad := range []string{"../secret.txt", "资料/../../secret.txt", "/etc/passwd", ".env.txt", ".hidden/password.txt", "node_modules/file.txt"} {
		if _, e := WorkspacePath(bad); e == nil {
			t.Fatal("allowed path", bad)
		}
	}
	for _, bad := range []string{"外部/secret.txt", "内部链接/预算.csv"} {
		if _, _, e := ReadWorkspace(context.Background(), root, bad); e == nil {
			t.Fatal("followed symlink", bad)
		}
	}
	_, doc, err := ReadWorkspace(context.Background(), root, "资料/预算.csv")
	if err != nil || !strings.Contains(fmt.Sprint(doc.Segments), "96") {
		t.Fatal(doc, err)
	}
	if _, err = ListWorkspace(context.Background(), root, "资料", -1); err == nil {
		t.Fatal("negative page")
	}
	if _, _, err = ReadWorkspace(context.Background(), root, "资料"); err == nil {
		t.Fatal("read directory")
	}
	for i := 0; i < 102; i++ {
		if err = os.WriteFile(filepath.Join(dir, "资料", fmt.Sprintf("%03d.txt", i)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err = ListWorkspace(context.Background(), root, "资料", 0)
	if err != nil || len(p.Files) != 100 || p.Next != 100 {
		t.Fatal(p, err)
	}
	second, err := ListWorkspace(context.Background(), root, "资料", p.Next)
	if err != nil || len(second.Files) != 3 || second.Next != 0 {
		t.Fatal(second, err)
	}
	for i := 0; i < 2; i++ {
		name, err := CopyToWorkspace(dir, filepath.Join(outside, "secret.txt"), []byte("selected"))
		if err != nil || name != "secret.txt" {
			t.Fatal(name, err)
		}
	}
	if name, err := CopyToWorkspace(dir, filepath.Join(outside, "secret.txt"), []byte("changed")); err != nil || name != "secret (2).txt" {
		t.Fatal(name, err)
	}
	name, err := CopyToWorkspace(dir, filepath.Join(dir, "资料", "预算.csv"), []byte("unused"))
	if err != nil || name != "资料/预算.csv" {
		t.Fatal(name, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, name))
	if !strings.Contains(string(data), "96") {
		t.Fatal("overwrote original")
	}
}
