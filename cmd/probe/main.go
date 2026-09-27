// probe verifies Eino without opening a desktop window. Default mode is offline.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"tongxi/internal/agent"
)

func main() {
	live := flag.Bool("live", false, "use TONGXI_BASE_URL, TONGXI_MODEL and TONGXI_API_KEY")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var cm model.ToolCallingChatModel = &agent.LocalModel{}
	secret := ""
	if *live {
		base, name := os.Getenv("TONGXI_BASE_URL"), os.Getenv("TONGXI_MODEL")
		secret = os.Getenv("TONGXI_API_KEY")
		if base == "" || name == "" || secret == "" {
			fmt.Fprintln(os.Stderr, "请通过环境变量配置 Base URL、模型和 API Key；不要把密钥写入源码。")
			os.Exit(1)
		}
		var err error
		cm, err = agent.NewModel(ctx, base, name, secret)
		if err != nil {
			fmt.Fprintln(os.Stderr, "模型初始化失败")
			os.Exit(1)
		}
	} else {
		fmt.Println("本地固定测试：不调用真实模型。")
	}
	toolCount := 0
	err := agent.Run(ctx, cm, agent.LocalPrompt, func(u agent.Update) {
		if u.Tool != "" {
			toolCount++
			fmt.Printf("\n[工具] %s\n", u.Tool)
		}
		fmt.Print(u.Text)
	})
	fmt.Println()
	if err != nil {
		msg := err.Error()
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, "[已隐藏]")
		}
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(1)
	}
	if toolCount == 0 {
		fmt.Fprintln(os.Stderr, "未实际调用工具，本次工具验证不通过。")
		os.Exit(1)
	}
}
