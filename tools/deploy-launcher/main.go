package main

import (
	"fmt"
	"io"
	"net"
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

const (
	remoteRobotUploadPath = "/root/robot.new"
	robotUploadCommand    = "cat > " + remoteRobotUploadPath
	robotStartCommand     = "mkdir -p /root/config/logs; nohup sh -c '/root/robot 2>&1 | /root/robot --bounded-log-sink /root/config/logs/stdout.log' >/dev/null 2>/root/config/logs/start_error.log &"
	remoteCommandTimeout  = 30 * time.Second
	remoteUploadTimeout   = 10 * time.Minute
)

type DeployWindow struct {
	*walk.MainWindow
	deployBtn  *walk.PushButton
	restartBtn *walk.PushButton
	hostEdit   *walk.LineEdit
	portEdit   *walk.LineEdit
	userEdit   *walk.LineEdit
	passEdit   *walk.LineEdit
	logEdit    *walk.TextEdit

	operationMu      sync.Mutex
	operationRunning bool
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

func (dw *DeployWindow) tryBeginOperation() bool {
	dw.operationMu.Lock()
	defer dw.operationMu.Unlock()
	if dw.operationRunning {
		return false
	}
	dw.operationRunning = true
	return true
}

func (dw *DeployWindow) releaseOperation() {
	dw.operationMu.Lock()
	dw.operationRunning = false
	dw.operationMu.Unlock()
}

func (dw *DeployWindow) finishOperation() {
	dw.releaseOperation()
	dw.safeSync(func() {
		dw.setOperationButtons("", false)
	})
}

func (dw *DeployWindow) setOperationButtons(operation string, running bool) {
	if dw.deployBtn != nil {
		dw.deployBtn.SetEnabled(!running)
		dw.deployBtn.SetText("部署 robot")
		if running && operation == "部署" {
			dw.deployBtn.SetText("部署中...")
		}
	}
	if dw.restartBtn != nil {
		dw.restartBtn.SetEnabled(!running)
		dw.restartBtn.SetText("重启 robot")
		if running && operation == "重启" {
			dw.restartBtn.SetText("重启中...")
		}
	}
}

func (dw *DeployWindow) startOperation(operation, success string, run func() error) {
	if !dw.tryBeginOperation() {
		dw.appendLog("已有部署或重启操作正在执行")
		return
	}
	if dw.logEdit != nil {
		dw.logEdit.SetText("")
	}
	if err := dw.validateInput(); err != nil {
		dw.releaseOperation()
		dw.appendLog(fmt.Sprintf("%s失败: %v", operation, err))
		dw.appendLog("end")
		return
	}
	dw.setOperationButtons(operation, true)

	go func() {
		var operationErr error
		defer func() {
			if r := recover(); r != nil {
				operationErr = fmt.Errorf("%s异常: %v", operation, r)
			}
			if operationErr != nil {
				dw.appendLog(fmt.Sprintf("%s失败: %v", operation, operationErr))
			} else {
				dw.appendLog(success)
			}
			dw.appendLog("end")
			dw.finishOperation()
		}()
		operationErr = run()
	}()
}

func (dw *DeployWindow) deploy() {
	dw.startOperation("部署", "部署成功: robot 已运行", dw.doDeploy)
}

func (dw *DeployWindow) restart() {
	dw.startOperation("重启", "重启成功: robot 已运行", dw.doRestart)
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

	if err := dw.killRemoteRobot(client); err != nil {
		return fmt.Errorf("停止旧 robot 失败: %v", err)
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
	dw.appendLog(fmt.Sprintf("robot 已启动 (pid: %s)", robotPid))
	return nil
}

type remoteRobotStopper struct {
	run    func(string) error
	output func(string) (string, error)
	wait   func()
}

func (dw *DeployWindow) killRemoteRobot(client *ssh.Client) error {
	return stopRemoteRobot(remoteRobotStopper{
		run: func(command string) error {
			return runCmd(client, command)
		},
		output: func(command string) (string, error) {
			return runCmdOutput(client, command)
		},
		wait: func() { time.Sleep(2 * time.Second) },
	}, dw.appendLog)
}

func stopRemoteRobot(remote remoteRobotStopper, report func(string)) error {
	if remote.run == nil || remote.output == nil {
		return fmt.Errorf("远程进程控制器未初始化")
	}
	if err := remote.run("pkill -TERM -f '^/root/robot$' 2>/dev/null; pkill -TERM -f '^/root/robot --web-admin' 2>/dev/null; pkill -TERM -f '^/root/robot --bounded-log-sink( |$)' 2>/dev/null; true"); err != nil {
		return fmt.Errorf("发送 SIGTERM 失败: %v", err)
	}
	if remote.wait != nil {
		remote.wait()
	}

	check, err := remote.output("pgrep -f '^/root/robot$|^/root/robot --web-admin|^/root/robot --bounded-log-sink( |$)' || true")
	if err != nil {
		return fmt.Errorf("检查 SIGTERM 结果失败: %v", err)
	}
	check = strings.TrimSpace(check)
	if check != "" {
		if report != nil {
			report(fmt.Sprintf("仍有 robot 残留 PID: %s，逐个强杀 ...", check))
		}
		if err := remote.run(fmt.Sprintf("kill -9 %s 2>/dev/null; true", strings.ReplaceAll(check, "\n", " "))); err != nil {
			return fmt.Errorf("发送 SIGKILL 失败: %v", err)
		}
		if remote.wait != nil {
			remote.wait()
		}
		check2, err := remote.output("pgrep -f '^/root/robot$|^/root/robot --web-admin|^/root/robot --bounded-log-sink( |$)' || true")
		if err != nil {
			return fmt.Errorf("检查 SIGKILL 结果失败: %v", err)
		}
		check2 = strings.TrimSpace(check2)
		if check2 != "" {
			return fmt.Errorf("SIGKILL 后仍有 robot 残留 PID: %s", strings.ReplaceAll(check2, "\n", " "))
		}
	}
	if report != nil {
		report("旧 robot 已停止")
	}
	return nil
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

	if err := uploadFile(client, robotPath); err != nil {
		return fmt.Errorf("上传 robot 失败: %v", err)
	}
	dw.appendLog("上传 " + remoteRobotUploadPath + " 完成")

	remoteSize, err := runCmdOutput(client, fmt.Sprintf("stat -c %%s %s", remoteRobotUploadPath))
	if err != nil {
		return fmt.Errorf("无法验证远程文件: %v", err)
	}
	remoteSize = strings.TrimSpace(remoteSize)
	rs, parseErr := strconv.ParseInt(remoteSize, 10, 64)
	if parseErr != nil || rs != localSize {
		return fmt.Errorf("文件大小校验失败: 本地 %d, 远程 %q", localSize, remoteSize)
	}
	dw.appendLog(fmt.Sprintf("文件大小校验通过 (%d bytes)", rs))

	if err := runCmd(client, fmt.Sprintf("chmod +x %s", remoteRobotUploadPath)); err != nil {
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

	if err := dw.killRemoteRobot(client); err != nil {
		return fmt.Errorf("停止旧 robot 失败: %v", err)
	}

	if err := runCmd(client, fmt.Sprintf("mv %s /root/robot", remoteRobotUploadPath)); err != nil {
		return fmt.Errorf("替换 robot 失败: %v", err)
	}
	dw.appendLog("替换 robot 完成")

	configBackup := fmt.Sprintf("/root/config.bak.%s", time.Now().Format("20060102_150405.000"))
	dw.appendLog(fmt.Sprintf("重建 /root/config；旧配置存在时备份到 %s", configBackup))
	if err := runCmd(client, backupAndResetConfigCommand(configBackup)); err != nil {
		return fmt.Errorf("备份并重建 /root/config 失败: %v", err)
	}
	dw.appendLog("/root/config 已重建，旧配置备份最多保留 3 份")

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

func backupAndResetConfigCommand(backupPath string) string {
	return fmt.Sprintf("backup_keep=3; if [ -e /root/config ] || [ -L /root/config ]; then if [ -e %[1]s ] || [ -L %[1]s ]; then echo 'config backup already exists: %[1]s' >&2; exit 1; fi; mv -- /root/config %[1]s || exit $?; fi; backup_count=0; for old_backup in $(ls -1d -- /root/config.bak.* 2>/dev/null | sort -r); do backup_count=$((backup_count + 1)); if [ \"$backup_count\" -gt \"$backup_keep\" ]; then case \"$old_backup\" in /root/config.bak.*) rm -rf -- \"$old_backup\" || exit $? ;; *) echo \"unsafe config backup path: $old_backup\" >&2; exit 1 ;; esac; fi; done; mkdir -m 755 /root/config", backupPath)
}

func verifyRemoteRobot(client *ssh.Client, report func(string)) (string, error) {
	return waitForRemoteRobot(robotVerificationProbes{
		readPorts: func() (robotListenPorts, error) {
			return readRemoteRobotListenPorts(client)
		},
		readMainPID: func() (string, error) {
			return runCmdOutput(client, "pgrep -f '^/root/robot$' | head -1 || true")
		},
		readSinkPID: func() (string, error) {
			return runCmdOutput(client, "pgrep -f '^/root/robot --bounded-log-sink /root/config/logs/stdout.log( |$)' | head -1 || true")
		},
		readListeners: func() (string, error) {
			return runCmdOutput(client, "ss -ltn 2>/dev/null | awk 'NR>1 {print $4}' || true")
		},
		wait: func() { time.Sleep(time.Second) },
	}, 180, report)
}

type robotVerificationProbes struct {
	readPorts     func() (robotListenPorts, error)
	readMainPID   func() (string, error)
	readSinkPID   func() (string, error)
	readListeners func() (string, error)
	wait          func()
}

func waitForRemoteRobot(probes robotVerificationProbes, attempts int, report func(string)) (string, error) {
	if attempts <= 0 {
		return "", fmt.Errorf("robot 启动校验失败: 无效等待次数 %d", attempts)
	}
	if report != nil {
		report(fmt.Sprintf("开始启动校验: 等待 %s 生成并监听配置端口，最长等待%d秒", remoteConfigPath, attempts))
	}

	lastReason := ""
	lastReportedReason := ""
	missingMainChecks := 0
	var expectedPorts robotListenPorts
	portsReady := false
	for attempt := 0; attempt < attempts; attempt++ {
		if !portsReady {
			ports, err := probes.readPorts()
			if err != nil {
				lastReason = "配置尚未就绪: " + err.Error()
				missingMainChecks = 0
			} else {
				expectedPorts = ports
				portsReady = true
				lastReason = ""
				if report != nil {
					report(fmt.Sprintf("启动配置已就绪: RobotAPI=%d Web=%d", expectedPorts.robotAPI, expectedPorts.web))
				}
			}
		}

		if portsReady {
			robotPID, probeErr := probes.readMainPID()
			robotPID = strings.TrimSpace(robotPID)
			switch {
			case probeErr != nil:
				lastReason = "读取主进程状态失败: " + probeErr.Error()
				missingMainChecks = 0
			case robotPID == "":
				lastReason = "主进程未运行"
				missingMainChecks++
			default:
				missingMainChecks = 0
				sinkPID, sinkErr := probes.readSinkPID()
				switch {
				case sinkErr != nil:
					lastReason = "读取 stdout 日志进程状态失败: " + sinkErr.Error()
				case strings.TrimSpace(sinkPID) == "":
					lastReason = "stdout 日志进程未运行"
				default:
					listeners, listenersErr := probes.readListeners()
					if listenersErr != nil {
						lastReason = "读取监听端口失败: " + listenersErr.Error()
					} else if listenerPortsReady(listeners, expectedPorts) {
						if report != nil {
							report(fmt.Sprintf("启动校验通过: pid=%s，端口 %d/%d 已监听", robotPID, expectedPorts.robotAPI, expectedPorts.web))
						}
						return robotPID, nil
					} else {
						lastReason = fmt.Sprintf("端口 %d/%d 未就绪", expectedPorts.robotAPI, expectedPorts.web)
					}
				}
			}
		}
		if report != nil && (lastReason != lastReportedReason || (attempt+1)%5 == 0) {
			report(fmt.Sprintf("等待 robot 启动: %s，已等待%d秒", lastReason, attempt+1))
			lastReportedReason = lastReason
		}
		if missingMainChecks >= 5 {
			return "", fmt.Errorf("robot 启动校验失败: 主进程连续5秒未运行")
		}
		if attempt+1 < attempts && probes.wait != nil {
			probes.wait()
		}
	}
	return "", fmt.Errorf("robot 启动校验失败: %s，请检查 /root/config/logs/start_error.log", lastReason)
}

func (dw *DeployWindow) appendRemoteRobotDiagnostics(client *ssh.Client) {
	dw.appendLog("--- robot 启动失败诊断 ---")
	dw.appendRemoteDiagnostic(client, "进程", "pgrep -af '^/root/robot$|^/root/robot --web-admin|^/root/robot --bounded-log-sink' || true")
	dw.appendRemoteDiagnostic(client, "监听端口", "ss -ltnp 2>/dev/null | grep -E ':(8111|8112)\\b' || true")
	dw.appendRemoteDiagnostic(client, "start_error.log", "tail -n 40 /root/config/logs/start_error.log 2>/dev/null || true")
	dw.appendRemoteDiagnostic(client, "stdout.log", "tail -n 60 /root/config/logs/stdout.log 2>/dev/null || true")
	dw.appendRemoteDiagnostic(client, "robot.log", "tail -n 60 /root/config/logs/robot.log 2>/dev/null || true")
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
	return runRemoteCommand(client, cmd, nil, true, remoteCommandTimeout)
}

func runCmd(client *ssh.Client, cmd string) error {
	_, err := runRemoteCommand(client, cmd, nil, false, remoteCommandTimeout)
	return err
}

func runCmdBg(client *ssh.Client, cmd string) error {
	// The remote command backgrounds the long-lived process itself. Waiting for
	// its short-lived shell gives startup a deadline and avoids leaking sessions.
	return runCmd(client, cmd)
}

type remoteCommandResult struct {
	output []byte
	err    error
}

func awaitRemoteCommand(result <-chan remoteCommandResult, timeout time.Duration, cancel func()) remoteCommandResult {
	if timeout <= 0 {
		return remoteCommandResult{err: fmt.Errorf("远程命令超时必须大于 0")}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case completed := <-result:
		return completed
	case <-timer.C:
		if cancel != nil {
			cancel()
		}
		return remoteCommandResult{err: fmt.Errorf("远程命令执行超时 (%s)", timeout)}
	}
}

func runRemoteCommand(client *ssh.Client, cmd string, stdin io.Reader, captureOutput bool, timeout time.Duration) (string, error) {
	if client == nil {
		return "", fmt.Errorf("SSH 客户端为空")
	}
	result := make(chan remoteCommandResult, 1)
	go func() {
		session, err := client.NewSession()
		if err != nil {
			result <- remoteCommandResult{err: err}
			return
		}
		defer session.Close()
		session.Stdin = stdin
		if captureOutput {
			output, outputErr := session.Output(cmd)
			result <- remoteCommandResult{output: output, err: outputErr}
			return
		}
		result <- remoteCommandResult{err: session.Run(cmd)}
	}()

	completed := awaitRemoteCommand(result, timeout, func() {
		_ = client.Close()
	})
	return string(completed.output), completed.err
}

type uploadRunner func(command string, stdin io.Reader) error

func uploadFile(client *ssh.Client, local string) error {
	file, err := os.Open(local)
	if err != nil {
		return err
	}
	defer file.Close()

	return uploadReader(file, func(command string, stdin io.Reader) error {
		_, err := runRemoteCommand(client, command, stdin, false, remoteUploadTimeout)
		return err
	})
}

func uploadReader(source io.Reader, run uploadRunner) error {
	if source == nil {
		return fmt.Errorf("nil upload source")
	}
	if run == nil {
		return fmt.Errorf("nil upload runner")
	}
	return run(robotUploadCommand, source)
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
	if _, err := (MainWindow{
		AssignTo: &dw.MainWindow,
		Title:    "DNF Robot 部署启动器",
		MinSize:  Size{Width: 480, Height: 420},
		Size:     Size{Width: 480, Height: 520},
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
