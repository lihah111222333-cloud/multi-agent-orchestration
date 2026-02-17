// debug_server.go — 调试模式 HTTP 服务: 浏览器直接访问 UI。
//
// 启动方式: ./agent-terminal --debug
// 访问地址: http://localhost:4501
//
// 功能:
//   - 从磁盘 frontend/ 目录提供静态文件 (修改即刷新)
//   - 注入 wails-shim.js 替代 Wails runtime, 通过 HTTP 调用 apiserver
//   - 独立于 Wails 窗口, 可在 Chrome DevTools 中调试
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const debugPort = 4501

// debugWailsRuntimeModule 提供 /wails/runtime.js，兼容 Wails v3 runtime 接口。
//
// 目标：让前端优先走 Call.ByID / Events.On，保持与桌面端路径一致。
const debugWailsRuntimeModule = `// Debug Wails runtime bridge (ESM)
const METHOD_IDS = Object.freeze({
  CALL_API: 1055257995,
  GET_BUILD_INFO: 3168473285,
  GET_GROUP: 4127719990,
  SAVE_CLIPBOARD_IMAGE: 3932748547,
  SELECT_FILES: 937743440,
  SELECT_PROJECT_DIR: 373469749,
});

function appOrThrow() {
  const app = window.go?.main?.App;
  if (!app) throw new Error('debug runtime: window.go.main.App not ready');
  return app;
}

export const Call = {
  ByID: async (methodID, ...args) => {
    const app = appOrThrow();
    switch (methodID) {
      case METHOD_IDS.CALL_API:
        return app.CallAPI(args[0], args[1]);
      case METHOD_IDS.GET_BUILD_INFO:
        return app.GetBuildInfo ? app.GetBuildInfo() : '{}';
      case METHOD_IDS.GET_GROUP:
        return app.GetGroup ? app.GetGroup() : '';
      case METHOD_IDS.SAVE_CLIPBOARD_IMAGE:
        return app.SaveClipboardImage ? app.SaveClipboardImage(args[0]) : '';
      case METHOD_IDS.SELECT_FILES:
        return app.SelectFiles ? app.SelectFiles() : [];
      case METHOD_IDS.SELECT_PROJECT_DIR:
        return app.SelectProjectDir ? app.SelectProjectDir() : '';
      default:
        throw new Error('debug runtime: unknown methodID ' + methodID);
    }
  },
};

export const Events = {
  On: (eventName, callback) => {
    if (!window.runtime?.EventsOn) throw new Error('debug runtime: EventsOn not ready');
    window.runtime.EventsOn(eventName, callback);
    return () => {
      try {
        window.runtime?.EventsOff?.(eventName, callback);
      } catch {
        // ignore
      }
    };
  },
  Off: (eventName) => {
    window.runtime?.EventsOff?.(eventName);
  },
};

export default { Call, Events };
`

