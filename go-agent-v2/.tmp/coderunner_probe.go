package main

import (
  "context"
  "fmt"
  "time"

  "github.com/multi-agent/go-agent-v2/internal/executor"
)

func main() {
  r, err := executor.NewCodeRunner(".")
  if err != nil { panic(err) }
  defer r.Cleanup()
  res, err := r.Run(context.Background(), executor.RunRequest{
    Mode: executor.ModeProjectCmd,
    Command: "dd if=/dev/zero bs=1024 count=600 2>/dev/null | tr '\\0' 'A'",
    Timeout: 10 * time.Second,
  })
  if err != nil {
    fmt.Printf("run_err=%v\n", err)
    return
  }
  fmt.Printf("success=%v exit=%d trunc=%v out_len=%d\n", res.Success, res.ExitCode, res.Truncated, len(res.Output))
}
