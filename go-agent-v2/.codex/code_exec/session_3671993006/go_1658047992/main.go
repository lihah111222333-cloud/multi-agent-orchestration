package main

import (
  "fmt"
  "os"
  "os/exec"
)

func main() {
  cmd := exec.Command("go", "build", "./...")
  cmd.Dir = "/Users/mima0000/Desktop/wj/multi-agent-orchestration/.worktrees/a4/go-agent-v2"
  cmd.Stdout = os.Stdout
  cmd.Stderr = os.Stderr
  if err := cmd.Run(); err != nil {
    fmt.Println("ERR:", err)
    os.Exit(1)
  }
}