// shimScript 注入到 HTML 的 Wails runtime 兼容层。
//
// 功能:
//   - window.go.main.App.CallAPI → fetch http://localhost:4500/rpc
//   - window.go.main.App.LaunchAgent → fetch http://localhost:4500/rpc
//   - window.go.main.App.SubmitInput → fetch http://localhost:4500/rpc
//   - window.go.main.App.SubmitWithFiles → fetch http://localhost:4500/rpc
//   - window.runtime.EventsOn → SSE 连接 (简化)
const shimScript = `<script>
// Wails Runtime Shim — 浏览器调试模式
(function() {
    'use strict';
    const API = 'http://localhost:4500';
    let reqId = 0;

    // JSON-RPC 调用
    async function rpcCall(method, params) {
        const id = ++reqId;
        const body = JSON.stringify({ jsonrpc: '2.0', id, method, params: params || {} });
        const resp = await fetch(API + '/rpc', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: body,
        });
        const data = await resp.json();
        if (data.error) throw new Error(data.error.message || JSON.stringify(data.error));
        return data.result;
    }

    // Wails App bindings shim
    const AppShim = {
        CallAPI: async function(method, paramsJSON) {
            const params = paramsJSON ? JSON.parse(paramsJSON) : {};
            const result = await rpcCall(method, params);
            return result ? JSON.stringify(result) : '{}';
        },
        LaunchAgent: async function(name, prompt, cwd) {
            const result = await rpcCall('thread/start', { model: '', cwd: cwd || '.' });
            if (prompt && result?.thread?.id) {
                const input = [{type: 'text', text: prompt}];
                await rpcCall('turn/start', { threadId: result.thread.id, input: input });
            }
            return JSON.stringify(result);
        },
        SubmitInput: async function(agentId, prompt) {
            const input = [{type: 'text', text: prompt}];
            await rpcCall('turn/start', { threadId: agentId, input: input });
        },
        SubmitWithFiles: async function(agentId, prompt, images, files) {
            const input = [{type: 'text', text: prompt}];
            if (images) images.forEach(p => input.push({type: 'localImage', path: p}));
            if (files) files.forEach(p => input.push({type: 'fileContent', path: p}));
            await rpcCall('turn/start', { threadId: agentId, input: input });
        },
        SelectProjectDir: async function() {
            try {
                const resp = await fetch('/select-project-dir', { method: 'POST' });
                if (!resp.ok) return '';
                const data = await resp.json();
                return data?.path || '';
            } catch (err) {
                console.warn('[shim] SelectProjectDir failed:', err);
                return '';
            }
        },
        SelectFiles: async function() {
            try {
                const resp = await fetch('/select-files', { method: 'POST' });
                if (!resp.ok) return [];
                const data = await resp.json();
                return Array.isArray(data?.paths) ? data.paths : [];
            } catch (err) {
                console.warn('[shim] SelectFiles failed:', err);
                return [];
            }
        },
        GetBuildInfo: async function() {
            try {
                const resp = await fetch('/build-info');
                if (!resp.ok) return '{}';
                const data = await resp.json();
                return JSON.stringify(data || {});
            } catch (err) {
                console.warn('[shim] GetBuildInfo failed:', err);
                return '{}';
            }
        },
        GetGroup: async function() { return ''; },
        GetLSPStatus: async function() { return '[]'; },
        GetLSPDiagnostics: async function() { return '{}'; },
        SaveClipboardImage: async function(base64) {
            console.warn('[shim] SaveClipboardImage not available in debug mode');
            return '';
        },
        ListAgents: async function() { return []; },
    };

    // Event system shim (SSE)
    const listeners = {};
    const RuntimeShim = {
        EventsOn: function(eventName, callback) {
            if (!listeners[eventName]) listeners[eventName] = [];
            listeners[eventName].push(callback);
        },
        EventsOff: function(eventName, callback) {
            if (!listeners[eventName]) return;
            if (!callback) {
                delete listeners[eventName];
                return;
            }
            listeners[eventName] = listeners[eventName].filter(fn => fn !== callback);
            if (listeners[eventName].length === 0) {
                delete listeners[eventName];
            }
        },
        EventsEmit: function(eventName, data) {
            (listeners[eventName] || []).forEach(fn => fn(data));
        },
    };

    // SSE 事件流 — 将 apiserver JSON-RPC 通知转为 Wails 事件格式
    // apiserver Notify 格式: {jsonrpc:"2.0", method:"agent/event/...", params:{threadId:..., delta:...}}
    // 前端期望格式:          {agent_id, type, data: JSON字符串}
    const methodToType = {
        // item/*: 与前端事件分发保持一致 (直传, 避免类型错配)
        'item/agentMessage/delta': 'item/agentMessage/delta',
        'item/agentMessage/started': 'item/started',
        'item/completed': 'item/completed',
        'item/started': 'item/started',
        'item/reasoning/textDelta': 'item/reasoning/textDelta',
        'item/commandExecution/outputDelta': 'item/commandExecution/outputDelta',
        'item/commandExecution/requestApproval': 'item/commandExecution/requestApproval',
        'item/fileChange/started': 'item/fileChange/started',
        'item/fileChange/completed': 'item/fileChange/completed',
        'item/userMessage': 'item/userMessage',

        // 线程/轮次事件 (直传)
        'turn/started': 'turn/started',
        'turn/completed': 'turn/completed',
        'thread/started': 'thread/started',
        'thread/tokenUsage/updated': 'thread/tokenUsage/updated',

        // 错误/MCP
        'error': 'error',
        'mcpServer/startupUpdate': 'mcpServer/startupUpdate',
        'mcpServer/startupComplete': 'mcpServer/startupComplete',

        // 兼容旧 agent/event/* 前缀
        'agent/event/agent_message_content_delta': 'item/agentMessage/delta',
        'agent/event/agent_message_delta': 'agent_message_delta',
        'agent/event/turn_started': 'turn/started',
        'agent/event/turn_completed': 'turn/completed',
        'agent/event/agent_reasoning_delta': 'item/reasoning/textDelta',
        'agent/event/exec_command_output_delta': 'item/commandExecution/outputDelta',
        'agent/event/exec_approval_request': 'item/commandExecution/requestApproval',
        'agent/event/patch_apply_begin': 'item/fileChange/started',
        'agent/event/patch_apply_end': 'item/fileChange/completed',
    };

    try {
        const sse = new EventSource(API + '/events');
        sse.onopen = function() {
            console.log('[shim] SSE connected to', API + '/events');
        };
        sse.onmessage = function(e) {
            try {
                const msg = JSON.parse(e.data);
                // JSON-RPC notification → Wails event
                if (msg.method && msg.params) {
                    const eventType = methodToType[msg.method] || msg.method.replace(/^agent\/event\//, '');
                    const agentId = msg.params.threadId || '';
                    console.log('[shim] SSE event:', msg.method, '→', eventType, 'agent:', agentId);
                    RuntimeShim.EventsEmit('bridge-event', {
                        type: eventType,
                        payload: msg.params,
                        data: JSON.stringify(msg.params),
                    });
                    RuntimeShim.EventsEmit('agent-event', {
                        agent_id: agentId,
                        type: eventType,
                        data: JSON.stringify(msg.params),
                    });
                } else {
                    console.log('[shim] SSE non-event msg:', msg);
                }
            } catch(err) {
                console.error('[shim] SSE parse error:', err, e.data);
            }
        };
        sse.onerror = function() {
            console.warn('[shim] SSE reconnecting...');
        };
    } catch(err) {
        console.error('[shim] SSE init error:', err);
    }

    // 挂载到 window
    window.go = { main: { App: AppShim } };
    window.runtime = RuntimeShim;

    console.log('[shim] ✓ Wails runtime shim loaded (debug mode)');
    console.log('[shim] API endpoint:', API);
})();
</script>`

