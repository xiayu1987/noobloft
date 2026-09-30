[English](README.md) | 简体中文

# noobloft

P2P 大模型发现、穿透与转发节点。支持 PSK 私网，以及共享传输底座上的发布者微网络：
发布者独立签发服务委托和消费凭证，一个消费者可以订阅多个发布者。

**定位：仅自用 / 内部组网。** 不运营公共 bootstrap，不运营公共中继，默认不转发到
闭源商业 API。这些不是暂未实现，而是刻意的设计约束，理由见 `docs/COMPLIANCE.zh-CN.md`。

## 快速开始

```bash
# 构建前端（Node.js 20.19+ / 22.12+）并嵌入 Go 可执行文件
./scripts/build.sh             # Linux / macOS
# powershell -File scripts/build.ps1  # Windows

# 第一个节点：新建内部网络
bin/noobloftd init -dir ~/.noobloft
bin/noobloftd run  -dir ~/.noobloft

# 其他节点：把上面生成的 swarm.key 安全复制过来后加入
bin/noobloftd init -dir ~/.noobloft -join /path/to/swarm.key
bin/noobloftd run  -dir ~/.noobloft
```

`internal/controlplane/webui/dist/` 是生成目录，不纳入版本控制。不要绕过构建脚本直接构建全新
检出的仓库；缺少前端产物时，Go 的 `embed` 会按预期拒绝编译，避免产出没有管理控制台的二进制。

以上为默认 `network.mode=private`，swarm.key 是传输层入网凭证，必须安全交付。
显式 `shared` 模式不生成或加载 PSK；模型服务必须安装发布者签名委托并验证消费者凭证。
连接成功不等于有权调用模型。操作见 [发布者微网络](docs/PUBLISHERS.zh-CN.md)。

子命令：`init` / `run` / `info` / `peers` / `models` / `invite export` / `invite import` / `publisher`。

跨局域网部署见 [公网私有组网与签名邀请](docs/WAN.zh-CN.md)。支持在自己的公网 VPS 上部署
私有引导和中继节点，通过 `.noobloft` 签名文件导入锚点；密钥单独安全传递。
该文档针对 private 模式；shared 使用发布者文档，不使用旧 PSK 邀请。

其中 `peers` 和 `models` 是查询工具，必须在 `run` 之后使用：它们通过本机网关的
管理端点询问正在运行的守护进程，而不是另起一个临时节点。这是刻意的设计——
临时节点既看不到守护进程已建立的连接，又会与其争抢监听端口，结果没有意义。
因此守护进程是 peer 视图的唯一事实源。

## 可视化管理

启动 `run` 后打开网关地址（默认 `http://127.0.0.1:8760/`），使用节点目录中的
`admin.token` 登录 Vue 3 + Element Plus 控制台。该凭证与推理调用 token 独立。
登录后下发 7 天有效的 HttpOnly 会话 cookie，刷新页面和重启节点都无需重新登录。
支持节点概览、多发布者订阅与凭证导入、发布服务安装、签名吊销快照更新、
模型流式调试、连接与信誉查看、节点配置编辑。表单统一为右侧抽屉。

订阅和配置保存后需手动重启，页面会显示待重启状态；运行中服务的吊销快照可热更新。
完整操作、构建和测试见 [管理控制台](docs/CONSOLE.zh-CN.md)。

“我的发布服务 → 发布者签发管理”支持直接签发服务委托、消费凭证、查看记录和吊销。
启用后由本机后端保管独立发布者身份（不复用节点身份、不自动接管旧离线身份），
自动维护作废名单并应用到本机同发布者服务。其他节点目前仍需导入导出的签名名单；
本机成功不代表全网已生效。请备份节点目录下的 `publisher-authority`。

## 一键启动与后台运行

停止节点（可重复执行，已停止时成功返回；保留配置、身份和日志）：

