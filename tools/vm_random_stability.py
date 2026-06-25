#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""
VM-local random stability pressure for the robot system.

Compatible with the VM's default Python 2.7 and modern Python 3.

Default full scenario:
- run 8 hours
- sample CPU/status every 30 seconds
- collect filtered robot/web logs every 10 minutes
- run fixed debug phases: 20 smoke, 100, 600, monitor fault, game fault,
  shrink/re-expand, robot restart
- continue random target changes and small UID cleanup during the run

Run on the VM:
  python /root/vm_random_stability.py
"""
from __future__ import print_function

import argparse
import csv
import datetime
import io
import json
import os
import random
import re
import socket
import subprocess
import sys
import time


KEYWORDS = [
    "connect_queue_full",
    "message_queue_full",
    "msg_queue_full",
    "timer_queue_overflow",
    "panic",
    "fatal",
    "store_concurrent_limit",
    "online_confirm_timeout",
    "broken_lease",
    "broken_cleanup",
    "lease_health_check_failed",
    "web admin exited",
    "WebAdmin",
]

SAMPLE_FIELDS = [
    "time",
    "target",
    "actors",
    "leased",
    "running",
    "connecting",
    "idle",
    "blocked",
    "recycling",
    "actor_idle",
    "actor_assigned",
    "actor_online",
    "actor_running",
    "actor_busy",
    "actor_releasing",
    "store_running",
    "scheduler_mode",
    "scheduler_reason",
    "goroutines",
    "robot_cpu_api",
    "robot_mem_mb",
    "robot_pid_cpu",
    "df_game_cpu",
    "load1",
    "load5",
    "load15",
    "top_cpu",
    "keyword_hits",
    "api_error",
    "event",
]


def now_text():
    return datetime.datetime.now().strftime("%Y-%m-%d %H:%M:%S")


def safe_text(value):
    if value is None:
        return ""
    if isinstance(value, bytes):
        return value.decode("utf-8", "replace")
    return str(value)


def json_text(value, limit):
    raw = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    if not isinstance(raw, str):
        raw = raw.encode("utf-8")
    if len(raw) > limit:
        return raw[:limit] + "...<truncated>"
    return raw


class RobotAPI(object):
    def __init__(self, host, port, timeout):
        self.host = host
        self.port = port
        self.timeout = timeout

    def call(self, command, payload=None):
        if payload is None:
            payload = {}
        body = json.dumps(payload, ensure_ascii=True, separators=(",", ":"))
        packet = ("<tw><c>%s</c><json>%s</json></tw>" % (command, body)).encode("utf-8")
        data = b""
        sock = socket.create_connection((self.host, self.port), self.timeout)
        try:
            sock.settimeout(self.timeout)
            sock.sendall(packet)
            while b"</tw>" not in data:
                chunk = sock.recv(65536)
                if not chunk:
                    break
                data += chunk
        finally:
            sock.close()
        text = data.decode("utf-8", "replace")
        match = re.search(r"<result>(.*)</result>", text, re.S)
        if not match:
            return {"ok": False, "error": "missing result tag", "raw": text[:1000]}
        try:
            return json.loads(match.group(1))
        except Exception as exc:
            return {"ok": False, "error": "invalid json: %r" % (exc,), "raw": match.group(1)[:1000]}


class StabilityRun(object):
    def __init__(self, args):
        self.args = args
        stamp = datetime.datetime.now().strftime("%Y%m%d-%H%M%S")
        self.out_dir = args.out_dir or ("/root/robot_stability_%s" % stamp)
        if not os.path.isdir(self.out_dir):
            os.makedirs(self.out_dir)
        self.api = RobotAPI(args.robot_host, args.robot_port, args.api_timeout)
        self.events = io.open(os.path.join(self.out_dir, "events.log"), "a", encoding="utf-8", buffering=1)
        self.samples_file = open(os.path.join(self.out_dir, "samples.csv"), "ab")
        self.samples = csv.DictWriter(self.samples_file, fieldnames=SAMPLE_FIELDS)
        if os.path.getsize(os.path.join(self.out_dir, "samples.csv")) == 0:
            self.samples.writerow(dict((k, k) for k in SAMPLE_FIELDS))
        self.deleted_total = 0
        self.started = time.time()

    def log(self, message):
        line = u"[%s] %s" % (now_text(), safe_text(message))
        print(line.encode("utf-8") if sys.version_info[0] < 3 else line)
        self.events.write(line + u"\n")

    def run(self):
        random.seed(self.args.seed or int(time.time() * 1000000))
        self.log("start out_dir=%s args=%s" % (self.out_dir, vars(self.args)))
        self.ensure_auto()
        next_target = time.time() + random.randint(self.args.target_min_interval, self.args.target_max_interval)
        next_cleanup = time.time() + random.randint(self.args.cleanup_min_interval, self.args.cleanup_max_interval)
        next_log_snapshot = time.time()
        next_sample = 0
        end_at = time.time() + self.args.hours * 3600
        scenario = self.scenario_events()
        fired = {}
        try:
            while time.time() < end_at:
                now = time.time()
                if now >= next_sample:
                    self.sample()
                    next_sample = now + self.args.sample_interval
                if now >= next_log_snapshot:
                    self.collect_logs("periodic")
                    next_log_snapshot = now + self.args.log_snapshot_interval
                for event in scenario:
                    if event["name"] in fired:
                        continue
                    if now - self.started >= event["at"]:
                        fired[event["name"]] = True
                        self.log("scenario event start name=%s at=%ss" % (event["name"], event["at"]))
                        try:
                            event["fn"]()
                        except Exception as exc:
                            self.log("scenario event error name=%s err=%r" % (event["name"], exc))
                        self.log("scenario event done name=%s" % event["name"])
                if now >= next_target:
                    self.random_target()
                    next_target = now + random.randint(self.args.target_min_interval, self.args.target_max_interval)
                if now >= next_cleanup:
                    self.random_cleanup()
                    next_cleanup = now + random.randint(self.args.cleanup_min_interval, self.args.cleanup_max_interval)
                time.sleep(1)
        except KeyboardInterrupt:
            self.log("interrupted by user")
        finally:
            self.write_summary()
            self.events.close()
            self.samples_file.close()

    def scenario_events(self):
        if self.args.scenario == "random":
            return []
        return [
            {"name": "target20", "at": 0, "fn": lambda: self.set_target(20)},
            {"name": "smoke_actions", "at": 2 * 60, "fn": self.smoke_actions},
            {"name": "target100", "at": 10 * 60, "fn": lambda: self.set_target(100)},
            {"name": "target600", "at": 30 * 60, "fn": lambda: self.set_target(600)},
            {"name": "monitor_fault", "at": 90 * 60, "fn": self.monitor_fault},
            {"name": "game_port_fault", "at": 150 * 60, "fn": self.game_port_fault},
            {"name": "shrink100", "at": 240 * 60, "fn": lambda: self.set_target(100)},
            {"name": "reexpand600", "at": 270 * 60, "fn": lambda: self.set_target(600)},
            {"name": "robot_restart", "at": 330 * 60, "fn": self.robot_restart},
            {"name": "final_reexpand600", "at": 390 * 60, "fn": lambda: self.set_target(600)},
        ]

    def ensure_auto(self):
        if self.args.scenario == "full":
            self.set_target(20)
            return
        self.set_target(random.randint(self.args.target_min, self.args.target_max))

    def set_target(self, target):
        payload = {"updates": {"auto.auto_target_online_count": str(target), "auto.auto_actions": "true"}}
        res = self.safe_call("robotConfigUpdate", payload)
        self.log("set_target target=%s config=%s" % (target, json_text(res, 1200)))
        res = self.safe_call("autoStart", {})
        self.log("autoStart result=%s" % json_text(res, 1200))
        self.sample_with_event("after_set_target:%s" % target)
        if self.args.scenario == "full" and target >= 100:
            self.burst_sample("set_target:%s" % target, 60, 5)

    def random_target(self):
        self.set_target(random.randint(self.args.target_min, self.args.target_max))

    def random_cleanup(self):
        if self.args.no_cleanup:
            self.log("cleanup skipped no_cleanup=true")
            return
        if self.deleted_total >= self.args.cleanup_max_total:
            self.log("cleanup skipped deleted_total=%s max=%s" % (self.deleted_total, self.args.cleanup_max_total))
            return
        count = random.randint(self.args.cleanup_min_count, self.args.cleanup_max_count)
        count = min(count, self.args.cleanup_max_total - self.deleted_total)
        status = self.safe_call("robotsStatus", {"count": self.args.status_count})
        rows = (((status or {}).get("result") or {}).get("robots") or [])
        if not rows:
            self.log("cleanup skipped no robots status=%s" % json_text(status, 1000))
            return
        unhealthy = []
        for row in rows:
            if (
                row.get("missing_core")
                or row.get("health_state") in ("broken", "suspect", "disconnected")
                or row.get("runtime_state") not in ("running", "store")
                or not row.get("actor_attached")
            ):
                unhealthy.append(row)
        pool = unhealthy
        reason = "unhealthy"
        if not pool and self.args.allow_online_cleanup:
            pool = [r for r in rows if r.get("uid")]
            reason = "online_sample"
        if not pool:
            self.log("cleanup skipped no candidates")
            return
        random.shuffle(pool)
        uids = []
        for row in pool[:count]:
            uid = int(row.get("uid") or 0)
            if uid > 0:
                uids.append(uid)
        if not uids:
            self.log("cleanup skipped empty uid list")
            return
        self.log("cleanup selected reason=%s uids=%s" % (reason, uids))
        if reason == "online_sample":
            logout = self.safe_call("robotsLogoutAsync", {"uids": uids})
            self.log("cleanup pre_logout uids=%s result=%s" % (uids, json_text(logout, 1200)))
            time.sleep(self.args.cleanup_logout_wait)
        result = self.safe_call("cleanupRobots", {"uids": uids, "force": True})
        deleted = int((((result or {}).get("result") or {}).get("deleted")) or 0)
        self.deleted_total += deleted
        self.log("cleanup result uids=%s deleted=%s total=%s result=%s" % (uids, deleted, self.deleted_total, json_text(result, 2000)))
        self.sample_with_event("after_cleanup:%s" % deleted)

    def smoke_actions(self):
        status = self.safe_call("robotsStatus", {"count": 20})
        rows = (((status or {}).get("result") or {}).get("robots") or [])
        uids = [int(r.get("uid") or 0) for r in rows if int(r.get("uid") or 0) > 0][:3]
        if not uids:
            self.log("smoke_actions skipped no uids status=%s" % json_text(status, 1000))
            return
        self.log("smoke_actions uids=%s" % uids)
        actions = [
            ("robotsMove", {"uids": uids[:2]}),
            ("robotsShoutLocal", {"uids": uids[:2]}),
            ("robotsShoutWorld", {"uids": uids[:1]}),
            ("robotsStoreAsync", {"uids": uids[:1]}),
        ]
        for command, payload in actions:
            res = self.safe_call(command, payload)
            self.log("smoke_action command=%s payload=%s result=%s" % (command, payload, json_text(res, 1600)))
            self.sample_with_event("smoke:%s" % command)
            time.sleep(8)

    def monitor_fault(self):
        self.log("monitor_fault stop df_monitor_r")
        self.sample_with_event("monitor_fault_stop")
        self.shell("pkill -TERM -f './df_monitor_r mnt_cain start' || true; sleep 8; ss -lntp | grep ':30303' || true", 30)
        status = self.safe_call("robotsStatus", {"count": 20})
        rows = (((status or {}).get("result") or {}).get("robots") or [])
        uids = [int(r.get("uid") or 0) for r in rows if int(r.get("uid") or 0) > 0][:1]
        if uids:
            res = self.safe_call("robotsShoutWorld", {"uids": uids})
            self.log("monitor_fault world_shout_down uids=%s result=%s" % (uids, json_text(res, 1600)))
        self.log("monitor_fault restore /root/run")
        self.shell("cd /root && (./run >/tmp/vm_random_run_monitor.log 2>&1 || true); sleep 20; ss -lntp | grep ':30303' || true; pgrep -af 'df_monitor_r|df_game_r' || true", 180)
        self.sample_with_event("monitor_fault_restore")
        self.burst_sample("monitor_fault_recover", 60, 5)

    def game_port_fault(self):
        self.log("game_port_fault stop /root/stop")
        self.sample_with_event("game_port_fault_stop")
        self.shell("cd /root && (./stop >/tmp/vm_random_stop_game.log 2>&1 || true); sleep 15; ss -lntp | grep ':10011' || true; pgrep -af 'df_game_r' || true", 180)
        self.sample_with_event("game_port_down")
        time.sleep(120)
        self.log("game_port_fault restore /root/run")
        self.shell("cd /root && (./run >/tmp/vm_random_run_game.log 2>&1 || true); sleep 30; ss -lntp | grep -E ':(10011|30303)' || true; pgrep -af 'df_game_r|df_monitor_r' || true", 240)
        self.sample_with_event("game_port_fault_restore")
        self.burst_sample("game_port_fault_recover", 60, 5)

    def robot_restart(self):
        self.log("robot_restart begin")
        self.sample_with_event("robot_restart_stop")
        script = r"""