// startDebugServer 启动调试 HTTP 服务器, 提供前端静态文件。
func startDebugServer(ctx context.Context) {
	// 查找 frontend 目录
	frontendDir := findFrontendDir()
	if frontendDir == "" {
		slog.Error("debug: frontend directory not found")
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/select-project-dir", handleDebugSelectProjectDir)
	mux.HandleFunc("/select-files", handleDebugSelectFiles)
	mux.HandleFunc("/build-info", handleDebugBuildInfo)
	mux.HandleFunc("/wails/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		fmt.Fprint(w, debugWailsRuntimeModule)
	})

	// 注入 shim 的 index.html handler
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			data, err := os.ReadFile(filepath.Join(frontendDir, "index.html"))
			if err != nil {
				http.Error(w, "index.html not found", 404)
				return
			}
			html := string(data)
			// 在 </head> 前注入 shim
			html = strings.Replace(html, "</head>", shimScript+"\n</head>", 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			fmt.Fprint(w, html)
			return
		}
		// 其他静态文件
		http.FileServer(http.Dir(frontendDir)).ServeHTTP(w, r)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", debugPort),
		Handler: mux,
	}

	go func() {
		slog.Info("debug: UI server started", "url", fmt.Sprintf("http://localhost:%d", debugPort))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("debug server failed", "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		server.Close()
	}()
}

// handleDebugSelectProjectDir 调试模式目录选择接口。
//
// POST /select-project-dir -> {"path":"..."}，取消返回空字符串。
func handleDebugSelectProjectDir(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slog.Info("debug: select project dir invoked")
	path, err := selectProjectDirNative()
	if err != nil {
		slog.Warn("debug: select project dir failed", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"path": path,
	})
}

// handleDebugSelectFiles 调试模式文件选择接口。
//
// POST /select-files -> {"paths":["/a","/b"]}
func handleDebugSelectFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slog.Info("debug: select files invoked")
	paths, err := selectFilesNative()
	if err != nil {
		slog.Warn("debug: select files failed", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"paths": paths,
	})
}

// handleDebugBuildInfo 返回当前构建信息。
func handleDebugBuildInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(currentBuildInfo())
}

