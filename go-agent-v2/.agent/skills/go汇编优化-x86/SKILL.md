---
name: Go汇编优化-x86
description: Go 语言汇编与 SIMD 向量化优化指南，涵盖热点函数识别、AVX2/AVX-512 指令集、Plan 9 汇编语法和跨平台兼容。适用于回测引擎、量化计算等高性能场景。
tags: [assembly, simd, sse, avx2, avx-512, performance, go, optimization, x86, intel, 汇编, 向量化, 高性能计算]
---

# Go 汇编优化 - x86 平台

适用于 Go 后端高性能计算场景的 x86 汇编优化规范指南。

## 何时使用

在以下场景使用此技能：

- 回测引擎统计计算优化
- 技术指标批量计算（SMA/ATR/Sharpe）
- 大规模浮点数运算加速
- 热点函数的 SIMD 向量化

---

## 第一部分：优化前置准备

### 黄金法则

> ⚠️ **先优化算法，再考虑汇编**。O(n²) 的汇编无法击败 O(n) 的纯 Go。

### 1.1 性能基准测量

```bash
# 记录基准性能
cd backend && go test -bench=. -benchmem ./internal/backtest/... -count=3 | tee benchmark_baseline.txt

# CPU 火焰图定位热点
go test -bench=BenchmarkTargetFunction -cpuprofile=cpu.prof ./internal/backtest/
go tool pprof -http=:8080 cpu.prof
```

### 1.2 热点函数识别标准

| 优先级 | 条件 | 示例 |
|:---:|:---|:---|
| P0 | 循环内调用 >10万次 | `calculateMean`, `sumFloat64` |
| P1 | 单次调用 >1ms | `calculateSharpeRatio` |
| P2 | 涉及类型转换 | `decimal.Decimal` → `float64` |

---

## 第二部分：算法级优化（必做）

### 2.1 滑动窗口优化

```go
// ❌ O(n × period) 嵌套循环
func calculateSMANaive(prices []float64, period int) []float64 {
    result := make([]float64, len(prices))
    for i := period - 1; i < len(prices); i++ {
        sum := 0.0
        for j := i - period + 1; j <= i; j++ {
            sum += prices[j]
        }
        result[i] = sum / float64(period)
    }
    return result
}

// ✅ O(n) 滑动窗口
func calculateSMAOptimized(prices []float64, period int) []float64 {
    result := make([]float64, len(prices))
    if len(prices) < period {
        return result
    }
    
    windowSum := 0.0
    for i := 0; i < period-1; i++ {
        windowSum += prices[i]
    }
    for i := period - 1; i < len(prices); i++ {
        windowSum += prices[i]
        result[i] = windowSum / float64(period)
        windowSum -= prices[i-period+1]
    }
    return result
}
```

### 2.2 类型预转换

```go
// ✅ 数据加载时一次性转换
type EquityPoint struct {
    Time      time.Time
    Equity    decimal.Decimal
    EquityF64 float64 // 统计计算专用
}

// ✅ 批量预转换
func preConvertToFloat64(decimals []decimal.Decimal) []float64 {
    result := make([]float64, len(decimals))
    for i, d := range decimals {
        result[i], _ = d.Float64()
    }
    return result
}
```

---

## 第三部分：SSE/SSE2 基础指令

### 3.1 SSE2 指令速查（128-bit）

| 指令 | 功能 | 示例 |
|:---|:---|:---|
| `MOVAPD` | 对齐加载 128-bit | `MOVAPD (SI), X0` |
| `MOVUPD` | 非对齐加载 128-bit | `MOVUPD (SI), X0` |
| `ADDPD` | 并行加法 (2×f64) | `ADDPD X1, X0` |
| `MULPD` | 并行乘法 (2×f64) | `MULPD X1, X0` |
| `SUBPD` | 并行减法 (2×f64) | `SUBPD X1, X0` |
| `DIVPD` | 并行除法 (2×f64) | `DIVPD X1, X0` |
| `MAXPD` | 并行最大值 | `MAXPD X1, X0` |
| `MINPD` | 并行最小值 | `MINPD X1, X0` |
| `SQRTPD` | 并行平方根 | `SQRTPD X0, X0` |
| `XORPD` | 清零/异或 | `XORPD X0, X0` |
| `ADDSD` | 标量加法 | `ADDSD X1, X0` |
| `MOVSD` | 标量移动 | `MOVSD X0, ret+16(FP)` |
| `MOVHLPS` | 高位→低位 | `MOVHLPS X0, X1` |

