package scriptrun

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"tongxi/internal/skill"
)

const maxOutput = 16 * 1024

type limitedOutput struct {
	data      []byte
	truncated bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	left := maxOutput - len(b.data)
	if n > left {
		b.truncated = true
		p = p[:left]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (b *limitedOutput) String() string {
	s := strings.ToValidUTF8(string(b.data), "�")
	if b.truncated {
		s += "\n[输出已截断]"
	}
	return s
}
func interpreter(path string) (string, []string, error) {
	var candidates, options []string
	switch filepath.Ext(path) {
	case ".py":
		candidates = []string{"/opt/homebrew/bin/python3", "/usr/local/bin/python3", "/Library/Developer/CommandLineTools/usr/bin/python3", "/usr/bin/python3"}
		options = []string{"-E", "-s", "-B"}
	case ".js", ".mjs", ".cjs":
		candidates = []string{"/opt/homebrew/bin/node", "/usr/local/bin/node"}
	case ".sh":
		candidates = []string{"/bin/bash"}
		options = []string{"--noprofile", "--norc"}
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			resolved, e := filepath.EvalSymlinks(p)
			if e == nil {
				return resolved, options, nil
			}
		}
	}
	return "", nil, errors.New("未找到脚本解释器，请安装 Python 3 或 Node.js 后重试；Shell 使用系统 Bash")
}
func sandboxProfile(workspace, stage, tmp, out, program string) string {
	// Process-control remains denied, so child processes cannot leave the execution's process group.
	p := `(version 1)
(deny default)
(allow process-fork process-exec)
(allow signal (target self))
(allow sysctl-read)
(allow file-read-metadata)
(allow file-read* (literal "/") (subpath "/System/Library") (subpath "/usr") (subpath "/bin") (subpath "/Library/Developer") (subpath "/Applications/Xcode.app") (subpath "/private/var/db/dyld") (subpath "/private/preboot") (subpath "/opt/homebrew") (literal "/dev/null") (literal "/dev/random") (literal "/dev/urandom"))
(allow file-write-data (literal "/dev/null"))
`
	for _, v := range []string{workspace, stage, tmp, out, filepath.Dir(filepath.Dir(program))} {
		if v == "/" {
			continue
		}
		p += "(allow file-read* (subpath " + strconv.Quote(v) + "))\n"
	}
	for _, v := range []string{tmp, out} {
		p += "(allow file-write* (subpath " + strconv.Quote(v) + "))\n"
	}
	p += "(deny file-read* (regex " + strconv.Quote("^"+regexp.QuoteMeta(workspace)+"/(.*/)?[.][^/]+(/|$)") + "))\n"
	return p
}
func execute(ctx context.Context, r Request) Result {
	return executeWithTimeout(ctx, r, 30*time.Second)
}
func executeWithTimeout(ctx context.Context, r Request, timeout time.Duration) (result Result) {
	result = Result{Status: "failed", ExitCode: -1, Files: []string{}}
	fail := func(err error) Result { result.Error = err.Error(); return result }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if !filepath.IsLocal(r.ID) || strings.ContainsAny(r.ID, "/\\") || r.ID == "." {
		return fail(errors.New("无效的执行标识"))
	}
	if _, err := skill.Validate(r.Bundle); err != nil {
		return fail(err)
	}
	found := false
	for _, f := range r.Bundle.Scripts {
		found = found || f.Path == r.Path
	}
	if !found {
		return fail(errors.New("当前技能不包含该脚本"))
	}
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		return fail(errors.New("此系统缺少脚本隔离组件，无法执行脚本"))
	}
	program, options, err := interpreter(r.Path)
	if err != nil {
		return fail(err)
	}
	workspace, err := filepath.EvalSymlinks(r.Workspace)
	if err != nil || workspace == "" {
		return fail(errors.New("会话工作目录不可用"))
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return fail(err)
	}
	defer root.Close()
	outputRelative := filepath.Join("成果", "脚本", r.ID)
	if err = root.MkdirAll(filepath.Dir(outputRelative), 0700); err != nil {
		return fail(errors.New("无法创建脚本成果目录"))
	}
	if err = root.Mkdir(outputRelative, 0700); err != nil {
		return fail(errors.New("无法创建本次脚本成果目录"))
	}
	output, err := filepath.EvalSymlinks(filepath.Join(workspace, outputRelative))
	if err != nil {
		return fail(err)
	}
	temp, err := os.MkdirTemp("", "tongxi-script-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(temp)
	temp, err = filepath.EvalSymlinks(temp)
	if err != nil {
		return fail(err)
	}
	stage, tmp := filepath.Join(temp, "skill"), filepath.Join(temp, "tmp")
	for _, dir := range []string{stage, tmp} {
		if err = os.Mkdir(dir, 0700); err != nil {
			return fail(err)
		}
	}
	for _, f := range append(append([]skill.Resource{}, r.Bundle.Resources...), r.Bundle.Scripts...) {
		name := filepath.Join(stage, filepath.FromSlash(f.Path))
		if err = os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return fail(err)
		}
		if err = os.WriteFile(name, []byte(f.Content), 0400); err != nil {
			return fail(err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Only this constant launcher is interpreted as shell. All model arguments remain argv entries.
	args := []string{"-p", sandboxProfile(workspace, stage, tmp, output, program), "/bin/sh", "-c", `ulimit -c 0; ulimit -t 30; ulimit -f 16384; ulimit -n 128; exec "$@"`, "tongxi-script", program}
	args = append(args, options...)
	args = append(args, filepath.Join(stage, filepath.FromSlash(r.Path)))
	args = append(args, r.Args...)
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", args...)
	cmd.Dir = workspace
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin", "HOME=" + tmp, "TMPDIR=" + tmp, "LANG=en_US.UTF-8", "TONGXI_OUTPUT_DIR=" + output, "TONGXI_SKILL_DIR=" + stage, "PYTHONDONTWRITEBYTECODE=1"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	killGroup := func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Cancel = killGroup
	cmd.WaitDelay = 500 * time.Millisecond
	var stdout, stderr limitedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	_ = killGroup()
	result.Stdout, result.Stderr = stdout.String(), stderr.String()
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		result.Status = "timed_out"
		result.Error = "脚本超过运行时限，已停止"
	case ctx.Err() != nil:
		result.Status = "cancelled"
		result.Error = "脚本已停止"
	case err != nil:
		result.Error = fmt.Sprintf("脚本执行失败（退出码%d）", result.ExitCode)
	default:
		result.Status = "completed"
	}
	outRoot, e := root.OpenRoot(outputRelative)
	if e == nil {
		defer outRoot.Close()
		_ = fs.WalkDir(outRoot.FS(), ".", func(path string, d fs.DirEntry, e error) error {
			if e != nil {
				return fs.SkipDir
			}
			if len(result.Files) >= 32 {
				return fs.SkipAll
			}
			if d.Type().IsRegular() {
				result.Files = append(result.Files, filepath.ToSlash(filepath.Join(outputRelative, path)))
			}
			return nil
		})
	}
	return result
}
