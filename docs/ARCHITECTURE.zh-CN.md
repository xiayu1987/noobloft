[English](ARCHITECTURE.md) | 简体中文

# 项目目录与职责

本项目是一个 Go 节点应用及其内嵌 Vue 控制台。`internal` 保留 Go 的内部包导入保护；下一级目录按职责归组，不作为额外的 Go 包，也不代表严格单向分层。

```text
cmd/noobloftd/             可执行程序、CLI、运行编排及平台托盘入口
internal/
  network/
    node/                  libp2p 主机、连接、发现基础设施和网络准入
    discovery/             模型服务发现、公告及续签
  serving/
    backend/               Ollama / OpenAI-compatible 模型后端适配器
    capability/            模型能力声明与验证
    inference/             节点间推理协议、客户端、服务端和授权检查
    router/                本地/远端模型选择、请求路由和抽检
    reputation/            服务质量记账、评分与持久化
  security/
    identity/              节点密钥、PeerID 和私网密钥
    invitation/            私网签名邀请及验证
    publisher/             发布者身份、签名授权文档、签发历史和吊销
  controlplane/
    gateway/               共用 HTTP 服务入口：管理 API、OpenAI API 及鉴权
    management/            CLI 与 HTTP 复用的发布者文档应用操作
    webui/                 控制台静态文件嵌入与 HTTP 安全响应头
      dist/                前端构建产物，不入库
  config/                  配置校验、默认值、保存、恢复和回退记录
  audit/                   不含对话正文的审计日志
  localization/            Go 语言选择、翻译回退和请求语言解析
locales/                   前后端共享语义 key 词库，唯一事实源
  embed.go                 Go 嵌入桥接，不承载语言选择业务
web/
  src/
    App.vue                控制台外壳与页面组合
    main.ts                Vue 启动入口
    api/client.ts          HTTP 请求与流式调用
    components/            跨功能通用组件
    features/
      authorization/       授权状态与服务导出
      issuer/              签发与吊销
      resources/           资源、重启、恢复与使用说明
      settings/            配置表单
    i18n/                  前端语言适配及偏好保存
    styles/                全局样式
  tests/                   浏览器端到端测试
scripts/                   成对的 Windows/Linux 构建、启动和停止入口
docs/                      操作、架构、部署和合规文档
bin/                       本机构建产物，不入库
testrun/                   本地运行数据，不入库
```

## 依赖与边界

- `cmd/noobloftd` 负责装配、命令分派和进程生命周期。保留同一个 main 包，兼容现有 CLI 与 Windows 托盘构建参数。
- HTTP 网关目前同时服务管理和推理 API，仍复用现有监听、鉴权和测试。目录归组不意味着两类 API 已拆成独立服务。
- `management` 是应用操作；`publisher` 是签名文档及发布者持久化。不要把 HTTP 请求类型下沉到签名文档包。
- 网络发现需要能力声明，路由需要发现服务；这些是真实依赖，不应为了目录分组引入复制类型或转发包。
- 前端功能目录使用公共 API 客户端和 i18n 适配器。通用组件放 `components`，专属组件跟随功能。
- 单元测试与 Go 包同目录。浏览器测试位于 `web/tests`，由网关测试启动真实 HTTP API。
- `locales/*.json` 同时供前端导入和 Go 嵌入；不复制词库，不维护第二份生成后的翻译源文件。

## 构建与验证

继续使用 `scripts/build.ps1` 或 `scripts/build.sh`。前端构建输出到 `internal/controlplane/webui/dist`，随后 Go 将它嵌入可执行文件。干净检出必须先构建前端。

```sh
cd web
npm ci
npm run build
cd ..
go test ./...
go vet ./...
```

浏览器回归：Windows PowerShell 设置 `$env:NOOBLOFT_BROWSER_TEST='1'` 后执行 `go test ./internal/controlplane/gateway -run TestManagementBrowser -count=1 -v`；Linux 使用 `NOOBLOFT_BROWSER_TEST=1 go test ./internal/controlplane/gateway -run TestManagementBrowser -count=1 -v`。需要事先安装 Playwright Chromium。

## 源码与运行数据

目录整理只涉及源码位置。节点数据仍由 `-dir` 决定，默认目录、配置格式、PeerID、凭证文件、授权和备份路径没有迁移。`bin`、`testrun` 与前端生成产物不是业务源码，不要加入版本控制。启动、停止脚本和 CLI 命令的现有入口保持兼容。
