# kspeeder-lite

Docker 镜像加速代理 — 多源带宽叠加，开源自部署。

## 特性

- **多源带宽叠加** — 多个 mirror / socks5 代理并发下载，带宽叠加 2~3 倍
- **内置公共 mirror** — 开箱即用 5 个国内可用的 dockerhub mirror
- **双集成模式** — registry-mirror 模式 + CONNECT 代理模式
- **断点续传** — HTTP Range 支持，客户端/分块双层续传
- **节点管理** — 自动测速、健康检查、熔断恢复、负载均衡
- **配置热加载** — 修改 `nodes.yaml` 无需重启
- **Prometheus 指标** — 下载量、节点速度、错误率全监控

## 快速开始

### Docker 部署

```bash
git clone https://github.com/kspeeder/kspeeder-lite.git
cd kspeeder-lite

# 准备配置
mkdir -p docker/config
cp configs/nodes.sample.yaml docker/config/nodes.yaml

# 编辑配置（可选：添加 socks5 代理节点）
vim docker/config/nodes.yaml

# 启动
cd docker && docker compose up -d
docker compose logs -f
```

### dockerd 集成

**方式 A：registry-mirror 模式**

```json
{
  "registry-mirrors": ["https://<host>:5443"],
  "insecure-registries": ["<host>:5443"]
}
```

**方式 B：CONNECT 代理模式（推荐）**

```json
{
  "registry-mirrors": ["https://registry.local"]
}
```

```ini
# /etc/systemd/system/docker.service.d/proxy.conf
[Service]
Environment="HTTP_PROXY=http://<host>:5003"
Environment="HTTPS_PROXY=http://<host>:5003"
Environment="NO_PROXY=127.0.0.1,localhost"
```

### 验证

```bash
docker pull nginx:latest
```

## 项目结构

```
kspeeder-lite/
├── cmd/kspeeder-lite/     # 入口
├── internal/
│   ├── config/            # 配置加载、热加载
│   ├── server/            # Registry HTTPS + CONNECT 代理
│   ├── registry/          # Docker Registry API V2
│   ├── downloader/        # 多源并发下载器
│   ├── nodemgr/           # 节点管理 + 负载均衡
│   ├── auth/              # DockerHub token 获取
│   ├── proxyclient/       # socks5/http 代理拨号
│   ├── tlsutil/           # 自签证书
│   ├── admin/             # 管理 API
│   └── metrics/           # Prometheus 指标
├── configs/               # 配置模板
├── docker/                # Dockerfile + compose
├── scripts/               # 构建脚本
└── test/                  # 测试
```

## 管理 API

| 端点 | 说明 |
|------|------|
| `GET /admin/nodes` | 节点列表 + 状态 |
| `POST /admin/nodes/{id}/test` | 触发测速 |
| `GET /admin/stats` | 全局统计 |
| `POST /admin/config/reload` | 手动重载配置 |
| `GET /metrics` | Prometheus 指标 |
| `GET /healthz` | 健康检查 |

## 开发

```bash
# 本地运行
go run ./cmd/kspeeder-lite -config configs/nodes.sample.yaml

# 构建
bash scripts/build.sh

# 测试
bash scripts/test.sh
```

## License

MIT
