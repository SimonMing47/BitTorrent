# 协议与功能详解

本文档面向第一次接触这个仓库的读者，目标是用最短路径讲清楚三件事：

- 这个库在解决什么问题
- 它的功能是怎么分层实现的
- 它在 BitTorrent 协议层到底做了哪些事情

## 1. 这个库的定位

这个库是一个“单文件 torrent 下载器”，不是一个完整的 BitTorrent 生态客户端。

它的输入是：

- 一个 `.torrent` 文件
- 一个输出目录路径

它的输出是：

- 下载并校验后的目标文件

最终输出文件名不是由命令行直接给定，而是自动使用 torrent 元数据里的 `name` 字段。

它当前覆盖的是最小可用下载闭环：

1. 解析 `.torrent`
2. 找 tracker
3. 找 peers
4. 跟 peer 建立连接
5. 下载并校验 pieces
6. 写出文件

## 2. 功能分层

### 2.1 `internal/bencode`

这是整个项目最底层的编码工具。

它负责：

- 解析 bencode 整数
- 解析 bencode 字节串
- 解析 bencode 列表
- 解析 bencode 字典
- 把这些值重新编码回 bencode

为什么需要它：

- `.torrent` 文件本身就是 bencode
- tracker 返回也通常是 bencode

### 2.2 `internal/manifest`

这一层负责 torrent 元数据。

它的输入是 `.torrent` 原始字节，输出是 `Manifest`。

`Manifest` 里保存的是下载真正需要的信息：

- tracker 地址
- 文件名
- 文件总长度
- 标准 piece 长度
- 每个 piece 的 SHA-1
- `info_hash`

### 2.3 `internal/discovery`

这一层负责发现 peer。

它做的事情是：

1. 组装 announce URL
2. 发起 HTTP 或 HTTPS 请求
3. 解析 tracker 返回
4. 把 compact peers 转换成 `Endpoint`

它的输出是：

- announce 间隔
- 可连接的 peer 列表

### 2.4 `internal/peerwire`

这一层负责 BitTorrent peer wire 协议的消息格式。

它并不直接做下载调度，只负责“消息长什么样”。

它定义了：

- 握手帧 `Greeting`
- 普通消息帧 `Packet`
- bitfield 位图 `Bitmap`

### 2.5 `internal/engine`

这一层是下载器核心。

它负责：

- 建立 peer 会话
- 根据 bitfield 选择 piece
- 控制 request pipeline
- 收集 block
- 校验 piece
- 把 piece 写到正确文件偏移

## 3. 下载时序

可以把当前下载流程理解成下面这条线：

```text
.torrent -> Manifest -> tracker announce -> peers -> peer handshake
-> bitfield -> interested -> request/piece 循环 -> SHA-1 校验 -> 写盘
```

更细一点是：

1. `cmd/btclient` 读取命令行参数
2. `manifest.Load` 解析 `.torrent`
3. 入口层根据 `-o` 目录和 torrent 的 `name` 组装最终输出文件路径
4. `discovery.HTTPClient.Announce` 找到 peers
5. `engine.Manager.Save` 为 peers 启动 worker
5. `engine.establishSession` 完成握手和 bitfield 读取
6. `peerSession.FetchPiece` 按块请求数据
7. `Manager.runPeer` 对 piece 做校验并写盘

## 4. `.torrent` 协议细节

### 4.1 根字典

当前实现只依赖：

- `announce`
- `info`

### 4.2 `info` 字典

当前实现只支持单文件，因此只关心：

- `name`
- `length`
- `piece length`
- `pieces`

如果发现 `files` 字段，就会判定为多文件 torrent，并直接返回“不支持”。

### 4.3 `pieces`

`pieces` 是一个二进制串，不是人类可读文本。

它的结构是：

- 第 1 个 20 字节：第 0 个 piece 的 SHA-1
- 第 2 个 20 字节：第 1 个 piece 的 SHA-1
- 第 3 个 20 字节：第 2 个 piece 的 SHA-1
- 依次类推

因此，piece 总数就是：

```text
len(pieces) / 20
```

### 4.4 `info_hash`

`info_hash` 的计算方式是：

1. 取出 `info` 字典
2. 对这个字典重新做 bencode 编码
3. 对编码结果做 SHA-1

这一步非常关键，因为：

- tracker announce 要用它
- peer 握手也要用它
- 如果 `info_hash` 不一致，说明根本不是同一个 torrent

## 5. tracker 协议细节

### 5.1 announce 参数

当前实现会带上这些核心参数：

- `info_hash`
- `peer_id`
- `port`
- `uploaded`
- `downloaded`
- `left`
- `compact`

含义如下：

- `info_hash`
  - 当前 torrent 的唯一标识
- `peer_id`
  - 当前客户端的 peer 标识
- `port`
  - 告诉 tracker 当前客户端声称使用的端口
- `uploaded`
  - 已上传字节数，当前实现固定为 0
- `downloaded`
  - 已下载字节数，当前实现固定为 0
- `left`
  - 剩余未完成字节数，当前实现初始为文件总长度
