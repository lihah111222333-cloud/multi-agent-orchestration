---
name: MySQL 高可用运维
description: 专注于 MySQL Group Replication (MGR) 金融级高可用集群的运维、部署与故障恢复。
tags: [mysql, mgr, dba, ha, cluster, 数据库, 运维, 高可用]
---

# MySQL MGR 高可用集群运维规范

> 🛡️ **核心架构**: 本项目采用 3 节点 **MySQL Group Replication (MGR)** 单主模式 (Single-Primary) 集群。提供 RPO=0 (零数据丢失) 的强一致性保障。

## 架构拓扑

参考 `deploy/docker-compose.mgr.yml`:

| 节点 | 角色 | 端口 (SQL) | 端口 (X-Com) | 说明 |
|---|---|---|---|---|
| **mysql-mgr-1** | Primary (R/W) | 3306 : 3306 | 33061 | 负责所有写入 |
| **mysql-mgr-2** | Secondary (R/O) | 3307 : 3306 | 33061 | 实时同步备库 |
| **mysql-mgr-3** | Secondary (R/O) | 3308 : 3306 | 33061 | 实时同步备库 |

> 👉 **应用连接**: 应用应始终连接 **Primary** 节点进行写入。V2 目前通过配置固定指向，未来可引入 ProxySQL 做读写分离。

---

## 常用运维命令

### 1. 查看集群成员状态

登录任意节点 MySQL：

```sql
SELECT * FROM performance_schema.replication_group_members;
```

**状态说明**:
- `ONLINE`: 正常
- `RECOVERING`: 正在同步数据
- `UNREACHABLE`: 失联
- `OFFLINE`: 离线

### 2. 查看节点角色 (Primary/Secondary)

```sql
SELECT variable_value FROM performance_schema.global_status WHERE variable_name= 'group_replication_primary_member';
-- 返回 Primary 节点的 UUID
```

### 3. 先发制人切换 Primary

如果需要维护 Primary 节点，可手动切换：

```sql
SELECT group_replication_set_as_primary('MEMBER_UUID');
```

---

## 故障恢复流程

### 场景一：单节点宕机 (Secondary)
1.  **重启容器**: `docker restart mysql-mgr-2`
2.  **自动恢复**: 节点启动后会自动加入 Group 并同步缺失的 Binlog。在此期间状态为 `RECOVERING`。

### 场景二：Primary 宕机
1.  **自动选举**: MGR 会自动从剩余节点中选举新的 Primary (通常是 UUID 排序或权重)。
2.  **应用感知**: 应用侧可能出现短暂连接错误，需重连。
3.  **节点修复**: 重启宕机的旧 Primary，它将作为 Secondary 重新加入。

### 场景三：集群完全崩溃 (Bootstrap)
如果所有节点都关闭了，需要重新引导集群：

1.  **选择数据最全的节点** (通常是最后关闭的 Primary)。
2.  **设置引导标志**:
    ```sql
    SET GLOBAL group_replication_bootstrap_group=ON;
    START GROUP_REPLICATION;
    SET GLOBAL group_replication_bootstrap_group=OFF;
    ```
3.  **启动其他节点**: 正常执行 `START GROUP_REPLICATION;`。

---

## 备份策略

- **mysqldump**: 每天在 Secondary 节点执行全量备份。
- **Binlog**: 开启 Row 格式 Binlog，保留至少 7 天。

> ⚠️ **禁止操作**: 严禁在 MGR 集群中执行 `MyISAM` 表操作或非事务性 DDL，这会导致集群报错退出。所有表必须是 `InnoDB`。
