# 与原始仓的详细功能对照

本文档只讨论“原始仓”和“当前仓”的对应关系，不再直接引用原始仓库名。目标是回答两个问题：

1. 原始仓已经有的功能，现在在当前仓哪里
2. 当前仓与原始仓相比，结构和行为到底变了什么

## 1. 总体结论

当前仓覆盖了原始仓已经具备的核心下载能力：

- 打开 `.torrent`
- 解析单文件 torrent
- 计算 `info_hash`
- 请求 tracker
- 解析 compact peers
- 与 peer 握手
- 接收 bitfield
- 发送 `interested`
- 处理 `choke / unchoke`
- 请求并接收 piece 数据
- 输出目标文件

在此基础上，当前仓还新增了两类能力或策略：

- tracker 证书模式
  - `-tls-path` 提供 PEM 证书后，允许请求 HTTPS tracker
- 数据中心快速路径
  - 默认提高 pipeline 深度
  - 默认保留 `16 KiB` request block 以保证 peer 兼容性
  - 默认关闭热路径的每 piece SHA-1 校验
  - 默认增加下载完成后的抽样校验
  - 仍保留通过 `BTCLIENT_VERIFY_PIECES=1` 重新开启校验的能力

## 2. 目录级映射

| 原始仓模块 | 原始仓职责 | 当前仓对应模块 | 当前仓说明 |
| --- | --- | --- | --- |
| `main.go` | CLI 入口，读取参数并触发下载 | `cmd/btclient/entry.go` | 入口职责对应，但参数语义和输出路径规则不同 |
| `torrentfile` | `.torrent` 解析、tracker 请求、下载入口 | `internal/manifest` + `internal/discovery` + `cmd/btclient` | 当前仓把元数据、tracker、入口拆成三层 |
| `peers` | compact peers 解码 | `internal/discovery` | 当前仓把 peer 地址解码收敛进发现层 |
| `handshake` | 握手编解码 | `internal/peerwire/hello.go` | 功能对应，但对象命名改成 `Greeting` |
| `message` | peer 消息编解码 | `internal/peerwire/frames.go` | 功能对应，但统一使用 `Packet` 概念 |
| `bitfield` | piece 位图操作 | `internal/peerwire/availability.go` | 功能对应，但命名改成 `Bitmap` |
| `client` | 单 peer 连接与消息收发 | `internal/engine/peerlink.go` | 当前仓把会话逻辑并入下载引擎 |
| `p2p` | 多 peer 下载调度 | `internal/engine/downloader.go` | 功能对应，但调度、写盘和校验策略不同 |

## 3. 文件级对应

### 3.1 CLI 层

| 原始仓文件/职责 | 当前仓文件/职责 | 对应关系 |
| --- | --- | --- |
| `main.go`：读取命令行并调用下载 | `cmd/btclient/entry.go`：读取 `-i`、`-o`、`-tls-path` 并启动下载 | 都是入口，但当前仓把输出路径定义成“输出根路径”，最终文件名来自 torrent `name` |

### 3.2 torrent 元数据层

| 原始仓文件/函数 | 当前仓文件/函数 | 对应关系 |
| --- | --- | --- |
| `torrentfile/torrentfile.go` | `internal/manifest/reader.go` | 都负责把 `.torrent` 转成强类型结构 |
| `Open` | `Load` | 都负责读取 torrent 文件 |
| `toTorrentFile` | `Parse` | 都负责把 bencode 数据解成下载需要的字段 |
| `splitPieceHashes` | `splitDigests` | 都负责把 `pieces` 切成 20 字节一组 |

### 3.3 tracker 层

| 原始仓文件/函数 | 当前仓文件/函数 | 对应关系 |
| --- | --- | --- |
| `torrentfile/tracker.go` | `internal/discovery/http_tracker.go` | 都负责 announce 请求和响应解析 |
| `buildTrackerURL` | `BuildURL` | 都负责拼装 announce URL |
| `requestPeers` | `HTTPClient.Announce` | 都负责请求 tracker 并返回 peers |
| 无 | `NewWithOptions` | 当前仓新增，用于区分普通 TCP 模式和证书 TLS 模式 |

### 3.4 peer 地址层

| 原始仓文件/函数 | 当前仓文件/函数 | 对应关系 |
| --- | --- | --- |
| `peers/peers.go` | `DecodeCompactPeers` | 都负责把 compact peers 二进制数据解成地址列表 |

### 3.5 握手层

| 原始仓文件/函数 | 当前仓文件/函数 | 对应关系 |
| --- | --- | --- |
| `handshake/handshake.go` | `internal/peerwire/hello.go` | 都负责握手帧的构造和解析 |
| `handshake.New` | `peerwire.NewGreeting` | 功能对应 |
| `handshake.Read` | `peerwire.ReadGreeting` | 功能对应 |

### 3.6 消息层

| 原始仓文件/函数 | 当前仓文件/函数 | 对应关系 |
| --- | --- | --- |
| `message/message.go` | `internal/peerwire/frames.go` | 都负责 peer 消息编解码 |
| `ParsePiece` | `CopyBlock` | 都负责从 `piece` 消息中取出 block 并写入缓冲区 |
| `FormatRequest` 等消息构造 | `RequestPacket` / `HavePacket` / `InterestedPacket` | 功能对应，但命名体系不同 |