### 3.2 SSE2 累加示例

```asm
// sum_sse2.s - 最广泛兼容的实现
#include "textflag.h"

// func sumFloat64SSE2(xs []float64) float64
TEXT ·sumFloat64SSE2(SB), NOSPLIT, $0-32
    MOVQ xs_base+0(FP), SI
    MOVQ xs_len+8(FP), CX
    XORPD X0, X0                    // X0 = 0 (累加器)
    XORPD X1, X1                    // X1 = 0 (次累加器)

loop:
    CMPQ CX, $4
    JLT tail2
    ADDPD (SI), X0                  // X0 += xs[0:2]
    ADDPD 16(SI), X1                // X1 += xs[2:4]
    ADDQ $32, SI
    SUBQ $4, CX
    JMP loop

tail2:
    ADDPD X1, X0                    // 合并累加器
    CMPQ CX, $2
    JLT tail1
    ADDPD (SI), X0
    ADDQ $16, SI
    SUBQ $2, CX

tail1:
    // 水平归约
    MOVHLPS X0, X1
    ADDSD X1, X0
    
    TESTQ CX, CX
    JZ done
    ADDSD (SI), X0

done:
    MOVSD X0, ret+24(FP)
    RET
```

---

## 第四部分：AVX2 向量化优化

### 4.1 AVX2 指令速查（256-bit）

| 指令 | 功能 | 示例 |
|:---|:---|:---|
| `VXORPD` | 清零 256-bit | `VXORPD Y0, Y0, Y0` |
| `VMOVUPD` | 非对齐加载 | `VMOVUPD (SI), Y0` |
| `VMOVAPD` | 对齐加载 | `VMOVAPD (SI), Y0` |
| `VADDPD` | 并行加法 (4×f64) | `VADDPD Y1, Y0, Y0` |
| `VMULPD` | 并行乘法 (4×f64) | `VMULPD Y1, Y0, Y0` |
| `VSUBPD` | 并行减法 (4×f64) | `VSUBPD Y1, Y0, Y0` |
| `VDIVPD` | 并行除法 (4×f64) | `VDIVPD Y1, Y0, Y0` |
| `VMAXPD` | 并行最大值 | `VMAXPD Y1, Y0, Y0` |
| `VMINPD` | 并行最小值 | `VMINPD Y1, Y0, Y0` |
| `VSQRTPD` | 并行平方根 | `VSQRTPD Y0, Y0` |
| `VFMADD132PD` | 融合乘加 a=a*b+c | `VFMADD132PD Y2, Y1, Y0` |
| `VFMADD213PD` | 融合乘加 a=b*a+c | `VFMADD213PD Y2, Y1, Y0` |
| `VFMADD231PD` | 融合乘加 a=b*c+a | `VFMADD231PD Y2, Y1, Y0` |
| `VEXTRACTF128` | 提取高 128-bit | `VEXTRACTF128 $1, Y0, X1` |
| `VBROADCASTSD` | 标量广播 | `VBROADCASTSD (SI), Y0` |
| `VPERMILPD` | 通道内排列 | `VPERMILPD $0x5, Y0, Y0` |
| `VPERM2F128` | 跨通道排列 | `VPERM2F128 $0x31, Y1, Y0, Y0` |

### 4.2 AVX2 累加实现

