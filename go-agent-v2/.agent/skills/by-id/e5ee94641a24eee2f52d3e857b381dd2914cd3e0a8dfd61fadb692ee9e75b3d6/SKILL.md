---
name: Docker 容器化部署
description: QUANT_MASTER 量化交易平台 Docker 容器化与编排指南，涵盖多阶段构建、金融级 HA 基础设施、Swarm 部署和生产优化。
tags: [docker, container, deployment, devops, swarm, 容器化, Docker, 部署, 运维, DevOps, docker-compose, ha, TimescaleDB, etcd, NATS, Prometheus, Grafana]
---

# Docker 容器化部署

QUANT_MASTER 量化交易平台的 Docker 容器化与编排规范指南。

> 📂 **项目部署目录**：`deploy/`（Dockerfile、docker-compose、swarm、nginx）

## 何时使用

在以下场景使用此技能：
- 构建后端/前端 Docker 镜像
- 配置本地开发环境 (`docker-compose.yml`)
- 部署金融级高可用基础设施 (`docker-compose.ha.yml`)
- 部署到 Docker Swarm 集群 (`docker-stack.yml`)
- 配置 Nginx 反向代理
- 排查容器问题

---

## 第一部分：项目 Docker 结构

```
deploy/
├── Dockerfile.backend          # Go 后端多阶段构建 ⭐
├── Dockerfile.frontend         # 前端多阶段构建 ⭐
├── docker-compose.ha.yml       # 金融级 HA 基础设施 ⭐
├── prometheus.yml              # Prometheus 监控配置
├── local/
│   ├── docker-compose.yml      # 本地开发环境
│   ├── docker-compose.dev.yml  # 热重载开发
│   ├── deploy.sh               # 部署脚本
│   └── init-db/                # 数据库初始化
├── nginx/
│   ├── nginx.conf              # Nginx 主配置
│   ├── default.conf            # 虚拟主机配置
│   └── api.conf                # API 反向代理
├── swarm/
│   └── docker-stack.yml        # Swarm 生产部署
└── chaos/                      # 混沌工程测试
```

---

## 第二部分：后端 Dockerfile

> 📂 **文件**：`deploy/Dockerfile.backend`

### 2.1 多阶段构建（Go 1.24）

```dockerfile
# deploy/Dockerfile.backend

# ============================================
# 阶段 1: 构建阶段 - 使用 Debian 镜像手动安装 Go 1.24
# ============================================
FROM debian:bookworm-slim AS builder

# 安装构建依赖
RUN apt-get update && apt-get install -y --no-install-recommends \
    git ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

# 下载并安装 Go 1.24（根据架构自动选择）
ARG TARGETARCH
RUN case "${TARGETARCH}" in \
        "arm64") GOARCH="arm64" ;; \
        "amd64") GOARCH="amd64" ;; \
        *) GOARCH="arm64" ;; \
    esac && \
    curl -fsSL "https://go.dev/dl/go1.24.2.linux-${GOARCH}.tar.gz" -o go.tar.gz && \
    tar -C /usr/local -xzf go.tar.gz && \
    rm go.tar.gz

# 设置 Go 环境变量
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GO111MODULE=on
ENV GOPROXY=https://goproxy.cn,https://goproxy.io,direct

WORKDIR /build

# 复制 go.mod 和 go.sum，利用 Docker 缓存
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# 复制源代码并编译
COPY backend/ ./

ARG VERSION=1.0.0
ARG BUILD_TIME
ARG GIT_COMMIT
ARG TARGETOS=linux

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}" \
    -o /build/quant-server \
    ./cmd/server/main.go

# ============================================
# 阶段 2: 运行阶段
# ============================================
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata curl

ENV TZ=Asia/Shanghai

# 创建非 root 用户
RUN addgroup -g 1000 quant && \
    adduser -u 1000 -G quant -s /bin/sh -D quant

RUN mkdir -p /app/data /app/logs /app/config && \
    chown -R quant:quant /app

WORKDIR /app

COPY --from=builder /build/quant-server /app/quant-server

USER quant

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/quant-server"]
CMD ["-config", "/app/config/config.yaml"]
```

