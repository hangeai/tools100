package chrome

import (
    "chromes/config"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "strconv"
    "strings"
    "sync"
    "syscall"
)

// Instance 封装了一个 Chrome 进程及其配置和运行时状态。
// 它负责管理单个 Chrome 浏览器实例的生命周期。
type Instance struct {
    config    *config.ChromeConfig // 实例的配置信息
    cmd       *exec.Cmd            // 运行中的 Chrome 进程命令对象
    isRunning bool                 // 标记 Chrome 实例当前是否正在运行
    mu        sync.Mutex           // 用于保护对此结构体内部状态（cmd, isRunning）的并发访问
}

// NewInstance 根据给定的配置创建一个新的 Instance。
// cfg: Chrome 配置对象。
// 返回一个新的 Instance 指针。
func NewInstance(cfg *config.ChromeConfig) *Instance {
    return &Instance{
        config:    cfg,
        isRunning: isChromeDirInUse(cfg.UserDataDir),
    }
}

// Config 返回此 Chrome 实例的配置信息。
func (ci *Instance) Config() *config.ChromeConfig {
    return ci.config
}

// Start 启动 Chrome 实例。
// 它会根据操作系统类型和配置中的用户数据目录来构建并执行启动命令。
// 如果实例已在运行，则返回错误。
func (ci *Instance) Start() error {
    ci.mu.Lock() // 获取锁以修改共享状态
    defer ci.mu.Unlock()

    if ci.isRunning {
        return fmt.Errorf("chrome instance %s is already running", ci.config.Name)
    }

    var cmd *exec.Cmd
    var err error

    userDataDir := ci.config.UserDataDir // 从配置中获取用户数据目录

    // 根据不同操作系统构建 Chrome 启动命令
    switch runtime.GOOS {
    case "darwin": // macOS
        chromePath := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
        args := []string{}
        if userDataDir != "" {
            absPath, err := filepath.Abs(userDataDir) // 确保路径是绝对路径
            if err != nil {
                return fmt.Errorf("failed to get absolute path for %s: %w", userDataDir, err)
            }
            args = append(args, "--user-data-dir="+absPath)
        }
        args = append(args, "--no-first-run", "--no-default-browser-check") // 添加通用启动参数
        cmd = exec.Command(chromePath, args...)
    case "windows":
        chromePath := "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe" // Windows Chrome路径
        args := []string{}
        if userDataDir != "" {
            args = append(args, "--user-data-dir="+strings.ReplaceAll(userDataDir, "/", "\\")) // 适配Windows路径分隔符
        }
        args = append(args, "--no-first-run", "--no-default-browser-check")
        cmd = exec.Command(chromePath, args...)
    case "linux":
        chromePath := "google-chrome" // Linux 下通常的 Chrome 命令
        args := []string{}
        if userDataDir != "" {
            args = append(args, "--user-data-dir="+userDataDir)
        }
        args = append(args, "--no-first-run", "--no-default-browser-check")
        cmd = exec.Command(chromePath, args...)
    default: // 其他或未知操作系统
        args := []string{}
        if userDataDir != "" {
            args = append(args, "--user-data-dir="+userDataDir)
        }
        args = append(args, "--no-first-run", "--no-default-browser-check")
        cmd = exec.Command("chrome", args...) // 尝试通用 "chrome" 命令
    }

    err = cmd.Start() // 异步启动 Chrome 进程
    if err != nil {
        return fmt.Errorf("failed to start chrome %s (dir: %s): %w", ci.config.Name, userDataDir, err)
    }

    ci.cmd = cmd        // 保存命令对象
    ci.isRunning = true // 更新运行状态
    return nil
}

