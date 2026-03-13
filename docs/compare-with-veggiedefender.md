# 与 `veggiedefender/torrent-client` 的详细功能对照

本文档的目标不是泛泛而谈“两个仓库都能下载 BT 文件”，而是把原仓的功能点、模块职责、文件位置和本仓的对应实现逐项列清楚，让阅读者能直接知道：

- 原仓某个功能现在在本仓哪里
- 本仓是否完整覆盖了该能力
- 本仓的实现方式与原仓有什么不同

## 1. 总体结论

本仓覆盖了原仓已经具备的核心下载能力：

- 读取 `.torrent`
- 解析单文件 torrent
- 计算 `info_hash`
- 请求 tracker
- 解析 compact peers
- 与 peer 建立握手
- 读取 bitfield
- 发送 `interested`
- 处理 `choke / unchoke`
- 按块请求并接收 piece 数据
- 做 piece 的 SHA-1 校验
- 输出目标文件

同时，本仓新增了原仓没有的 tracker HTTPS/TLS 能力：

- 通过 `-tls-path` 指定 PEM 证书
- 在 tracker 连接层显式区分普通模式和 TLS 安全模式

## 2. 原仓目录与本仓目录的整体映射

| 原仓模块 | 原仓职责 | 本仓对应模块 | 对应说明 |
| --- | --- | --- | --- |
| `main.go` | CLI 入口，读取参数并触发下载 | `cmd/btclient/entry.go` | 都是入口，但参数组织和命名体系不同 |
| `torrentfile` | `.torrent` 解析、tracker 请求、下载入口 | `internal/manifest` + `internal/discovery` + `cmd/btclient` | 本仓把元数据、tracker、CLI 拆开了 |
| `peers` | compact peer 列表解码 | `internal/discovery` | 本仓把 peer 地址解码放到 tracker 发现层 |
| `handshake` | BitTorrent 握手编解码 | `internal/peerwire/hello.go` | 功能对应，但文件名和对象命名不同 |
| `message` | peer 消息编解码 | `internal/peerwire/frames.go` | 功能对应，但改成统一的 `Packet` 视角 |
| `bitfield` | piece 位图操作 | `internal/peerwire/availability.go` | 功能对应，但命名改成 `Bitmap` / `availability` |
| `client` | 单个 peer 连接、收发消息 | `internal/engine/peerlink.go` | 本仓把连接行为并入下载引擎 |
| `p2p` | 多 peer 下载调度 | `internal/engine/downloader.go` | 功能对应，但写盘策略和调度组织不同 |

## 3. 文件级详细对应

### 3.1 入口层

| 原仓文件 | 原仓作用 | 本仓文件 | 本仓作用 | 差异 |
| --- | --- | --- | --- | --- |
| `main.go` | 从命令行拿输入、输出路径并调用下载 | `cmd/btclient/entry.go` | 解析命令行、构造 tracker 选项、启动下载 | 本仓改成 `-i`、`-o`、`-tls-path` 这 3 个参数 |

### 3.2 torrent 元数据层

| 原仓文件 | 原仓作用 | 本仓文件 | 本仓作用 | 差异 |
| --- | --- | --- | --- | --- |
| `torrentfile/torrentfile.go` | 解析 `.torrent`、构造 `TorrentFile`、触发下载 | `internal/manifest/reader.go` | 解析 `.torrent`、构造 `Manifest` | 本仓把“元数据解析”和“下载执行”分离了 |
| `torrentfile/torrentfile.go` 中 `Open` | 读取 torrent 文件 | `manifest.Load` | 读取 torrent 文件 | 对应关系直接 |
| `torrentfile/torrentfile.go` 中 `toTorrentFile` | 将 bencode 数据转为强类型结构 | `manifest.Parse` | 将原始字节转成 `Manifest` | 本仓没有沿用 `TorrentFile` 命名 |
| `torrentfile/torrentfile.go` 中 `splitPieceHashes` | 拆分 `pieces` | `splitDigests` | 拆分 `pieces` | 功能一致，命名不同 |

