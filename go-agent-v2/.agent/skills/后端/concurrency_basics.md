# Go 并发基础与通用模式

> **加载条件**: 启动 goroutine、创建 channel、使用 select/context/WaitGroup/mutex、pipeline/fan-out/worker pool 时加载。

---

## Goroutine

轻量级线程，初始栈 ~2KB，由运行时复用到 OS 线程。

```go
go expensiveComputation(x, y, z)
```

## Channel

类型安全的 goroutine 间通信:

```go
ch := make(chan int)       // 无缓冲 - 同步
ch := make(chan int, 100)  // 有缓冲 - 异步 (直到满)

func receive(ch <-chan int) { }  // 只读
func send(ch chan<- int) { }     // 只写
```

通过 Channel 同步:

```go
func computeAndSend(ch chan int, x, y, z int) {
    ch <- expensiveComputation(x, y, z)
}

func main() {
    ch := make(chan int)
    go computeAndSend(ch, x, y, z)
    v2 := anotherExpensiveComputation(a, b, c)
    v1 := <-ch  // 阻塞直到结果可用
    fmt.Println(v1, v2)
}
```

## Select 多路复用

```go
select {
case result := <-resultCh:
    return result
case <-ctx.Done():
    return ctx.Err()
case <-time.After(5 * time.Second):
    return errors.New("timeout")
}
```

## Context 请求作用域

MUST 将 context 作为第一个参数:

```go
// 可取消的 context
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// 带超时
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

func DoSomething(ctx context.Context, arg1 Type1) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
        // 继续
    }
    return nil
}
```

## WaitGroup

```go
var wg sync.WaitGroup
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        // 执行工作
    }(i)
}
wg.Wait()
```

## Mutex 保护共享状态

```go
var (
    cache   map[string]interface{}
    cacheMu sync.RWMutex
)

func Get(key string) interface{} {
    cacheMu.RLock()
    defer cacheMu.RUnlock()
    return cache[key]
}

func Set(key string, value interface{}) {
    cacheMu.Lock()
    defer cacheMu.Unlock()
    cache[key] = value
}
```

---

## Pipeline

```go
func gen(nums ...int) <-chan int {
    out := make(chan int)
    go func() {
        for _, n := range nums {
            out <- n
        }
        close(out)
    }()
    return out
}

func sq(in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        for n := range in {
            out <- n * n
        }
        close(out)
    }()
    return out
}

func main() {
    c := gen(2, 3)
    out := sq(c)
    for n := range out {
        fmt.Println(n)  // 4 然后 9
    }
}
```

## Fan-Out/Fan-In

```go
func merge(cs ...<-chan int) <-chan int {
    var wg sync.WaitGroup
    out := make(chan int)

    output := func(c <-chan int) {
        for n := range c {
            out <- n
        }
        wg.Done()
    }

    wg.Add(len(cs))
    for _, c := range cs {
        go output(c)
    }

    go func() {
        wg.Wait()
        close(out)
    }()
    return out
}
```

## Worker Pool

```go
func handle(queue chan *Request) {
    for r := range queue {
        process(r)
    }
}

func Serve(clientRequests chan *Request, quit chan bool) {
    for i := 0; i < MaxOutstanding; i++ {
        go handle(clientRequests)
    }
    <-quit
}
```

## 信号量

```go
var sem = make(chan int, MaxOutstanding)

func handle(r *Request) {
    sem <- 1        // 获取
    process(r)
    <-sem           // 释放
}
```
