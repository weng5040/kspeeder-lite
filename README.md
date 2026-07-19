# PullFusion

> **Docker 镜像多源带宽叠加代理** — 从 `status.anye.xyz` 自动获取免费加速节点，让你的 `docker pull` 快如闪电。

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License">
  <img src="https://img.shields.io/badge/Version-1.0.0-green.svg" alt="Version">
</p>

## 特性

- **零配置启动** — 首次启动自动从 `status.anye.xyz` 抓取免费节点，持久化到本地 JSON
- **自动测速** — 5 分钟间隔对所有节点测速排序，下载时自动择优
- **故障熔断** — 节点超时自动标记不可用，恢复后自动启用
- **Web 管理面板** — 内置 Dashboard，一键刷新节点、查看状态
- **TLS 双模式** — 自签证书开箱即用，支持 Let's Encrypt 真证书
- **多 registry** — 同时支持 Docker Hub + GHCR

---

## 快速部署

### 方式一：Docker（推荐）

```bash
git clone https://github.com/weng5040/PullFusion.git
cd PullFusion
docker compose -f docker/docker-compose.yml up -d
```

首次启动会自动抓取 20+ 免费节点，查看日志：

```bash
docker compose logs -f
# 看到 "auto-fetch complete added=18 total=24" 即就绪
```

访问管理面板：`http://<服务器IP>:5003`

### 方式二：二进制部署

```bash
# 编译
cd PullFusion && go build -o bin/pullfusion ./cmd/pullfusion/

# 首次启动（零配置，自动抓取节点）
./bin/pullfusion -config configs/nodes.sample.yaml
```

---

## Docker 客户端配置

部署完 PullFusion 后，在所有需要加速的机器上配置 Docker 客户端。

> **注意：** 你需要知道 PullFusion 服务器的 IP（以下用 `PULLFUSION_IP` 代替）。

### 方案 A：IP 直连（最简单）

```bash
cat > /etc/docker/daemon.json << 'EOF'
{
    "registry-mirrors": ["https://PULLFUSION_IP:5443"],
    "insecure-registries": ["PULLFUSION_IP:5443"]
}
EOF
systemctl restart docker
```

### 方案 B：域名 + 真证书（无需 insecure-registries）

如果你有域名（如 `docker.your-domain.com`），可以申请 Let's Encrypt 证书：

```bash
# 1. DNS 指向 PullFusion 服务器
#    docker.your-domain.com → A → <PULLFUSION_IP>

# 2. 安装 certbot
apt install -y certbot python3-certbot-dns-cloudflare  # CloudFlare 用户
# 或
apt install -y certbot                                     # 命令行手动验证

# 3. 申请证书
certbot certonly --manual --preferred-challenges dns -d docker.your-domain.com
# 按照提示在 DNS 添加 TXT 记录

# 4. 复制到 PullFusion 的 data 目录
cp /etc/letsencrypt/live/docker.your-domain.com/fullchain.pem data/cert.crt
cp /etc/letsencrypt/live/docker.your-domain.com/privkey.pem data/cert.key

# 5. 修改 configs/nodes.sample.yaml
#    server.tls.cert: data/cert.crt
#    server.tls.key: data/cert.key
#    server.registry_domain: docker.your-domain.com

# 6. 重启 PullFusion
systemctl restart pullfusion

# 7. Docker 客户端只需一行配置（不需要 insecure-registries！）
cat > /etc/docker/daemon.json << 'EOF'
{"registry-mirrors":["https://docker.your-domain.com:5443"]}
EOF
systemctl restart docker
```

### 方案 C：域名 + /etc/hosts（无公网 DNS）

如果域名不能改 DNS，在内网机器上用 `/etc/hosts`：

```bash
echo '<PULLFUSION_IP> docker.your-domain.com' >> /etc/hosts
```

---

## 验证

```bash
docker pull alpine:latest
docker pull python:latest
```

正常应该看到各层以 `Download complete` 完成。

---

## 域名证书续期

### 自签证书（默认）

自动生成，有效期 1 年，保存在 `data/` 目录，每次重启复用。无需手动续期。

### Let's Encrypt 证书

certbot 安装后会自动注册定时任务，执行：

```bash
# 查看续期状态
certbot renew --dry-run

# 手动续期
certbot renew

# 续期后重载 PullFusion
cp /etc/letsencrypt/live/<域名>/fullchain.pem data/cert.crt
cp /etc/letsencrypt/live/<域名>/privkey.pem data/cert.key
systemctl restart pullfusion
```

也可以写个 cron 任务自动化：

```bash
# /etc/cron.daily/pullfusion-cert
#!/bin/sh
certbot renew --quiet
cp /etc/letsencrypt/live/docker.your-domain.com/fullchain.pem /opt/pullfusion/data/cert.crt
cp /etc/letsencrypt/live/docker.your-domain.com/privkey.pem /opt/pullfusion/data/cert.key
systemctl restart pullfusion
```

---

## Dashboard 使用

访问 `http://<PULLFUSION_IP>:5003`

| 功能 | 说明 |
|------|------|
| 查看节点 | 节点列表、速度、状态（🟢在线 ⚪离线） |
| 获取免费节点 | 点击紫色按钮，从 `status.anye.xyz` 拉取最新公开镜像源 |
| 刷新配置 | 修改 `nodes.yaml` 后点击热加载 |

---

## 配置文件

`configs/nodes.sample.yaml` 默认零配置，所有节点自动抓取：

```yaml
mirrors:
  dockerhub: []   # 留空，启动时自动填充
  ghcr: []        # 留空

server:
  registry_port: 5443
  proxy_port: 5003
  registry_domain: registry.local  # 自签证书域名
  tls:
    cert: ''      # 留空则自动生成自签证书
    key: ''
```

### 手动添加节点

```yaml
mirrors:
  dockerhub:
    - url: https://docker.your-mirror.com
      priority: 10          # 越小越优先
      display_name: 我的镜像源
```

---

## 管理 API

| 端点 | 方法 | 说明 |
|------|------|------|
| `/healthz` | GET | 健康检查 |
| `/admin/nodes` | GET | 节点列表 + 状态 |
| `/admin/nodes/fetch` | POST | 从 `status.anye.xyz` 抓取免费节点 |
| `/admin/nodes/{id}/test` | POST | 触发单节点测速 |
| `/admin/stats` | GET | 全局统计 |
| `/admin/config/reload` | POST | 热加载配置 |
| `/dashboard` | GET | Web 管理面板 |
| `/metrics` | GET | Prometheus 指标 |

---

## 架构

```
status.anye.xyz ──→ 自动抓取 ──→ data/nodes.json (持久化)
                                      │
                                      ▼
                           ┌── nodemgr (节点管理)
                           │     ├── 自动测速排序
                           │     └── 故障熔断恢复
                           │
Docker 客户端 ──→ PullFusion :5443
                   ├── /v2/         → 版本握手
                   ├── manifest     → docker.1ms.run 内部 token + 代理
                   └── blob         → docker.1ms.run 代理下载
```

---

## FAQ

**Q: 为什么需要 `insecure-registries`？**
A: 默认使用自签证书。配置 Let's Encrypt 真证书后可去掉。

**Q: 节点会持久化吗？**
A: 会。默认保存在 `data/nodes.json`，重启不丢失。

**Q: 节点多久刷新一次？**
A: 启动时如果没数据会自动抓取。运行中可通过 Dashboard "获取免费节点" 手动刷新。

**Q: 和 KSpeeder 有什么区别？**
A: PullFusion 是开源版，节点从公开聚合站自动获取，完全零配置。KSpeeder 需要域名和内置证书。

---

## License

MIT