// Stop 停止 Chrome 实例。
// 它首先尝试通过已保存的 cmd 对象发送 SIGTERM 信号来优雅地关闭进程。
// 如果失败或 cmd 对象不存在（例如，应用重启后），则会尝试通过用户数据目录来查找并停止进程。
func (ci *Instance) Stop() error {
    ci.mu.Lock() // 获取锁以修改共享状态
    defer ci.mu.Unlock()

    if !ci.isRunning {
        return fmt.Errorf("chrome instance %s is not running", ci.config.Name)
    }

    // 优先尝试通过已知的进程对象停止
    if ci.cmd != nil && ci.cmd.Process != nil {
        err := ci.cmd.Process.Signal(syscall.SIGTERM) // 发送终止信号
        if err != nil {
            // 如果发送信号失败 (例如进程已自行退出)，则尝试备用方法
            // 这也处理了进程可能已经不存在的情况
            stopped, stopErr := chromeStop(ci.config.UserDataDir)
            if stopErr != nil {
                return fmt.Errorf("failed to send signal to %s and fallback stop failed: %v, fallback error: %v", ci.config.Name, err, stopErr)
            }
            if !stopped {
                // 备用方法也未能找到或停止进程
                return fmt.Errorf("failed to send signal to %s and fallback stop could not find process: %v", ci.config.Name, err)
            }
        }
        // 信号发送成功或备用方法成功后，标记为非运行
        // Wait() goroutine (如果存在) 会处理 cmd.Wait() 的返回
    } else {
        // 如果没有 cmd 对象 (例如应用重启后，只知道配置和目录)
        // 则直接尝试通过用户数据目录停止
        stopped, err := chromeStop(ci.config.UserDataDir)
        if err != nil {
            return fmt.Errorf("failed to stop chrome %s (dir: %s) by user data dir: %w", ci.config.Name, ci.config.UserDataDir, err)
        }
        if !stopped {
            // 无法通过用户数据目录找到并停止进程
            return fmt.Errorf("could not find chrome process for %s (dir: %s) to stop", ci.config.Name, ci.config.UserDataDir)
        }
    }

    ci.isRunning = false // 标记为已停止
    // ci.cmd = nil // cmd 的清理最好在 Wait() 成功返回后，或 chromeStop 确认进程已消失后
    // 此处仅更新 isRunning 状态，UI 会据此刷新
    return nil
}

// IsRunning 返回 Chrome 实例是否正在运行。
// 它会检查 isRunning 标志，并且如果存在 cmd 对象，还会检查进程是否已退出。
func (ci *Instance) IsRunning() bool {
    ci.mu.Lock()
    defer ci.mu.Unlock()

    // 如果有 cmd 对象并且其关联的进程已退出，则更新状态
    if ci.cmd != nil && ci.cmd.ProcessState != nil && ci.cmd.ProcessState.Exited() {
        ci.isRunning = false
        ci.cmd = nil // 清理已退出的进程命令对象
    }
    return ci.isRunning
}

// SetRunningState 允许外部逻辑（例如，应用启动时通过 isChromeDirInUse 检测）更新实例的运行状态。
// isRunning: true 表示正在运行, false 表示已停止。
func (ci *Instance) SetRunningState(isRunning bool) {
    ci.mu.Lock()
    defer ci.mu.Unlock()
    ci.isRunning = isRunning
    if !isRunning {
        ci.cmd = nil // 如果设置为非运行状态，则清除 cmd 对象
    }
}

// Wait 等待由 Start() 方法启动的 Chrome 进程结束。
// 此方法是阻塞的，通常应该在一个单独的 goroutine 中调用。
// 当进程退出后，它会更新实例的运行状态。
// 返回进程的退出错误（如果有）。
func (ci *Instance) Wait() error {
    currentCmd := func() *exec.Cmd { // Safely get current command
        ci.mu.Lock()
        defer ci.mu.Unlock()
        return ci.cmd
    }()

    if currentCmd == nil {
        ci.mu.Lock()
        wasRunning := ci.isRunning
        ci.isRunning = false // 确保状态一致性
        ci.cmd = nil         // 确保 cmd 清理
        ci.mu.Unlock()
        if wasRunning {
        }
        return nil
    }

    err := currentCmd.Wait() // 等待进程退出

    // 进程已退出，更新状态
    ci.mu.Lock()
    ci.isRunning = false
    ci.cmd = nil // 清理命令对象
    ci.mu.Unlock()

    return err // 返回 Wait 的错误（通常是 nil 或 *ExitError）
}

