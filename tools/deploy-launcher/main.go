package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.org/x/crypto/ssh"
)

const robotStartCommand = "nohup sh -c '/root/robot 2>&1 | /root/robot --bounded-log-sink /root/robot_stdout.log' >/dev/null 2>/root/robot_start_error.log &"

type DeployWindow struct {
	*walk.MainWindow
	deployBtn  *walk.PushButton
	restartBtn *walk.PushButton
	runBtn     *walk.PushButton
	hostEdit   *walk.LineEdit
	userEdit   *walk.LineEdit
	passEdit   *walk.LineEdit
	logEdit    *walk.TextEdit
}

func (dw *DeployWindow) safeSync(fn func()) {
	if dw == nil || dw.MainWindow == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "safeSync panic: %v\n", r)
		}
	}()
	dw.Synchronize(fn)
}

func (dw *DeployWindow) appendLog(line string) {
	dw.safeSync(func() {
		if dw.logEdit == nil {
			return
		}
		now := time.Now().Format("15:04:05")
		dw.logEdit.AppendText(fmt.Sprintf("[%s] %s\r\n", now, line))
	})
}

func (dw *DeployWindow) validateInput() error {
	if strings.TrimSpace(dw.hostEdit.Text()) == "" {
		return fmt.Errorf("主机地址不能为空")
	}
	if strings.TrimSpace(dw.userEdit.Text()) == "" {
		return fmt.Errorf("用户名不能为空")
	}
	if strings.TrimSpace(dw.passEdit.Text()) == "" {
		return fmt.Errorf("密码不能为空")
	}
	return nil
}

func (dw *DeployWindow) deploy() {
	if err := dw.validateInput(); err != nil {
		walk.MsgBox(dw.MainWindow, "输入错误", err.Error(), walk.MsgBoxIconError)
		return
	}

	dw.deployBtn.SetEnabled(false)
	dw.deployBtn.SetText("部署中...")
	dw.logEdit.SetText("")

	go func() {
		defer func() {
			if r := recover(); r != nil {
				dw.safeSync(func() {
					walk.MsgBox(dw.MainWindow, "异常", fmt.Sprintf("部署异常: %v", r), walk.MsgBoxIconError)
				})
			}
		}()

		err := dw.doDeploy()
		dw.deployBtn.Synchronize(func() {
			dw.deployBtn.SetEnabled(true)
			dw.deployBtn.SetText("部署 robot")
			if err != nil {
				walk.MsgBox(dw.MainWindow, "部署失败", err.Error(), walk.MsgBoxIconError)
			} else {
				walk.MsgBox(dw.MainWindow, "部署成功", "部署 robot 完成，新进程已运行", walk.MsgBoxIconInformation)
			}
		})
	}()
}

func (dw *DeployWindow) restart() {
	if err := dw.validateInput(); err != nil {
		walk.MsgBox(dw.MainWindow, "输入错误", err.Error(), walk.MsgBoxIconError)
		return
	}

	dw.restartBtn.SetEnabled(false)
	dw.restartBtn.SetText("重启中...")
	dw.logEdit.SetText("")

	go func() {
		defer func() {
			if r := recover(); r != nil {
				dw.safeSync(func() {
					walk.MsgBox(dw.MainWindow, "异常", fmt.Sprintf("重启异常: %v", r), walk.MsgBoxIconError)
				})
			}
		}()

		err := dw.doRestart()
		dw.restartBtn.Synchronize(func() {
			dw.restartBtn.SetEnabled(true)
			dw.restartBtn.SetText("重启 robot")
			if err != nil {
				walk.MsgBox(dw.MainWindow, "重启失败", err.Error(), walk.MsgBoxIconError)
			} else {
				walk.MsgBox(dw.MainWindow, "重启成功", "robot 已重启", walk.MsgBoxIconInformation)
			}
		})
	}()
}

func (dw *DeployWindow) doRestart() error {
	client, err := sshConnectWithRetry(dw.hostEdit.Text(), dw.userEdit.Text(), dw.passEdit.Text(), 3)
	if err != nil {
		return fmt.Errorf("SSH 连接 %s 失败(已重试): %v", dw.hostEdit.Text(), err)
	}
	defer client.Close()
	dw.appendLog(fmt.Sprintf("SSH %s 连接成功", dw.hostEdit.Text()))

	dw.killRemoteRobot(client)

	if err := runCmdBg(client, robotStartCommand); err != nil {
		return fmt.Errorf("启动 robot 失败: %v", err)
	}

	time.Sleep(2 * time.Second)

	robotPid, err := verifyRemoteRobot(client)
	if err != nil {
		return err
	}
	dw.appendLog(fmt.Sprintf("robot 已启动 (pid: %s)", robotPid))
	return nil
}