pids=$(ps -eo pid,args | awk '$2=="/root/robot" || ($2=="/root/robot" && $3=="--web-admin") {print $1}')
[ -z "$pids" ] || kill -TERM $pids || true
sleep 8
left=$(ps -eo pid,args | awk '$2=="/root/robot" || ($2=="/root/robot" && $3=="--web-admin") {print $1}')
[ -z "$left" ] || kill -KILL $left || true
nohup /root/robot > /root/robot_stdout.log 2>&1 &
sleep 10
pgrep -af '/root/robot|df_game_r|df_monitor_r' || true
ss -lntp | grep -E ':(8111|8112|10011|30303)' || true
"""
        self.shell(script, 120)
        time.sleep(20)
        self.set_target(self.args.target_max)
        self.burst_sample("robot_restart_recover", 60, 5)

    def sample(self):
        row = self.sample_row()
        self.writerow(row)
        self.log(
            "sample target=%s actors=%s leased=%s running=%s connecting=%s mode=%s load=%s/%s/%s top=%s hits=%s api_error=%s"
            % (
                row.get("target"),
                row.get("actors"),
                row.get("leased"),
                row.get("running"),
                row.get("connecting"),
                row.get("scheduler_mode"),
                row.get("load1"),
                row.get("load5"),
                row.get("load15"),
                row.get("top_cpu"),
                row.get("keyword_hits"),
                row.get("api_error"),
            )
        )

    def writerow(self, row):
        encoded = {}
        for key in SAMPLE_FIELDS:
            value = safe_text(row.get(key, ""))
            if sys.version_info[0] < 3:
                encoded[key] = value.encode("utf-8")
            else:
                encoded[key] = value
        self.samples.writerow(encoded)
        self.samples_file.flush()

    def safe_call(self, command, payload=None):
        try:
            return self.api.call(command, payload)
        except Exception as exc:
            self.log("api_error command=%s err=%r" % (command, exc))
            return {"ok": False, "error": repr(exc)}

    def load_average(self):
        try:
            raw = io.open("/proc/loadavg", "r", encoding="utf-8").read().split()
            return raw[0], raw[1], raw[2]
        except Exception:
            return "", "", ""

    def top_cpu(self):
        try:
            out = subprocess.check_output(["ps", "-eo", "pid,ppid,pcpu,pmem,nlwp,comm,args", "--sort=-pcpu"])
            if not isinstance(out, str):
                out = out.decode("utf-8", "replace")
            lines = [line.strip() for line in out.splitlines()[1:8] if line.strip()]
            return " | ".join(lines)
        except Exception as exc:
            return "ps_error=%r" % (exc,)

    def keyword_hits(self):
        counts = dict((key, 0) for key in KEYWORDS)
        for path in ("/root/config/log_robot", "/root/robot_stdout.log"):
            try:
                out = subprocess.check_output(["tail", "-n", str(self.args.log_tail_lines), path])
            except Exception:
                continue
            if not isinstance(out, str):
                out = out.decode("utf-8", "replace")
            for key in KEYWORDS:
                counts[key] += out.count(key)
        return ";".join("%s=%s" % (key, value) for key, value in counts.items() if value)

    def proc_pid_cpu(self, pattern):
        try:
            out = subprocess.check_output(["pgrep", "-f", pattern]) or b""
            if not isinstance(out, str):
                out = out.decode("utf-8", "replace")
            pids = [int(x) for x in out.strip().split("\n") if x]
            if not pids:
                return ""
            total = 0.0
            for pid in pids:
                cpu = subprocess.check_output(["ps", "-p", str(pid), "-o", "pcpu=", "--no-headers"]) or b""
                if not isinstance(cpu, str):
                    cpu = cpu.decode("utf-8", "replace")
                try:
                    total += float(cpu.strip())
                except ValueError:
                    pass
            return "%.1f" % total
        except Exception:
            return ""

    def sample_with_event(self, event):
        row = self.sample_row()
        row["event"] = safe_text(event)
        self.writerow(row)
        self.log(
            "sample event=%s target=%s running=%s mode=%s load=%s/%s/%s robot_cpu=%s df_game_cpu=%s goroutines=%s"
            % (
                event,
                row.get("target"),
                row.get("running"),
                row.get("scheduler_mode"),
                row.get("load1"),
                row.get("load5"),
                row.get("load15"),
                row.get("robot_pid_cpu"),
                row.get("df_game_cpu"),
                row.get("goroutines"),
            )
        )

    def burst_sample(self, event, duration_sec=60, interval_sec=5):
        self.log("burst_sample start event=%s duration=%ss" % (event, duration_sec))
        deadline = time.time() + duration_sec
        while time.time() < deadline:
            time.sleep(interval_sec)
            self.sample_with_event("burst:%s" % event)
        self.log("burst_sample done event=%s" % event)

    def sample_row(self):
        row = dict((key, "") for key in SAMPLE_FIELDS)
        row["time"] = now_text()
        try:
            auto = (self.api.call("autoStatus").get("result") or {})
            sched = (self.api.call("schedulerStatus").get("result") or {})
            system = (self.api.call("systemStatus").get("result") or {})
            row.update(
                {
                    "target": auto.get("target_online"),
                    "actors": auto.get("actors"),
                    "leased": auto.get("leased"),
                    "running": auto.get("running"),
                    "connecting": auto.get("connecting"),
                    "idle": auto.get("idle"),
                    "blocked": auto.get("blocked_uids"),
                    "recycling": auto.get("recycling"),
                    "actor_idle": auto.get("actor_idle"),
                    "actor_assigned": auto.get("actor_assigned"),
                    "actor_online": auto.get("actor_online"),
                    "actor_running": auto.get("actor_running"),
                    "actor_busy": auto.get("actor_busy"),
                    "actor_releasing": auto.get("actor_releasing"),
                    "store_running": auto.get("store_running"),
                    "scheduler_mode": sched.get("mode"),
                    "scheduler_reason": sched.get("reason"),
                    "goroutines": sched.get("goroutines"),
                    "robot_cpu_api": system.get("robot_cpu_percent"),
                    "robot_mem_mb": system.get("robot_memory_mb"),
                }
            )
        except Exception as exc:
            row["api_error"] = repr(exc)
        row["robot_pid_cpu"] = self.proc_pid_cpu("'/root/robot'")
        row["df_game_cpu"] = self.proc_pid_cpu("'./df_game_r'")
        load1, load5, load15 = self.load_average()
        row["load1"], row["load5"], row["load15"] = load1, load5, load15
        row["top_cpu"] = self.top_cpu()
        row["keyword_hits"] = self.keyword_hits()
        return row

    def collect_logs(self, label):
        path = os.path.join(self.out_dir, "collected_logs.log")
        command = """
