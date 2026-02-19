---
name: 运维工程师规范
description: DevOps/SRE 运维工程最佳实践，涵盖 Linux 系统管理、服务部署、监控告警、故障排查、自动化运维和 SLA 保障。适用于系统运维、生产环境管理和基础设施维护。
tags: [devops, sre, linux, monitoring, deployment, 运维, 系统管理, 监控, 部署, 自动化]
---

# 运维工程师规范

适用于生产环境运维管理的规范指南，确保系统稳定性、可用性和可维护性。

## 何时使用

在以下场景使用此技能：

- Linux 系统管理和配置
- 服务部署和发布管理
- 监控告警配置和优化
- 故障排查和应急响应
- 自动化运维脚本编写
- 日志分析和问题定位
- 性能调优和容量规划
- 备份恢复和灾难恢复

---

## 第一部分：Linux 系统管理

### 系统信息查看

```bash
# 系统基本信息
uname -a                    # 内核版本
cat /etc/os-release         # 发行版信息
hostnamectl                 # 主机名和系统信息

# 硬件资源
lscpu                       # CPU 信息
free -h                     # 内存使用
df -h                       # 磁盘使用
lsblk                       # 块设备列表

# 系统负载
uptime                      # 运行时间和负载
top -bn1 | head -20         # 进程概览
vmstat 1 5                  # 系统活动统计
```

### 进程管理

```bash
# 查看进程
ps aux | grep <process>     # 查找进程
pgrep -a <name>             # 按名称查找
pstree -p                   # 进程树

# 进程控制
kill -15 <pid>              # 优雅终止
kill -9 <pid>               # 强制终止
killall <name>              # 按名称终止

# 后台运行
nohup ./server &            # 后台运行（不推荐生产）
screen -S myapp             # 使用 screen
tmux new -s myapp           # 使用 tmux（推荐）
```

### 用户和权限

```bash
# 用户管理
useradd -m -s /bin/bash <user>    # 创建用户
usermod -aG sudo <user>           # 添加到 sudo 组
passwd <user>                     # 设置密码

# 权限设置
chmod 755 script.sh               # 设置权限
chown user:group file             # 更改所有者
chmod +x /path/to/script          # 添加执行权限
```

### 网络配置

```bash
# 网络状态
ip addr show                      # IP 地址
ip route show                     # 路由表
ss -tulpn                         # 监听端口

# 网络诊断
ping -c 4 <host>                  # 连通性测试
traceroute <host>                 # 路由追踪
curl -v <url>                     # HTTP 请求调试

# DNS
dig <domain>                      # DNS 查询
nslookup <domain>                 # DNS 查找
```

### 磁盘和存储

```bash
# 磁盘管理
fdisk -l                          # 分区列表
lsblk -f                          # 文件系统信息
mount | column -t                 # 挂载信息

# 空间分析
du -sh /path/*                    # 目录大小
ncdu /path                        # 交互式分析
find / -size +100M -type f        # 大文件查找

# LVM 管理
lvs                               # 逻辑卷列表
lvextend -L +10G /dev/vg/lv       # 扩展逻辑卷
resize2fs /dev/vg/lv              # 调整文件系统
```

---

## 第二部分：服务管理

### Systemd 服务

```bash
# 服务控制
systemctl start <service>         # 启动服务
systemctl stop <service>          # 停止服务
systemctl restart <service>       # 重启服务
systemctl status <service>        # 查看状态

# 开机启动
systemctl enable <service>        # 启用开机启动
systemctl disable <service>       # 禁用开机启动

# 查看日志
journalctl -u <service>           # 服务日志
journalctl -u <service> -f        # 实时日志
journalctl -u <service> --since "1 hour ago"
```

### 自定义服务单元

```ini
# /etc/systemd/system/myapp.service
[Unit]
Description=My Application
After=network.target mysql.service
Requires=mysql.service

[Service]
Type=simple
User=appuser
Group=appgroup
WorkingDirectory=/opt/myapp
ExecStart=/opt/myapp/bin/server
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5

# 资源限制
LimitNOFILE=65535
LimitNPROC=4096

# 安全加固
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/myapp/data

[Install]
WantedBy=multi-user.target
```

---

## 第三部分：监控告警

### 系统监控

```bash
# 实时监控工具
htop                              # 增强版 top
iotop                             # IO 监控
iftop                             # 网络流量
dstat                             # 综合统计

# 性能分析
sar -u 1 10                       # CPU 使用率
sar -r 1 10                       # 内存使用
sar -d 1 10                       # 磁盘 IO
sar -n DEV 1 10                   # 网络统计
```

### Prometheus 告警规则

