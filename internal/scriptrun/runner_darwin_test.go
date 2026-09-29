package scriptrun

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"tongxi/internal/skill"
)

func request(t *testing.T, path, content string) Request {
	t.Helper()
	return Request{ID: "test", Path: path, Workspace: t.TempDir(), Bundle: skill.Bundle{Content: "---\nname: script-check\ndescription: test\n---\nRun script", Scripts: []skill.Resource{{Path: path, Content: content}}}}
}
func TestInterpretersProduceRealFilesAndPassLiteralArguments(t *testing.T) {
	code := map[string]string{
		"scripts/test.py": "import os,sys\nfrom pathlib import Path\nprint(sys.argv[1])\nPath(os.environ['TONGXI_OUTPUT_DIR'],'result.txt').write_text('python')\n",
		"scripts/test.js": "const fs=require('fs'); console.log(process.argv[2]); fs.writeFileSync(process.env.TONGXI_OUTPUT_DIR+'/result.txt','node');",
		"scripts/test.sh": "printf '%s\\n' \"$1\"\nprintf shell > \"$TONGXI_OUTPUT_DIR/result.txt\"\n",
	}
	for path, body := range code {
		t.Run(path, func(t *testing.T) {
			if _, _, err := interpreter(path); err != nil {
				t.Skip(err)
			}
			r := request(t, path, body)
			r.Args = []string{"$(touch injected); echo unsafe"}
			got := Execute(context.Background(), r)
			if got.Status != "completed" || got.ExitCode != 0 || !strings.Contains(got.Stdout, r.Args[0]) || len(got.Files) != 1 {
				t.Fatal(got)
			}
			if _, err := os.Stat(filepath.Join(r.Workspace, "injected")); !os.IsNotExist(err) {
				t.Fatal("argument interpreted by shell")
			}
			if _, err := os.ReadFile(filepath.Join(r.Workspace, got.Files[0])); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestSandboxDeniesOutsideFilesInputWritesNetworkAndNewSessions(t *testing.T) {
	r := request(t, "scripts/check.py", `import os,sys,socket
from pathlib import Path
blocked=[]
for name, action in [
 ('outside',lambda:Path(sys.argv[1]).read_text()),
 ('input-write',lambda:Path('input.txt').write_text('changed')),
 ('hidden',lambda:Path('.secret').read_text()),
 ('symlink',lambda:Path('linked.txt').read_text()),
 ('outside-write',lambda:Path(sys.argv[1]).write_text('changed')),
 ('network',lambda:socket.socket().connect(('127.0.0.1',9))),
 ('session',lambda:os.setsid())]:
 try: action()
 except PermissionError: blocked.append(name)
print(','.join(blocked))
print('env-clean',os.getenv('TONGXI_TEST_SECRET') is None)
`)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("secret"), 0600)
	os.WriteFile(filepath.Join(r.Workspace, "input.txt"), []byte("original"), 0600)
	os.WriteFile(filepath.Join(r.Workspace, ".secret"), []byte("hidden"), 0600)
	os.Symlink(outside, filepath.Join(r.Workspace, "linked.txt"))
	r.Args = []string{outside}
	t.Setenv("TONGXI_TEST_SECRET", "must-not-leak")
	got := Execute(context.Background(), r)
	if got.Status != "completed" || !strings.Contains(got.Stdout, "outside,input-write,hidden,symlink,outside-write,network,session") || !strings.Contains(got.Stdout, "env-clean True") {
		t.Fatal(got)
	}
}
func TestCancellationAndTimeoutKillShellChildren(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelled), func(t *testing.T) {
			r := request(t, "scripts/wait.sh", "sleep 20 &\nprintf '%s' \"$!\" > \"$TONGXI_OUTPUT_DIR/child.txt\"\nwait\n")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelled {
				time.AfterFunc(300*time.Millisecond, cancel)
			}
			got := executeWithTimeout(ctx, r, 700*time.Millisecond)
			want := "timed_out"
			if cancelled {
				want = "cancelled"
			}
			if got.Status != want {
				t.Fatal(got)
			}
			b, err := os.ReadFile(filepath.Join(r.Workspace, "成果", "脚本", r.ID, "child.txt"))
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(string(b))
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				if syscall.Kill(pid, 0) != nil {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("child survived stop", pid)
		})
	}
}
func TestOutputIsBoundedAndFailureIsReported(t *testing.T) {
	r := request(t, "scripts/output.sh", "printf '%s\\n' problem >&2\nyes x | head -n 20000\nexit 7\n")
	got := Execute(context.Background(), r)
	if got.Status != "failed" || got.ExitCode != 7 || got.Stderr != "problem\n" || len(got.Stdout) > maxOutput+100 || !strings.Contains(got.Stdout, "输出已截断") {
		t.Fatal(got.Status, got.ExitCode, len(got.Stdout), got.Stderr)
	}
}
func TestOutputDirectoryCannotEscapeWorkspace(t *testing.T) {
	r := request(t, "scripts/test.sh", "echo hi")
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(r.Workspace, "成果"))
	got := Execute(context.Background(), r)
	if got.Status != "failed" {
		t.Fatal(got)
	}
	files, _ := os.ReadDir(outside)
	if len(files) != 0 {
		t.Fatal("wrote outside workspace")
	}
}