```asm
// sum_avx2.s
#include "textflag.h"

// func sumFloat64AVX2(xs []float64) float64
TEXT ·sumFloat64AVX2(SB), NOSPLIT, $0-32
    MOVQ xs_base+0(FP), SI
    MOVQ xs_len+8(FP), CX
    VXORPD Y0, Y0, Y0               // Y0 = 0 (主累加器)
    VXORPD Y1, Y1, Y1               // Y1 = 0 (次累加器)

loop8:
    CMPQ CX, $8
    JLT loop4
    VADDPD (SI), Y0, Y0             // Y0 += xs[0:4]
    VADDPD 32(SI), Y1, Y1           // Y1 += xs[4:8]
    ADDQ $64, SI
    SUBQ $8, CX
    JMP loop8

loop4:
    VADDPD Y1, Y0, Y0               // 合并累加器
    CMPQ CX, $4
    JLT tail
    VADDPD (SI), Y0, Y0
    ADDQ $32, SI
    SUBQ $4, CX

tail:
    // 水平归约 256-bit → 128-bit → 64-bit
    VEXTRACTF128 $1, Y0, X1
    ADDPD X1, X0
    MOVHLPS X0, X1
    ADDSD X1, X0
    
    // 处理剩余元素
    TESTQ CX, CX
    JZ done
remain:
    ADDSD (SI), X0
    ADDQ $8, SI
    DECQ CX
    JNZ remain

done:
    MOVSD X0, ret+24(FP)
    VZEROUPPER                      // 必须：清除 AVX 状态
    RET
```

---

## 第五部分：AVX-512 高级优化

### 5.1 AVX-512 指令集家族

| 子集 | 功能 | 服务器支持 |
|:---|:---|:---:|
| AVX-512F | 基础指令（Foundation） | Skylake-X+ |
| AVX-512CD | 冲突检测（Conflict Detection） | Skylake-X+ |
| AVX-512BW | 字节/字操作（Byte/Word） | Skylake-X+ |
| AVX-512DQ | 双字/四字操作（Dword/Qword） | Skylake-X+ |
| AVX-512VL | 向量长度扩展（Vector Length） | Skylake-X+ |
| AVX-512VNNI | 神经网络整数指令 | Ice Lake+ |
| AVX-512IFMA | 52-bit 整数 FMA | Cannon Lake+ |
| AVX-512VBMI | 字节级排列 | Cannon Lake+ |
| AVX-512VPOPCNTDQ | 并行 popcount | Ice Lake+ |
| AVX-512BF16 | BFloat16 支持 | Cooper Lake+ |
| AVX-512FP16 | FP16 支持 | Sapphire Rapids+ |

### 5.2 运行时 CPU 特性检测

```go
// cpu_detect.go
package simd

import "golang.org/x/sys/cpu"

type FeatureLevel int

const (
    LevelGeneric FeatureLevel = iota
    LevelSSE2
    LevelAVX2
    LevelAVX512
)

var detectedLevel FeatureLevel

func init() {
    switch {
    case cpu.X86.HasAVX512F && cpu.X86.HasAVX512DQ:
        detectedLevel = LevelAVX512
    case cpu.X86.HasAVX2 && cpu.X86.HasFMA:
        detectedLevel = LevelAVX2
    case cpu.X86.HasSSE2:
        detectedLevel = LevelSSE2
    default:
        detectedLevel = LevelGeneric
    }
}

// 检测 AVX-512 子集
func HasAVX512VNNI() bool { return cpu.X86.HasAVX512VNNI }
func HasAVX512VPOPCNTDQ() bool { return cpu.X86.HasAVX512VPOPCNTDQ }
```

### 5.3 AVX-512 指令速查（512-bit）

| 指令 | 功能 | 示例 |
|:---|:---|:---|
| `VXORPD` | 清零 512-bit | `VXORPD Z0, Z0, Z0` |
| `VMOVUPD` | 非对齐加载 | `VMOVUPD (SI), Z0` |
| `VADDPD` | 8 路并行加法 | `VADDPD Z1, Z0, Z0` |
| `VMULPD` | 8 路并行乘法 | `VMULPD Z1, Z0, Z0` |
| `VFMADD213PD` | 融合乘加 | `VFMADD213PD Z2, Z1, Z0` |
| `VEXTRACTF64X4` | 提取高 256-bit | `VEXTRACTF64X4 $1, Z0, Y1` |
| `VEXTRACTF64X2` | 提取 128-bit | `VEXTRACTF64X2 $3, Z0, X1` |
| `VCMPPD` | 比较生成掩码 | `VCMPPD $14, Z0, Z1, K1` |
| `VADDPD {K1}` | 掩码条件加法 | `VADDPD Z1, Z0, K1, Z0` |
| `VBROADCASTSD` | 标量广播到 512-bit | `VBROADCASTSD (SI), Z0` |
| `VPERMQ` | 跨通道排列 | `VPERMQ $0x39, Z0, Z0` |
| `VPERMPD` | 双精度排列 | `VPERMPD $0xB1, Z0, Z0` |
| `VCOMPRESSSD` | 压缩存储 | `VCOMPRESSSD Z0, K1, (DI)` |
| `VEXPANDSD` | 扩展加载 | `VEXPANDSD (SI), K1, Z0` |
| `VREDUCEPD` | 范围归约 | `VREDUCEPD $4, Z0, Z1` |

