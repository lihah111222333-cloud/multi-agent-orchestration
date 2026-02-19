---
name: Python 量化机器学习
description: 专注于 Python 量化交易与机器学习开发指南，涵盖因子挖掘、特征工程、ML 模型训练、回测框架与生产部署。
tags: [python, machine-learning, quant, trading, factor, backtest, sklearn, pytorch, 量化, 机器学习, 因子]
---

# Python 量化机器学习开发指南

> 🤖 **核心定义**: 使用 Python 在 WJBoot V2 生态中构建数据驱动的量化交易系统，与 Go 引擎无缝集成。

---

## 第一部分：WJBoot V2 ML 架构

### 集成架构

```
┌───────────────────────────────────────────────────────────────┐
│                    Go 量化引擎 (Engine)                         │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │  engine/ml/python/simple_model.go                       │  │
│  │    SimpleLGBModel.Predict(features []float64)           │  │
│  └───────────────────────┬─────────────────────────────────┘  │
│                          │ 调用方式 (3种)                       │
│  ┌───────────────────────↓─────────────────────────────────┐  │
│  │  1. 子进程调用 (simple_model.go)  ← 开发验证              │  │
│  │  2. HTTP API (http_client.go)    ← 生产推荐              │  │
│  │  3. CGO 嵌入 (qlib.go)           ← 高性能                 │  │
│  └───────────────────────┬─────────────────────────────────┘  │
│                          ↓                                    │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │  qlib2_lite/inference.py                                │  │
│  │    compute_alpha158_features(klines: DataFrame)         │  │
│  │    CryptoLGBModel.predict(features) / get_signal()      │  │
│  └─────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────────┘
```

### 目录结构

```
wjboot-v2/
├── qlib2_lite/                      # Python ML 核心
│   ├── inference.py                 # 推理接口
│   ├── config.py                    # 配置管理
│   ├── server.py                    # FastAPI 服务
│   ├── rl_lite.py                   # 强化学习
│   ├── model/                       # 模型定义
│   │   ├── lgb.py                   # LightGBM
│   │   └── tabnet.py                # TabNet
│   ├── contrib/                     # 高级模型
│   │   └── model/                   # LSTM/Transformer
│   └── rl/                          # RL 模块
│       └── order_execution/         # 订单执行优化
│
├── backend/internal/engine/ml/      # Go 集成层
│   └── python/
│       ├── simple_model.go          # 子进程调用
│       ├── http_client.go           # HTTP 客户端
│       ├── http_strategy.go         # HTTP 策略
│       ├── qlib.go                  # CGO 封装
│       ├── interpreter.go           # CGO Python 解释器
│       ├── rl_agent.go              # RL Agent
│       └── strategy.go              # CGO ML策略
```

---

## 第二部分：依赖配置

### Python 依赖 (`qlib2_lite/requirements.txt`)

```txt
# 核心
numpy>=1.24.0
pandas>=2.0.0
lightgbm>=4.0.0

# 可选 (深度学习)
torch>=2.0.0
pytorch-lightning>=2.0.0

# HTTP 服务
fastapi>=0.100.0
uvicorn>=0.23.0

# 工具
scikit-learn>=1.3.0
optuna>=3.0.0
joblib>=1.3.0
```

### 安装

```bash
cd qlib2_lite
pip install -r requirements.txt
```

---

## 第三部分：特征工程 (qlib2_lite)

### Alpha158 特征 (53个)

> **源文件**: `qlib2_lite/inference.py`

```python
from qlib2_lite.inference import compute_alpha158_features

# 输入: DataFrame (必须包含 OHLCV)
klines = pd.DataFrame({
    'open': [...],
    'high': [...],
    'low': [...],
    'close': [...],
    'volume': [...]
})

# 计算特征
features = compute_alpha158_features(klines)
# 输出: DataFrame (53 列)
```

### 特征列表

