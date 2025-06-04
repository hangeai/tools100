package config

import (
    "encoding/json"
    "fmt"
    "log"
    "os"
    "path/filepath"
    "runtime"
    "strings"
)

// ChromeConfig 存储每个 Chrome 实例的基本配置信息。
// 这些信息用于启动和识别特定的 Chrome 浏览器会话。
// 运行时状态（如进程命令、运行状态标志和互斥锁）由 `chrome.ChromeInstance` 管理。
type ChromeConfig struct {
    Name        string `json:"name"`          // 配置的名称，用于用户界面显示和识别
    UserDataDir string `json:"user_data_dir"` // Chrome 用户数据目录的路径，用于隔离不同的浏览器实例
    IsDefault   bool   `json:"-"`             // 标记是否为默认实例，不序列化到json
}

// configFile 定义了存储 Chrome 配置的 JSON 文件的名称和相对路径。
var configFile = getDefaultConfigFile() // 修改为调用函数获取路径

// DefaultChromeConfigName 定义了默认 Chrome 实例的名称
const DefaultChromeConfigName = "[默认配置]"

// GetDefaultUserDataDir 返回当前操作系统的默认 Chrome 用户数据目录。
// 注意：这些路径是常见的默认值，可能因 Chrome 版本或安装方式而异。
func GetDefaultUserDataDir() string {
    var path string
    switch runtime.GOOS {
    case "windows":
        // 通常是 C:\\Users\\<Username>\\AppData\\Local\\Google\\Chrome\\User Data
        path = filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "User Data")
    case "darwin": // macOS
        // 通常是 ~/Library/Application Support/Google/Chrome
        homeDir, err := os.UserHomeDir()
        if err != nil {
            log.Printf("Error getting home directory: %v", err)
            return "" // Or a sensible fallback
        }
        path = filepath.Join(homeDir, "Library", "Application Support", "Google", "Chrome")
    case "linux":
        // 通常是 ~/.config/google-chrome
        homeDir, err := os.UserHomeDir()
        if err != nil {
            log.Printf("Error getting home directory: %v", err)
            return "" // Or a sensible fallback
        }
        path = filepath.Join(homeDir, ".config", "google-chrome")
    default:
        path = "" // 不支持的操作系统或无法确定
    }
    // 对于默认实例，我们约定 UserDataDir 为空字符串，由 chrome/chrome.go 中的逻辑特殊处理
    // 此函数返回的是实际的默认路径，用于校验用户是否尝试添加这个路径
    return path
}

// getDefaultConfigFile 根据操作系统确定配置文件的默认路径。
func getDefaultConfigFile() string {
    var configDir string
    switch runtime.GOOS {
    case "windows":
        appData := os.Getenv("APPDATA")
        if appData == "" {
            // 如果 APPDATA 未设置，则回退到用户主目录
            homeDir, err := os.UserHomeDir()
            if err != nil {
                // 如果无法获取主目录，则使用当前目录
                return filepath.Join(".", "chromes", "configs.json")
            }
            configDir = filepath.Join(homeDir, "AppData", "Roaming", "chromes")
        } else {
            configDir = filepath.Join(appData, "chromes")
        }
    case "darwin", "linux":
        homeDir, err := os.UserHomeDir()
        if err != nil {
            // 如果无法获取主目录，则使用当前目录
            return filepath.Join(".", "chromes", "configs.json")
        }
        configDir = filepath.Join(homeDir, ".config", "chromes")
    default: // 其他操作系统
        homeDir, err := os.UserHomeDir()
        if err != nil {
            // 如果无法获取主目录，则使用当前目录
            return filepath.Join(".", "chromes", "configs.json")
        }
        configDir = filepath.Join(homeDir, ".chromes") // 或者一个更通用的位置
    }
    return filepath.Join(configDir, "configs.json")
}

// LoadConfigs 从 JSON 文件加载 Chrome 配置列表。
// 总是会在列表开头添加一个代表默认 Chrome 实例的配置。
func LoadConfigs() []*ChromeConfig {
    defaultInstance := &ChromeConfig{
        Name:        DefaultChromeConfigName, //  "Default"
        UserDataDir: "",                      // 空字符串表示默认实例
        IsDefault:   true,
    }

    data, err := os.ReadFile(configFile)
    if err != nil {
        if os.IsNotExist(err) {
            // 配置文件不存在时，只返回默认实例
            return []*ChromeConfig{defaultInstance}
        }
        log.Printf("[load] read failed. path=%v, err=%v", configFile, err)
        // 读取失败也返回默认实例，确保程序基本可用
        return []*ChromeConfig{defaultInstance}
    }
    log.Printf("[load] read success. path=%v, size=%d bytes", configFile, len(data))

    var userConfigs []*ChromeConfig
    if err = json.Unmarshal(data, &userConfigs); err != nil {
        log.Printf("[load] json failed. path=%v, err=%v", configFile, err)
        // 解析失败也返回默认实例
        return []*ChromeConfig{defaultInstance}
    }

    // 校验加载的配置，确保没有用户配置的 UserDataDir 与实际的默认路径冲突
    // 或者 Name 与 DefaultChromeConfigName 冲突
    actualDefaultDir := GetDefaultUserDataDir()
    validUserConfigs := make([]*ChromeConfig, 0, len(userConfigs))
    for _, cfg := range userConfigs {
        if cfg.Name == DefaultChromeConfigName {
            log.Printf("Warning: Config name '%s' is reserved for the default instance and will be ignored from file.", cfg.Name)
            continue
        }
        // 将路径转换为绝对路径并进行比较
        absCfgPath, errCfg := filepath.Abs(cfg.UserDataDir)
        absDefaultPath, errDef := filepath.Abs(actualDefaultDir)

        if actualDefaultDir != "" && errCfg == nil && errDef == nil && strings.EqualFold(absCfgPath, absDefaultPath) {
            log.Printf("Warning: Config '%s' uses the default Chrome profile path '%s' and will be ignored.", cfg.Name, cfg.UserDataDir)
            continue
        }
        if cfg.UserDataDir == "" { // 用户配置的 UserDataDir 不应为空
            log.Printf("Warning: Config '%s' has an empty UserDataDir and will be ignored.", cfg.Name)
            continue
        }
        cfg.IsDefault = false // 明确标记非默认
        validUserConfigs = append(validUserConfigs, cfg)
    }

    // 将默认实例放在列表首位
    allConfigs := make([]*ChromeConfig, 0, len(validUserConfigs)+1)
    allConfigs = append(allConfigs, defaultInstance)
    allConfigs = append(allConfigs, validUserConfigs...)

    return allConfigs
}