### 5.4 AVX-512 掩码操作

AVX-512 的革命性特性是掩码寄存器 K0-K7：

| 掩码指令 | 功能 | 示例 |
|:---|:---|:---|
| `VCMPPD ... K1` | 比较生成掩码 | `VCMPPD $14, Z0, Z1, K1` |
| `KANDW` | 掩码与 | `KANDW K1, K2, K3` |
| `KORW` | 掩码或 | `KORW K1, K2, K3` |
| `KXORW` | 掩码异或 | `KXORW K1, K2, K3` |
| `KNOTW` | 掩码取反 | `KNOTW K1, K2` |
| `KMOVW` | 掩码移动 | `KMOVW K1, AX` |
| `KORTESTW` | 掩码测试 | `KORTESTW K1, K1` |

### 5.5 掩码谓词值

| 谓词值 | 含义 | Intel 助记符 |
|:---:|:---|:---|
| `$0` | 等于 | EQ_OQ |
| `$1` | 小于 | LT_OS |
| `$2` | 小于等于 | LE_OS |
| `$4` | 不等于 | NEQ_UQ |
| `$5` | 不小于 (≥) | NLT_US |
| `$6` | 不小于等于 (>) | NLE_US |
| `$13` | 大于等于 | GE_OS |
| `$14` | 大于 | GT_OS |
| `$17` | 有序等于 | EQ_OS |
| `$25` | 无序或不等于 | NEQ_US |

### 5.6 AVX-512 累加实现

```asm
// sum_avx512.s
#include "textflag.h"

// func sumFloat64AVX512(xs []float64) float64
TEXT ·sumFloat64AVX512(SB), NOSPLIT, $0-32
    MOVQ xs_base+0(FP), SI
    MOVQ xs_len+8(FP), CX
    VXORPD Z0, Z0, Z0               // Z0 = 0 主累加器
    VXORPD Z1, Z1, Z1               // Z1 = 0 次累加器

loop16:
    CMPQ CX, $16
    JLT loop8
    VADDPD (SI), Z0, Z0             // Z0 += xs[0:8]
    VADDPD 64(SI), Z1, Z1           // Z1 += xs[8:16]
    ADDQ $128, SI
    SUBQ $16, CX
    JMP loop16

loop8:
    VADDPD Z1, Z0, Z0               // 合并累加器
    CMPQ CX, $8
    JLT loop4
    VADDPD (SI), Z0, Z0
    ADDQ $64, SI
    SUBQ $8, CX

loop4:
    // 水平归约
    VEXTRACTF64X4 $1, Z0, Y1
    VADDPD Y1, Y0, Y0
    VEXTRACTF128 $1, Y0, X1
    ADDPD X1, X0
    MOVHLPS X0, X1
    ADDSD X1, X0
    
    // 剩余元素
    CMPQ CX, $4
    JLT tail
    VADDPD (SI), Y0, Y0
    VEXTRACTF128 $1, Y0, X1
    ADDPD X1, X0
    MOVHLPS X0, X1
    ADDSD X1, X0
    ADDQ $32, SI
    SUBQ $4, CX

tail:
    TESTQ CX, CX
    JZ done
remain:
    ADDSD (SI), X0
    ADDQ $8, SI
    DECQ CX
    JNZ remain

done:
    MOVSD X0, ret+24(FP)
    VZEROUPPER
    RET
```

### 5.7 AVX-512 掩码条件累加