func (dw *DeployWindow) killRemoteRobot(client *ssh.Client) {
	runCmd(client, "pkill -TERM -f '^/root/robot$' 2>/dev/null; pkill -TERM -f '^/root/robot --web-admin' 2>/dev/null; pkill -TERM -f '^/root/robot --bounded-log-sink /root/robot_stdout.log' 2>/dev/null; true")
	time.Sleep(2 * time.Second)

	check, _ := runCmdOutput(client, "pgrep -f '^/root/robot$|^/root/robot --web-admin|^/root/robot --bounded-log-sink /root/robot_stdout.log' || true")
	check = strings.TrimSpace(check)
	if check != "" {
		dw.appendLog(fmt.Sprintf("仍有 robot 残留 PID: %s，逐个强杀 ...", check))
		runCmd(client, fmt.Sprintf("kill -9 %s 2>/dev/null; true", strings.ReplaceAll(check, "\n", " ")))
		time.Sleep(2 * time.Second)
		check2, _ := runCmdOutput(client, "pgrep -f '^/root/robot$|^/root/robot --web-admin|^/root/robot --bounded-log-sink /root/robot_stdout.log' || true")
		check2 = strings.TrimSpace(check2)
		if check2 != "" {
			dw.appendLog(fmt.Sprintf("警告: robot 残留 PID %s，继续", check2))
			return
		}
	}
	dw.appendLog("旧 robot 已停止")
}

func (dw *DeployWindow) doDeploy() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法获取程序路径: %v", err)
	}
	robotPath := filepath.Join(filepath.Dir(exe), "robot")

	localInfo, err := os.Stat(robotPath)
	if err != nil {
		return fmt.Errorf("找不到 robot 程序 (同目录下): %v", err)
	}
	localSize := localInfo.Size()
	dw.appendLog(fmt.Sprintf("本地 robot: %s (%d bytes)", robotPath, localSize))

	client, err := sshConnectWithRetry(dw.hostEdit.Text(), dw.userEdit.Text(), dw.passEdit.Text(), 3)
	if err != nil {
		return fmt.Errorf("SSH 连接 %s 失败(已重试): %v", dw.hostEdit.Text(), err)
	}
	defer client.Close()
	dw.appendLog(fmt.Sprintf("SSH %s 连接成功", dw.hostEdit.Text()))

	if err := uploadFile(client, robotPath, "/root/robot.new"); err != nil {
		return fmt.Errorf("上传 robot 失败: %v", err)
	}
	dw.appendLog("上传 /root/robot.new 完成")

	remoteSize, err := runCmdOutput(client, fmt.Sprintf("stat -c %%s /root/robot.new"))
	if err != nil {
		return fmt.Errorf("无法验证远程文件: %v", err)
	}
	remoteSize = strings.TrimSpace(remoteSize)
	rs, parseErr := strconv.ParseInt(remoteSize, 10, 64)
	if parseErr != nil || rs != localSize {
		return fmt.Errorf("文件大小校验失败: 本地 %d, 远程 %q", localSize, remoteSize)
	}
	dw.appendLog(fmt.Sprintf("文件大小校验通过 (%d bytes)", rs))

	if err := runCmd(client, "chmod +x /root/robot.new"); err != nil {
		return fmt.Errorf("设置权限失败: %v", err)
	}
	dw.appendLog("chmod +x 完成")

	backupName := fmt.Sprintf("/root/robot.bak.%s", time.Now().Format("20060102_150405"))
	if err := runCmd(client, fmt.Sprintf("cp /root/robot %s 2>/dev/null", backupName)); err != nil {
		dw.appendLog("备份旧 robot 无文件可备份（首次部署）")
	} else {
		dw.appendLog(fmt.Sprintf("备份旧 robot → %s", backupName))
		runCmd(client, "ls -1t /root/robot.bak.* 2>/dev/null | tail -n +4 | xargs -r rm -f; true")
	}

	dw.killRemoteRobot(client)

	if err := runCmd(client, "mv /root/robot.new /root/robot"); err != nil {
		return fmt.Errorf("替换 robot 失败: %v", err)
	}
	dw.appendLog("替换 robot 完成")

	if err := runCmdBg(client, robotStartCommand); err != nil {
		return fmt.Errorf("启动 robot 失败: %v", err)
	}

	time.Sleep(2 * time.Second)

	robotPid, err := verifyRemoteRobot(client)
	if err != nil {
		return err
	}
	dw.appendLog(fmt.Sprintf("新 robot 已启动 (pid: %s)", robotPid))

	return nil
}