// SaveConfigs 将 Chrome 配置列表保存到 JSON 文件。
// "默认"实例不会被保存到文件中。
// 在保存前会检查是否有自定义配置与默认路径冲突。
func SaveConfigs(cfgs []*ChromeConfig) error {
    userConfigs := make([]*ChromeConfig, 0, len(cfgs))
    actualDefaultDir := GetDefaultUserDataDir()

    for _, cfg := range cfgs {
        if cfg.IsDefault { // 跳过默认实例
            continue
        }
        if cfg.Name == DefaultChromeConfigName {
            return fmt.Errorf("config name '%s' is reserved for the default instance", DefaultChromeConfigName)
        }
        if cfg.UserDataDir == "" {
            return fmt.Errorf("user-defined config '%s' cannot have an empty UserDataDir", cfg.Name)
        }

        // 检查是否与实际的默认路径冲突
        absCfgPath, errCfg := filepath.Abs(cfg.UserDataDir)
        absDefaultPath, errDef := filepath.Abs(actualDefaultDir)

        if actualDefaultDir != "" && errCfg == nil && errDef == nil && strings.EqualFold(absCfgPath, absDefaultPath) {
            return fmt.Errorf("config '%s' cannot use the default Chrome profile path: %s", cfg.Name, cfg.UserDataDir)
        }
        userConfigs = append(userConfigs, cfg)
    }

    data, err := json.MarshalIndent(userConfigs, "", "  ")
    if err != nil {
        return err
    }
    // 确保目录存在 (如果 configFile 包含子目录)
    // Ensure directory exists (if configFile includes subdirectories)
    if err := os.MkdirAll(filepath.Dir(configFile), 0750); err != nil {
        return err
    }
    return os.WriteFile(configFile, data, 0640)
}

// AddConfig 向配置列表中添加一个新的 ChromeConfig，并保存。
// 会检查 name 和 user_data_dir 是否重复，以及 user_data_dir 是否为默认路径。
func AddConfig(name string, userDataDir string, currentConfigs []*ChromeConfig) ([]*ChromeConfig, error) {
    if name == DefaultChromeConfigName {
        return currentConfigs, fmt.Errorf("cannot add config with reserved name '%s'", DefaultChromeConfigName)
    }
    if strings.TrimSpace(userDataDir) == "" {
        return currentConfigs, fmt.Errorf("user data directory cannot be empty for a custom profile")
    }

    actualDefaultDir := GetDefaultUserDataDir()
    absNewPath, errNew := filepath.Abs(userDataDir)
    absDefaultPath, errDef := filepath.Abs(actualDefaultDir)

    if actualDefaultDir != "" && errNew == nil && errDef == nil && strings.EqualFold(absNewPath, absDefaultPath) {
        return currentConfigs, fmt.Errorf("the user data directory '%s' is reserved for the default Chrome profile", userDataDir)
    }

    for _, cfg := range currentConfigs {
        if cfg.IsDefault {
            continue // 跳过与默认实例的比较，因为它的 UserDataDir 是 ""
        }
        if cfg.Name == name {
            return currentConfigs, fmt.Errorf("config name '%s' already exists", name)
        }
        // 比较绝对路径以避免大小写和相对路径问题
        absExistingPath, errExisting := filepath.Abs(cfg.UserDataDir)
        if errExisting == nil && errNew == nil && strings.EqualFold(absExistingPath, absNewPath) {
            return currentConfigs, fmt.Errorf("user data directory '%s' (resolved to '%s') already exists in config '%s'", userDataDir, absNewPath, cfg.Name)
        }
    }

    newConfig := &ChromeConfig{Name: name, UserDataDir: userDataDir, IsDefault: false}
    updatedConfigs := append(currentConfigs, newConfig)

    if err := SaveConfigs(updatedConfigs); err != nil {
        return currentConfigs, fmt.Errorf("failed to save configs after adding: %w", err)
    }
    return updatedConfigs, nil
}

// RemoveConfig 从配置列表中移除指定名称的 ChromeConfig，并保存。
// 不允许删除 "Default" 实例。
func RemoveConfig(name string, currentConfigs []*ChromeConfig) ([]*ChromeConfig, error) {
    if name == DefaultChromeConfigName {
        return currentConfigs, fmt.Errorf("cannot remove the default Chrome instance")
    }

    found := false
    updatedConfigs := make([]*ChromeConfig, 0)
    for _, cfg := range currentConfigs {
        if cfg.Name == name {
            found = true
            continue // Skip this config
        }
        updatedConfigs = append(updatedConfigs, cfg)
    }

    if (!found) {
        return currentConfigs, fmt.Errorf("config name '%s' not found", name)
    }

    if err := SaveConfigs(updatedConfigs); err != nil {
        return currentConfigs, fmt.Errorf("failed to save configs after removing: %w", err)
    }
    return updatedConfigs, nil
}