```asm
// 只累加正数示例
// func sumPositiveAVX512(xs []float64) float64
TEXT ·sumPositiveAVX512(SB), NOSPLIT, $0-32
    MOVQ xs_base+0(FP), SI
    MOVQ xs_len+8(FP), CX
    VXORPD Z0, Z0, Z0               // 累加器
    VXORPD Z2, Z2, Z2               // 0.0 用于比较

loop:
    CMPQ CX, $8
    JLT tail
    VMOVUPD (SI), Z1                // 加载 8 个 float64
    VCMPPD $14, Z2, Z1, K1          // K1 = (Z1 > 0)
    VADDPD Z1, Z0, K1, Z0           // 条件累加
    ADDQ $64, SI
    SUBQ $8, CX
    JMP loop

tail:
    // 水平归约 + 标量处理剩余
    // ...
```

---

## 第六部分：内存优化与预取

### 6.1 内存对齐

```go
// 确保 64 字节对齐（AVX-512 缓存行）
import "unsafe"

func alignedAlloc(n int) []float64 {
    // 分配额外空间用于对齐
    raw := make([]byte, n*8+64)
    ptr := uintptr(unsafe.Pointer(&raw[0]))
    aligned := (ptr + 63) &^ 63
    offset := aligned - ptr
    return unsafe.Slice((*float64)(unsafe.Pointer(&raw[offset])), n)
}
```

### 6.2 预取指令

| 指令 | 功能 | 延迟 |
|:---|:---|:---:|
| `PREFETCHT0` | 预取到 L1 缓存 | 最低 |
| `PREFETCHT1` | 预取到 L2 缓存 | 低 |
| `PREFETCHT2` | 预取到 L3 缓存 | 中 |
| `PREFETCHNTA` | 预取到 L1，标记为非临时 | 低 |

```asm
// 带预取的累加
TEXT ·sumFloat64Prefetch(SB), NOSPLIT, $0-32
    MOVQ xs_base+0(FP), SI
    MOVQ xs_len+8(FP), CX
    VXORPD Y0, Y0, Y0

loop:
    CMPQ CX, $16
    JLT tail
    
    PREFETCHT0 256(SI)              // 预取 4 个缓存行后的数据
    VADDPD (SI), Y0, Y0
    VADDPD 32(SI), Y0, Y0
    VADDPD 64(SI), Y0, Y0
    VADDPD 96(SI), Y0, Y0
    
    ADDQ $128, SI
    SUBQ $16, CX
    JMP loop

tail:
    // ...
```

### 6.3 非临时存储（Streaming Store）

```asm
// 绕过缓存直接写入内存，用于大数据输出
// VMOVNTPD - 非临时存储，避免缓存污染

TEXT ·copyNonTemporal(SB), NOSPLIT, $0-48
    MOVQ src+0(FP), SI
    MOVQ dst+24(FP), DI
    MOVQ len+8(FP), CX

loop:
    CMPQ CX, $4
    JLT done
    VMOVUPD (SI), Y0                // 加载
    VMOVNTPD Y0, (DI)               // 非临时存储
    ADDQ $32, SI
    ADDQ $32, DI
    SUBQ $4, CX
    JMP loop

done:
    SFENCE                          // 必须：确保写入完成
    RET
```

### 6.4 缓存行优化

```go
// 避免伪共享：填充到缓存行边界
type PaddedCounter struct {
    value uint64
    _     [56]byte // 填充到 64 字节
}
```

---

## 第七部分：常用算法模式

### 7.1 向量点积

```asm
// func dotProduct(a, b []float64) float64
TEXT ·dotProduct(SB), NOSPLIT, $0-56
    MOVQ a_base+0(FP), SI
    MOVQ a_len+8(FP), CX
    MOVQ b_base+24(FP), DI
    VXORPD Y0, Y0, Y0               // 累加器

loop:
    CMPQ CX, $4
    JLT tail
    VMOVUPD (SI), Y1                // a[0:4]
    VMOVUPD (DI), Y2                // b[0:4]
    VFMADD231PD Y2, Y1, Y0          // Y0 += Y1 * Y2
    ADDQ $32, SI
    ADDQ $32, DI
    SUBQ $4, CX
    JMP loop

tail:
    VEXTRACTF128 $1, Y0, X1
    ADDPD X1, X0
    MOVHLPS X0, X1
    ADDSD X1, X0
    
    TESTQ CX, CX
    JZ done
remain:
    MOVSD (SI), X1
    MULSD (DI), X1
    ADDSD X1, X0
    ADDQ $8, SI
    ADDQ $8, DI
    DECQ CX
    JNZ remain

done:
    MOVSD X0, ret+48(FP)
    VZEROUPPER
    RET
```