```yaml
# alerts/system.yml
groups:
  - name: system
    rules:
      - alert: HighCPUUsage
        expr: 100 - (avg by(instance) (irate(node_cpu_seconds_total{mode="idle"}[5m])) * 100) > 80
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "高 CPU 使用率 ({{ $labels.instance }})"

      - alert: HighMemoryUsage
        expr: (1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) * 100 > 85
        for: 5m
        labels:
          severity: warning

      - alert: DiskSpaceLow
        expr: (node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"}) * 100 < 15
        for: 5m
        labels:
          severity: critical

      - alert: ServiceDown
        expr: up == 0
        for: 1m
        labels:
          severity: critical
```

---

## 第四部分：日志管理

### 日志查看

```bash
# 系统日志
tail -f /var/log/syslog            # 系统日志
tail -f /var/log/auth.log          # 认证日志

# Journald
journalctl -f                      # 实时日志
journalctl --since "2 hours ago"   # 时间范围
journalctl -p err                  # 按级别过滤

# 日志分析
grep "ERROR" app.log | wc -l       # 错误计数
awk '{print $1}' access.log | sort | uniq -c | sort -rn | head
```

### 日志轮转

```bash
# /etc/logrotate.d/myapp
/var/log/myapp/*.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    create 0644 appuser appgroup
    postrotate
        systemctl reload myapp > /dev/null 2>&1 || true
    endscript
}
```

---

## 第五部分：故障排查

### 排查流程

```
1. 收集信息 → 告警内容、影响范围、最近变更
2. 快速诊断 → 服务状态、资源使用、日志检查
3. 深入分析 → 错误日志、性能指标、相关服务
4. 解决恢复 → 临时措施、根本解决、验证恢复
5. 复盘总结 → 根因分析、改进措施、文档更新
```

### 常见问题排查

```bash
# CPU 高负载
ps aux --sort=-%cpu | head -5
strace -p <pid>
perf top -p <pid>

# 内存问题
ps aux --sort=-%mem | head -5
pmap -x <pid>

# 磁盘 IO 问题
iotop -oP
iostat -xz 1

# 网络问题
ss -s
tcpdump -i eth0 port 80

# OOM 排查
dmesg | grep -i "out of memory"
journalctl -k | grep -i oom
```

---

## 第六部分：自动化运维

### Shell 脚本规范

```bash
#!/bin/bash
set -euo pipefail  # 严格模式

# 日志函数
log_info() { echo -e "\033[32m[INFO]\033[0m $(date '+%Y-%m-%d %H:%M:%S') $*"; }
log_error() { echo -e "\033[31m[ERROR]\033[0m $(date '+%Y-%m-%d %H:%M:%S') $*" >&2; }

# 错误处理
trap 'log_error "脚本执行失败: 行 $LINENO"; exit 1' ERR

# 参数验证
if [[ $# -lt 1 ]]; then
    log_error "用法: $0 <version>"
    exit 1
fi
```

### Cron 定时任务

```bash
# 常用格式
0 2 * * * /opt/scripts/backup.sh          # 每天 2:00 备份
*/5 * * * * /opt/scripts/healthcheck.sh   # 每 5 分钟健康检查
0 0 1 * * /opt/scripts/cleanup.sh         # 每月 1 日清理

# 日志输出
0 2 * * * /opt/scripts/backup.sh >> /var/log/backup.log 2>&1
```

---

## 第七部分：安全加固

### SSH 安全

```bash
# /etc/ssh/sshd_config
Port 22022                          # 修改默认端口
PermitRootLogin no                  # 禁止 root 登录
PasswordAuthentication no           # 禁用密码登录
PubkeyAuthentication yes            # 启用密钥登录
MaxAuthTries 3                      # 最大尝试次数
AllowUsers deploy admin             # 允许的用户
```

### 防火墙

```bash
# UFW (Ubuntu)
ufw enable
ufw default deny incoming
ufw default allow outgoing
ufw allow 22022/tcp                 # SSH
ufw allow 80/tcp                    # HTTP
ufw allow 443/tcp                   # HTTPS
```

---

## 第八部分：备份恢复

### 备份策略

```bash
# 文件备份 (rsync)
rsync -avz --delete /opt/myapp/ backup@backup-server:/backups/myapp/

# 数据库备份 (MySQL)
mysqldump -u root -p --all-databases --single-transaction \
  | gzip > /backups/mysql-$(date +%Y%m%d).sql.gz
```

---

## 运维清单

### 日常检查
- [ ] 系统负载正常（load < CPU核心数）
- [ ] 内存使用率 < 85%
- [ ] 磁盘使用率 < 80%
- [ ] 关键服务运行正常
- [ ] 无异常错误日志
- [ ] 备份任务正常执行

### 上线检查
- [ ] 代码已通过测试
- [ ] 配置文件已更新
- [ ] 数据库迁移已执行
- [ ] 回滚方案已准备
- [ ] 监控告警已配置

### 故障响应
- [ ] 确认故障范围和影响
- [ ] 通知相关人员
- [ ] 收集诊断信息
- [ ] 执行恢复操作
- [ ] 验证恢复结果
- [ ] 编写故障报告


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