echo '===== %s %s uptime ====='
date
uptime
echo '===== ps top ====='
ps -eo pid,ppid,pcpu,pmem,nlwp,comm,args --sort=-pcpu | head -n 25
echo '===== robot log filtered ====='
tail -n %s /root/config/log_robot 2>/dev/null | grep -a -E '%s' | tail -n 200 || true
echo '===== web stdout filtered ====='
tail -n %s /root/robot_stdout.log 2>/dev/null | grep -a -E '%s|request pid|auth rejected|web admin exited' | tail -n 120 || true
""" % (
            label,
            now_text(),
            self.args.log_tail_lines,
            "|".join(re.escape(k) for k in KEYWORDS),
            self.args.log_tail_lines,
            "|".join(re.escape(k) for k in KEYWORDS),
        )
        out = self.shell(command, 60, log_output=False)
        try:
            fh = io.open(path, "a", encoding="utf-8")
            fh.write(out.decode("utf-8", "replace") if isinstance(out, bytes) else out)
            fh.write(u"\n")
            fh.close()
        except Exception as exc:
            self.log("collect_logs write_error err=%r" % (exc,))
        self.log("collect_logs label=%s path=%s bytes=%s" % (label, path, len(out)))

    def shell(self, command, timeout, log_output=True):
        proc = subprocess.Popen(command, shell=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        start = time.time()
        while proc.poll() is None:
            if time.time() - start > timeout:
                try:
                    proc.kill()
                except Exception:
                    pass
                out = proc.communicate()[0] or b""
                text = out.decode("utf-8", "replace") if not isinstance(out, str) else out
                if log_output:
                    self.log("shell_timeout command=%s output=%s" % (command[:160], text[:2000]))
                return text
            time.sleep(1)
        out = proc.communicate()[0] or b""
        text = out.decode("utf-8", "replace") if not isinstance(out, str) else out
        if log_output:
            self.log("shell command=%s output=%s" % (command[:160], text[:2000]))
        return text

    def write_summary(self):
        summary = {
            "started_at": datetime.datetime.fromtimestamp(self.started).isoformat(),
            "finished_at": datetime.datetime.now().isoformat(),
            "duration_sec": int(time.time() - self.started),
            "deleted_total": self.deleted_total,
            "out_dir": self.out_dir,
            "args": vars(self.args),
        }
        path = os.path.join(self.out_dir, "summary.json")
        raw = json.dumps(summary, ensure_ascii=False, indent=2)
        if not isinstance(raw, type(u"")):
            raw = raw.decode("utf-8")
        io.open(path, "w", encoding="utf-8").write(raw)
        self.log("summary %s" % json_text(summary, 2000))


def parse_args():
    parser = argparse.ArgumentParser(description="VM-local random stability pressure script")
    parser.add_argument("--hours", type=float, default=8.0)
    parser.add_argument("--scenario", choices=["full", "random"], default="full")
    parser.add_argument("--robot-host", default="127.0.0.1")
    parser.add_argument("--robot-port", type=int, default=8111)
    parser.add_argument("--api-timeout", type=float, default=20.0)
    parser.add_argument("--out-dir", default="")
    parser.add_argument("--sample-interval", type=int, default=30)
    parser.add_argument("--log-snapshot-interval", type=int, default=10 * 60)
    parser.add_argument("--target-min", type=int, default=100)
    parser.add_argument("--target-max", type=int, default=600)
    parser.add_argument("--target-min-interval", type=int, default=20 * 60)
    parser.add_argument("--target-max-interval", type=int, default=40 * 60)
    parser.add_argument("--cleanup-min-interval", type=int, default=60 * 60)
    parser.add_argument("--cleanup-max-interval", type=int, default=90 * 60)
    parser.add_argument("--cleanup-min-count", type=int, default=1)
    parser.add_argument("--cleanup-max-count", type=int, default=3)
    parser.add_argument("--cleanup-max-total", type=int, default=30)
    parser.add_argument("--cleanup-logout-wait", type=int, default=15)
    parser.add_argument("--status-count", type=int, default=1000)
    parser.add_argument("--log-tail-lines", type=int, default=2000)
    parser.add_argument("--no-cleanup", action="store_true")
    parser.add_argument("--allow-online-cleanup", dest="allow_online_cleanup", action="store_true", default=True)
    parser.add_argument("--no-allow-online-cleanup", dest="allow_online_cleanup", action="store_false")
    parser.add_argument("--seed", type=int, default=0)
    return parser.parse_args()


def main():
    args = parse_args()
    if args.target_min > args.target_max:
        args.target_min, args.target_max = args.target_max, args.target_min
    StabilityRun(args).run()
    return 0


if __name__ == "__main__":
    sys.exit(main())