func verifyRemoteRobot(client *ssh.Client) (string, error) {
	lastReason := ""
	for attempt := 0; attempt < 180; attempt++ {
		robotPID, _ := runCmdOutput(client, "pgrep -f '^/root/robot$' | head -1 || true")
		robotPID = strings.TrimSpace(robotPID)
		if robotPID == "" {
			lastReason = "主进程未运行"
		} else {
			sinkPID, _ := runCmdOutput(client, "pgrep -f '^/root/robot --bounded-log-sink /root/robot_stdout.log( |$)' | head -1 || true")
			if strings.TrimSpace(sinkPID) == "" {
				lastReason = "stdout 日志进程未运行"
			} else {
				ports, _ := runCmdOutput(client, "ss -ltnH 2>/dev/null | awk '{print $4}' || true")
				if strings.Contains(ports, ":8111") && strings.Contains(ports, ":8112") {
					return robotPID, nil
				}
				lastReason = "端口 8111/8112 未就绪"
			}
		}
		time.Sleep(time.Second)
	}
	return "", fmt.Errorf("robot 启动校验失败: %s，请检查 /root/robot_start_error.log", lastReason)
}

func (dw *DeployWindow) runGame() {
	if err := dw.validateInput(); err != nil {
		walk.MsgBox(dw.MainWindow, "输入错误", err.Error(), walk.MsgBoxIconError)
		return
	}

	dw.runBtn.SetEnabled(false)
	dw.runBtn.SetText("启动中...")
	dw.logEdit.SetText("")

	go func() {
		defer func() {
			if r := recover(); r != nil {
				dw.safeSync(func() {
					walk.MsgBox(dw.MainWindow, "异常", fmt.Sprintf("启动异常: %v", r), walk.MsgBoxIconError)
				})
			}
		}()

		defer dw.runBtn.Synchronize(func() {
			if dw.runBtn == nil {
				return
			}
			dw.runBtn.SetEnabled(true)
			dw.runBtn.SetText("启动 /root/run")
		})

		client, err := sshConnectWithRetry(dw.hostEdit.Text(), dw.userEdit.Text(), dw.passEdit.Text(), 2)
		if err != nil {
			dw.appendLog(fmt.Sprintf("SSH 连接失败(已重试): %v", err))
			return
		}
		defer client.Close()
		dw.appendLog("SSH 连接成功，正在启动游戏服务 ...")

		if err := runCmdBg(client, "nohup /root/run >/dev/null 2>&1 &"); err != nil {
			dw.appendLog(fmt.Sprintf("执行 /root/run 失败: %v", err))
			return
		}
		dw.appendLog("/root/run 已提交，开始监控 ...")

		processFound := false
		stableCount := 0
		lastProcCount := 0
		stableAt := 0
		seenPorts := make(map[string]bool)
		portStable := 0
		lastPortCount := -1
		success := false

		for i := 1; i <= 60; i++ {
			time.Sleep(2 * time.Second)

			out, _ := runCmdOutput(client, "pgrep -cf 'df_game_r' || true")
			procCount, _ := strconv.Atoi(strings.TrimSpace(out))

			if procCount >= 1 {
				if procCount == lastProcCount {
					stableCount++
				} else {
					stableCount = 1
					lastProcCount = procCount
				}
				if !processFound && stableCount >= 3 && procCount >= 1 {
					processFound = true
					stableAt = i
					dw.appendLog(fmt.Sprintf("[%ds] df_game_r 进程已稳定 (%d 个)，等待端口就绪 ...", i*2, procCount))
				}
			} else {
				stableCount = 0
				lastProcCount = 0
			}

			// 进程稳定后等 3 个周期再开始检测端口
			shouldCheckPorts := processFound && i > stableAt+3

			if shouldCheckPorts {
				allPorts := detectOpenGamePorts(client)
				for _, p := range allPorts {
					if !seenPorts[p] {
						seenPorts[p] = true
						dw.appendLog(fmt.Sprintf("[%ds] 游戏端口 %s 已监听", i*2, p))
					}
				}
				if len(allPorts) > 0 {
					if len(allPorts) == lastPortCount {
						portStable++
					} else {
						portStable = 1
						lastPortCount = len(allPorts)
					}
					if portStable >= 3 {
						success = true
						break
					}
				} else {
					portStable = 0
					lastPortCount = -1
				}
			}

			if i%5 == 0 {
				pc := "OK"
				if !processFound {
					pc = "等待"
				}
				dw.appendLog(fmt.Sprintf("[%ds] 进程:%s 端口:%d 个", i*2, pc, len(seenPorts)))
			}
		}

		if success {
			portList := make([]string, 0, len(seenPorts))
			for p := range seenPorts {
				portList = append(portList, p)
			}
			dw.Synchronize(func() {
				walk.MsgBox(dw.MainWindow, "启动成功",
					fmt.Sprintf("游戏服务已完全启动！（df_game_r 运行中，端口 %s 已监听）", strings.Join(portList, ", ")),
					walk.MsgBoxIconInformation)
			})
		} else {
			if !processFound {
				dw.appendLog("--- 启动监控超时: df_game_r 未启动 ---")
			} else {
				dw.appendLog("--- 启动监控超时: df_game_r 已运行但无端口监听 ---")
			}
		}
	}()
}

