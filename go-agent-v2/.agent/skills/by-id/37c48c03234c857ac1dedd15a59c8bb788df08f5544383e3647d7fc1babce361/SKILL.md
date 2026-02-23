---
name: Swagger 文档规范
description: 为 Go API 添加 Swagger 注解、遇到 swag init 生成错误、DTO 类型解析失败、或配置 Monorepo 多模块文档时使用
tags: [swagger, swag, openapi, api-documentation, go, gin, 文档, API]
aliases: ["@Swagger", "@swagger", "@swag"]
---

# Swagger 文档规范 (swaggo/swag)

> 📝 **核心工具**: `github.com/swaggo/swag` (v1.16+)，配合 Gin 框架使用 `gin-swagger` 中间件。

## 何时使用

- 为 Go API 添加 Swagger 文档注解
- 遇到 `swag init` 生成错误
- 需要解决 DTO 类型解析问题
- 配置多模块/Monorepo 项目的文档生成

---

## 第一部分：权威生成命令

### 标准命令 (单模块)

```bash
cd backend && swag init -g cmd/main.go -o docs --parseDependency --parseInternal
```

### 多模块命令 (排除问题模块)

```bash
# 生成 User 模块文档，排除有问题的 Admin 模块
swag init -g cmd/user/main.go -o docs/user --parseDependency --parseInternal --exclude internal/admin
```

### 关键参数说明

| 参数 | 说明 | 必需性 |
|------|------|--------|
| `-g` | 入口文件 (main.go 或 router.go) | ✅ 必须 |
| `-o` | 输出目录 | ✅ 必须 |
| `--parseDependency` | 解析外部包依赖类型 | ⚠️ 强烈推荐 |
| `--parseInternal` | 解析内部 internal 包类型 | ⚠️ 强烈推荐 |
| `--exclude` | 排除指定目录 | 按需使用 |
| `--dir` | 限定搜索目录范围 | 大项目优化 |

> [!WARNING]
> **常见陷阱**: 不使用 `--parseDependency` 会导致所有外部包 DTO 无法解析！

---

## 第二部分：常见陷阱与解决方案

### 🕳️ 陷阱 1: DTO 类型解析失败

**错误信息**:
```
cannot find type definition: dto.LoginRequest
ParseComment error in file /path/to/handler.go
```

**原因**:
1. DTO 定义在 swag 无法访问的包中
2. 循环依赖导致类型解析死锁
3. 模块间交叉引用复杂

**解决方案 - Object 映射法**:

```go
// ❌ 错误写法 - swag 可能无法解析 dto.LoginRequest
// @Param request body dto.LoginRequest true "登录请求"

// ✅ 正确写法 - 使用 object 绕过解析
// @Param request body object true "登录请求"
// @Success 200 {object} object "返回数据"
```

> [!TIP]
> **何时使用 Object 映射**: 当 DTO 来自复杂的共享包、外部依赖或存在循环引用时。这不会影响 API 功能，只是文档显示为通用对象。

---

### 🕳️ 陷阱 2: 注解与函数之间有空行

**错误行为**: Swagger 注解被忽略

```go
// ❌ 错误写法 - 空行导致注解失效
// @Summary 用户登录
// @Tags 认证

func (h *AuthHandler) Login(c *gin.Context) { // 注解不会生效！
```

```go
// ✅ 正确写法 - 注解紧贴函数
// @Summary 用户登录
// @Tags 认证
func (h *AuthHandler) Login(c *gin.Context) { // 正常工作
```

---

### 🕳️ 陷阱 3: 路径参数语法错误

**常见错误**:

```go
// ❌ 错误 - 缺少 required 标识
// @Param id path string "用户ID"

// ❌ 错误 - 类型错误
// @Param id path int true "用户ID"  // path 参数应为 string

// ✅ 正确写法
// @Param id path string true "用户ID"
```

**路径参数模板**:
```
@Param {name} path string true "{description}"
```

---

### 🕳️ 陷阱 4: 数组响应语法

**错误**:
```go
// ❌ 错误 - array 后面不能直接接类型
// @Success 200 {array} []dto.User "用户列表"
```

**正确写法**:
```go
// ✅ 对象数组
// @Success 200 {array} dto.User "用户列表"

// ✅ 基础类型数组
// @Success 200 {array} string "字符串列表"
```

---

### 🕳️ 陷阱 5: 转义引号导致解析失败

**问题**: 注解中使用了转义引号

```go
// ❌ 错误 - 转义引号可能导致解析错误
// @Description 返回 \"success\" 消息

// ✅ 正确 - 使用单引号或去掉引号
// @Description 返回 success 消息
// @Description 返回 'success' 消息
```

---