| 类别 | 特征 | 数量 |
|------|------|------|
| **基础** | returns, log_returns | 2 |
| **KBAR** | kbar_upper/lower/body | 3 |
| **滚动** | sma/std/max/min/vol_mean_{5,10,20,60} | 20 |
| **比率** | close_sma_ratio/high_low_ratio_{N} | 8 |
| **动量** | return_{1,2,3,5,10,20}d | 6 |
| **RSI** | rsi_{6,12,24} | 3 |
| **MACD** | ema_12/26, macd, signal, hist | 5 |
| **布林** | bb_mid/std/upper/lower/width/position | 6 |

### Alpha360 特征 (360个)

```python
from qlib2_lite.inference import compute_alpha360_features

features = compute_alpha360_features(klines)
# 输出: DataFrame (360 列)
```

---

## 第四部分：模型训练

### LGBModelLite 训练

> **源文件**: `qlib2_lite/inference.py :: LGBModelLite.train()`

```python
from qlib2_lite.inference import LGBModelLite, compute_alpha158_features
import pandas as pd

# 1. 准备数据
klines = pd.read_csv('btcusdt_1h.csv')
features = compute_alpha158_features(klines)

# 2. 创建标签 (未来 N 期收益)
y = klines['close'].pct_change(5).shift(-5)  # 5期未来收益

# 3. 切分数据 (时序切分!)
split_idx = int(len(features) * 0.8)
X_train, X_valid = features.iloc[:split_idx], features.iloc[split_idx:]
y_train, y_valid = y.iloc[:split_idx], y.iloc[split_idx:]

# 4. 训练
model = LGBModelLite()
result = model.train(
    X_train=X_train,
    y_train=y_train,
    X_valid=X_valid,
    y_valid=y_valid,
    params={
        'objective': 'regression',
        'num_leaves': 31,
        'learning_rate': 0.05,
    },
    num_boost_round=1000,
    early_stopping_rounds=50
)

# 5. 保存
model.save('models/btc_lgb.txt')
print(f"Best iteration: {result['best_iteration']}")
print(f"Top features: {list(result['feature_importance'].items())[:10]}")
```

### CryptoLGBModel (加密货币专用)

```python
from qlib2_lite.inference import CryptoLGBModel

model = CryptoLGBModel()
model.threshold_buy = 0.6    # 买入阈值
model.threshold_sell = -0.3  # 卖出阈值

# 训练
model.train(X_train, y_train, X_valid, y_valid)
model.save('models/crypto_lgb.txt')

# 获取信号
signal = model.get_signal(features.iloc[-1].values)
# {'signal': 1, 'score': 0.75, 'confidence': 0.375}
```

### 训练参数优化

```python
import optuna

def objective(trial):
    params = {
        'num_leaves': trial.suggest_int('num_leaves', 15, 63),
        'learning_rate': trial.suggest_float('learning_rate', 0.01, 0.1, log=True),
        'feature_fraction': trial.suggest_float('feature_fraction', 0.6, 1.0),
        'bagging_fraction': trial.suggest_float('bagging_fraction', 0.6, 1.0),
        'reg_alpha': trial.suggest_float('reg_alpha', 1e-8, 10.0, log=True),
        'reg_lambda': trial.suggest_float('reg_lambda', 1e-8, 10.0, log=True),
    }
    
    model = LGBModelLite()
    result = model.train(X_train, y_train, X_valid, y_valid, params=params)
    
    # 评估 IC (信息系数)
    preds = model.predict(X_valid)
    ic = np.corrcoef(preds, y_valid.values)[0, 1]
    return ic

study = optuna.create_study(direction='maximize')
study.optimize(objective, n_trials=100)
print(f"Best IC: {study.best_value}")
print(f"Best params: {study.best_params}")
```

---

## 第五部分：Go 集成

### 方式 1: 子进程调用 (开发用)

> **源文件**: `backend/internal/engine/ml/python/simple_model.go`

```go
import "github.com/wjboot/backend/internal/engine/ml/python"

// 创建模型
model := python.NewSimpleLGBModel(
    "./qlib2_lite",           // qlib2_lite 路径
    "./models/crypto_lgb.txt", // 模型文件
    "crypto_lgb",              // 模型类型
)

// 预测
features := []float64{0.01, -0.02, 0.03, ...}  // 53个特征
result, err := model.Predict(features)

// 使用结果
if result.Signal == 1 {
    // 买入
} else if result.Signal == -1 {
    // 卖出
}
```