### 2.2 构建命令

```bash
# 单架构构建
docker build -t quant-backend:v1 -f deploy/Dockerfile.backend .

# 多平台构建（ARM64 + AMD64）
docker buildx build \
    --platform linux/amd64,linux/arm64 \
    --build-arg VERSION=$(git describe --tags) \
    --build-arg BUILD_TIME=$(date -u +%Y%m%d-%H%M%S) \
    --build-arg GIT_COMMIT=$(git rev-parse --short HEAD) \
    -t registry.example.com/quant-backend:v1 \
    -f deploy/Dockerfile.backend \
    --push .
```

---

## 第三部分：前端 Dockerfile

> 📂 **文件**：`deploy/Dockerfile.frontend`

```dockerfile
# deploy/Dockerfile.frontend

# 阶段 1: 依赖安装
FROM node:22-alpine AS deps
RUN corepack enable && corepack prepare pnpm@10.4.1 --activate
WORKDIR /app
COPY client/package.json client/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# 阶段 2: 构建
FROM node:22-alpine AS builder
RUN corepack enable && corepack prepare pnpm@10.4.1 --activate
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY client/ ./

ARG VITE_API_URL=/api
ARG VITE_WS_URL=/ws
ENV VITE_API_URL=${VITE_API_URL}
ENV VITE_WS_URL=${VITE_WS_URL}

RUN pnpm build

# 阶段 3: 生产运行
FROM nginx:alpine
COPY deploy/nginx/nginx.conf /etc/nginx/nginx.conf
COPY deploy/nginx/default.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html

RUN chown -R nginx:nginx /usr/share/nginx/html && \
    chmod -R 755 /usr/share/nginx/html

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost/health || exit 1

CMD ["nginx", "-g", "daemon off;"]
```

---

## 第四部分：本地开发环境

> 📂 **文件**：`deploy/local/docker-compose.yml`

### 4.1 完整配置

```yaml
# deploy/local/docker-compose.yml
version: '3.8'

services:
  # MySQL 数据库
  mysql:
    image: mysql:8.0
    platform: linux/arm64  # Intel 用 linux/amd64
    container_name: wjboot-mysql
    restart: unless-stopped
    environment:
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD:-root123456}
      MYSQL_DATABASE: ${MYSQL_DATABASE:-quant_trading}
      MYSQL_USER: ${MYSQL_USER:-quant}
      MYSQL_PASSWORD: ${MYSQL_PASSWORD:-quant123456}
      TZ: Asia/Shanghai
    ports:
      - "${MYSQL_PORT:-3306}:3306"
    volumes:
      - mysql_data:/var/lib/mysql
      - ./init-db:/docker-entrypoint-initdb.d
    command:
      - --character-set-server=utf8mb4
      - --collation-server=utf8mb4_unicode_ci
      - --max_connections=1000
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 60s
    networks:
      - wjboot-network

  # Redis 缓存
  redis:
    image: redis:7-alpine
    container_name: wjboot-redis
    restart: unless-stopped
    ports:
      - "${REDIS_PORT:-6379}:6379"
    volumes:
      - redis_data:/data
    command: redis-server --appendonly yes --requirepass ${REDIS_PASSWORD:-redis123456}
    healthcheck:
      test: ["CMD", "redis-cli", "-a", "${REDIS_PASSWORD:-redis123456}", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
    networks:
      - wjboot-network

  # 后端服务
  backend:
    build:
      context: ../..
      dockerfile: deploy/Dockerfile.backend
      args:
        VERSION: ${VERSION:-1.0.0}
        BUILD_TIME: ${BUILD_TIME:-unknown}
        GIT_COMMIT: ${GIT_COMMIT:-unknown}
    container_name: wjboot-backend
    restart: unless-stopped
    environment:
      QUANT_APP_ENVIRONMENT: development
      QUANT_DATABASE_HOST: mysql
      QUANT_DATABASE_PORT: 3306
      QUANT_REDIS_HOST: redis
      QUANT_REDIS_PORT: 6379
      TZ: Asia/Shanghai
    ports:
      - "${BACKEND_PORT:-8080}:8080"
    volumes:
      - backend_data:/app/data
      - backend_logs:/app/logs
      - ./config:/app/config:ro
    depends_on:
      mysql:
        condition: service_healthy
      redis:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
    networks:
      - wjboot-network

  # 前端服务
  frontend:
    build:
      context: ../..
      dockerfile: deploy/Dockerfile.frontend
      args:
        VITE_API_URL: /api
        VITE_WS_URL: /ws
    container_name: wjboot-frontend
    restart: unless-stopped
    ports:
      - "${FRONTEND_PORT:-80}:80"
    depends_on:
      - backend
    healthcheck:
      test: ["CMD", "wget", "--spider", "http://localhost/health"]
      interval: 30s
      timeout: 10s
      retries: 3
    networks:
      - wjboot-network

networks:
  wjboot-network:
    driver: bridge

volumes:
  mysql_data:
  redis_data:
  backend_data:
  backend_logs:
```

