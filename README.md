# bt-refractor

`bt-refractor` 是一个用 Go 从零实现的 BitTorrent 单文件下载客户端。当前仓库面向数据中心式部署环境做了取舍：优先保证下载链路简单、吞吐直接、运行稳定，同时保留在需要时重新打开完整性校验的能力。

编译后的二进制名称为 `btclient`。

## 1. 当前能力

当前仓库已经覆盖一条完整的单文件 BT 下载闭环：

- 读取 `.torrent` 文件
- 解析单文件 torrent 的 `announce`、`info`、`name`、`length`、`piece length`、`pieces`
- 计算 `info_hash`
- 向 HTTP tracker 发起 announce
- 在提供证书时向 HTTPS tracker 发起安全请求
- 解析 compact peers
- 与 peer 建立 TCP 会话
- 完成 BitTorrent 握手
- 处理 `bitfield`、`interested`、`choke`、`unchoke`、`request`、`piece`、`have`
- 将下载数据直接写入目标文件
- 在需要时按 piece 做 SHA-1 校验

当前明确不支持：

- magnet
- UDP tracker
- 多文件 torrent
- seeding / uploading
- DHT / PEX / 扩展协议

## 2. 命令行

命令行只保留 3 个参数：

- `-i`
  - `.torrent` 文件路径
- `-o`
  - 输出根路径
- `-tls-path`
  - tracker 安全请求使用的 PEM 证书路径

示例：

```bash
go build -o btclient ./cmd/btclient
./btclient -i /data/job/input.torrent -o /data/output
```

如果 torrent 的 `name` 是 `debian.iso`，最终文件路径就是：

```text
/data/output/debian.iso
```

如果 torrent 的 `name` 本身带相对子路径，例如 `images/debian.iso`，最终文件路径就是：

```text
/data/output/images/debian.iso
```

也就是说，`-o` 只提供输出根路径，最终文件名严格来自 torrent 元数据。

## 3. tracker 访问模式

tracker 访问有两种模式：

1. 普通模式
   - 不传 `-tls-path`
   - 只允许请求非 TLS tracker
   - 连接层使用普通 TCP 拨号
2. 安全模式
   - 传入 `-tls-path`
   - 读取 PEM 证书并建立 TLS 请求
   - 适用于 HTTPS tracker

因此：

- 没有证书时
  - 走非安全模式
- 传入证书时
  - 走安全模式

## 4. 数据中心默认策略

当前实现默认按“可信网络、优先吞吐”的思路运行：

- 提高了 request pipeline 深度
- 降低了空闲轮询等待
- 把高频 piece 成功日志改成批量进度日志
- 默认不对每个 piece 做 SHA-1 校验

这样做的目的，是把 CPU 和日志 IO 尽量让给真实下载流量。

如果你要在更强调完整性的环境里运行，可以显式打开 piece 校验：

```bash
BTCLIENT_VERIFY_PIECES=1 ./btclient -i /data/job/input.torrent -o /data/output
```

## 5. 一次下载在做什么

一次完整下载的执行路径如下：

1. `cmd/btclient` 读取参数并生成 `peer_id`
2. `internal/manifest` 解析 `.torrent`
3. 基于 torrent 的 `name` 计算最终输出文件路径
4. `internal/discovery` 向 tracker announce，得到 peers
5. `internal/engine` 为每个 peer 启动一个 worker
6. worker 完成握手，读取 bitfield，发送 `interested`
7. peer `unchoke` 之后，worker 按 pipeline 连续发送 `request`
8. peer 返回 `piece` 后，客户端下载 block 并按偏移拼装
9. 数据直接写入目标文件
10. 如果启用了 `BTCLIENT_VERIFY_PIECES=1`，每个 piece 在写盘前会先做 SHA-1 校验
11. 全部 piece 完成后，下载结束

## 6. 仓库结构

- `cmd/btclient`
  - CLI 入口，参数解析、环境变量读取、启动下载流程
- `internal/bencode`
  - 最小化 bencode 编解码器
- `internal/manifest`
  - `.torrent` 解析、`info_hash` 计算、piece 边界计算
- `internal/discovery`
  - tracker announce、compact peers 解码、TCP/TLS 拨号策略
- `internal/peerwire`
  - 握手帧、普通消息帧、bitfield 位图
- `internal/engine`
  - peer 会话、piece 调度、文件写入、可选 piece 校验

## 7. 协议要点

这个仓库只实现当前下载流程真正需要的 BitTorrent 子集：

### 7.1 `.torrent`

关心的字段只有：

- 根字典
  - `announce`
  - `info`
- `info` 字典
  - `name`
  - `length`
  - `piece length`
  - `pieces`

其中 `pieces` 是一串连续的 SHA-1 摘要，每 20 个字节对应一个 piece。

### 7.2 `info_hash`

计算方式是：

1. 取出 `info` 字典
2. 重新做 bencode 编码
3. 对编码结果做 SHA-1

tracker announce 和 peer 握手都依赖这个值。

### 7.3 tracker announce

当前使用的参数包括：

- `info_hash`
- `peer_id`
- `port`
- `uploaded`
- `downloaded`
- `left`
- `compact`

tracker 返回里当前只消费：

- `interval`
- `peers`
- `failure reason`

### 7.4 peer wire

握手之后，当前会处理这些消息：

- `bitfield`
- `interested`
- `choke`
- `unchoke`
- `request`
- `piece`
- `have`

下载过程中，worker 会在 peer 允许发送数据后持续保持多个 block request 在飞，以降低 RTT 对吞吐的影响。

## 8. 测试

当前测试覆盖包括：

- `internal/bencode`
  - bencode 编解码单元测试
- `internal/manifest`
  - torrent 解析、`info_hash`、piece span 单元测试
- `internal/discovery`
  - tracker announce、TLS 证书、HTTPS 限制、compact peers 单元测试
- `internal/peerwire`
  - 握手帧、消息帧、bitfield 单元测试
- `internal/engine`
  - piece 调度和 peer 会话单元测试
- `workflow_integration_test.go`
  - 本地 fake tracker + fake peer 的端到端测试

## 9. 文档

- [原始仓对比文档](docs/compare-with-original.md)
- [协议与功能详解](docs/protocol-and-features.md)

## 10. License

本仓库使用 `0BSD`，属于极宽松协议。

它的含义可以简单理解为：

- 可商用
- 可修改
- 可分发
- 可私有集成
- 不要求署名