### 7.2 向量最大值

```asm
// func maxFloat64(xs []float64) float64
TEXT ·maxFloat64(SB), NOSPLIT, $0-32
    MOVQ xs_base+0(FP), SI
    MOVQ xs_len+8(FP), CX
    
    TESTQ CX, CX
    JZ empty
    
    VBROADCASTSD (SI), Y0           // 初始化为第一个元素
    ADDQ $8, SI
    DECQ CX

loop:
    CMPQ CX, $4
    JLT tail
    VMAXPD (SI), Y0, Y0             // Y0 = max(Y0, xs[i:i+4])
    ADDQ $32, SI
    SUBQ $4, CX
    JMP loop

tail:
    // 水平归约
    VEXTRACTF128 $1, Y0, X1
    MAXPD X1, X0
    MOVHLPS X0, X1
    MAXSD X1, X0
    
    TESTQ CX, CX
    JZ done
remain:
    MAXSD (SI), X0
    ADDQ $8, SI
    DECQ CX
    JNZ remain

done:
    MOVSD X0, ret+24(FP)
    VZEROUPPER
    RET

empty:
    // 返回 -Inf
    MOVQ $0xFFF0000000000000, AX
    MOVQ AX, ret+24(FP)
    RET
```

### 7.3 向量归一化

```asm
// func normalize(xs []float64)  // in-place
TEXT ·normalize(SB), NOSPLIT, $0-24
    MOVQ xs_base+0(FP), SI
    MOVQ xs_len+8(FP), CX
    MOVQ SI, DI                     // 保存起始地址
    MOVQ CX, R8                     // 保存长度
    
    // 第一遍：计算平方和
    VXORPD Y0, Y0, Y0
loop1:
    CMPQ CX, $4
    JLT tail1
    VMOVUPD (SI), Y1
    VFMADD231PD Y1, Y1, Y0          // Y0 += Y1^2
    ADDQ $32, SI
    SUBQ $4, CX
    JMP loop1

tail1:
    // 水平归约得到 sum of squares
    VEXTRACTF128 $1, Y0, X1
    ADDPD X1, X0
    MOVHLPS X0, X1
    ADDSD X1, X0
    
    // 计算 1/sqrt(sum)
    SQRTSD X0, X0
    MOVSD $1.0, X1
    DIVSD X0, X1                    // X1 = 1/norm
    VBROADCASTSD X1, Y2             // Y2 = [1/norm, 1/norm, ...]
    
    // 第二遍：除以范数
    MOVQ DI, SI
    MOVQ R8, CX
loop2:
    CMPQ CX, $4
    JLT tail2
    VMOVUPD (SI), Y1
    VMULPD Y2, Y1, Y1
    VMOVUPD Y1, (SI)
    ADDQ $32, SI
    SUBQ $4, CX
    JMP loop2

tail2:
    // 处理剩余
    // ...

done:
    VZEROUPPER
    RET
```

### 7.4 批量比较（返回索引）

```asm
// AVX-512 版本：找出所有大于阈值的索引
// func findGreaterThan(xs []float64, threshold float64, indices []int32) int
TEXT ·findGreaterThan(SB), NOSPLIT, $0-64
    MOVQ xs_base+0(FP), SI
    MOVQ xs_len+8(FP), CX
    MOVSD threshold+24(FP), X0
    MOVQ indices_base+32(FP), DI
    
    VBROADCASTSD X0, Z1             // Z1 = [threshold...]
    XORQ R8, R8                     // 计数器
    XORQ R9, R9                     // 索引

loop:
    CMPQ CX, $8
    JLT done
    VMOVUPD (SI), Z0                // 加载 8 个元素
    VCMPPD $14, Z1, Z0, K1          // K1 = (Z0 > threshold)
    KMOVW K1, AX
    
    // 对每个设置的位，存储索引
    TESTW AX, AX
    JZ next
    
    // 展开处理每一位...
    // (简化示意)

next:
    ADDQ $64, SI
    ADDQ $8, R9
    SUBQ $8, CX
    JMP loop

done:
    MOVQ R8, ret+56(FP)
    VZEROUPPER
    RET
```

