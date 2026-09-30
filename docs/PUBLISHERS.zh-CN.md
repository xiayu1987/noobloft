[English](PUBLISHERS.md) | 简体中文

# 发布者微网络

共享传输底座，发布者独立授权。一个消费者 Host 可以订阅多个发布者；指定的发布者不可被同名本地模型或其他发布者替代。默认仍为 private；只有显式 `init -mode shared` 才不生成/加载 swarm.key。shared 与 PSK 私网不能互通，不会因密钥错误自动降级。

## 身份与文档

- 发布者根身份：独立 Ed25519 identity.key；Publisher ID 是其 PeerID。根私钥应离线保护，不复制到服务节点。
- 服务节点与消费者：各自拥有独立 identity.key，不能共享身份私钥。
- service 文档：根签名的服务委托，列出允许的服务 PeerID、模型、连接地址及可选中继；兼作订阅描述文件，可公开分享。
- access 文档：根签名的消费凭证，绑定消费者 PeerID，限定模型、生成 token 上限、并发、每分钟请求数。
- revocations 文档：根签名的吊销快照，包含递增 sequence 与累积撤销项。即使尚无吊销，也必须安装有效的空快照。

当前由根密钥直接签发，不实现在线签发子密钥链。导入必须通过独立可信渠道核对 Publisher ID；文件自带签名不能建立初始信任。所有文档默认有效期 24h、最长 8760h，签发命令不覆盖已有文件。

## 1. 初始化

以下占位符必须替换为实际值。命令应在相应节点或离线签发机器执行。Windows 二进制加 `.exe`。

```sh
noobloftd publisher init -root ./publisher-root
noobloftd init -mode shared -profile wan -dir ./provider-data
noobloftd init -mode shared -profile wan -dir ./consumer-data
```

记录输出中的 Publisher ID、服务节点 PeerID 和消费者 PeerID。初始化也输出本地网关 token，不要公开完整输出。多个节点在同机运行时更改网关端口，避免默认 8760 冲突。

给服务节点配置固定监听端口；如其可直接从公网访问，配置 `network.announceAddrs`（不含 /p2p/PeerID），并保证端口映射/防火墙可达。例如监听 `/ip4/0.0.0.0/tcp/4001`、公告 `/ip4/<公网IP>/tcp/4001`。公网 DHT 节点设置 `network.dhtMode=server`，普通 WAN 节点使用 auto。无直连地址时使用后文可选中继。

## 2. 签发并安装服务

在离线签发机器：

```sh
noobloftd publisher service -root ./publisher-root -file service.noobloft -peers <服务PeerID> -models llama3.1:8b -addresses /ip4/<公网IP>/tcp/4001/p2p/<服务PeerID> -ttl 720h
noobloftd publisher revocations -root ./publisher-root -file revocations-1.json -ttl 168h
```

多个 PeerID、模型和地址用逗号分隔。没有可达服务地址时，addresses 可以填写共享引导入口；relays 可单独指定中继地址。空地址描述可签发，但异地节点必须另有可用锚点才能发现彼此。

把两个签名文档交付服务节点，保留根私钥在签发机器。服务节点执行：

```sh
noobloftd publisher install -dir ./provider-data -file service.noobloft -trust <PublisherID> -revocations ./revocations-1.json
```

install 保存吊销文件的绝对路径，文件必须长期存在。然后编辑 provider-data/config.json：配置实际本地后端、`provider.enabled=true`。`provider.advertiseModels` 同时限制公告和远程调用，并与 service.models 取交集；空列表表示不额外限制。install 不自动开启算力服务。

```sh
noobloftd run -dir ./provider-data
```

授权服务只注册 `/noobloft/2.0.0/infer`，不开放旧协议绕过路径。

## 3. 消费者订阅与调用

离线签发机器：

```sh
noobloftd publisher grant -root ./publisher-root -file consumer-access.json -subject <消费者PeerID> -models llama3.1:8b -max-tokens 1024 -concurrency 2 -rpm 30 -ttl 168h
```

消费者收到 service 与自己的 access 后：

```sh
noobloftd publisher subscribe -dir ./consumer-data -file service.noobloft -trust <PublisherID>
noobloftd publisher credential -dir ./consumer-data -file consumer-access.json -trust <PublisherID>
noobloftd run -dir ./consumer-data
noobloftd models -dir ./consumer-data
```

对每个发布者重复 subscribe/credential 即可。配置变更需重启；subscribe 仅建立信任并合并地址，不授予消费权。凭证复制到另一个 PeerID 无效。

向本地 OpenAI 兼容网关发送：