### 4.2 常用命令

```bash
# 启动本地环境
cd deploy/local
docker-compose up -d

# 查看日志
docker-compose logs -f backend

# 重建后端
docker-compose up -d --build backend

# 停止并清理
docker-compose down -v
```

---

## 第五部分：金融级 HA 基础设施

> 📂 **文件**：`deploy/docker-compose.ha.yml`
>
> **组件**：TimescaleDB + MySQL + Redis + etcd 3 节点 + NATS JetStream + Prometheus + Grafana

### 5.1 基础设施拓扑

```
┌─────────────────────────────────────────────────────────────┐
│                    金融级高可用基础设施                        │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │ TimescaleDB  │  │   MySQL 8.0  │  │  Redis 7     │       │
│  │ (K 线时序)    │  │ (业务数据)    │  │ (缓存/会话)   │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────────────────────────────────────────────┐   │
│  │                   etcd 3 节点集群                      │   │
│  │      etcd1 ◀───▶ etcd2 ◀───▶ etcd3 (Raft 共识)       │   │
│  └──────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │ NATS         │  │ Prometheus   │  │  Grafana     │       │
│  │ JetStream    │  │ (监控)        │  │ (可视化)     │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
└─────────────────────────────────────────────────────────────┘
```

### 5.2 etcd 3 节点集群配置

```yaml
# deploy/docker-compose.ha.yml 片段

# etcd 节点 1
etcd1:
  image: quay.io/coreos/etcd:v3.5.14
  container_name: quant_etcd1
  command:
    - etcd
    - --name=etcd1
    - --data-dir=/etcd-data
    - --initial-advertise-peer-urls=http://etcd1:2380
    - --listen-peer-urls=http://0.0.0.0:2380
    - --advertise-client-urls=http://etcd1:2379
    - --listen-client-urls=http://0.0.0.0:2379
    - --initial-cluster=etcd1=http://etcd1:2380,etcd2=http://etcd2:2380,etcd3=http://etcd3:2380
    - --initial-cluster-state=new
    - --initial-cluster-token=quant-etcd-cluster
  volumes:
    - etcd1_data:/etcd-data
  restart: unless-stopped

# etcd 节点 2、3 类似配置...
```

### 5.3 NATS JetStream 配置

```yaml
nats:
  image: nats:2.10-alpine
  container_name: quant_nats
  ports:
    - "4222:4222"   # 客户端端口
    - "8222:8222"   # 监控端口
  command: -js -sd /data
  volumes:
    - nats_data:/data
  restart: unless-stopped
```

### 5.4 启动 HA 基础设施

```bash
# 启动金融级基础设施
docker-compose -f deploy/docker-compose.ha.yml up -d

# 验证 etcd 集群状态
docker exec quant_etcd1 etcdctl member list

# 验证 NATS JetStream
curl http://localhost:8222/jsz

# 访问 Grafana
open http://localhost:3000  # admin/admin
```

---

## 第六部分：Swarm 生产部署

> 📂 **文件**：`deploy/swarm/docker-stack.yml`

### 6.1 部署命令