---

## 第八部分：性能陷阱与最佳实践

### 8.1 AVX-512 降频问题

> ⚠️ **Intel 处理器在运行 AVX-512 时可能降低频率 10-30%**

| CPU 代际 | 降频幅度 | 建议 |
|:---|:---:|:---|
| Skylake-X | 较严重 | 避免短小循环使用 AVX-512 |
| Ice Lake | 中等 | 评估实际收益 |
| Sapphire Rapids | 轻微 | 通常值得使用 |

**策略**：仅在数据量足够大（>1000 元素）时使用 AVX-512。

### 8.2 SSE/AVX 切换惩罚

```asm
// ❌ 错误：AVX 代码后未清除状态
TEXT ·badFunction(SB), NOSPLIT, $0-16
    VADDPD Y1, Y0, Y0
    RET                             // 缺少 VZEROUPPER！

// ✅ 正确：始终在 AVX 代码后调用
TEXT ·goodFunction(SB), NOSPLIT, $0-16
    VADDPD Y1, Y0, Y0
    VZEROUPPER                      // 清除 YMM 高位
    RET
```

**惩罚**：遗漏 VZEROUPPER 可能导致后续 SSE 代码慢 **50-100 周期**。

### 8.3 内存对齐惩罚

| 对齐情况 | 性能影响 |
|:---|:---|
| 对齐到 32 字节（AVX2） | 最优 |
| 跨缓存行（64 字节） | 慢 2-10x |
| 跨页（4KB） | 慢 10-100x |

```asm
// 使用对齐加载指令（会在非对齐时触发异常，但更快）
VMOVAPD (SI), Y0    // 要求 32 字节对齐

// 使用非对齐加载指令（更安全，略慢）
VMOVUPD (SI), Y0    // 不要求对齐
```

### 8.4 循环展开最佳实践

```asm
// ❌ 过度展开：寄存器溢出
loop:
    VADDPD (SI), Y0, Y0
    VADDPD 32(SI), Y1, Y1
    VADDPD 64(SI), Y2, Y2
    VADDPD 96(SI), Y3, Y3
    VADDPD 128(SI), Y4, Y4          // 开始影响 I-cache
    VADDPD 160(SI), Y5, Y5
    VADDPD 192(SI), Y6, Y6
    VADDPD 224(SI), Y7, Y7
    // ...

// ✅ 适度展开：2-4 倍
loop:
    VADDPD (SI), Y0, Y0
    VADDPD 32(SI), Y1, Y1           // 隐藏延迟
    ADDQ $64, SI
    SUBQ $8, CX
    JNZ loop
```

### 8.5 分支预测友好

```asm
// ❌ 不可预测的分支
loop:
    CMPQ (SI), $100
    JGE skip                        // 随机跳转
    ADDSD (SI), X0
skip:
    ADDQ $8, SI
    DECQ CX
    JNZ loop

// ✅ 使用 CMOV 或掩码操作
// AVX-512 掩码版本完全消除分支
loop:
    VCMPPD $14, Z1, Z0, K1
    VADDPD Z0, Z2, K1, Z2           // 条件累加，无分支
```

### 8.6 依赖链优化

```asm
// ❌ 长依赖链：每次迭代依赖上一次结果
loop:
    ADDSD (SI), X0                  // 等待 X0
    ADDQ $8, SI
    DECQ CX
    JNZ loop

// ✅ 多累加器打断依赖
loop:
    ADDSD (SI), X0                  // 累加器 1
    ADDSD 8(SI), X1                 // 累加器 2（独立）
    ADDQ $16, SI
    SUBQ $2, CX
    JNZ loop
    ADDSD X1, X0                    // 最后合并
```

---

## 第九部分：Plan 9 汇编速查

### 9.1 寄存器命名

