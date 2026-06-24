package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.org/x/crypto/ssh"
)

type DeployWindow struct {
	*walk.MainWindow
	deployBtn  *walk.PushButton
	runBtn     *walk.PushButton
	hostEdit   *walk.LineEdit
	userEdit   *walk.LineEdit
	passEdit   *walk.LineEdit
	logEdit    *walk.TextEdit
	geoipCount int
	geoipMu    sync.Mutex
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
			if dw.deployBtn == nil {
				return
			}
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
		dw.appendLog(fmt.Sprintf("备份旧 robot 无文件可备份（首次部署）"))
	} else {
		dw.appendLog(fmt.Sprintf("备份旧 robot → %s", backupName))
	}

	runCmd(client, "pkill -f '/root/robot' 2>/dev/null || true")
	time.Sleep(1 * time.Second)

	stillRunning, _ := runCmdOutput(client, "pgrep -cf '/root/robot' || true")
	stillRunning = strings.TrimSpace(stillRunning)
	count, _ := strconv.Atoi(stillRunning)
	if count > 0 {
		return fmt.Errorf("旧 robot 进程未能停止 (剩余 %d 个进程)", count)
	}
	dw.appendLog("旧 robot 已停止")

	if err := runCmd(client, "mv /root/robot.new /root/robot"); err != nil {
		return fmt.Errorf("替换 robot 失败: %v", err)
	}
	dw.appendLog("替换 robot 完成")

	if err := runCmdBg(client, "nohup /root/robot >/root/robot_stdout.log 2>&1 &"); err != nil {
		return fmt.Errorf("启动 robot 失败: %v", err)
	}

	time.Sleep(2 * time.Second)

	robotPid, _ := runCmdOutput(client, "pgrep -f '/root/robot' | head -1 || true")
	robotPid = strings.TrimSpace(robotPid)
	if robotPid == "" {
		return fmt.Errorf("新 robot 进程未运行，请检查 /root/robot_stdout.log")
	}
	dw.appendLog(fmt.Sprintf("新 robot 已启动 (pid: %s)", robotPid))

	return nil
}

func (dw *DeployWindow) runGame() {
	if err := dw.validateInput(); err != nil {
		walk.MsgBox(dw.MainWindow, "输入错误", err.Error(), walk.MsgBoxIconError)
		return
	}

	dw.runBtn.SetEnabled(false)
	dw.runBtn.SetText("启动中...")
	dw.logEdit.SetText("")
	dw.geoipCount = 0

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
			dw.runBtn.SetText("启动游戏")
		})

		client, err := sshConnectWithRetry(dw.hostEdit.Text(), dw.userEdit.Text(), dw.passEdit.Text(), 2)
		if err != nil {
			dw.appendLog(fmt.Sprintf("SSH 连接失败(已重试): %v", err))
			return
		}
		defer client.Close()
		dw.appendLog("SSH 连接成功，正在执行 /root/run ...")

		session, err := client.NewSession()
		if err != nil {
			dw.appendLog(fmt.Sprintf("创建 SSH session 失败: %v", err))
			return
		}
		defer session.Close()

		modes := ssh.TerminalModes{ssh.ECHO: 0, ssh.OPOST: 0}
		if err := session.RequestPty("xterm", 120, 40, modes); err != nil {
			dw.appendLog(fmt.Sprintf("请求 PTY 失败: %v", err))
			return
		}

		stdout, err := session.StdoutPipe()
		if err != nil {
			dw.appendLog(fmt.Sprintf("获取 stdout 失败: %v", err))
			return
		}
		stderr, err := session.StderrPipe()
		if err != nil {
			dw.appendLog(fmt.Sprintf("获取 stderr 失败: %v", err))
			return
		}

		if err := session.Start("/root/run"); err != nil {
			dw.appendLog(fmt.Sprintf("执行 /root/run 失败: %v", err))
			return
		}

		output := io.MultiReader(stdout, stderr)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 64*1024), 512*1024)

		lineCh := make(chan string, 100)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for scanner.Scan() {
				lineCh <- scanner.Text()
			}
		}()

		timeout := time.After(5 * time.Minute)

		for {
			select {
			case line, ok := <-lineCh:
				if !ok {
					dw.appendLog("--- /root/run 已结束 ---")
					return
				}
				dw.appendLog(line)

				if strings.Contains(line, "Geoip Allow Country Code") {
					dw.geoipMu.Lock()
					dw.geoipCount++
					count := dw.geoipCount
					dw.geoipMu.Unlock()

					if count >= 5 {
						dw.safeSync(func() {
							walk.MsgBox(dw.MainWindow, "启动成功",
								"游戏服务已完全启动！（检测到 5 次 Geoip Allow Country Code）",
								walk.MsgBoxIconInformation)
						})
						dw.appendLog("游戏服务启动完成，日志监听继续...")
					}
				}

			case <-timeout:
				dw.appendLog("--- 日志监听超时（5分钟），已自动停止 ---")
				session.Close()
				return

			case <-done:
				dw.appendLog("--- /root/run 已结束 ---")
				return
			}
		}
	}()
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
						AssignTo: &dw.runBtn,
						Text:     "启动游戏",
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
