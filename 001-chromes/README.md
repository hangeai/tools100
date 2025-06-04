# chromes 多开管理器实现方案

## 目标
- 使用 Go 语言和 fyne GUI 框架实现一个 Chrome 多开管理器。
- 程序维护一个配置列表，每项包含名称和 user-data-dir 路径。
- 主程序和 Chrome 实例解耦，互不影响生命周期。
- 提供清晰的配置项状态显示（运行中/已停止）及相应的操作（启动/停止）。

## 构建运行
1.  确保 Go 环境已配置, 版本 1.18 及以上, 通过 `go version` 检查。
2.  在 `chromes` 目录下执行 `go mod tidy` 下载依赖。
3.  `export CGO_ENABLED=1` 确保 CGO 可用（Fyne 需要）。
    -   如果在 Windows 上运行，确保安装了 Visual Studio 的 C++ 开发工具集。
    -   如果在 macOS 上运行，确保安装了 Xcode 命令行工具。
    -   如果在 Linux 上运行，确保安装了 `build-essential` 包。
4.  执行 `go run main.go` 启动程序 (从 `chromes` 目录内，或 `go run chromes/main.go` 从项目根目录)。
    或者构建可执行文件：`go build -o chromes_manager main.go` 然后运行 `./chromes_manager`。

## 核心设计思想
1.  **数据与UI分离**：
    *   `config.ChromeConfig` 结构体负责存储配置的持久化信息（名称、路径）以及非持久化的运行时状态（`*exec.Cmd`、`IsRunning` 标志、`sync.Mutex`）。
    *   运行时状态字段使用 `json:"-"` 标签，确保不被写入 JSON 配置文件。
    *   UI（主要在 `main.go` 的 `widget.List` 的 `UpdateItem` 回调中）根据 `ChromeConfig` 的状态动态渲染，实现数据驱动UI。
2.  **进程管理与状态同步**：
    *   通过 `chrome.StartChrome` 启动 Chrome 实例，该函数返回 `*exec.Cmd` 以便管理进程。
    *   每个配置项的 `IsRunning` 状态用于防止重复启动同一配置。
    *   为每个启动的进程创建一个 goroutine，在其中调用 `Cmd.Wait()` 来异步监控进程的退出。
    *   当进程退出时，goroutine 更新对应 `ChromeConfig` 的 `IsRunning` 状态，并通过 Fyne 的主循环机制 (`globalApp.Driver().CallOnMain`) 触发UI刷新，确保线程安全。
3.  **生命周期解耦**：
    *   `chromes` 主程序与各个 Chrome 实例进程保持独立的生命周期。
    *   主程序关闭不影响已启动的 Chrome 实例；
    *   Chrome 实例的关闭会被主程序检测到并更新UI状态（如果主程序仍在运行）。

## 主要功能
1.  **配置管理**：
    *   配置项包含：名称（持久化）、用户数据目录路径（持久化）、运行时命令对象（非持久化）、运行状态标志（非持久化）、互斥锁（非持久化）。
    *   配置通过 `config/config.go` 中的函数进行加载和保存到用户 `configs.json` 配置文件。
    *   支持通过UI新增配置项，并进行简单的重名/重路径检查。
    *   (未来可扩展：编辑、删除配置项)。
2.  **Chrome 实例控制**：
    *   通过 `open -n -a "Google Chrome" --args --wait-apps --user-data-dir=<路径>` 启动 Chrome 实例，确保为新实例并允许 `Cmd.Wait()` 工作。
    *   列表项清晰显示每个配置的当前状态：“运行中”（例如绿色）或“已停止”（例如灰色）。
    *   根据状态提供“启动”或“停止”按钮。
    *   “停止”按钮通过 `Cmd.Process.Kill()` 终止对应进程。
3.  **用户界面 (fyne)**：
    *   主界面使用 `widget.List` 展示配置项。
    *   每个列表项包含配置名称、路径、状态指示器和操作按钮。
    *   提供输入字段和按钮用于新增配置。

## 代码结构
-   `main.go`：应用主入口，负责初始化 Fyne 应用、窗口、UI布局（包括配置列表、新增配置区域），处理用户交互，调用配置管理和 Chrome 控制逻辑，协调UI更新。
-   `config/config.go`：定义 `ChromeConfig` 结构体（包含持久化数据和运行时状态），提供加载 (`LoadConfigs`) 和保存 (`SaveConfigs`) 配置到 JSON 文件的功能。
-   `chrome/chrome.go`：封装启动 Chrome 实例的逻辑 (`StartChrome` 函数)，返回 `*exec.Cmd` 对象。