// isChromeDirInUse 检查指定的用户数据目录是否被Chrome进程正在使用
// 如果 userDataDir 为空字符串，则检查默认的 Chrome 实例（未明确指定 --user-data-dir 的实例）是否正在运行
func isChromeDirInUse(userDataDir string) bool {
    var cmd *exec.Cmd
    var output []byte
    var err error

    if userDataDir == "" { // Check for default instance (no --user-data-dir arg)
        switch runtime.GOOS {
        case "darwin":
            script := `ps -eo command | grep "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" | grep -v grep | grep -v -- '--user-data-dir='`
            cmd = exec.Command("sh", "-c", script)
        case "windows":
            psScript := `Get-CimInstance Win32_Process -Filter "Name='chrome.exe'" | Where-Object {$_.CommandLine -notlike '*--user-data-dir=*'} | Select-Object -ExpandProperty ProcessId`
            cmd = exec.Command("powershell", "-Command", psScript)
        case "linux":
            script := `ps -eo command | grep -E '(^|/)google-chrome( |$)' | grep -v grep | grep -v -- '--user-data-dir='`
            cmd = exec.Command("sh", "-c", script)
        default:
            return false // Unsupported OS for this specific default check
        }
        output, err = cmd.Output()
        if err != nil {
            return false // Error executing command or no process found
        }
        return len(strings.TrimSpace(string(output))) > 0

    } else { // Check for specific userDataDir
        absUserDataDir := userDataDir
        var errPath error
        if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
            absUserDataDir, errPath = filepath.Abs(userDataDir)
            if errPath != nil {
                fmt.Printf("Warning: could not get absolute path for %s: %v\n", userDataDir, errPath)
                absUserDataDir = userDataDir
            }
        } else if runtime.GOOS == "windows" {
            absUserDataDir, errPath = filepath.Abs(userDataDir)
            if errPath != nil {
                fmt.Printf("Warning: could not get absolute path for %s: %v\n", userDataDir, errPath)
                absUserDataDir = userDataDir
            }
            absUserDataDir = strings.ReplaceAll(absUserDataDir, "/", "\\\\")
        }

        switch runtime.GOOS {
        case "darwin":
            chromeExecutablePath := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
            cmd = exec.Command("pgrep", "-f", fmt.Sprintf("%s.*--user-data-dir=%s", chromeExecutablePath, absUserDataDir))
        case "windows":
            psScript := fmt.Sprintf(`Get-CimInstance Win32_Process -Filter "Name='chrome.exe' AND CommandLine LIKE '%%%%--user-data-dir=%s%%%%'" | Select-Object -ExpandProperty ProcessId`, absUserDataDir)
            cmd = exec.Command("powershell", "-Command", psScript)
        case "linux":
            cmd = exec.Command("pgrep", "-f", fmt.Sprintf("chrome.*--user-data-dir=%s", absUserDataDir))
        default:
            cmd = exec.Command("pgrep", "-f", fmt.Sprintf("chrome.*--user-data-dir=%s", absUserDataDir))
        }
        output, err = cmd.Output()
        if err != nil {
            return false // Error executing command or no process found
        }
        return len(strings.TrimSpace(string(output))) > 0
    }
}