### 3.3 tracker 层

| 原仓文件 | 原仓作用 | 本仓文件 | 本仓作用 | 差异 |
| --- | --- | --- | --- | --- |
| `torrentfile/tracker.go` | 组装 announce URL、发请求、解析 tracker 响应 | `internal/discovery/http_tracker.go` | 组装 announce URL、发请求、解析 tracker 响应 | 本仓增加 HTTPS/TLS 连接控制 |
| `buildTrackerURL` | 生成 announce URL | `BuildURL` | 生成 announce URL | 功能对应 |
| `requestPeers` | 请求 tracker 并拿到 peers | `HTTPClient.Announce` | 请求 tracker 并解析成 `AnnounceReply` | 本仓把返回结果包装成更明确的结构 |
| 无 | 无 | `NewWithOptions` | 构造支持 TLS 证书/跳过校验的客户端 | 原仓没有这一层 |

### 3.4 peer 地址层

| 原仓文件 | 原仓作用 | 本仓文件 | 本仓作用 | 差异 |
| --- | --- | --- | --- | --- |
| `peers/peers.go` | 把 compact peer 字节串解码成 peer 列表 | `internal/discovery/http_tracker.go` 中 `DecodeCompactPeers` | 把 compact peer 字节串解码成 `Endpoint` 列表 | 本仓把这一步并入发现层，不再单独拆包 |

### 3.5 握手层

| 原仓文件 | 原仓作用 | 本仓文件 | 本仓作用 | 差异 |
| --- | --- | --- | --- | --- |
| `handshake/handshake.go` | 握手结构体、序列化、反序列化 | `internal/peerwire/hello.go` | `Greeting` 的编码和解码 | 本仓把 `Handshake` 改名为 `Greeting` |

### 3.6 消息层

| 原仓文件 | 原仓作用 | 本仓文件 | 本仓作用 | 差异 |
| --- | --- | --- | --- | --- |
| `message/message.go` | 各类消息 ID、消息编解码、Have/Piece 解析 | `internal/peerwire/frames.go` | `Packet` 编解码，`Request/Have/Piece` 处理 | 本仓统一改成 `Packet` 语义，不再用 `Message` 命名 |

### 3.7 bitfield 层

| 原仓文件 | 原仓作用 | 本仓文件 | 本仓作用 | 差异 |
| --- | --- | --- | --- | --- |
| `bitfield/bitfield.go` | 查询和设置 piece 位 | `internal/peerwire/availability.go` | 查询和设置 piece 位 | 功能一致，但命名换成 `Bitmap` |

### 3.8 单 peer 连接层

| 原仓文件 | 原仓作用 | 本仓文件 | 本仓作用 | 差异 |
| --- | --- | --- | --- | --- |
| `client/client.go` | 建立 TCP 连接、握手、收发消息 | `internal/engine/peerlink.go` | 建立 peer 会话、握手、收发消息 | 本仓不再单独暴露 `Client` 类型 |

### 3.9 多 peer 调度层

| 原仓文件 | 原仓作用 | 本仓文件 | 本仓作用 | 差异 |
| --- | --- | --- | --- | --- |
| `p2p/p2p.go` | piece 调度、并发下载、完整文件拼装 | `internal/engine/downloader.go` | piece 调度、并发下载、piece 校验、按偏移写盘 | 本仓改成了“校验后直接写盘” |

## 4. 功能点逐项对应

下面按“用户能感知到的能力”做逐项映射。

### 4.1 打开 `.torrent`

- 原仓
  - `torrentfile.Open`
- 本仓
  - `manifest.Load`
  - `manifest.Parse`

对应关系：

- 两者都负责把 `.torrent` 文件变成后续下载能用的数据结构
- 本仓的数据结构叫 `Manifest`，不是 `TorrentFile`

