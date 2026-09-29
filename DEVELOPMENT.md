# 本地开发

技术栈：Go、Eino ADK、Wails v2、React / TypeScript、SQLite。

当前主要验证 macOS Apple Silicon。依赖版本由 `go.mod`、`go.sum`、`.node-version` 和前端锁文件记录。需要安装 Go、Node.js 和 Xcode Command Line Tools。

## 启动

在仓库根目录执行：

```sh
make setup
make dev
```

`make setup` 将固定版本的 Wails CLI 安装到项目 `.tools/`。`make dev` 默认使用 `.local/dev` 的独立数据目录，可用 `TONGXI_DATA_DIR` 覆盖；开发模型配置需单独填写。仅运行前端 Vite 不具备 Go 绑定。

## 检查与打包

```sh
make test
go test -race ./internal/...
go vet ./...
make build
make release
```

构建应用位于 `build/bin/tongxi.app`。`make release` 生成对应版本的 macOS arm64 DMG 与 SHA256，包含使用说明和第三方许可证，不包含本地数据库或密钥。安装边界见 [INSTALL.md](INSTALL.md)。

`make probe` 运行离线工具调用验证。系统凭据存储集成测试可显式运行：

```sh
TONGXI_TEST_KEYRING=1 go test ./internal/app -run TestSystemVaultRoundTrip -v
```

## 开发数据

项目尚未上线，按 [AGENTS.md](AGENTS.md) 直接调整当前结构，不新增旧数据兼容链。重建数据库前先备份应用数据目录；用户工作目录中的原始文件保留。

业务和接口位于 `internal/app`，模型执行位于 `internal/agent`，SQLite 读写位于 `internal/store`，前端位于 `frontend/src`。详细设计与验收见 [plan](plan/README.md)。