```powershell
.\scripts\stop.ps1                       # 默认 %USERPROFILE%\.noobloft
.\scripts\stop.ps1 --dir D:\node1        # 与 start.ps1 使用同一个节点目录
```

```sh
sudo sh scripts/stop.sh                  # 停止 noobloft.service
```

Windows 脚本默认请求托盘正常退出，无需点击确认；超时返回错误。
旧版托盘不支持此请求时，请先用托盘菜单退出，再运行 `start.ps1` 构建并启动新版。
同一个 `stop.ps1` 默认也停止本项目 `bin\noobloftd.exe` 中显式使用绝对路径 `run -dir` 且目录匹配的命令行节点，无需额外开关。当前命令行节点没有脚本正常退出接口，因此脚本会提示并终止进程，会中断正在处理的请求，无法保证内存数据落盘；需要正常退出时可先在原窗口按 Ctrl+C。不删除配置、身份或日志。其他程序、其他目录以及无法可靠识别的节点均不会被终止。
Linux 执行 `sudo sh scripts/stop.sh`，自定义目录加 `--dir /srv/noobloft`（与启动时一致；默认调用者家目录下 `.noobloft`）。同目录的 systemd 服务和手工命令行节点都会停止，无需 Force；命令行节点先收到 SIGTERM，30 秒后仍未退出则 SIGKILL。需要 Python 3.9+ 和 Linux 5.3+ 的 pidfd 支持，以避免 PID 复用误杀；仅管理本项目构建路径或安装路径的节点，不停止其他目录。停止后不会因 `Restart=on-failure` 自动重启，但保留开机自启；若还需取消开机自启，执行 `sudo systemctl disable noobloft`。

`scripts/start.ps1`（Windows）和 `scripts/start.sh`（Linux）把「构建 → 初始化 → 后台拉起」合成一步，
两种系统都是可反复执行的，不需要另跑初始化脚本：

```powershell
.\scripts\start.ps1                                                  # 默认节点目录 %USERPROFILE%\.noobloft
.\scripts\start.ps1 --dir D:\node1 -- -profile wan -join D:\swarm.key   # 指定目录，`--` 之后透传给首次 init
```

```sh
scripts/start.sh                                                     # 默认节点目录 ~/.noobloft
scripts/start.sh --dir /srv/noobloft --user llm                     # 指定目录与运行用户
scripts/start.sh --dir /srv/noobloft -- -profile wan -join /tmp/swarm.key
```

脚本里已经包含初始化：节点目录没有 `config.json` 时才执行 `init`。幂等体现在四件事上——
二进制缺失或比源码旧才构建；已有配置永不被覆盖；安装到 `/usr/local/lib/noobloft/` 时内容相同不覆盖；
systemd unit 内容不变不重写，服务已在运行且二进制与 unit 都没变时不重启。因此重复执行是安全的，
第二次运行通常只会打印一串「跳过」。

Windows 上节点以**托盘程序**在后台运行，不留控制台窗口：

`build.ps1` 同时生成两个程序：`bin\noobloftd.exe` 保留命令行输出（用于 `init`、`run`、`publisher` 等）；
`bin\noobloftd-tray.exe` 使用 `-H=windowsgui` 构建，启动时不会创建控制台，双击直接进入托盘。
首次请运行 `scripts\start.ps1` 完成初始化；之后可直接双击托盘程序（默认 `%USERPROFILE%\.noobloft`），
或用 `noobloftd-tray.exe -dir D:\node1` 指定已有节点目录。启动失败会弹框或在状态窗口显示错误。
旧的 `noobloftd tray` 入口仍可使用，但要避免控制台闪现请使用独立托盘程序。
更新版本前请先退出旧托盘/停止旧节点，再运行启动脚本；脚本不会擅自结束正在运行的节点。
需要在不覆盖运行中程序的情况下构建验证时，可运行 `scripts\build.ps1 -OutputDir D:\swarm-build`。