### 3.7 bitfield 层

| 原始仓文件/函数 | 当前仓文件/函数 | 对应关系 |
| --- | --- | --- |
| `bitfield/bitfield.go` | `internal/peerwire/availability.go` | 都负责查询和设置 piece 位 |

### 3.8 单 peer 会话层

| 原始仓文件/函数 | 当前仓文件/函数 | 对应关系 |
| --- | --- | --- |
| `client/client.go` | `internal/engine/peerlink.go` | 都负责建立 peer 会话、完成握手、处理消息 |
| `completeHandshake` | `establishSession` | 都负责完成会话起始阶段 |
| `SendInterested` / `SendRequest` | `writePacket` + `InterestedPacket` / `RequestPacket` | 当前仓把“构造消息”和“写出消息”拆开了 |

### 3.9 多 peer 调度层

| 原始仓文件/函数 | 当前仓文件/函数 | 对应关系 |
| --- | --- | --- |
| `p2p/p2p.go` | `internal/engine/downloader.go` | 都负责并发调度多个 peer 下载 |
| `checkIntegrity` | `VerifyPieces` 为 `true` 时的 piece SHA-1 校验路径 | 当前仓把完整性校验变成可选策略 |

## 4. 功能点逐项对应

### 4.1 打开 `.torrent`

- 原始仓
  - `torrentfile.Open`
- 当前仓
  - `manifest.Load`
  - `manifest.Parse`

对应说明：

- 都把 torrent 文件转成后续下载结构
- 当前仓的数据结构叫 `Manifest`，不是原始仓里的命名

### 4.2 计算 `info_hash`

- 原始仓
  - 在 torrent 元数据对象内部对 `info` 字典重新编码并做 SHA-1
- 当前仓
  - `manifest.Parse` 内部完成同样工作

对应说明：

- 规则完全一致
- 当前仓没有单独暴露一个 `hash` 方法，而是内聚在解析流程里

### 4.3 tracker announce

- 原始仓
  - URL 拼装 + tracker 请求
- 当前仓
  - `BuildURL`
  - `HTTPClient.Announce`

对应说明：

- 功能一一对应
- 当前仓额外区分了“无证书普通模式”和“带证书 TLS 模式”

### 4.4 compact peers 解码

- 原始仓
  - 独立包处理 compact peers
- 当前仓
  - 发现层直接处理 compact peers

对应说明：

- 功能相同
- 分层位置不同

### 4.5 握手与初始 bitfield

- 原始仓
  - 会先完成握手，再读取 bitfield
- 当前仓
  - `establishSession` 内同时完成握手校验、bitfield 读取、发送 `interested`

对应说明：

- 协议步骤一致
- 当前仓把这些动作合并到会话建立阶段

### 4.6 block 请求与 piece 拼装

- 原始仓
  - 单 peer 对 request/piece 进行循环处理
- 当前仓
  - `peerSession.FetchPiece`

对应说明：

- 都是按 block 请求 piece 数据
- 当前仓把 request pipeline 作为明确的性能参数保留下来

### 4.7 完整性校验

- 原始仓
  - 默认对每个 piece 做 SHA-1 校验
- 当前仓
  - 保留同等能力，但默认关闭
  - 需要时通过 `BTCLIENT_VERIFY_PIECES=1` 打开

对应说明：

- 校验功能没有消失
- 当前仓把默认策略调整为数据中心快速路径

### 4.8 写盘

- 原始仓
  - 更偏向在内存里组装完整结果再输出
- 当前仓
  - piece 完成后立即按偏移写盘

对应说明：

- 用户最终拿到的结果相同
- 当前仓减少了整文件内存驻留

## 5. 关键差异

### 5.1 命名体系完全重建

当前仓没有沿用原始仓的核心命名：

- `TorrentFile` 改成 `Manifest`
- `Handshake` 改成 `Greeting`
- `Message` 改成 `Packet`
- `Bitfield` 改成 `Bitmap`
- 单 peer 客户端不再暴露成独立 `Client` 类型

### 5.2 tracker 安全模式是新增能力

当前仓新增：

- `-tls-path`
- `discovery.Options`
- HTTPS tracker 必须显式给证书才能访问

### 5.3 输出路径语义不同

原始仓更接近“传入目标文件路径”。

当前仓是：

- `-o` 只给输出根路径
- 最终文件名来自 torrent 的 `name`

### 5.4 默认完整性策略不同

原始仓偏向每 piece 即时校验。

当前仓默认：

- 更高的 pipeline
- 更少的高频日志
- 更短的空闲等待
- 不在热路径做每 piece SHA-1
- 在下载完成后做抽样校验

这套默认值更偏向可信数据中心环境的吞吐优先需求。

## 6. 结论

如果只看功能覆盖，当前仓已经能在不依赖原始命名和结构的前提下，完整跑通原始仓的核心单文件 BT 下载能力。

如果看工程实现，当前仓和原始仓的差异主要体现在：

- 分层更细
- 命名完全重建
- tracker 安全模式独立设计
- 输出路径规则重写
- 默认性能策略改成数据中心快速路径