### 方式 2: HTTP API (生产推荐)

> **源文件**: `backend/internal/engine/ml/python/http_client.go`

```go
import "github.com/wjboot/backend/internal/engine/ml/python"

client := python.NewHTTPClient("http://localhost:8000")

result, err := client.Predict(features)
```

### 方式 3: CGO 嵌入 (高性能)

> **源文件**: `backend/internal/engine/ml/python/qlib.go`

```go
// CGO 方式需要编译时链接 Python
// 详见 docs/qlib CGO集成.md
```

---

## 第六部分：HTTP 服务

### FastAPI 服务 (`qlib2_lite/server.py`)

```python
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import List
from inference import CryptoLGBModel, compute_alpha158_features
import pandas as pd

app = FastAPI(title="WJBoot ML Service")

# 全局模型
model = CryptoLGBModel()

@app.on_event("startup")
def load_model():
    model.load("models/crypto_lgb.txt")

class KlineInput(BaseModel):
    open: List[float]
    high: List[float]
    low: List[float]
    close: List[float]
    volume: List[float]

class PredictResponse(BaseModel):
    signal: int
    score: float
    confidence: float

@app.post("/predict", response_model=PredictResponse)
def predict(klines: KlineInput):
    df = pd.DataFrame(klines.dict())
    features = compute_alpha158_features(df)
    return model.get_signal(features.iloc[-1].values)

@app.post("/predict/batch")
def predict_batch(klines: KlineInput):
    df = pd.DataFrame(klines.dict())
    features = compute_alpha158_features(df)
    predictions = model.predict(features)
    return {"predictions": predictions.tolist()}

# 启动: uvicorn server:app --host 0.0.0.0 --port 8000
```

### 健康检查

```bash
curl http://localhost:8000/health
```

---

## 第七部分：策略集成

### ML 策略示例 (Go)

```go
type MLStrategy struct {
    base.BaseStrategy
    model   *python.SimpleLGBModel
    barBuf  []*entity.Kline
    bufSize int
}

func NewMLStrategy() *MLStrategy {
    return &MLStrategy{
        model:   python.NewSimpleLGBModel("./qlib2_lite", "./models/crypto_lgb.txt", "crypto_lgb"),
        bufSize: 100,
    }
}

func (s *MLStrategy) OnBar(ctx context.Context, bar *entity.Kline) error {
    // 1. 累积 K 线
    s.barBuf = append(s.barBuf, bar)
    if len(s.barBuf) > s.bufSize {
        s.barBuf = s.barBuf[1:]
    }
    if len(s.barBuf) < s.bufSize {
        return nil
    }
    
    // 2. 计算特征 (调用 Python)
    features := s.computeFeatures(s.barBuf)
    
    // 3. 预测
    result, err := s.model.Predict(features)
    if err != nil {
        return err
    }
    
    // 4. 执行交易
    position := s.Context().GetPosition()
    
    if result.Signal == 1 && position.Long.IsZero() {
        qty := s.Context().GetCapital().Div(bar.Close).Mul(decimal.NewFromFloat(0.95))
        s.Context().OpenLong(qty)
    } else if result.Signal == -1 && position.Long.GreaterThan(decimal.Zero) {
        s.Context().CloseLong(position.Long)
    }
    
    return nil
}

func (s *MLStrategy) computeFeatures(bars []*entity.Kline) []float64 {
    // 调用 Python 计算特征
    // 或在 Go 端实现简化版特征计算
    // ...
}
```

---

## 第八部分：强化学习 (RL)

> **源文件**: `qlib2_lite/rl_lite.py`

### RL Agent 训练

```python
from qlib2_lite.rl_lite import RLAgent, TradingEnv

# 创建环境
env = TradingEnv(
    klines=klines,
    initial_capital=10000,
    commission=0.0004
)

# 创建 Agent
agent = RLAgent(
    state_dim=env.observation_space.shape[0],
    action_dim=env.action_space.n,
    lr=0.001
)

# 训练
agent.train(env, episodes=1000)

# 保存
agent.save('models/rl_agent.npz')
```