| x86-64 | Plan 9 | 用途 |
|:---:|:---:|:---|
| RAX | AX | 通用/返回值 |
| RBX | BX | 通用 |
| RCX | CX | 计数器 |
| RDX | DX | 数据 |
| RSI | SI | 源地址 |
| RDI | DI | 目标地址 |
| RSP | SP | 栈指针 |
| RBP | BP | 帧指针 |
| R8-R15 | R8-R15 | 扩展通用寄存器 |
| XMM0-15 | X0-X15 | 128-bit SIMD (SSE) |
| YMM0-15 | Y0-Y15 | 256-bit SIMD (AVX) |
| ZMM0-31 | Z0-Z31 | 512-bit SIMD (AVX-512) |
| K0-K7 | K0-K7 | AVX-512 掩码寄存器 |

### 9.2 函数签名映射

```go
// Go 函数
func Add(a, b float64) float64
```

```asm
// 汇编栈布局 (参数从 FP 偏移)
// a      +0(FP)  [8 bytes]
// b      +8(FP)  [8 bytes]
// ret    +16(FP) [8 bytes]

TEXT ·Add(SB), NOSPLIT, $0-24
    MOVSD a+0(FP), X0
    ADDSD b+8(FP), X0
    MOVSD X0, ret+16(FP)
    RET
```

### 9.3 切片参数布局

```go
func Sum(xs []float64) float64
```

```asm
// 切片在栈上占 24 字节
// xs_base  +0(FP)   [8 bytes] 指针
// xs_len   +8(FP)   [8 bytes] 长度
// xs_cap   +16(FP)  [8 bytes] 容量
// ret      +24(FP)  [8 bytes] 返回值

TEXT ·Sum(SB), NOSPLIT, $0-32
    MOVQ xs_base+0(FP), SI
    MOVQ xs_len+8(FP), CX
    // ...
```

---

## 第十部分：验证与基准测试

### 10.1 单元测试

```go
func TestSumFloat64(t *testing.T) {
    tests := []struct {
        input    []float64
        expected float64
    }{
        {[]float64{1, 2, 3, 4}, 10},
        {[]float64{1.1, 2.2, 3.3}, 6.6},
        {make([]float64, 1000), 0},
    }
    
    for _, tt := range tests {
        got := simd.SumFloat64(tt.input)
        if math.Abs(got-tt.expected) > 1e-10 {
            t.Errorf("SumFloat64(%v) = %v, want %v", tt.input, got, tt.expected)
        }
    }
}
```

### 10.2 基准比较

```bash
# 优化后测试
go test -bench=. -benchmem ./internal/simd/... -count=5 | tee benchmark_optimized.txt

# 使用 benchstat 比较
go install golang.org/x/perf/cmd/benchstat@latest
benchstat benchmark_baseline.txt benchmark_optimized.txt
```

### 10.3 CPU 特性验证

```bash
# 检查 CPU 支持的指令集
cat /proc/cpuinfo | grep -E "flags|model name" | head -4

# 或使用 Go 代码
go run -exec "env GODEBUG=cpu.all=1" main.go 2>&1 | head -20
```

---

## 预期收益

| 优化项 | 预期提升 | 适用场景 |
|:---|:---:|:---|
| SMA 滑动窗口 | 10-50x | 所有 |
| float64 预转换 | 2-3x | 所有 |
| SSE2 累加 | 2x | 最广兼容 |
| AVX2 累加 | 4x | Haswell+ |
| AVX-512 累加 | 8x | Skylake-X+ |
| FMA 融合乘加 | 1.5-2x | Haswell+ |
| 预取优化 | 1.2-1.5x | 大数据集 |

**总体预期**：热点函数性能提升 **5-20x**。

---

## ⚠️ 注意事项

1. **保持向后兼容**：优化不得改变函数签名或返回值语义
2. **浮点精度容差**：SIMD 优化可能导致微小精度差异，单测应容忍 1e-10 误差
3. **先分析后优化**：始终确认函数是真正的热点再进行修改
4. **VZEROUPPER 必需**：AVX/AVX-512 代码结束前必须调用，避免 SSE 切换惩罚
5. **AVX-512 降频**：评估实际收益，小数据集可能不值得
6. **内存对齐**：尽量对齐到 32/64 字节边界


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
