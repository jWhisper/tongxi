//go:build !darwin

package scriptrun

import "context"

func execute(context.Context, Request) Result {
	return Result{Status: "failed", ExitCode: -1, Error: "脚本隔离执行当前仅支持 macOS", Files: []string{}}
}