- 单击右下角托盘图标唤出状态窗口，显示网关地址、运行状态与日志路径；
- 关闭窗口只隐藏到托盘，节点继续运行——点 X、按最小化都一样；
- 只有托盘图标右键选「退出」，或状态窗口里点「停止并退出」，才会真正停止节点；
- 再次运行 `start.ps1` 不会起第二个实例，只会把已有状态窗口调到前台；
- 目标端口已被别的进程占用时，脚本会直接报出占用者的 PID 与进程名并退出，不会把别人的 `/healthz`
  误判成本次启动已就绪。

Linux 上注册为 systemd 服务 `noobloft` 并设开机自启：

```sh
systemctl status noobloft
systemctl restart noobloft
systemctl stop noobloft
journalctl -u noobloft -f
```

`start.sh` 注册系统服务需要 root，以非 root 调用时会经 `sudo` 重新执行自身；节点进程以调用者身份运行
（`sudo` 时取 `SUDO_USER`，或用 `--user` 指定），避免节点数据变成 root 所有。`--` 之后的参数只在首次
初始化时透传给 `init`。`noobloftd tray` 是 Windows 专用，在 Linux 上会拒绝执行并提示改用 `systemctl`。

## 对接方式

网关提供 OpenAI 兼容接口，默认绑 `127.0.0.1:8760`，强制 Bearer token 鉴权：

```bash
curl http://127.0.0.1:8760/v1/chat/completions \
  -H "Authorization: Bearer <init 时生成的 token>" \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3.1:8b","messages":[{"role":"user","content":"你好"}],"stream":true}'
```

裸模型名优先使用本地后端；private 可查找旧式远端，shared 禁止无发布者的远端路由。
指定 `发布者ID::模型名` 时只路由到订阅授权的服务节点，不被同名本地模型替代，也不自动跨发布者切换。启用本地
信誉账本后，可信候选会按历史成功率和最近延迟加权选择；低于阈值的节点只在没有
可信候选时兜底。账本只记录计数、延迟和时间，不记录请求或响应正文。
任何现成的 OpenAI 客户端把 base_url 指到这里即可。

端点一览：

| 端点 | 鉴权 | 说明 |
| --- | --- | --- |
| `POST /v1/chat/completions` | Bearer | OpenAI 兼容，支持 `stream: true` 的 SSE |
| `GET /v1/models` | Bearer | 本地与网络可用模型，`owned_by` 区分来源 |
| `GET /admin/peers` | Bearer | 只读 peer 视图：签名能力、信誉及 connectionPaths 直连/中继路径 |
| `GET /healthz` | 无 | 仅返回存活状态，不泄漏模型或节点信息 |

`/admin` 是本机管理面，只读，不接受任何变更操作。单个 peer 能力查询失败只降级为
该条目的 `error` 字段，不影响整体响应——内部网络里节点随时上下线，一个失联节点
不该让整个查询失败。`Peers` 未注入时（纯本地模式）该端点不注册。

## 架构

```
cmd/noobloftd        CLI：init / run / info / peers / models
internal/config       配置与 fail-closed 校验
internal/security/identity     Ed25519 节点身份 + pnet swarm.key
internal/security/invitation   不含密钥的签名邀请、发布者信任与网络指纹校验
internal/network/node         libp2p 主机：pnet、mDNS、DHT、DCUtR 打洞、中继
internal/serving/capability   能力声明的签名与验签
internal/security/publisher    发布者签名文档、服务委托、消费凭证、吊销与订阅
internal/network/discovery    旧模型名或发布者+模型 -> CID，复用 DHT provider record
internal/serving/inference    v1 私网协议与 v2 授权协议、限流、服务端/客户端
internal/serving/router       发布者隔离、授权候选信誉加权与主动抽检
internal/serving/backend      后端适配器注册表（ollama / openai-compatible）
internal/controlplane/gateway      OpenAI 兼容 HTTP 网关（回环 + token）
internal/audit        JSONL 审计，结构上不含对话正文
internal/serving/reputation   本地信誉账本，仅存服务质量元数据
```