- `compact`
  - 要求 tracker 用 compact peer 格式返回 peers

### 5.2 tracker 返回

当前实现只消费这几个字段：

- `interval`
- `peers`
- `failure reason`

处理方式：

- 有 `failure reason`
  - 直接报错
- 没有 `interval`
  - 报错
- 没有 `peers`
  - 报错

### 5.3 tracker 安全模式

当前实现支持通过证书进入 tracker 安全模式。

命令行参数为：

- `-tls-path`

这部分是通过自定义 `http.Transport` 完成的。

具体分支如下：

- 未配置证书
  - 使用普通模式请求 tracker
- 配置了证书
  - 使用 TLS 拨号请求 tracker

## 6. peer 握手协议细节

握手帧格式：

```text
<pstrlen><pstr><reserved 8 bytes><info_hash 20 bytes><peer_id 20 bytes>
```

字段解释：

- `pstrlen`
  - 协议名称长度
- `pstr`
  - 固定文本 `BitTorrent protocol`
- `reserved`
  - 8 字节保留位
- `info_hash`
  - torrent 标识
- `peer_id`
  - peer 标识

本实现的校验点：

- `pstrlen` 不能为 0
- `pstr` 必须是 `BitTorrent protocol`
- 对端返回的 `info_hash` 必须与本地 torrent 一致

如果 `info_hash` 不一致，说明这个 peer 不是当前 torrent 的正确对端，连接会被丢弃。

## 7. peer 消息协议细节

### 7.1 通用格式

普通消息：

```text
<length prefix 4 bytes><message id 1 byte><payload>
```

keepalive：

```text
<0x00000000>
```

### 7.2 当前实现处理的消息

#### `choke`

- 编号：`0`
- 含义：对端暂时不允许我们继续发请求

#### `unchoke`

- 编号：`1`
- 含义：对端允许我们继续请求 block

#### `interested`

- 编号：`2`
- 含义：我们告诉对端，我对你持有的数据感兴趣

#### `have`

- 编号：`4`
- 含义：某个 piece 已经可用

#### `bitfield`

- 编号：`5`
- 含义：对端当前持有哪些 piece

#### `request`

- 编号：`6`
- payload：

```text
<piece index 4 bytes><begin 4 bytes><length 4 bytes>
```

#### `piece`

- 编号：`7`
- payload：

```text
<piece index 4 bytes><begin 4 bytes><block bytes>
```

## 8. bitfield 细节

`bitfield` 本质上是一个按位表示的 piece 集合。

例如一个字节：

```text
10100000
```

表示前几个 piece 中：

- 第 0 个 piece：有
- 第 1 个 piece：没有
- 第 2 个 piece：有
- 第 3 个 piece：没有

`Bitmap.Contains` 用于判断对端是否有某个 piece，`Bitmap.Mark` 用于把某个 piece 标记成可用。

## 9. piece 下载细节

### 9.1 为什么要按 block 下载

一个 piece 往往比单次网络请求大，因此通常会把 piece 拆成多个 block。

当前实现里：

- `BlockSize` 默认 `16 KiB`
- `PipelineDepth` 默认 `8`

也就是说，一个 peer 在 unchoke 状态下，最多可以在一轮里挂起 8 个未完成请求。

### 9.2 下载过程

对一个 piece，当前实现做的是：

1. 创建一块 piece 缓冲区
2. 不断发送 `request`
3. 读取 `piece` 响应
4. 把返回的 `block` 按 `begin` 偏移写入缓冲区
5. 直到该 piece 所有字节全部收齐

### 9.3 校验与落盘

piece 收齐后不会立刻认为成功，而是：

1. 对 piece 缓冲区做 SHA-1
2. 与 `Manifest.PieceDigests[index]` 对比
3. 一致才写盘
4. 不一致则把 piece 放回待下载状态

这种设计的作用是：

- 防止坏数据污染输出文件
- 防止单个 peer 发来错误 piece 时直接导致结果文件损坏

## 10. 并发调度细节

`engine.Manager` 会为每个 peer 启动一个 worker。

worker 的行为大致是：

1. 先读取该 peer 的 bitfield
2. 从 `catalog` 里领取一个该 peer 真正持有的 piece
3. 下载这个 piece
4. 校验
5. 写盘
6. 继续领取下一个 piece

`catalog` 的作用相当于一个并发安全的任务分发器：

- `waiting`
  - 还没被领取
- `leased`
  - 已经被某个 worker 领取
- `done`
  - 已完成

如果 worker 下载失败，会把 piece 从 `leased` 放回 `waiting`，让别的 peer 继续尝试。

## 11. 当前实现的边界

当前这个库的设计目标是“先把最小闭环下载链路打通”，所以它有明确边界：

- 只支持单文件 torrent
- 只支持 tracker 模式发现 peer
- 只支持 compact peers
- 没有做 seeding
- 没有做上传路径
- 没有做扩展协议协商
- 没有做更复杂的 piece 选择策略

这也是为什么它适合作为一个“协议学习清晰、代码边界明确”的 BT 下载器基础版本。