### 🕳️ 陷阱 6: 模块间类型冲突

**场景**: Monorepo 中 Admin 和 User 模块有同名但不同定义的 DTO

**解决方案**:

```bash
# 分别生成各模块文档到独立目录
swag init -g cmd/admin/main.go -o docs/admin --exclude internal/user
swag init -g cmd/user/main.go -o docs/user --exclude internal/admin
```

---

## 第三部分：注解速查表

### 基础注解

```go
// @Summary 简短描述 (显示在接口列表)
// @Description 详细描述 (展开后显示)
// @Tags 模块名称
// @Accept json
// @Produce json
// @Router /api/v1/users [get]
```

### 参数注解

```go
// Query 参数
// @Param page query int false "页码" default(1)
// @Param size query int false "每页数量" default(20)

// Path 参数
// @Param id path string true "资源ID"

// Body 参数
// @Param request body dto.CreateRequest true "请求体"

// Header 参数
// @Param Authorization header string true "Bearer Token"

// Form 参数
// @Param file formData file true "上传文件"
```

### 响应注解

```go
// @Success 200 {object} dto.Response "成功"
// @Success 200 {array} dto.Item "列表成功"
// @Success 204 "无内容"
// @Failure 400 {object} dto.ErrorResponse "请求错误"
// @Failure 401 {object} dto.ErrorResponse "未授权"
// @Failure 404 {object} dto.ErrorResponse "未找到"
// @Failure 500 {object} dto.ErrorResponse "服务器错误"
```

### 认证注解

```go
// @Security BearerAuth
```

需要在 main.go 中定义 SecurityDefinition:
```go
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
```

---

## 第四部分：完整 Handler 模板

```go
// Login godoc
// @Summary 用户登录
// @Description 使用邮箱密码登录系统
// @Tags 认证
// @Accept json
// @Produce json
// @Param request body object true "登录请求 {email, password}"
// @Success 200 {object} object "登录成功 {token, user}"
// @Failure 400 {object} object "请求格式错误"
// @Failure 401 {object} object "邮箱或密码错误"
// @Router /api/v1/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
    // 实现代码
}
```

---

## 第五部分：批量修复脚本

### 将所有 DTO 替换为 Object (紧急修复)

```bash
# 替换 body 参数中的 dto
sed -i '' '/@Param.*body dto\./s/dto\.[A-Za-z]*/object/g' handlers.go

# 替换 Success 响应中的 dto
sed -i '' '/@Success.*{object} dto\./s/dto\.[A-Za-z]*/object/g' handlers.go
sed -i '' '/@Success.*{array} dto\./s/dto\.[A-Za-z]*/object/g' handlers.go
```

> [!CAUTION]
> **危险**: `sed -i` 会直接修改文件！操作前请先 `git stash` 或提交当前更改。

### 验证生成结果

```bash
# 检查生成的 swagger.json 是否有效
cd backend && swag init -g cmd/main.go -o docs && cat docs/swagger.json | jq '.paths | keys | length'
```

---

## 第六部分：main.go 配置模板

```go
package main

import (
    "github.com/gin-gonic/gin"
    swaggerFiles "github.com/swaggo/files"
    ginSwagger "github.com/swaggo/gin-swagger"
    _ "your-project/docs" // 导入生成的 docs 包
)

// @title Your API
// @version 1.0
// @description API 文档描述
// @host localhost:8080
// @BasePath /api/v1
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
    r := gin.Default()
    
    // Swagger 文档路由
    r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
    
    r.Run(":8080")
}
```

---

## 检查清单

- [ ] 使用 `--parseDependency --parseInternal` 参数
- [ ] 注解与函数之间无空行
- [ ] Path 参数使用 `string` 类型
- [ ] 复杂 DTO 考虑使用 `object` 替代
- [ ] 数组响应使用 `{array} Type` 而非 `{array} []Type`
- [ ] main.go 包含 SecurityDefinition (如需认证)
- [ ] 运行 `swag init` 无错误输出

---

## 故障排查流程

```
swag init 失败
     │
     ▼
┌─────────────────────────┐
│ 1. 检查错误信息中的文件路径 │
└───────────┬─────────────┘
            │
            ▼
┌─────────────────────────┐
│ 2. 定位问题 Handler 函数  │
└───────────┬─────────────┘
            │
            ▼
┌─────────────────────────┐
│ 3. DTO 无法解析?         │
│   → 替换为 object        │
└───────────┬─────────────┘
            │
            ▼
┌─────────────────────────┐
│ 4. 跨模块冲突?          │
│   → 使用 --exclude 排除  │
└───────────┬─────────────┘
            │
            ▼
┌─────────────────────────┐
│ 5. 重新运行 swag init    │
└─────────────────────────┘
```
