#!/usr/bin/env bash
# cli-abstraction 验证辅助函数 — 所有阶段 (P3/P4/P5) 共享。
#
# 用法: source .agent/workflows/cli-abstraction-handoffs/verify-helpers.sh
#
# 提供:
#   extract_codex_aliases <file>  — 解析 Go 文件中 internal/codex 的导入别名
#   strip_go_noise                — 管道过滤: 去除 Go 注释与字符串字面量

set -euo pipefail

# extract_codex_aliases: 解析 internal/codex 导入别名。
# 支持 `codex "path"` 与默认别名；拒绝 "." / "_" 导入。
extract_codex_aliases() {
  local file="$1"
  awk '
    /"github.com\/multi-agent\/go-agent-v2\/internal\/codex"/ {
      line=$0
      sub(/\/\/.*/, "", line)
      split(line, arr, "\"")
      prefix=arr[1]
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", prefix)
      if (prefix == "" || prefix == "import" || prefix == "(") {
        print "codex"
        next
      }
      n=split(prefix, toks, /[[:space:]]+/)
      alias=toks[n]
      if (alias == "" || alias == "import" || alias == "(") alias="codex"
      print alias
    }
  ' "$file"
}

# strip_go_noise: 管道过滤器 — 去除 Go 源码中的注释与字符串字面量，避免文本误报。
# 用法: strip_go_noise < file.go | rg ...
strip_go_noise() {
  perl -0777 -pe 's#/\*.*?\*/##gs; s#//.*$##mg; s#"(?:\\.|[^"\\])*"##gs; s#`[^`]*`##gs'
}