### RL Agent 推理 (Go)

> **源文件**: `backend/internal/engine/ml/python/rl_agent.go`

```go
agent := python.NewRLAgent("./qlib2_lite", "./models/rl_agent.npz")
action, err := agent.GetAction(state)
// action: 0=持有, 1=买入, 2=卖出
```

---

## 第九部分：最佳实践

### ⚠️ 常见陷阱

| 陷阱 | 说明 | 解决方案 |
|------|------|----------|
| **未来数据泄露** | 使用未来信息训练 | 严格时序切分，标签用 `shift(-N)` |
| **过拟合** | 模型在样本内表现好，样本外差 | Walk-Forward 验证 |
| **特征不对齐** | Go/Python 特征计算结果不一致 | 统一使用 Python 计算 |
| **模型版本** | 模型格式不兼容 | 使用 `.txt` 格式保存 LightGBM |

### ✅ 检查清单

- [ ] 数据无未来泄露 (标签使用 `shift(-N)`)
- [ ] 使用时序切分 (不能 shuffle)
- [ ] 特征计算使用 `qlib2_lite/inference.py`
- [ ] 模型保存为 `.txt` 格式
- [ ] HTTP 服务有健康检查
- [ ] Go 策略有错误处理
- [ ] 置信度过滤 (低置信度不交易)

---

## 第十部分：调试与验证

### 验证 Python 环境

```bash
cd qlib2_lite
python -c "from inference import LGBModelLite, CryptoLGBModel; print('OK')"
```

### 验证 Go 集成

```go
err := python.ValidateQlibLite("./qlib2_lite")
if err != nil {
    log.Fatal(err)
}

model := python.NewSimpleLGBModel(...)
err = model.Test()
if err != nil {
    log.Fatal(err)
}
```

### 性能基准

| 调用方式 | 延迟 | 适用场景 |
|---------|------|---------|
| 子进程 | ~50ms | 开发验证 |
| HTTP | ~5ms | 生产推荐 |
| CGO | ~0.1ms | 高频交易 |

---

## 参考文件

| 文件 | 说明 |
|------|------|
| `qlib2_lite/README.md` | ML 集成完整文档 |
| `qlib2_lite/inference.py` | 特征工程 + 模型推理 |
| `qlib2_lite/ensemble.py` | 多模型集成 |
| `backend/internal/engine/ml/python/` | Go 集成层 |
| `backend/internal/engine/ml/cache/` | 特征缓存 |
| `backend/internal/engine/ml/monitor/` | 模型监控 |

---

## 第十一部分：进阶优化

### 模型热更新 (P1)

```go
// 原子替换模型，无服务中断
model.Reload(newModelPath)

// 检查状态
model.IsLoaded()
model.GetModelName()
```

### 特征缓存 (P2)

```go
import "internal/engine/ml/cache"

// 创建缓存 (10000 条, 1h TTL)
fc := cache.NewFeatureCache(10000, time.Hour)

// 获取或计算
features := fc.GetOrCompute(key, func() []float64 {
    return computeAlpha158(bars)
})

// 统计
hits, misses, rate := fc.Stats()
```

### 模型监控 (P3)

```go
import "internal/engine/ml/monitor"

// 创建监控器
m := monitor.NewModelMonitor(monitor.MonitorConfig{
    Name:        "crypto_lgb",
    Window:      100,
    ICThreshold: 0.02,
})

// 记录预测与实际值
m.RecordPrediction(pred, actual)

// 检查是否需要重训练
if m.ShouldRetrain() {
    // 触发重训练
}
```

### 多模型集成 (P4)

```python
from ensemble import EnsembleModel, CryptoLGBModel

# 创建集成
model1 = CryptoLGBModel("model1.txt")
model2 = CryptoLGBModel("model2.txt")
ensemble = EnsembleModel([model1, model2], weights=[0.6, 0.4])

# 加权平均预测
score = ensemble.predict(features)

# 多数投票信号
signal = ensemble.get_signal(features)
# {'signal': 1, 'score': 0.75, 'confidence': 0.6, 'votes': {...}}
```
