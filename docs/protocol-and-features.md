# 协议与功能详解

本文档用于快速说明这个仓库到底在做什么、协议上实现了哪些内容、为什么默认行为会偏向数据中心环境。

## 1. 这个库的定位

这个仓库是一个单文件 torrent 下载器。

它的输入是：

- `.torrent` 文件
- 输出根路径

它的输出是：

- 基于 torrent 元数据 `name` 生成最终文件路径
- 下载完成的目标文件

这个仓库不是一个完整生态客户端，因此没有实现：

- magnet
- DHT
- PEX
- UDP tracker
- 多文件 torrent
- seeding

## 2. 功能分层

### 2.1 `internal/bencode`

这一层提供最小 bencode 能力：

- 解析整数
- 解析字节串
- 解析列表
- 解析字典
- 将这些值重新编码

`.torrent` 本身和 tracker 响应都依赖这一层。

### 2.2 `internal/manifest`

这一层负责 torrent 元数据解析。

它把 `.torrent` 变成 `Manifest`，其中最关键的字段有：

- `Announce`
- `Name`
- `TotalLength`
- `StandardPieceLength`
- `PieceDigests`
- `InfoHash`

### 2.3 `internal/discovery`

这一层负责 peer 发现。

它负责：

1. 组装 announce URL
2. 请求 tracker
3. 解析 tracker bencode 响应
4. 解 compact peers

### 2.4 `internal/peerwire`

这一层负责 BitTorrent peer wire 协议的消息格式。

它定义了：

- 握手帧 `Greeting`
- 普通消息帧 `Packet`
- bitfield 位图 `Bitmap`

### 2.5 `internal/engine`

这一层是下载调度核心。

它负责：

- 建立 peer 会话
- 管理 piece 领取和归还
- 按 pipeline 连续请求 block
- 收集 piece 数据
- 可选做 SHA-1 校验
- 写盘

## 3. 输出路径规则

命令行里：

- `-o` 只表示输出根路径

最终文件名不由命令行给出，而是直接取 torrent `name`。

因此：

```text
btclient -i /jobs/a.torrent -o /data/out
```

如果 torrent 的 `name` 是 `release.iso`，结果就是：

```text
/data/out/release.iso
```

如果 torrent 的 `name` 是 `images/release.iso`，结果就是：

```text
/data/out/images/release.iso
```

为了不让 torrent 元数据跳出输出根路径，当前实现会拒绝绝对路径和 `..` 路径穿越。

## 4. `.torrent` 协议细节

### 4.1 根字典

当前实现只消费：

- `announce`
- `info`

### 4.2 `info` 字典

当前实现只支持单文件模式，因此只消费：

- `name`
- `length`
- `piece length`
- `pieces`

如果 `info` 中出现 `files`，当前实现会直接判定为多文件 torrent，不继续执行。

### 4.3 `pieces`

`pieces` 是连续的 SHA-1 摘要字节串，不是普通文本。

规则是：

- 每 20 个字节表示一个 piece 的 SHA-1
- piece 总数等于 `len(pieces) / 20`

### 4.4 `info_hash`

`info_hash` 的计算方式是：

1. 取出 `info` 字典
2. 对 `info` 重新做 bencode 编码
3. 对编码结果做 SHA-1

tracker announce 和 peer 握手都依赖它。

## 5. tracker 协议细节

### 5.1 announce 参数

当前实现会带上这些参数：

- `info_hash`
- `peer_id`
- `port`
- `uploaded`
- `downloaded`
- `left`
- `compact`

语义分别是：

- `info_hash`
  - 当前 torrent 的唯一标识
- `peer_id`
  - 当前客户端标识
- `port`
  - 当前客户端对外声称的端口
- `uploaded`
  - 已上传字节数
- `downloaded`
  - 已下载字节数
- `left`
  - 剩余未完成字节数
- `compact`
  - 请求 tracker 用 compact 格式返回 peers

### 5.2 tracker 返回

当前实现只消费：

- `interval`
- `peers`
- `failure reason`

### 5.3 普通模式与安全模式

tracker 请求分成两类：

1. 普通模式
   - 不提供 `-tls-path`
   - 使用普通 TCP 拨号
   - 仅允许非 HTTPS tracker
2. 安全模式
   - 提供 `-tls-path`
   - 读取 PEM 证书
   - 通过 TLS 拨号访问 HTTPS tracker

这条规则保证了“有证书走安全模式，没有证书走非安全模式”。

## 6. peer 握手细节

握手帧格式是：

```text
<pstrlen><pstr><reserved><info_hash><peer_id>
```

其中：

- `pstr`
  - 固定为 `BitTorrent protocol`
- `reserved`
  - 8 字节保留位
- `info_hash`
  - 当前 torrent 标识
- `peer_id`
  - 当前 peer 标识

当前实现会在握手后立即检查：

- 对端返回的 `info_hash` 是否一致

如果不一致，连接直接失败。

## 7. peer wire 消息细节

当前实现会处理这些消息：

- `bitfield`
- `interested`
- `choke`
- `unchoke`
- `request`
- `piece`
- `have`

下载流程中最关键的是：

1. 先读取对端 bitfield
2. 发送 `interested`
3. 等待 `unchoke`
4. 连续发送多个 `request`
5. 读取 `piece`
6. 将 block 拷入目标 piece 缓冲区

## 8. piece 调度细节

当前实现中的调度由 `catalog` 管理。

每个 piece 只有 3 种状态：

- `waiting`
- `leased`
- `done`

流程是：

1. worker 只从当前 peer 已拥有的 piece 中领取任务
2. 领取后状态从 `waiting` 变为 `leased`
3. 下载失败时，piece 放回 `waiting`
4. 下载成功并写盘后，状态变为 `done`

## 9. 数据中心默认性能策略

当前仓库默认假设部署环境满足两点：

- peer 相对可信
- 吞吐比逐 piece 的完整性校验更重要

因此默认策略是：

- 更高的 request pipeline 深度
- 更短的空闲等待
- 更少的高频日志
- 默认关闭每 piece SHA-1 校验

这样做的收益是：

- 降低 CPU 消耗
- 降低日志 IO
- 提高多 peer 并发下的有效吞吐

## 10. 如何重新打开完整性校验

如果你不在可信数据中心环境里运行，或者要更强的落盘前校验，可以显式打开：

```bash
BTCLIENT_VERIFY_PIECES=1 ./btclient -i /data/job/input.torrent -o /data/out
```

打开之后，当前实现会在每个 piece 写盘前执行 SHA-1 校验。

## 11. 当前测试是如何覆盖这些能力的

测试分成三层：

1. 协议级单元测试
   - `internal/bencode`
   - `internal/manifest`
   - `internal/discovery`
   - `internal/peerwire`
2. 会话与调度级单元测试
   - `internal/engine/downloader_test.go`
   - `internal/engine/peerlink_test.go`
3. 本地端到端测试
   - `workflow_integration_test.go`
   - 使用 fake tracker + fake peer 跑完整下载链路

其中端到端测试显式打开了 piece 校验路径，用于确保“快速模式之外的完整性能力”仍然是可用的。