func detectOpenGamePorts(client *ssh.Client) []string {
	cmd := "ss -tlnp 2>/dev/null | grep df_game_r | awk '{for(i=4;i<=NF;i++) if($i~/:([0-9]+)$/){split($i,a,\":\"); print a[length(a)]}}' | sort -nu"
	out, err := runCmdOutput(client, cmd)
	if err != nil || strings.TrimSpace(out) == "" {
		cmd2 := "netstat -tlnp 2>/dev/null | grep df_game_r | awk '{for(i=4;i<=NF;i++) if($i~/:([0-9]+)$/){split($i,a,\":\"); print a[length(a)]}}' | sort -nu"
		out, _ = runCmdOutput(client, cmd2)
	}
	var ports []string
	for _, p := range strings.Split(strings.TrimSpace(out), "\n") {
		p = strings.TrimSpace(p)
		if p != "" {
			ports = append(ports, p)
		}
	}
	return ports
}

func sshConnectWithRetry(host, user, pass string, retries int) (*ssh.Client, error) {
	var lastErr error
	for i := 0; i < retries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i*2) * time.Second)
		}
		client, err := sshConnect(host, user, pass)
		if err == nil {
			return client, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func sshConnect(host, user, pass string) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	return ssh.Dial("tcp", fmt.Sprintf("%s:22", host), config)
}

func runCmdOutput(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	out, err := session.Output(cmd)
	return string(out), err
}

func runCmd(client *ssh.Client, cmd string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run(cmd)
}

func runCmdBg(client *ssh.Client, cmd string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	if err := session.Start(cmd); err != nil {
		session.Close()
		return err
	}
	go func() {
		session.Wait()
		session.Close()
	}()
	return nil
}

func uploadFile(client *ssh.Client, local, remote string) error {
	data, err := os.ReadFile(local)
	if err != nil {
		return err
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	session.Stdin = strings.NewReader(encoded)
	return session.Run(fmt.Sprintf("base64 -d > '%s'", remote))
}

func main() {
	var dw DeployWindow

	if _, err := (MainWindow{
		AssignTo: &dw.MainWindow,
		Title:    "DNF Robot 部署启动器",
		MinSize:  Size{480, 420},
		Size:     Size{480, 520},
		Layout:   VBox{},
		Children: []Widget{
			GroupBox{
				Title:  "SSH 连接",
				Layout: Grid{Columns: 2},
				Children: []Widget{
					Label{Text: "主机:"},
					LineEdit{AssignTo: &dw.hostEdit, Text: "192.168.200.131"},
					Label{Text: "用户:"},
					LineEdit{AssignTo: &dw.userEdit, Text: "root"},
					Label{Text: "密码:"},
					LineEdit{AssignTo: &dw.passEdit, Text: "123456"},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					PushButton{
						AssignTo: &dw.deployBtn,
						Text:     "部署 robot",
						OnClicked: func() {
							dw.deploy()
						},
					},
					PushButton{
						AssignTo: &dw.restartBtn,
						Text:     "重启 robot",
						OnClicked: func() {
							dw.restart()
						},
					},
					PushButton{
						AssignTo: &dw.runBtn,
						Text:     "启动 /root/run",
						OnClicked: func() {
							dw.runGame()
						},
					},
				},
			},
			TextEdit{
				AssignTo: &dw.logEdit,
				ReadOnly: true,
				VScroll:  true,
				Font:     Font{Family: "Consolas", PointSize: 10},
			},
		},
	}).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}
}
