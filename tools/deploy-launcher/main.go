package main

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.org/x/crypto/ssh"
)

const robotStartCommand = "mkdir -p /root/config; nohup sh -c '/root/robot 2>&1 | /root/robot --bounded-log-sink /root/config/robot_stdout.log' >/dev/null 2>/root/config/robot_start_error.log &"

type DeployWindow struct {
	*walk.MainWindow
	deployBtn    *walk.PushButton
	restartBtn   *walk.PushButton
	hostEdit     *walk.LineEdit
	portEdit     *walk.LineEdit
	userEdit     *walk.LineEdit
	passEdit     *walk.LineEdit
	logEdit      *walk.TextEdit
	freshInstall bool
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
	if _, err := validSSHPort(dw.portEdit.Text()); err != nil {
		return err
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
	dw.logEdit.SetText("")
	if err := dw.validateInput(); err != nil {
		dw.appendLog(fmt.Sprintf("部署失败: %v", err))
		dw.appendLog("end")
		return
	}

	dw.deployBtn.SetEnabled(false)
	dw.deployBtn.SetText("部署中...")

	go func() {
		var operationErr error
		defer func() {
			if r := recover(); r != nil {
				operationErr = fmt.Errorf("部署异常: %v", r)
			}
			if operationErr != nil {
				dw.appendLog(fmt.Sprintf("部署失败: %v", operationErr))
			} else {
				dw.appendLog("部署成功: robot 已运行")
			}
			dw.appendLog("end")
			dw.safeSync(func() {
				if dw.deployBtn == nil {
					return
				}
				dw.deployBtn.SetEnabled(true)
				dw.deployBtn.SetText("部署 robot")
			})
		}()

		operationErr = dw.doDeploy()
	}()
}

func (dw *DeployWindow) restart() {
	dw.logEdit.SetText("")
	if err := dw.validateInput(); err != nil {
		dw.appendLog(fmt.Sprintf("重启失败: %v", err))
		dw.appendLog("end")
		return
	}

	dw.restartBtn.SetEnabled(false)
	dw.restartBtn.SetText("重启中...")

	go func() {
		var operationErr error
		defer func() {
			if r := recover(); r != nil {
				operationErr = fmt.Errorf("重启异常: %v", r)
			}
			if operationErr != nil {
				dw.appendLog(fmt.Sprintf("重启失败: %v", operationErr))
			} else {
				dw.appendLog("重启成功: robot 已运行")
			}
			dw.appendLog("end")
			dw.safeSync(func() {
				if dw.restartBtn == nil {
					return
				}
				dw.restartBtn.SetEnabled(true)
				dw.restartBtn.SetText("重启 robot")
			})
		}()

		operationErr = dw.doRestart()
	}()
}

func (dw *DeployWindow) doRestart() error {
	endpoint, err := sshAddress(dw.hostEdit.Text(), dw.portEdit.Text())
	if err != nil {
		return err
	}
	client, err := sshConnectWithRetry(dw.hostEdit.Text(), dw.portEdit.Text(), dw.userEdit.Text(), dw.passEdit.Text(), 3)
	if err != nil {
		return fmt.Errorf("SSH 连接 %s 失败(已重试): %v", endpoint, err)
	}
	defer client.Close()
	dw.appendLog(fmt.Sprintf("SSH %s 连接成功", endpoint))

	dw.killRemoteRobot(client)

	if err := runCmdBg(client, robotStartCommand); err != nil {
		return fmt.Errorf("启动 robot 失败: %v", err)
	}

	time.Sleep(2 * time.Second)

	robotPid, err := verifyRemoteRobot(client, dw.appendLog)
	if err != nil {
		dw.appendRemoteRobotDiagnostics(client)
		return err
	}
	dw.appendLog(fmt.Sprintf("robot 已启动 (pid: %s)", robotPid))
	return nil
}

func (dw *DeployWindow) killRemoteRobot(client *ssh.Client) {
	runCmd(client, "pkill -TERM -f '^/root/robot$' 2>/dev/null; pkill -TERM -f '^/root/robot --web-admin' 2>/dev/null; pkill -TERM -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null; true")
	time.Sleep(2 * time.Second)

	check, _ := runCmdOutput(client, "pgrep -f '^/root/robot$|^/root/robot --web-admin|^/root/robot --bounded-log-sink .*/robot_stdout.log' || true")
	check = strings.TrimSpace(check)
	if check != "" {
		dw.appendLog(fmt.Sprintf("仍有 robot 残留 PID: %s，逐个强杀 ...", check))
		runCmd(client, fmt.Sprintf("kill -9 %s 2>/dev/null; true", strings.ReplaceAll(check, "\n", " ")))
		time.Sleep(2 * time.Second)
		check2, _ := runCmdOutput(client, "pgrep -f '^/root/robot$|^/root/robot --web-admin|^/root/robot --bounded-log-sink .*/robot_stdout.log' || true")
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

	endpoint, err := sshAddress(dw.hostEdit.Text(), dw.portEdit.Text())
	if err != nil {
		return err
	}
	client, err := sshConnectWithRetry(dw.hostEdit.Text(), dw.portEdit.Text(), dw.userEdit.Text(), dw.passEdit.Text(), 3)
	if err != nil {
		return fmt.Errorf("SSH 连接 %s 失败(已重试): %v", endpoint, err)
	}
	defer client.Close()
	dw.appendLog(fmt.Sprintf("SSH %s 连接成功", endpoint))

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

	if dw.freshInstall {
		configBackup := fmt.Sprintf("/root/config.bak.%s", time.Now().Format("20060102_150405.000"))
		dw.appendLog(fmt.Sprintf("全新部署: 备份旧 config 到 %s", configBackup))
		if err := runCmd(client, freshInstallConfigCommand(configBackup)); err != nil {
			return fmt.Errorf("重建 /root/config 失败: %v", err)
		}
		dw.appendLog("全新部署: /root/config 已清空")
	} else {
		dw.appendLog("兼容部署: 保留 /root/config")
	}

	if err := runCmdBg(client, robotStartCommand); err != nil {
		return fmt.Errorf("启动 robot 失败: %v", err)
	}

	time.Sleep(2 * time.Second)

	robotPid, err := verifyRemoteRobot(client, dw.appendLog)
	if err != nil {
		dw.appendRemoteRobotDiagnostics(client)
		return err
	}
	dw.appendLog(fmt.Sprintf("新 robot 已启动 (pid: %s)", robotPid))

	return nil
}

func freshInstallConfigCommand(backupPath string) string {
	return fmt.Sprintf("current_config=0; backup_keep=3; if [ -e /root/config ] || [ -L /root/config ]; then current_config=1; backup_keep=2; if [ -e %[1]s ] || [ -L %[1]s ]; then echo 'config backup already exists: %[1]s' >&2; exit 1; fi; fi; backup_count=0; for old_backup in $(ls -1dt -- /root/config.bak.* 2>/dev/null); do backup_count=$((backup_count + 1)); if [ \"$backup_count\" -gt \"$backup_keep\" ]; then case \"$old_backup\" in /root/config.bak.*) rm -rf -- \"$old_backup\" || exit $? ;; *) echo \"unsafe config backup path: $old_backup\" >&2; exit 1 ;; esac; fi; done; if [ \"$current_config\" -eq 1 ]; then mv -- /root/config %[1]s || exit $?; fi; mkdir -m 755 /root/config", backupPath)
}

func verifyRemoteRobot(client *ssh.Client, report func(string)) (string, error) {
	expectedPorts, err := readRemoteRobotListenPorts(client)
	if err != nil {
		return "", fmt.Errorf("robot 启动校验失败: %w", err)
	}
	if report != nil {
		report(fmt.Sprintf("开始启动校验: RobotAPI=%d Web=%d，最长等待180秒", expectedPorts.robotAPI, expectedPorts.web))
	}

	lastReason := ""
	lastReportedReason := ""
	missingMainChecks := 0
	for attempt := 0; attempt < 180; attempt++ {
		robotPID, _ := runCmdOutput(client, "pgrep -f '^/root/robot$' | head -1 || true")
		robotPID = strings.TrimSpace(robotPID)
		if robotPID == "" {
			lastReason = "主进程未运行"
			missingMainChecks++
		} else {
			missingMainChecks = 0
			sinkPID, _ := runCmdOutput(client, "pgrep -f '^/root/robot --bounded-log-sink /root/config/robot_stdout.log( |$)' | head -1 || true")
			if strings.TrimSpace(sinkPID) == "" {
				lastReason = "stdout 日志进程未运行"
			} else {
				ports, _ := runCmdOutput(client, "ss -ltn 2>/dev/null | awk 'NR>1 {print $4}' || true")
				if listenerPortsReady(ports, expectedPorts) {
					if report != nil {
						report(fmt.Sprintf("启动校验通过: pid=%s，端口 %d/%d 已监听", robotPID, expectedPorts.robotAPI, expectedPorts.web))
					}
					return robotPID, nil
				}
				lastReason = fmt.Sprintf("端口 %d/%d 未就绪", expectedPorts.robotAPI, expectedPorts.web)
			}
		}
		if report != nil && (lastReason != lastReportedReason || (attempt+1)%5 == 0) {
			report(fmt.Sprintf("等待 robot 启动: %s，已等待%d秒", lastReason, attempt+1))
			lastReportedReason = lastReason
		}
		if missingMainChecks >= 5 {
			return "", fmt.Errorf("robot 启动校验失败: 主进程连续5秒未运行")
		}
		time.Sleep(time.Second)
	}
	return "", fmt.Errorf("robot 启动校验失败: %s，请检查 /root/config/robot_start_error.log", lastReason)
}

func (dw *DeployWindow) appendRemoteRobotDiagnostics(client *ssh.Client) {
	dw.appendLog("--- robot 启动失败诊断 ---")
	dw.appendRemoteDiagnostic(client, "进程", "pgrep -af '^/root/robot$|^/root/robot --web-admin|^/root/robot --bounded-log-sink' || true")
	dw.appendRemoteDiagnostic(client, "监听端口", "ss -ltnp 2>/dev/null | grep -E ':(8111|8112)\\b' || true")
	dw.appendRemoteDiagnostic(client, "robot_start_error.log", "tail -n 40 /root/config/robot_start_error.log 2>/dev/null || true")
	dw.appendRemoteDiagnostic(client, "robot_stdout.log", "tail -n 60 /root/config/robot_stdout.log 2>/dev/null || true")
	dw.appendRemoteDiagnostic(client, "log_robot", "tail -n 60 /root/config/log_robot 2>/dev/null || true")
	dw.appendLog("--- 诊断结束 ---")
}

func (dw *DeployWindow) appendRemoteDiagnostic(client *ssh.Client, label, command string) {
	out, err := runCmdOutput(client, command)
	if err != nil {
		dw.appendLog(fmt.Sprintf("[%s] 读取失败: %v", label, err))
		return
	}
	dw.appendLog("[" + label + "]")
	out = strings.TrimSpace(out)
	if out == "" {
		dw.appendLog("(无输出)")
		return
	}
	for _, line := range strings.Split(out, "\n") {
		dw.appendLog(strings.TrimRight(line, "\r"))
	}
}

func validSSHPort(raw string) (string, error) {
	port := strings.TrimSpace(raw)
	if port == "" {
		return "", fmt.Errorf("SSH 端口不能为空")
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return "", fmt.Errorf("SSH 端口必须是 1-65535 之间的数字")
	}
	return strconv.Itoa(value), nil
}

func sshAddress(host, port string) (string, error) {
	port, err := validSSHPort(port)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(strings.TrimSpace(host), port), nil
}

func sshConnectWithRetry(host, port, user, pass string, retries int) (*ssh.Client, error) {
	var lastErr error
	for i := 0; i < retries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i*2) * time.Second)
		}
		client, err := sshConnect(host, port, user, pass)
		if err == nil {
			return client, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func sshConnect(host, port, user, pass string) (*ssh.Client, error) {
	address, err := sshAddress(host, port)
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	return ssh.Dial("tcp", address, config)
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
	launcherCfg := defaultLauncherConfig()
	if exe, err := os.Executable(); err != nil {
		fmt.Fprintf(os.Stderr, "无法获取启动器配置路径，使用默认配置: %v\n", err)
	} else {
		configPath := filepath.Join(filepath.Dir(exe), launcherConfigName)
		loaded, loadErr := loadLauncherConfig(configPath)
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "读取启动器配置失败，使用默认配置: %v\n", loadErr)
		} else {
			launcherCfg = loaded
		}
	}
	dw.freshInstall = launcherCfg.FreshInstall

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
					LineEdit{AssignTo: &dw.hostEdit, Text: launcherCfg.Host},
					Label{Text: "端口:"},
					LineEdit{AssignTo: &dw.portEdit, Text: launcherCfg.Port},
					Label{Text: "用户:"},
					LineEdit{AssignTo: &dw.userEdit, Text: launcherCfg.User},
					Label{Text: "密码:"},
					LineEdit{AssignTo: &dw.passEdit, Text: launcherCfg.Password},
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
