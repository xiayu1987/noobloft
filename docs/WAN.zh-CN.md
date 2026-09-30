[English](WAN.md) | 简体中文

# 跨公网私有 P2P 部署

本文仅适用于默认 `network.mode=private`。共享底座授权模式见 [PUBLISHERS.zh-CN.md](PUBLISHERS.zh-CN.md)，不使用 swarm.key 或旧 invite 命令。

## 拓扑与边界

至少部署一台可被所有成员访问的公网 VPS，兼任私有 DHT Server、引导和 Circuit Relay v2。
异地节点优先直连，AutoRelay 为 NAT 后节点维护预留，DCUtR 尝试升级为直连；失败时应用协议允许继续使用受限中继连接。
任何节点仍必须持有同一 swarm.key。公网节点不是公共 IPFS 节点，也不对陌生网络提供服务。
本版本使用 PSK + TCP，不支持 PSK + QUIC。双方无法访问中继时不能保证连通。

## 1. 公网入口

构建：`go build -o bin/noobloftd ./cmd/noobloftd`（Windows 自行加 .exe）。
以下命令中的公网 IP、路径、PeerID 必须替换为自己的实际值，不要原样使用占位符。

```sh
noobloftd init -dir ./relay-data -profile public-relay -announce /ip4/<公网IP>/tcp/4001
noobloftd invite export -dir ./relay-data -file network.noobloft -ttl 168h
noobloftd run -dir ./relay-data
```

也可公告 `/dns4/<域名>/tcp/4001`。云安全组和宿主防火墙允许成员访问 TCP 4001。
若公网 IP 由上游 NAT 映射，需把公告端口映射到本机监听的 4001。
配置模板由 `init -profile public-relay` 生成：固定 TCP 4001、DHT server、关闭 mDNS、网关、provider 及模型后端，开启私有中继。
`network.announceAddrs` 是不含 `/p2p/PeerID` 的可达传输地址，不应填写 0.0.0.0 或动态端口 0。

`invite export` 使用本机身份签名，合并 staticPeers、staticRelays 与公告地址，拒绝覆盖已有导出文件。
若已有私有网络，初始化公网入口时增加 `-join <已有swarm.key路径>`，否则会新建不同的网络。

## 2. 异地节点

通过可信渠道交付 swarm.key；邀请文件不含 PSK、身份私钥或 HTTP token。
通过独立可信渠道核对导出者 PeerID，不能只信任邀请文件自己声明的身份。

```sh
noobloftd init -dir ./node-data -profile wan -join /safe/path/swarm.key
noobloftd invite import -dir ./node-data -file network.noobloft -trust <发布者PeerID>
noobloftd run -dir ./node-data
```

导入检查版本、文件大小、签名、发布者身份、有效期和由本地 PSK 推导的 networkId；错误时不修改配置。
地址增量去重合并，配置原子写入。重复导入不会重复添加锚点。
导入启用 DHT、打洞及邀请指定的中继；非中继节点设为 DHT auto。
不会开启 provider 或 relay 服务，也不更换本机身份和密钥。已运行的节点需重启应用配置。
邀请默认 7 天、最长 30 天；到期只禁止新导入，不会撤销已经加入网络的成员。
撤销成员需要轮换共享 PSK 并更新其余成员，邀请签名不是独立访问控制系统。

模型提供者自行在 node-data/config.json 设置 provider.enabled=true，并配置本地后端。
模型列表仍以实时签名能力声明为准，邀请不复制第二份模型清单。
消费者本地不要部署同名模型，否则本地优先策略会直接使用本地模型。

## 3. 查询与验收

```sh
noobloftd peers -dir ./node-data
noobloftd models -dir ./node-data
```

peers 与 `/admin/peers` 的 connectionPaths 显示 `direct:` 或 `relay:` 加实际远端地址。
这是当前连接快照，不是 DCUtR 成功事件记录；同时存在两条连接时会显示两条。
公网中继默认关闭 HTTP 网关，不能在其上用 peers 查询；可从普通节点查询连接或查看入口日志。
DHT 收敛、可达性判断和中继预留需要时间，启动后模型不会保证立即可见。
模型公告按现有维护周期重试；锚点每 30 秒检查并重拨，拨号超时 10 秒，离线锚点不会阻塞启动。

真实网络验收应使用两个不同运营商/局域网的节点：

1. 验证 PSK 一致、两端能连入口，并通过模型名找到 provider。
2. 发起 stream=false 与 stream=true 推理，确认正文完整、结束帧正常。
3. 禁止两端直接通信但保留到入口的访问，确认 connectionPaths 为 relay，能力查询和推理仍成功。
4. 恢复直连条件，观察连接路径；严格 NAT 下不能要求一定打洞成功。
5. 重启公网入口及 provider，等待重新预留/公告，验证再次推理。
6. 使用第二台私有入口可减少单点风险：配置其完整地址后重新导出邀请，成员增量导入。

## 4. 中继限制与隐私

修复了中继资源配置未继承默认值的问题：保留库默认预留 TTL、缓冲区等，只覆盖并发额度。
当前单电路最多 30 分钟、1 GiB；maxCircuits 用于电路及预留配额，其余沿用库默认限制。
perPeerBandwidthKBps 尚未实现速率限制，非零值会明确拒绝启动，避免配置看似生效。
中继转发端到端加密流量，但可见 PeerID、流量大小、时序等元数据。
邀请属于网络拓扑元信息，虽然不含密钥，也建议仅向成员分享。

## 验证范围

自动化覆盖：签名篡改/过期/错误信任/错误 PSK/超大文件拒绝、CLI 导入导出及重复导入、锚点断开重连与取消退出。
同机三节点集成测试使用真实 PSK libp2p 节点、真实 Relay v2 预留，强制只有中继路径，完成签名能力查询与流式推理（模型使用测试后端）。
这证明应用层中继协议闭环，不等同于真实跨运营商 NAT 或公网 VPS 验收。真实部署需要实际公网地址、主机访问权限和异地节点。
