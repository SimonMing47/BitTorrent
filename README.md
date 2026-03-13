# bt-refractor

`bt-refractor` 是一个用 Go 从零重写的 BitTorrent 单文件下载客户端。它的目标不是复刻 [veggiedefender/torrent-client](https://github.com/veggiedefender/torrent-client) 的目录结构或命名方式，而是在保持功能覆盖不缺失的前提下，建立一套新的工程组织、数据模型和下载流程。

## 1. 这个库现在能做什么

当前已经支持：

- 读取 `.torrent` 文件
- 解析单文件 torrent 的 `announce`、`info`、`piece length`、`pieces`、`length`、`name`
- 计算 `info_hash`
- 向 HTTP tracker 发起 announce 请求
- 向 HTTPS tracker 发起 announce 请求
- 为 HTTPS tracker 指定 PEM 证书
- 为 HTTPS tracker 跳过 TLS 校验
- 解析 compact peers 列表
- 与 peer 建立 TCP 连接
- 完成 BitTorrent 握手
- 接收 bitfield
- 发送 `interested`
- 处理 `choke` / `unchoke`
- 按 block 请求 piece 数据
- 校验 piece 的 SHA-1
- 将校验通过的 piece 直接写入目标文件

当前明确不支持：

- magnet link
- UDP tracker
- 多文件 torrent
- 做 uploader / seeder
- DHT / PEX / 扩展协议

## 2. 一次完整下载在做什么

命令行入口位于 [entry.go](/Users/mac/projects/bt-refractor/cmd/riptide/entry.go)。一次下载的完整流程如下：

1. 读取 `.torrent` 文件。
2. 在 `internal/manifest` 中把 bencode 元数据解析成 `Manifest`。
3. 对 `info` 字典重新编码并计算 `info_hash`。
4. 在 `internal/discovery` 中向 tracker 发起 announce。
5. tracker 返回 compact peers 列表，客户端把它解码为 `IP:Port` 集合。
6. `internal/engine` 为每个 peer 启动一个下载 worker。
7. worker 与 peer 完成握手，读取 bitfield，并发送 `interested`。
8. 当 peer `unchoke` 后，worker 按 pipeline 连续发送多个 `request`。
9. peer 返回 `piece` 消息后，客户端下载对应 block，拼进 piece 缓冲区。
10. piece 下载完成后做 SHA-1 校验。
11. 校验通过的 piece 会直接写到目标文件的正确偏移处。
12. 所有 piece 完成后，下载结束。

这个实现和原仓的一个关键差异是：这里采用“分片校验后直接写盘”，不是“先把整个文件下载到内存，再一次性写盘”。

## 3. 仓库结构

- `cmd/riptide`
  - 命令行入口，负责参数解析、组装下载流程。
- `internal/bencode`
  - 最小化 bencode 编解码器。
- `internal/manifest`
  - `.torrent` 解析、`info_hash` 计算、piece 边界计算。
- `internal/discovery`
  - tracker announce、compact peers 解码、HTTPS/TLS 连接策略。
- `internal/peerwire`
  - BitTorrent 握手帧、消息帧、bitfield 位图处理。
- `internal/engine`
  - peer 会话、piece 调度、piece 校验、文件写入。

## 4. 功能点拆解

### 4.1 `internal/bencode`

这个包实现了本项目需要的最小 bencode 能力：

- `Parse`
  - 把原始 bencode 字节解析成通用 Go 值。
- `Decode`
  - 从 `io.Reader` 中读取全部内容再解码。
- `Marshal`
  - 将通用 Go 值重新编码回 bencode。

当前只使用这几类值：

- `[]byte`
- `int64`
- `[]any`
- `map[string]any`

它的用途主要有两个：

- 解析 `.torrent` 文件
- 解析 tracker 返回的 bencode 响应

### 4.2 `internal/manifest`

这个包负责 `.torrent` 元数据层。

核心结构是 `Manifest`，字段含义如下：

- `Announce`
  - tracker 地址。
- `Name`
  - 目标文件名。
- `TotalLength`
  - 文件总长度。
- `StandardPieceLength`
  - 标准 piece 长度。
- `PieceDigests`
  - 所有 piece 的 SHA-1 摘要。
- `InfoHash`
  - `info` 字典重新编码后的 SHA-1。

核心职责如下：

- 从 `.torrent` 中取出 `announce`
- 从 `info` 字典中取出 `name`、`length`、`piece length`、`pieces`
- 校验当前 torrent 是否为单文件模式
- 计算 `InfoHash`
- 将 `pieces` 字节串拆成每 20 字节一个摘要
- 为下载器提供 piece 编号到“文件偏移 + 实际长度”的映射

### 4.3 `internal/discovery`

这个包负责 tracker 发现。

核心能力如下：

- `BuildURL`
  - 组装 announce URL
- `HTTPClient.Announce`
  - 发请求并解析 tracker 返回值
- `DecodeCompactPeers`
  - 解析 tracker 的 compact peer 格式
- `NewWithOptions`
  - 为 HTTP / HTTPS tracker 构造不同的连接策略

这里支持三种 tracker 访问模式：

1. 默认模式
   - 走普通 `net.Dialer` / `DialContext`
2. 传入证书
   - 构造 `tls.Config`，把 PEM 证书加入信任池，再走 TLS 拨号
3. 显式跳过校验
   - 打开 `InsecureSkipVerify`，再走 TLS 拨号

也就是说，当前实现里：

- 没有证书、也没有开启 `skip verify`
  - 使用普通 TCP 拨号
- 有证书，或者显式要求跳过校验
  - 使用 TLS 拨号访问 HTTPS tracker

### 4.4 `internal/peerwire`

这个包负责 BitTorrent peer wire 协议的编解码。

它拆成三部分：

- `hello.go`
  - 握手帧
- `frames.go`
  - 长度前缀消息帧
- `availability.go`
  - peer bitfield 位图

核心对象如下：

- `Greeting`
  - 表示一次握手
- `Packet`
  - 表示一条 peer 消息
- `Bitmap`
  - 表示某个 peer 声明它拥有的 piece 集合

### 4.5 `internal/engine`

这个包是下载调度核心。

主要结构如下：

- `Manager`
  - 负责整体下载流程和 worker 调度
- `peerSession`
  - 表示与单个 peer 的连接会话
- `catalog`
  - 负责 piece 领取、归还、完成状态管理
- `pieceLease`
  - 表示一次被某个 worker 领取的 piece 任务
- `fileStore`
  - 负责按偏移写入目标文件

下载调度的关键点：

- 每个 peer 都会起一个 worker
- worker 只会领取该 peer 的 bitfield 中已有的 piece
- 下载失败或校验失败，会把 piece 放回待下载队列
- 下载成功后立即写盘
- 文件写入时带锁，避免并发写偏移冲突

## 5. 协议细节

这一节讲清楚这个库到底在跟谁说话、说的是什么。

### 5.1 `.torrent` 文件结构

当前实现只支持单文件 torrent，因此关心的核心字段是：

- 根字典
  - `announce`
  - `info`
- `info` 字典
  - `name`
  - `length`
  - `piece length`
  - `pieces`

其中：

- `pieces` 不是字符串文本，而是一串连续的 SHA-1 摘要
- 每 20 个字节对应一个 piece 的摘要

`info_hash` 的计算方式不是对整个 `.torrent` 文件求哈希，而是：

1. 取出 `info` 字典
2. 用 bencode 重新编码
3. 对编码结果做 SHA-1

### 5.2 tracker announce 请求

当前 tracker 请求使用的是经典 announce 形式，关键参数有：

- `info_hash`
- `peer_id`
- `port`
- `uploaded`
- `downloaded`
- `left`
- `compact`

本实现默认：

- `uploaded = 0`
- `downloaded = 0`
- `left = 文件总长度`
- `compact = 1`

tracker 返回值里，当前实现只消费：

- `interval`
- `peers`

如果响应里有 `failure reason`，则会直接报错。

### 5.3 compact peers 格式

compact peers 是 tracker 常用的二进制压缩格式：

- 每个 peer 固定 6 字节
- 前 4 字节是 IPv4
- 后 2 字节是大端序端口号

例如：

- `127 0 0 1 0x1A 0xE1`
  - 表示 `127.0.0.1:6881`

### 5.4 握手帧

BitTorrent 握手格式如下：

```text
<pstrlen><pstr><reserved 8 bytes><info_hash 20 bytes><peer_id 20 bytes>
```

当前实现中：

- `pstr` 固定为 `BitTorrent protocol`
- `reserved` 暂时全 0
- `info_hash` 必须和当前 torrent 一致
- `peer_id` 是本地随机生成的 20 字节标识

如果对端返回的 `info_hash` 不一致，连接会直接失败。

### 5.5 peer 消息帧

普通 peer 消息格式如下：

```text
<length prefix 4 bytes><message id 1 byte><payload>
```

如果 `length prefix = 0`，表示 keepalive，没有 `id` 和 `payload`。

当前实现处理的消息类型如下：

- `0 = choke`
  - 对端暂时不允许我们继续请求数据
- `1 = unchoke`
  - 对端允许我们继续请求数据
- `2 = interested`
  - 我们告诉对端：我对你的数据感兴趣
- `4 = have`
  - 某个 piece 已经完成
- `5 = bitfield`
  - 对端有哪些 piece
- `6 = request`
  - 请求某个 piece 内的一段 block
- `7 = piece`
  - 返回某个 block 的数据

### 5.6 `request` / `piece` 载荷结构

`request` 的 payload 固定 12 字节：

```text
<piece index 4 bytes><begin 4 bytes><length 4 bytes>
```

`piece` 的 payload 格式如下：

```text
<piece index 4 bytes><begin 4 bytes><block bytes>
```

这意味着 piece 并不是一次整块返回的，而是可以被切成多个 block 分批请求。

### 5.7 piece 下载和校验

当前实现的 piece 下载策略是：

1. 先根据 bitfield 判断某个 peer 是否拥有该 piece
2. 如果有，则为该 piece 分配一个缓冲区
3. 按 `BlockSize` 把 piece 切成多个请求
4. 最多同时保留 `PipelineDepth` 个未完成请求
5. 持续读取 `piece` 消息，按 `begin` 偏移把 block 拷回缓冲区
6. piece 完整后做 SHA-1 校验
7. 校验通过后写入目标文件的正确偏移

这个流程能保证：

- 下载是并发的
- 请求是流水线化的
- 写盘前一定做 piece 校验
- 单个 peer 出问题时，piece 可以回退给其他 peer 继续尝试

### 5.8 HTTPS tracker 的 TLS 细节

这是本仓库相对参考仓新增的一部分。

当使用 HTTPS tracker 时，本仓支持两种额外能力：

- `--tracker-cert`
  - 从 PEM 文件中加载额外信任证书
- `--tracker-skip-verify`
  - 显式跳过服务端证书校验

实现策略如下：

- 如果没有证书，且没有要求跳过校验
  - 使用普通 TCP 拨号策略
- 如果传入证书，或者显式要求跳过校验
  - 使用 TLS 拨号策略

这样做的目的，是把 tracker 访问和 peer 下载链路分开处理：

- tracker 访问支持更灵活的 TLS 连接
- peer 下载仍保持当前版本的简洁 TCP 模型

## 6. 与原仓的详细对照

下面两份文档分别承担不同目的：

- [docs/compare-with-veggiedefender.md](/Users/mac/projects/bt-refractor/docs/compare-with-veggiedefender.md)
  - 详细列出原仓模块与本仓模块的逐项对应关系
- [docs/protocol-and-features.md](/Users/mac/projects/bt-refractor/docs/protocol-and-features.md)
  - 更细地拆解协议细节和功能点

## 7. 使用方式

基础用法：

```bash
go run ./cmd/riptide --torrent path/to/file.torrent --out path/to/output.bin
```

也支持位置参数：

```bash
go run ./cmd/riptide path/to/file.torrent path/to/output.bin
```

对 HTTPS tracker 指定证书：

```bash
go run ./cmd/riptide \
  --torrent path/to/file.torrent \
  --out path/to/output.bin \
  --tracker-cert path/to/tracker.pem
```

对 HTTPS tracker 跳过 TLS 校验：

```bash
go run ./cmd/riptide \
  --torrent path/to/file.torrent \
  --out path/to/output.bin \
  --tracker-skip-verify
```

## 8. 验证方式

```bash
go test -count=1 ./...
go build ./cmd/riptide
```
