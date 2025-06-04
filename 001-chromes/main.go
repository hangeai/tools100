package main

import (
    "image/color"
    "log"

    "fyne.io/fyne/v2"
    "fyne.io/fyne/v2/app" // ignore errors here, use CGO_ENABLED=1 for build
    "fyne.io/fyne/v2/canvas"
    "fyne.io/fyne/v2/container"
    "fyne.io/fyne/v2/dialog"
    "fyne.io/fyne/v2/widget"

    "chromes/chrome"
    "chromes/config"
)

func main() {
    var instances []*chrome.Instance
    var configs []*config.ChromeConfig // 用于跟踪原始配置，主要用于保存

    myApp := app.New()
    w := myApp.NewWindow("Chromes -- Chrome 多开管理器")

    // 重新加载实例并刷新列表的辅助函数
    reloadInstancesAndRefreshList := func(list *widget.List) {
        configs = config.LoadConfigs() // 重新加载配置，包含默认实例
        newInstances := make([]*chrome.Instance, len(configs))
        for i, cfg := range configs {
            instance := chrome.NewInstance(cfg)
            newInstances[i] = instance
            if i == 0 && cfg.IsDefault { // 对默认实例的特殊日志
                log.Printf("启动检查: 默认实例 %s 状态: %v", cfg.Name, instance.IsRunning())
            } else {
                log.Printf("启动检查: 配置 %s (dir: %s) 状态: %v", cfg.Name, cfg.UserDataDir, instance.IsRunning())
            }
        }
        instances = newInstances
        if list != nil {
            list.Refresh()
        }
    }

    var list *widget.List
    list = widget.NewList(
        func() int { return len(instances) },
        func() fyne.CanvasObject { // CreateItem
            nameLabel := widget.NewLabel("配置名称")
            pathLabel := widget.NewLabel("工作目录")
            pathLabel.Wrapping = fyne.TextWrapWord
            pathLabel.TextStyle.Italic = true
            statusText := canvas.NewText("已停止", color.Gray{Y: 128})
            statusText.TextSize = 12
            actionButton := widget.NewButton("启动", nil)
            removeButton := widget.NewButton("删除", nil)

            controls := container.NewHBox(statusText, actionButton, removeButton)
            return container.NewBorder(nil, nil, nil, controls, container.NewVBox(nameLabel, pathLabel))
        },
        func(id widget.ListItemID, item fyne.CanvasObject) { // UpdateItem
            if id >= len(instances) {
                log.Printf("Error: UpdateItem called with invalid id %d, instances len %d", id, len(instances))
                return // 防止越界
            }
            instance := instances[id]
            cfg := instance.Config() // 获取配置信息

            borderLayout := item.(*fyne.Container)
            contentVBox := borderLayout.Objects[0].(*fyne.Container)
            controlsHBox := borderLayout.Objects[1].(*fyne.Container)

            nameLabel := contentVBox.Objects[0].(*widget.Label)
            pathLabel := contentVBox.Objects[1].(*widget.Label)
            statusText := controlsHBox.Objects[0].(*canvas.Text)
            actionButton := controlsHBox.Objects[1].(*widget.Button)
            removeButton := controlsHBox.Objects[2].(*widget.Button)

            nameLabel.SetText(cfg.Name)
            if cfg.IsDefault {
                pathLabel.SetText("(默认路径)")
                removeButton.Hide() // 隐藏默认实例的删除按钮
            } else {
                pathLabel.SetText(cfg.UserDataDir)
                removeButton.Show() // 显示非默认实例的删除按钮
                removeButton.OnTapped = func() {
                    dialog.ShowConfirm("确认删除", "确定要删除配置 \""+cfg.Name+"\"吗？", func(confirm bool) {
                        if confirm {
                            log.Printf("请求删除配置: %s", cfg.Name)
                            var err error
                            // configs 在 reloadInstancesAndRefreshList 中已经从磁盘加载了最新的
                            // 我们需要传递当前的 configs 列表给 RemoveConfig
                            currentConfigsForRemove := config.LoadConfigs() // 获取包含默认项的当前配置列表
                            updatedConfigs, err := config.RemoveConfig(cfg.Name, currentConfigsForRemove)
                            if err != nil {
                                log.Printf("删除配置 %s 失败: %v", cfg.Name, err)
                                dialog.ShowError(err, w)
                                return
                            }
                            // RemoveConfig 内部已经调用了 SaveConfigs
                            log.Printf("配置 %s 已删除", cfg.Name)
                            configs = updatedConfigs            // 更新内存中的 configs 列表
                            reloadInstancesAndRefreshList(list) // 重新加载并刷新UI
                        }
                    }, w)
                }
            }

            if instance.IsRunning() {
                statusText.Text = "运行中"
                statusText.Color = color.NRGBA{G: 180, A: 255}
                actionButton.SetText("停止")
                actionButton.OnTapped = func() {
                    log.Printf("请求停止实例: %s (dir: %s)", cfg.Name, cfg.UserDataDir)
                    if err := instance.Stop(); err != nil {
                        log.Printf("停止 %s 失败: %v", cfg.Name, err)
                        dialog.ShowError(err, w)
                    } else {
                        log.Printf("已发送停止命令给: %s", cfg.Name)
                    }
                    // UI 更新将依赖 IsRunning() 的状态，并在 list.RefreshItem() 时刷新
                    list.RefreshItem(id) // 立即刷新此项UI
                }
            } else {
                statusText.Text = "已停止"
                statusText.Color = color.Gray{Y: 128}
                actionButton.SetText("启动")
                actionButton.OnTapped = func() {
                    log.Printf("请求启动实例: %s (dir: %s)", cfg.Name, cfg.UserDataDir)
                    if instance.IsRunning() {
                        log.Printf("实例 %s 已经在运行中", cfg.Name)
                        dialog.ShowInformation("提示", "实例已经在运行中", w)
                        return
                    }

                    if err := instance.Start(); err != nil {
                        log.Printf("启动 %s 失败: %v", cfg.Name, err)
                        dialog.ShowError(err, w)
                        return
                    }
                    list.RefreshItem(id) // 立即刷新此项UI

                    go func(monitoredInstance *chrome.Instance, itemID widget.ListItemID) {
                        log.Printf("等待 %s (dir: %s) 进程退出...", monitoredInstance.Config().Name, monitoredInstance.Config().UserDataDir)
                        waitErr := monitoredInstance.Wait()
                        log.Printf("进程 %s (dir: %s) 已退出", monitoredInstance.Config().Name, monitoredInstance.Config().UserDataDir)
                        if waitErr != nil {
                            log.Printf("等待 %s 进程出错: %v", monitoredInstance.Config().Name, waitErr)
                        }

                        fyne.Do(func() {
                            list.RefreshItem(itemID)
                        })
                    }(instance, id)
                }
            }
            // 确保所有组件都刷新
            nameLabel.Refresh()
            pathLabel.Refresh()
            statusText.Refresh()
            actionButton.Refresh()
            removeButton.Refresh()
        },
    )

    // 初始加载
    reloadInstancesAndRefreshList(list)

    nameEntry := widget.NewEntry()
    workdirEntry := widget.NewEntry()

    selectDirButton := widget.NewButton("选择目录", func() {
        dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
            if err != nil {
                dialog.ShowError(err, w)
                return
            }
            if uri != nil {
                workdirEntry.SetText(uri.Path())
            }
        }, w)
    })

    // Combine workdirEntry and selectDirButton for the form item
    workdirInputWidget := container.NewBorder(nil, nil, nil, selectDirButton, workdirEntry)

    // Update placeholders now that there are labels
    nameEntry.SetPlaceHolder("例如：我的项目")
    workdirEntry.SetPlaceHolder("粘贴路径或点击右侧按钮选择")

    addForm := widget.NewForm(
        widget.NewFormItem("配置名称:", nameEntry),
        widget.NewFormItem("数据目录:", workdirInputWidget),
    )
    addForm.SubmitText = "新增配置"
    addForm.OnSubmit = func() {
        name := nameEntry.Text
        workdir := workdirEntry.Text

        // 使用 config.AddConfig 进行添加和校验
        // AddConfig 需要当前的配置列表（包含默认实例）
        currentConfigsForAdd := config.LoadConfigs()
        updatedConfigs, err := config.AddConfig(name, workdir, currentConfigsForAdd)
        if err != nil {
            log.Printf("新增配置失败: %v", err)
            dialog.ShowError(err, w)
            return
        }
        // AddConfig 内部已经调用了 SaveConfigs
        configs = updatedConfigs            // 更新内存中的 configs 列表
        reloadInstancesAndRefreshList(list) // 重新加载并刷新UI

        nameEntry.SetText("") // Clear fields after successful submission
        workdirEntry.SetText("")
        log.Println("新增配置成功:", name)
        dialog.ShowInformation("成功", "配置 \""+name+"\" 已添加", w)
    }

    // Create the section for adding new configurations
    addConfigSection := container.NewVBox(
        widget.NewSeparator(),
        widget.NewLabel("新增配置项："),
        addForm,
    )

    // Use a Border layout: list label at top, scrollable list in the center, add form at the bottom
    scrollableList := container.NewScroll(list)

    content := container.NewBorder(
        widget.NewLabel("Chrome 配置列表："), // Top
        addConfigSection,                  // Bottom (using the new form-based section)
        nil,                               // Left
        nil,                               // Right
        scrollableList,                    // Center object
    )

    w.SetContent(content)
    w.Resize(fyne.NewSize(700, 600)) // 稍微调大一点高度以容纳删除按钮和路径换行
    w.ShowAndRun()
}