完整目录树、职责边界、前端模块和构建说明见 [项目目录与职责](docs/ARCHITECTURE.zh-CN.md)。共享语言资源位于 `locales/`，前端按功能放在 `web/src/features/`。

发现使用 DHT provider record；发布者模型使用版本化的发布者/模型发现键，旧私网保留模型名键。
候选经过能力声明验签，发布者路由还要求服务委托与订阅一致。
DHT 和信誉分不能授予访问权限；签名与抽检不能证明实际模型权重、规格或输出正确性。

能力声明带签发时间，超过 `capability.MaxAge` 即视为过期，防止重放旧声明。
因此 provider 必须持续续签：`discovery.Service` 持有私钥与模型来源，
`StartMaintenanceLoop` 每 `RefreshInterval`（= `MaxAge / 3`）重签一次并重做
DHT 公告。两个常量由推导关系绑定，并有 `init()` 断言兜底——如果续签周期
大于有效期，节点会在两轮之间静默隐身：连接仍在、日志无异常，但所有
provider 查询返回空。这条约束靠测试和断言守，不靠记忆。

穿透靠 libp2p 的组合：AutoNAT 判断可达性、DCUtR 做打洞、打洞失败时走中继。
由于不运营公共中继，`relay.staticRelays` 需要你自己填内部可达的节点地址。

## 默认关闭的开关

这些默认值是安全取向，开启前请想清楚后果：

| 配置 | 默认 | 打开意味着 |
|---|---|---|
| `provider.enabled` | false | 对外提供本机模型算力 |
| `relay.enabled` | false | 为他人转发流量，消耗你的带宽 |
| `reputation.enabled` | false | 在配置目录写入信誉账本并用于远端加权选路 |
| `reputation.probeEnabled` | false | 定期发起最多生成 1 token 的真实抽检，消耗对端算力 |
| `policy.allowExternalApiForwarding` | false | 转发到闭源商业 API，你需自行承担其 ToS 风险 |
| `policy.allowAnonymousRouting` | false | 代码层面硬拒，见 COMPLIANCE |
| `policy.publicService` | false | 对公众提供服务，触发额外合规义务 |

网关 `authToken` 为空会直接拒绝启动；绑定到非回环地址会打印警告。

## 扩展后端

新增一种推理后端只需实现 `backend.Adapter` 并注册，不必改动 gateway、router、
discovery 任何一行：

```go
backend.Register("my-vendor", func(spec backend.Spec) (backend.Adapter, error) {
    return &myAdapter{spec: spec}, nil
})
```

若该后端指向第三方托管服务，`External()` 返回 `true`。注册表会在构造阶段按
`policy.allowExternalApiForwarding` 决定放行与否，默认拒绝。这是为闭源 API
预留的扩展位：结构就绪，是否启用由你决定。

## 测试

```bash
go test ./... -count=1
```

已覆盖：能力声明验签（含篡改、冒充、过期）、推理协议端到端流式、provider 并发闸门、
网关鉴权与回环判定、声明续签与刷新周期的时序约束、未启用 DHT 时仍能续签，以及
信誉平滑评分、持久化、加权选路、请求记账与低成本抽检。
发布者测试覆盖同名模型隔离、凭证冒用/过期、旧协议绕过拒绝、逐消费者限制、
吊销及更新防回退、共享中继白名单和授权流式推理。本机集成不等同于公网验收。

## 合规

`docs/COMPLIANCE.zh-CN.md` 记录了 DMCA §512(a) 纯通道、Grokster 诱导责任、中国《生成式人工
智能服务管理暂行办法》、欧盟 AI Act 与 Llama 许可的一手来源，以及历史设计要求。文档中的规划不代表已经实现，当前授权边界见 PUBLISHERS.zh-CN.md。

本项目不附带任何模型权重。使用第三方模型时，其许可义务（如 Llama 的署名与命名要求）
由部署者自行承担。