### 4.2 计算 `info_hash`

- 原仓
  - `bencodeInfo.hash`
- 本仓
  - `manifest.Parse` 内部对 `info` 重新编码后做 SHA-1

对应关系：

- 两边都遵守 BitTorrent 的标准做法
- 本仓把它内聚到 manifest 解析流程里，不暴露单独的 `hash` 方法

### 4.3 请求 tracker

- 原仓
  - `buildTrackerURL`
  - `requestPeers`
- 本仓
  - `discovery.BuildURL`
  - `discovery.HTTPClient.Announce`

对应关系：

- 功能一一对应
- 本仓额外支持通过证书切换到 tracker 安全模式

### 4.4 解析 compact peers

- 原仓
  - `peers.Unmarshal`
- 本仓
  - `discovery.DecodeCompactPeers`

对应关系：

- 都是在处理 tracker 的 compact peer 编码
- 本仓把这个能力收拢到 discovery 层

### 4.5 与 peer 握手

- 原仓
  - `handshake.New`
  - `handshake.Read`
  - `client.completeHandshake`
- 本仓
  - `peerwire.NewGreeting`
  - `peerwire.ReadGreeting`
  - `engine.establishSession`

对应关系：

- 原仓把握手对象和连接对象分开
- 本仓把握手过程折叠进 `peerSession` 建立流程

### 4.6 接收 bitfield

- 原仓
  - `client.recvBitfield`
  - `bitfield.Bitfield`
- 本仓
  - `engine.establishSession`
  - `peerwire.Bitmap`

对应关系：

- 两边都要求 peer 初始返回 bitfield
- 本仓通过 `Bitmap` 暴露 piece 可用性判断

### 4.7 发送 `interested` / `request`

- 原仓
  - `Client.SendInterested`
  - `Client.SendRequest`
- 本仓
  - `peerwire.InterestedPacket`
  - `peerwire.RequestPacket`
  - `peerSession.writePacket`

对应关系：

- 原仓把“生成消息”和“发送消息”包在 `Client` 方法里
- 本仓把消息构造和连接发送分成两层

### 4.8 处理 `piece`

- 原仓
  - `message.ParsePiece`
- 本仓
  - `peerwire.CopyBlock`

对应关系：

- 两者都负责从 `piece` 消息中取出 block，并按偏移放入缓冲区

### 4.9 piece 校验

- 原仓
  - `checkIntegrity`
- 本仓
  - `engine.Manager.runPeer` 中对下载完成的 piece 执行 SHA-1 校验

对应关系：

- 功能一致
- 本仓把校验放在调度器主流程里

### 4.10 输出文件

- 原仓
  - 全部 piece 完成后，把完整内存缓冲区写入文件
- 本仓
  - 每个 piece 校验通过后立即按偏移写入目标文件

对应关系：

- 结果相同，路径不同
- 本仓更偏流式和分片写入

## 5. 原仓没有、本仓新增的点

### 5.1 tracker HTTPS/TLS

本仓新增：

- `discovery.Options`
- `-tls-path`

具体行为：

- 没有传证书
  - 使用普通模式请求 tracker
- 传入证书
  - 使用 TLS 安全模式请求 tracker

这是参考仓没有的功能。

## 6. 为什么说它不是按原仓平移

原因不只是名字变了，而是工程边界也变了：

- 原仓把 torrent 元数据解析和 tracker 请求放在同一个包
- 本仓把它拆成 `manifest` 和 `discovery`
- 原仓有单独的 `client` 包
- 本仓把单 peer 会话并入 `engine`
- 原仓有 `message`、`handshake`、`bitfield`
- 本仓把它们统一收拢到 `peerwire`
- 原仓下载时先把完整文件放进内存
- 本仓下载时按 piece 校验后直接写盘

因此，本仓不是对原仓目录进行简单换名，而是重新组织了职责边界。