// chromeStop 通过用户数据目录停止Chrome进程
// 如果 userDataDir 为空字符串，则尝试停止默认的 Chrome 实例（未指定 --user-data-dir）
func chromeStop(userDataDir string) (bool, error) {
    var pidsToKill []string

    if userDataDir == "" { // Stop default instance (no --user-data-dir arg)
        var cmd *exec.Cmd
        var output []byte
        var err error
        switch runtime.GOOS {
        case "darwin":
            script := `ps -eo pid,command | grep "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" | grep -v grep | grep -v -- '--user-data-dir=' | awk '{print $1}'`
            cmd = exec.Command("sh", "-c", script)
        case "windows":
            psScript := `(Get-CimInstance Win32_Process -Filter "Name='chrome.exe'" | Where-Object {$_.CommandLine -notlike '*--user-data-dir=*'} | Select-Object -ExpandProperty ProcessId) -join ','`
            cmd = exec.Command("powershell", "-Command", psScript)
        case "linux":
            script := `ps -eo pid,command | grep -E '(^|/)google-chrome( |$)' | grep -v grep | grep -v -- '--user-data-dir=' | awk '{print $1}'`
            cmd = exec.Command("sh", "-c", script)
        default:
            return false, fmt.Errorf("unsupported OS for stopping default chrome instance")
        }
        output, err = cmd.Output()
        if err != nil {
            if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 && len(output) == 0 {
                return false, nil // No process found
            }
            return false, fmt.Errorf("failed to find default chrome process to stop: %w", err)
        }
        pidsStr := strings.TrimSpace(string(output))
        if pidsStr == "" {
            return false, nil // No process found
        }
        if runtime.GOOS == "windows" {
            pidsToKill = strings.Split(pidsStr, ",")
        } else {
            pidsToKill = strings.Split(pidsStr, "\n")
        }

    } else { // Stop instance with specific userDataDir
        var findCmd *exec.Cmd
        var output []byte
        var err error
        absUserDataDir := userDataDir
        var errPath error

        if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
            absUserDataDir, errPath = filepath.Abs(userDataDir)
            if errPath != nil {
                fmt.Printf("Warning: could not get absolute path for %s: %v\n", userDataDir, errPath)
                absUserDataDir = userDataDir
            }
        } else if runtime.GOOS == "windows" {
            absUserDataDir, errPath = filepath.Abs(userDataDir)
            if errPath != nil {
                fmt.Printf("Warning: could not get absolute path for %s: %v\n", userDataDir, errPath)
                absUserDataDir = userDataDir
            }
            absUserDataDir = strings.ReplaceAll(absUserDataDir, "/", "\\\\")
        }

        switch runtime.GOOS {
        case "darwin":
            chromeExecutablePath := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
            pgrepPattern := fmt.Sprintf("%s.*--user-data-dir=%s", chromeExecutablePath, absUserDataDir)
            findCmd = exec.Command("pgrep", "-f", pgrepPattern)
        case "windows":
            psScript := fmt.Sprintf(`(Get-CimInstance Win32_Process -Filter "Name='chrome.exe' AND CommandLine LIKE '%%%%--user-data-dir=%s%%%%'" | Select-Object -ExpandProperty ProcessId) -join ','`, absUserDataDir)
            findCmd = exec.Command("powershell", "-Command", psScript)
        case "linux":
            pgrepPattern := fmt.Sprintf("chrome.*--user-data-dir=%s", absUserDataDir)
            findCmd = exec.Command("pgrep", "-f", pgrepPattern)
        default:
            return false, fmt.Errorf("unsupported OS for stopping chrome instance by user data dir: %s", runtime.GOOS)
        }

        output, err = findCmd.Output()
        if err != nil {
            if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
                return false, nil // No process found
            }
            return false, fmt.Errorf("find process failed for %s: %w", userDataDir, err)
        }
        pidsStr := strings.TrimSpace(string(output))
        if pidsStr == "" {
            return false, nil // No process found
        }
        if runtime.GOOS == "windows" {
            pidsToKill = strings.Split(pidsStr, ",")
        } else {
            pidsToKill = strings.Split(pidsStr, "\n")
        }
    }

    if len(pidsToKill) == 0 || (len(pidsToKill) == 1 && strings.TrimSpace(pidsToKill[0]) == "") {
        return false, nil // No PIDs found or only empty strings
    }

    killedAtLeastOne := false
    var lastKillError error
    for _, pidStr := range pidsToKill {
        pidStr = strings.TrimSpace(pidStr)
        if pidStr == "" {
            continue
        }
        pid, err := strconv.Atoi(pidStr)
        if err != nil {
            fmt.Printf("Warning: invalid PID string \"%s\": %v\n", pidStr, err)
            lastKillError = fmt.Errorf("invalid PID string \"%s\": %w", pidStr, err)
            continue
        }

        var killErr error
        if runtime.GOOS == "windows" {
            killCmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F")
            killErr = killCmd.Run()
        } else {
            process, err := os.FindProcess(pid)
            if err != nil {
                if err == syscall.ESRCH || strings.Contains(err.Error(), "process already finished") {
                    killedAtLeastOne = true
                    continue
                }
                fmt.Printf("Warning: failed to find process for PID %d: %v\n", pid, err)
                lastKillError = fmt.Errorf("failed to find process for PID %d: %w", pid, err)
                continue
            }
            killErr = process.Signal(syscall.SIGTERM)
        }

        if killErr != nil {
            if runtime.GOOS == "windows" {
                if exitErr, ok := killErr.(*exec.ExitError); ok && exitErr.ExitCode() == 128 {
                    killedAtLeastOne = true
                    continue
                }
            } else if killErr == syscall.ESRCH {
                killedAtLeastOne = true
                continue
            }
            fmt.Printf("Warning: failed to send SIGTERM to PID %d: %v\n", pid, killErr)
            lastKillError = fmt.Errorf("failed to kill PID %d: %w", pid, killErr)
        } else {
            killedAtLeastOne = true
        }
    }

    if !killedAtLeastOne && lastKillError != nil {
        return false, lastKillError
    }
    return killedAtLeastOne, nil
}