```json
{"model":"<PublisherID>::llama3.1:8b","messages":[{"role":"user","content":"你好"}],"max_tokens":128,"stream":true}
```

HTTP 请求仍须携带本地网关 Bearer token。max_tokens 必须为正且不超过消费凭证限制。裸模型名在 shared 模式只调用本地后端，远端必须使用限定名称。DHT 只提供候选，实际路由还验证服务委托、能力声明和订阅中的服务文档 ID，再做信誉排序；信誉不代表身份授权。

## 4. 吊销与续签

吊销项可为 access 文档 ID、消费者 PeerID 或 service 文档 ID。仅撤销 access ID 可让同一消费者以后取得新凭证；撤销 PeerID 会拒绝该身份全部凭证。

```sh
noobloftd publisher revocations -root ./publisher-root -previous revocations-1.json -revoked <需要吊销的ID> -file revocations-2.json -ttl 168h
noobloftd publisher apply-revocations -dir ./provider-data -file revocations-2.json -trust <PublisherID>
```

第一条在签发机器运行，第二条在每个服务节点运行。续签即便没有新吊销项，也必须带 previous 保留历史并递增 sequence。apply-revocations 拒绝回退和删除历史撤销项，通过同目录替换更新；服务端每次准入读取，无需重启。缺失、损坏或过期快照拒绝新请求；已经运行的请求不被中断。

防回退检查在 CLI 更新路径中执行；拥有磁盘写权限的操作者可以绕过 CLI 替换文件，当前没有防磁盘回滚的外部可信单调存储。保护配置目录权限，不要手动回写旧快照。多服务节点没有自动快照同步，必须分发到所有节点，过期前续签。

service 换版或续签产生新 ID：服务端重新 install、消费者重新 subscribe 并分别重启。当前信任绑定具体 service ID，不支持透明轮换。access 续签后消费者重新 credential 并重启。根身份不变时 Publisher ID 不变；根密钥轮换则需要重新建立发布者信任。

## 5. 可选共享中继

中继不是推理发布者，无需模型后端或根私钥。初始化使用：

```sh
noobloftd init -mode shared -profile wan -dir ./relay-data
```

再编辑 relay-data/config.json：

- network.listenAddrs 使用固定 TCP 端口，announceAddrs 使用外部可达地址，dhtMode=server。
- gateway.enabled=false，provider.enabled=false，所有 backends[].enabled=false。
- relay.enabled=true，relay.allowedPeers 填服务节点和消费者的 PeerID；禁止空白名单启动。
- relay.useRelays=false；按资源容量设置 relay.maxCircuits。

`public-relay` 快捷 profile 目前面向 PSK 部署；shared 中继按上述配置，避免其默认开启中继但缺少白名单而校验失败。白名单预留要求目标在列表中，中继连接要求源和目标都在列表中；更改后重启中继。它与模型消费凭证是两套不同的权限边界。

把中继完整 `/ip4/<IP>/tcp/4001/p2p/<中继PeerID>` 地址写入双方的 network.staticPeers 和 relay.staticRelays，保持 relay.useRelays=true。签发 service 时也可用 `-relays` 携带该地址，subscribe 会导入。服务节点本身仍需配置锚点/中继，install 不合并地址。

无中继时必须能直接连接；现有 DCUtR 需要中继连接协调打洞。无法访问中继且直连失败时返回不可用，不承诺 NAT 穿透成功。中继转发端到端加密流量，但能观察身份、流量与时序；模型服务节点会看到提示词。

## 6. 范围与验收

- 并发、每分钟请求数按消费者 PeerID 在每个服务进程内统计，重启清零；没有集群共享额度、计费或结算。
- 当前每个服务进程安装一个发布者服务；一个服务文档可授权多个节点，一个消费者可订阅多个发布者。
- 同一 HTTP 网关下多个应用共用消费者 PeerID，网关 token 不是逐应用远端身份。
- 授权抽检使用相同凭证及限制，最多生成 1 token；不能证明权重或模型规格真实性。
- /admin/peers 展示 publisherId、serviceId、连接路径与信誉。未实现自动证书/吊销分发、在线签发子密钥、逐段结果签名或权重分发。
- 本机集成测试覆盖同名模型隔离、冒用拒绝、旧协议拒绝、限流/过期/吊销，以及真实 libp2p 共享中继上的流式调用。
- 公网验收仍需两个真实异地节点：分别测试流式/非流式、强制中继、入口重启恢复、授权吊销只影响目标发布者，以及证书续签。测试后端不能替代实际模型后端验收。