// selectProjectDirNative 打开系统目录选择对话框。
//
// 用户取消返回空字符串和 nil error。
func selectProjectDirNative() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		script := `try
POSIX path of (choose folder with prompt "选择项目目录")
on error number -128
return ""
end try`
		out, err := exec.Command("osascript", "-e", script).CombinedOutput()
		if err != nil {
			detail := strings.TrimSpace(string(out))
			if detail != "" {
				return "", fmt.Errorf("osascript choose folder failed: %w (%s)", err, detail)
			}
			return "", fmt.Errorf("osascript choose folder failed: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	case "windows":
		script := `Add-Type -AssemblyName System.Windows.Forms | Out-Null; $d = New-Object System.Windows.Forms.FolderBrowserDialog; if($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){ Write-Output $d.SelectedPath }`
		out, err := exec.Command("powershell", "-NoProfile", "-STA", "-Command", script).Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return "", nil
			}
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	default:
		for _, picker := range []struct {
			name string
			args []string
		}{
			{name: "zenity", args: []string{"--file-selection", "--directory", "--title=选择项目目录"}},
			{name: "kdialog", args: []string{"--getexistingdirectory", "~", "选择项目目录"}},
		} {
			out, err := exec.Command(picker.name, picker.args...).Output()
			if err == nil {
				return strings.TrimSpace(string(out)), nil
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				continue
			}
			if errors.Is(err, exec.ErrNotFound) {
				continue
			}
		}
		return "", fmt.Errorf("no supported folder picker found on %s", runtime.GOOS)
	}
}

// selectFilesNative 打开系统文件选择对话框(支持多选)。
//
// 用户取消返回空数组和 nil error。
func selectFilesNative() ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		script := `try
set picked to choose file with prompt "选择附件文件" with multiple selections allowed
if class of picked is list then
set out to ""
repeat with f in picked
set out to out & POSIX path of f & linefeed
end repeat
return out
else
return POSIX path of picked
end if
on error number -128
return ""
end try`
		out, err := exec.Command("osascript", "-e", script).CombinedOutput()
		if err != nil {
			detail := strings.TrimSpace(string(out))
			if detail != "" {
				return nil, fmt.Errorf("osascript choose file failed: %w (%s)", err, detail)
			}
			return nil, fmt.Errorf("osascript choose file failed: %w", err)
		}
		return splitPickerOutput(string(out)), nil
	case "windows":
		script := `$ofd = New-Object System.Windows.Forms.OpenFileDialog; $ofd.Multiselect = $true; if($ofd.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){ $ofd.FileNames | ForEach-Object { Write-Output $_ } }`
		out, err := exec.Command("powershell", "-NoProfile", "-STA", "-Command", script).Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return []string{}, nil
			}
			return nil, err
		}
		return splitPickerOutput(string(out)), nil
	default:
		for _, picker := range []struct {
			name string
			args []string
		}{
			{name: "zenity", args: []string{"--file-selection", "--multiple", "--separator=\n", "--title=选择附件文件"}},
			{name: "kdialog", args: []string{"--getopenfilename", "~", "*", "--multiple", "--separate-output"}},
		} {
			out, err := exec.Command(picker.name, picker.args...).Output()
			if err == nil {
				return splitPickerOutput(string(out)), nil
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				continue
			}
			if errors.Is(err, exec.ErrNotFound) {
				continue
			}
		}
		return nil, fmt.Errorf("no supported file picker found on %s", runtime.GOOS)
	}
}

func splitPickerOutput(raw string) []string {
	lines := strings.Split(raw, "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// findFrontendDir 查找 frontend 目录 (相对于可执行文件或 cwd)。
func findFrontendDir() string {
	// 尝试 1: 相对于 cwd
	candidates := []string{
		"cmd/agent-terminal/frontend",
		"frontend",
	}

	// 尝试 2: 相对于可执行文件
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "frontend"),
			filepath.Join(exeDir, "cmd", "agent-terminal", "frontend"),
		)
	}

	// 尝试 3: 相对于源文件
	_, file, _, ok := runtime.Caller(0)
	if ok {
		srcDir := filepath.Dir(file)
		candidates = append(candidates, filepath.Join(srcDir, "frontend"))
	}

	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(dir)
			slog.Info("debug: frontend directory found", "path", abs)
			return abs
		}
	}

	return ""
}

// 确保 fs.FS 接口满足 (供 embed 使用)。
var _ fs.FS = assets