```bash
# 初始化 Swarm
docker swarm init --advertise-addr <MANAGER-IP>

# 添加工作节点
docker swarm join-token worker

# 部署服务栈
docker stack deploy -c deploy/swarm/docker-stack.yml quant

# 查看服务状态
docker service ls
docker service ps quant_backend

# 扩缩容
docker service scale quant_backend=5

# 更新服务
docker service update --image quant-backend:v2 quant_backend

# 回滚
docker service rollback quant_backend
```

### 6.2 滚动更新配置

```yaml
deploy:
  replicas: 3
  update_config:
    parallelism: 1      # 每次更新 1 个实例
    delay: 10s          # 更新间隔
    failure_action: rollback
    monitor: 30s        # 监控时间
  rollback_config:
    parallelism: 1
    delay: 10s
```

---

## 第七部分：常用命令速查

### 镜像管理

```bash
# 构建
docker build -t quant-backend:v1 -f deploy/Dockerfile.backend .

# 多平台
docker buildx build --platform linux/amd64,linux/arm64 -t quant-backend:v1 --push .

# 清理
docker image prune -f
docker system prune -a --volumes
```

### 容器调试

```bash
# 进入容器
docker exec -it wjboot-backend /bin/sh

# 查看日志
docker logs -f --tail 100 wjboot-backend

# 资源使用
docker stats wjboot-backend

# 网络调试
docker run --rm --network container:wjboot-backend nicolaka/netshoot

# 复制文件
docker cp wjboot-backend:/app/logs ./logs
```

### 数据备份

```bash
# 备份 MySQL
docker exec wjboot-mysql mysqldump -u root -proot123456 quant_trading > backup.sql

# 备份卷
docker run --rm -v mysql_data:/data -v $(pwd):/backup alpine \
  tar cvzf /backup/mysql-backup.tar.gz /data
```

---

## 生产清单

### 构建阶段
- [ ] 使用多阶段构建（`Dockerfile.backend`、`Dockerfile.frontend`）
- [ ] 镜像基于最小基础镜像（alpine）
- [ ] Go 1.24 + ARM64/AMD64 双架构支持
- [ ] .dockerignore 已配置
- [ ] 版本号通过 build-arg 注入

### 安全
- [ ] 非 root 用户运行（`quant:quant`）
- [ ] 敏感信息使用环境变量或 secrets
- [ ] 只暴露必要端口
- [ ] 镜像已扫描漏洞

### 运行时
- [ ] 健康检查已配置（`/health` 端点）
- [ ] 资源限制已设置（CPU、Memory）
- [ ] 日志输出到 stdout/stderr
- [ ] 重启策略：`unless-stopped` 或 `on-failure`

### HA 基础设施
- [ ] etcd 3 节点集群已部署
- [ ] NATS JetStream 持久化已启用
- [ ] TimescaleDB K 线存储已配置
- [ ] Prometheus + Grafana 监控就绪

### 部署
- [ ] 滚动更新策略已配置
- [ ] 回滚策略已测试
- [ ] 备份脚本已就绪

---

> 📚 **相关技能**：[架构设计](../架构设计/SKILL.md) | [运维工程师规范](../运维工程师/SKILL.md) | [Go后端](../后端/SKILL.md)

**技能版本**: 3.0.0  
**最后更新**: 2026-01


---

## ⚠️ 强制输出 Token 空间

> **重要规则**：使用此技能时，必须在每次重要输出前检查上下文空间。

### 输出规范

所有对话回复内容都要输出

### 输出格式

```
📊 剩余上下文空间: ~{百分比}%
```

### 告警与自动保存

**当剩余上下文空间 ≤ 30%（即已使用 ≥ 70%）时，必须执行：**

1. **立即暂停当前工作**
2. **保存工作进度**：创建 `.agent/workflows/checkpoint-{timestamp}.md`
3. **通知用户**：
   ```
   ⚠️ 上下文空间即将耗尽 (剩余 ~{百分比}%)
   📋 工作进度已保存至: .agent/workflows/checkpoint-{timestamp}.md
   请检查后决定是否继续或开启新对话
   ```
