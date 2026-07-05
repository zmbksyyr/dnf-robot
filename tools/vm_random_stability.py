#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""
VM-local random stability pressure for the robot system.

Compatible with the VM's default Python 2.7 and modern Python 3.

Default full scenario:
- run 1 hour by default; all scenario intervals are scaled by --hours
- sample CPU/status/resource counters every 10 seconds
- collect filtered robot/web/resource logs every 5 minutes
- run service, market, DB, web/API, cleanup, monitor, game and robot fault phases
- every fault is expected to self-heal within a few minutes

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

try:
    text_type = unicode
except NameError:
    text_type = str


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
    "database is closed",
    "db_init",
    "market_service",
    "iteminfo",
    "RegistItem",
    "cannot assign requested address",
    "too many open files",
    "connection reset",
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
    "auction_cpu",
    "point_cpu",
    "db_open",
    "db_in_use",
    "db_idle",
    "db_latency_ms",
    "tcp_estab",
    "tcp_time_wait",
    "tcp_close_wait",
    "tcp_8111_estab",
    "tcp_10011_estab",
    "tcp_30603_estab",
    "tcp_30803_estab",
    "fd_robot",
    "port_10011",
    "port_30603",
    "port_30803",
    "market_auto",
    "market_last_status",
    "market_last_error",
    "market_auction_status",
    "market_auction_open",
    "market_point_status",
    "market_point_open",
    "market_services_ready",
    "market_auction_records",
    "market_auction_kinds",
    "market_cera_records",
    "market_cera_kinds",
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
        return u""
    if isinstance(value, text_type):
        return value
    if isinstance(value, bytes):
        return value.decode("utf-8", "replace")
    try:
        return text_type(value)
    except Exception:
        raw = repr(value)
        if isinstance(raw, bytes):
            return raw.decode("utf-8", "replace")
        return raw


def json_text(value, limit):
    raw = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    if not isinstance(raw, str):
        raw = raw.encode("utf-8")
    if len(raw) > limit:
        return raw[:limit] + "...<truncated>"
    return raw


def shell_quote(value):
    return "'" + safe_text(value).replace("'", "'\\''") + "'"


def sanitize_name(value):
    return re.sub(r"[^A-Za-z0-9_.-]+", "_", safe_text(value)).strip("_") or "snapshot"


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
        if sys.version_info[0] < 3:
            self.samples_file = open(os.path.join(self.out_dir, "samples.csv"), "ab")
        else:
            self.samples_file = open(os.path.join(self.out_dir, "samples.csv"), "a", newline="", encoding="utf-8")
        self.samples = csv.DictWriter(self.samples_file, fieldnames=SAMPLE_FIELDS)
        if os.path.getsize(os.path.join(self.out_dir, "samples.csv")) == 0:
            self.samples.writerow(dict((k, k) for k in SAMPLE_FIELDS))
        self.deleted_total = 0
        self.started = time.time()
        self.total_sec = max(int(args.hours * 3600), 600)
        self.baseline_dir = os.path.join(self.out_dir, "baseline")
        self.snapshot_dir = os.path.join(self.out_dir, "snapshots")
        if not os.path.isdir(self.snapshot_dir):
            os.makedirs(self.snapshot_dir)
        self.results = []
        self.market_zero_since = 0
        self.last_invariant_failure = {}

    def log(self, message):
        line = u"[%s] %s" % (now_text(), safe_text(message))
        print(line.encode("utf-8") if sys.version_info[0] < 3 else line)
        self.events.write(line + u"\n")

    def run(self):
        random.seed(self.args.seed or int(time.time() * 1000000))
        self.log("start out_dir=%s args=%s" % (self.out_dir, vars(self.args)))
        self.prepare_baseline()
        self.ensure_auto()
        next_target = time.time() + random.randint(self.args.target_min_interval, self.args.target_max_interval)
        next_cleanup = time.time() + random.randint(self.args.cleanup_min_interval, self.args.cleanup_max_interval)
        next_user_interleave = time.time() + random.randint(self.args.user_interleave_min_interval, self.args.user_interleave_max_interval)
        next_log_snapshot = time.time()
        next_sample = 0
        next_invariant = 0
        end_at = time.time() + self.total_sec
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
                        self.run_event(event)
                if now >= next_target:
                    self.random_target()
                    next_target = now + random.randint(self.args.target_min_interval, self.args.target_max_interval)
                if now >= next_cleanup:
                    self.random_cleanup()
                    next_cleanup = now + random.randint(self.args.cleanup_min_interval, self.args.cleanup_max_interval)
                if now >= next_user_interleave:
                    self.random_user_interleave()
                    next_user_interleave = now + random.randint(self.args.user_interleave_min_interval, self.args.user_interleave_max_interval)
                if now >= next_invariant:
                    self.check_market_invariants("main_loop")
                    next_invariant = now + self.args.sample_interval
                time.sleep(1)
        except KeyboardInterrupt:
            self.log("interrupted by user")
        finally:
            self.collect_logs("before_final_recover")
            self.final_recover_environment()
            self.collect_logs("final")
            self.write_report()
            self.write_summary()
            self.events.close()
            self.samples_file.close()

    def run_event(self, event):
        name = event["name"]
        self.log("scenario event start name=%s at=%ss" % (name, event["at"]))
        before_path = self.write_snapshot(name + "_before")
        started = time.time()
        err = ""
        recovered = False
        try:
            event["fn"]()
            recovered = self.check_recovered(name)
        except Exception as exc:
            err = repr(exc)
            self.log("scenario event error name=%s err=%s" % (name, err))
            recovered = self.check_recovered(name)
        after_path = self.write_snapshot(name + "_after")
        result = {
            "name": name,
            "started_at": datetime.datetime.fromtimestamp(started).isoformat(),
            "duration_sec": int(time.time() - started),
            "recovered": recovered,
            "error": err,
            "before": before_path,
            "after": after_path,
        }
        self.results.append(result)
        self.log("scenario event done name=%s recovered=%s error=%s" % (name, recovered, err))

    def record_failure(self, name, error):
        now = datetime.datetime.now().isoformat()
        item = {
            "name": name,
            "started_at": now,
            "duration_sec": 0,
            "recovered": False,
            "error": error,
            "before": "",
            "after": "",
        }
        self.results.append(item)
        self.log("invariant failure name=%s error=%s" % (name, error))

    def check_recovered(self, event):
        api = self.safe_call("systemStatus", {})
        market = self.market_status_result()
        ports = self.port_snapshot()
        ok = bool(isinstance(api, dict) and api.get("ok"))
        game_ok = bool(ports.get("10011"))
        market_ok = self.market_services_ready(market)
        if not ok:
            self.log("recover_check event=%s failed reason=robot_api api=%s" % (event, json_text(api, 1000)))
        if not game_ok:
            self.log("recover_check event=%s failed reason=game_port ports=%s" % (event, ports))
        if not market_ok:
            self.log("recover_check event=%s failed reason=market_services services=%s" % (event, json_text((market.get("services") or {}), 1400)))
        return bool(ok and game_ok and market_ok)

    def check_market_invariants(self, event):
        status = self.market_status_result()
        ports = self.port_snapshot()
        counts = self.market_db_counts()
        enabled = self.market_auto_enabled(status)
        running = bool(status.get("auto_running"))
        services_ready = self.market_services_ready(status)
        game_ready = bool(ports.get("10011"))
        now = time.time()
        if enabled and game_ready and services_ready and not running:
            key = "market_auto_stopped:%s" % event
            if now - self.last_invariant_failure.get(key, 0) > 60:
                self.last_invariant_failure[key] = now
                self.record_failure(key, "market auto enabled but not running while game and services are ready")
                self.safe_call("marketStart", {})
        auction_kinds = int(counts.get("auction_kinds") or 0)
        if enabled and running and game_ready and services_ready and auction_kinds <= 0:
            if not self.market_zero_since:
                self.market_zero_since = now
            elif now - self.market_zero_since > self.args.market_zero_grace:
                key = "market_zero_kinds:%s" % event
                if now - self.last_invariant_failure.get(key, 0) > 120:
                    self.last_invariant_failure[key] = now
                    self.record_failure(key, "market auto running but auction kinds stayed zero for %ss" % int(now - self.market_zero_since))
                    self.safe_call("marketStart", {})
        else:
            self.market_zero_since = 0

    def write_snapshot(self, label):
        snap = self.snapshot(label)
        path = os.path.join(self.snapshot_dir, sanitize_name(label) + ".json")
        raw = json.dumps(snap, ensure_ascii=False, indent=2, sort_keys=True)
        if not isinstance(raw, type(u"")):
            raw = raw.decode("utf-8")
        io.open(path, "w", encoding="utf-8").write(raw)
        self.log("snapshot label=%s path=%s" % (label, path))
        return path

    def snapshot(self, label):
        return {
            "label": label,
            "time": now_text(),
            "api": self.api_snapshot(),
            "ports": self.port_snapshot(),
            "processes": self.shell("pgrep -af '/root/robot|df_game_r|df_monitor_r|df_auction_r|df_point_r|mysqld' || true", 20, log_output=False)[:4000],
            "files": self.file_snapshot(),
            "db": self.db_snapshot(),
            "tcp": self.shell("ss -ant | awk 'NR>1 {c[$1]++} END {for (k in c) print k,c[k]}'", 20, log_output=False)[:2000],
            "disk": self.shell("df -h / /root /home 2>/dev/null || df -h", 20, log_output=False)[:2000],
        }

    def api_snapshot(self):
        data = {}
        for command in ("systemStatus", "autoStatus", "schedulerStatus", "databaseStatus", "marketStatus"):
            data[command] = self.safe_call(command, {})
        return data

    def port_snapshot(self):
        out = self.shell("ss -ltn", 20, log_output=False)
        return {
            "8111": int(":8111" in out),
            "8112": int(":8112" in out),
            "10011": int(":10011" in out),
            "30303": int(":30303" in out),
            "30603": int(":30603" in out),
            "30803": int(":30803" in out),
        }

    def file_snapshot(self):
        paths = [
            "/root/robot",
            "/root/run",
            "/root/stop",
            "/root/config/market_config.json",
            "/root/config/pvf_equipment_catalog.json",
            "/root/config/pvf_stackable_catalog.json",
            "/root/config/pvf_iteminfo.dat",
            "/home/neople/auction/iteminfo.dat",
            "/home/neople/point/iteminfo.dat",
            "/dp2/Script.pvf",
            "/home/neople/game/Script.pvf",
        ]
        quoted = " ".join(shell_quote(p) for p in paths)
        return self.shell("for f in %s; do [ -e \"$f\" ] && stat -c '%%n size=%%s mode=%%a mtime=%%Y' \"$f\" && md5sum \"$f\" 2>/dev/null | cut -d' ' -f1 | sed 's/^/md5=/' || echo \"$f missing\"; done" % quoted, 60, log_output=False)[:8000]

    def db_snapshot(self):
        query = (
            "SELECT 'auction_count',COUNT(*),COUNT(DISTINCT item_id) FROM taiwan_cain_auction_gold.auction_main;"
            "SELECT 'cera_count',COUNT(*),COUNT(DISTINCT item_id) FROM taiwan_cain_auction_cera.auction_main;"
            "SELECT 'auction_system',COUNT(*),COUNT(DISTINCT item_id) FROM taiwan_cain_auction_gold.auction_main WHERE owner_id>=90000001;"
            "SELECT 'cera_system',COUNT(*),COUNT(DISTINCT item_id) FROM taiwan_cain_auction_cera.auction_main WHERE owner_id>=90000001;"
            "SHOW COLUMNS FROM taiwan_cain_auction_gold.auction_main;"
            "SHOW COLUMNS FROM taiwan_cain_auction_cera.auction_main;"
        )
        return self.shell("mysql -ugame -puu5!^%%jg -e %s" % shell_quote(query), 60, log_output=False)[:12000]

    def market_db_counts(self):
        query = (
            "SELECT 'auction',COUNT(*),COUNT(DISTINCT item_id) FROM taiwan_cain_auction_gold.auction_main;"
            "SELECT 'cera',COUNT(*),COUNT(DISTINCT item_id) FROM taiwan_cain_auction_cera.auction_main;"
        )
        out = self.shell("mysql -ugame -puu5!^%%jg -N -e %s" % shell_quote(query), 30, log_output=False)
        counts = {}
        for line in safe_text(out).splitlines():
            parts = line.split()
            if len(parts) >= 3 and parts[0] in ("auction", "cera"):
                counts[parts[0] + "_records"] = parts[1]
                counts[parts[0] + "_kinds"] = parts[2]
        return counts

    def prepare_baseline(self):
        if not os.path.isdir(self.baseline_dir):
            os.makedirs(self.baseline_dir)
        self.log("prepare_baseline begin dir=%s" % self.baseline_dir)
        self.shell("cp -af /root/config %s/root_config 2>/dev/null || true" % shell_quote(self.baseline_dir), 120)
        self.shell("mkdir -p %s/home_neople_auction %s/home_neople_point; cp -af /home/neople/auction/iteminfo.dat %s/home_neople_auction/iteminfo.dat 2>/dev/null || true; cp -af /home/neople/point/iteminfo.dat %s/home_neople_point/iteminfo.dat 2>/dev/null || true" % (shell_quote(self.baseline_dir), shell_quote(self.baseline_dir), shell_quote(self.baseline_dir), shell_quote(self.baseline_dir)), 60)
        self.backup_market_database("baseline")
        restore_path = os.path.join(self.baseline_dir, "restore_baseline.sh")
        restore = """#!/bin/sh
set -e
BASE=%s
rm -rf /root/config
cp -af "$BASE/root_config" /root/config 2>/dev/null || mkdir -p /root/config
cp -af "$BASE/home_neople_auction/iteminfo.dat" /home/neople/auction/iteminfo.dat 2>/dev/null || true
cp -af "$BASE/home_neople_point/iteminfo.dat" /home/neople/point/iteminfo.dat 2>/dev/null || true
if [ -s "$BASE/market_robot_stock.sql" ]; then mysql -ugame -puu5!^%%jg < "$BASE/market_robot_stock.sql"; fi
echo RESTORED
""" % shell_quote(self.baseline_dir)
        try:
            fh = io.open(restore_path, "w", encoding="utf-8")
            fh.write(restore)
            fh.close()
            os.chmod(restore_path, 0o755)
        except Exception as exc:
            self.log("prepare_baseline restore_script_error err=%r" % (exc,))
        self.log("prepare_baseline done restore=%s" % restore_path)

    def final_recover_environment(self):
        self.log("final_recover_environment begin")
        restore_path = os.path.join(self.baseline_dir, "restore_baseline.sh")
        if os.path.isfile(restore_path):
            self.shell("sh %s" % shell_quote(restore_path), 180)
        else:
            self.log("final_recover_environment missing restore script=%s" % restore_path)
        self.shell("cd /root && (./run >/tmp/vm_random_final_run.log 2>&1 || true); sleep 20; ss -lntp | grep -E ':(10011|30303|30603|30803)' || true; pgrep -af 'df_game_r|df_monitor_r|df_auction_r|df_point_r' || true", 240)
        self.robot_restart_without_target("final_recover_robot")
        self.wait_robot_api("final_recover_api", 90, 5)
        self.market_enable_auto(max_concurrent=8)
        self.wait_market_services("final_recover_market", 240, 10)
        self.sample_with_event("final_recover_done")
        self.log("final_recover_environment done")

    def wait_robot_api(self, event, timeout_sec=90, interval_sec=5):
        self.log("wait_robot_api start event=%s timeout=%s" % (event, timeout_sec))
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            last = self.safe_call("systemStatus", {})
            if isinstance(last, dict) and last.get("ok"):
                self.log("wait_robot_api ready event=%s result=%s" % (event, json_text(last, 1200)))
                return True
            time.sleep(interval_sec)
        self.log("wait_robot_api timeout event=%s last=%s" % (event, json_text(last, 1200)))
        return False

    def scenario_events(self):
        high = self.args.target_max
        mid = max(self.args.target_min, min(high, max(100, high // 2)))
        return [
            {"name": "target20", "at": 0, "fn": lambda: self.set_target(20)},
            {"name": "smoke_actions", "at": self.event_at(0.03), "fn": self.smoke_actions},
            {"name": "announcement_check", "at": self.event_at(0.07), "fn": self.announcement_check},
            {"name": "market_fault", "at": self.event_at(0.10), "fn": self.market_fault},
            {"name": "market_startup_iteminfo_race", "at": self.event_at(0.16), "fn": self.market_startup_iteminfo_race},
            {"name": "pvf_file_fault", "at": self.event_at(0.20), "fn": self.pvf_file_fault},
            {"name": "market_button_flow", "at": self.event_at(0.27), "fn": self.market_button_flow},
            {"name": "target_mid", "at": self.event_at(0.34), "fn": lambda: self.set_target(mid)},
            {"name": "manual_collision", "at": self.event_at(0.40), "fn": self.manual_collision},
            {"name": "db_stock_external_clear", "at": self.event_at(0.46), "fn": self.db_stock_external_clear},
            {"name": "db_schema_drift", "at": self.event_at(0.52), "fn": self.db_schema_drift},
            {"name": "target_high", "at": self.event_at(0.58), "fn": lambda: self.set_target(high)},
            {"name": "cleanup_burst", "at": self.event_at(0.64), "fn": self.cleanup_burst},
            {"name": "config_dir_fault", "at": self.event_at(0.70), "fn": self.config_dir_fault},
            {"name": "web_api_fault", "at": self.event_at(0.76), "fn": self.web_api_fault},
            {"name": "port_conflict_fault", "at": self.event_at(0.81), "fn": self.port_conflict_fault},
            {"name": "mysql_restart_fault", "at": self.event_at(0.86), "fn": self.mysql_restart_fault},
            {"name": "monitor_fault", "at": self.event_at(0.90), "fn": self.monitor_fault},
            {"name": "game_port_fault", "at": self.event_at(0.93), "fn": self.game_port_fault},
            {"name": "robot_restart", "at": self.event_at(0.96), "fn": self.robot_restart},
            {"name": "custom_key_test", "at": self.event_at(0.975), "fn": self.custom_key_test},
            {"name": "final_target_mid", "at": self.event_at(0.98), "fn": lambda: self.set_target(mid)},
        ]

    def event_at(self, ratio):
        if ratio <= 0:
            return 0
        return int(min(max(self.total_sec * ratio, 1), max(self.total_sec - 30, 1)))

    def scaled_seconds(self, low, high):
        value = int(self.total_sec / 40)
        return max(low, min(high, value))

    def ensure_auto(self):
        self.set_target(20)

    def set_target(self, target):
        payload = {"updates": {"auto.auto_target_online_count": str(target), "auto.auto_actions": "true"}}
        res = self.safe_call("robotConfigUpdate", payload)
        self.log("set_target target=%s config=%s" % (target, json_text(res, 1200)))
        res = self.safe_call("autoStart", {})
        self.log("autoStart result=%s" % json_text(res, 1200))
        self.sample_with_event("after_set_target:%s" % target)

    def random_target(self):
        self.set_target(random.randint(self.args.target_min, self.args.target_max))

    def random_user_interleave(self):
        actions = [
            self.user_market_start,
            self.user_market_stop_start,
            self.user_market_iteminfo,
            self.user_market_clear_stock,
            self.user_market_restock_once,
            self.user_market_collect_once,
        ]
        action = random.choice(actions)
        name = getattr(action, "__name__", "user_action")
        self.log("random_user_interleave action=%s" % name)
        try:
            action()
        finally:
            self.check_market_invariants(name)

    def user_market_start(self):
        self.market_enable_auto(max_concurrent=8)
        self.sample_with_event("user_market_start")

    def user_market_stop_start(self):
        res = self.safe_call("marketStop", {})
        self.log("user_market_stop_start stop result=%s" % json_text(res, 1200))
        time.sleep(random.randint(2, 8))
        res = self.safe_call("marketStart", {})
        self.log("user_market_stop_start start result=%s" % json_text(res, 1200))
        self.sample_with_event("user_market_stop_start")

    def user_market_iteminfo(self):
        res = self.safe_call("marketSyncItemInfo", {})
        self.log("user_market_iteminfo result=%s" % json_text(res, 2200))
        self.wait_market_services("user_market_iteminfo_services", 180, 10)
        self.wait_market_auto_running("user_market_iteminfo_auto", 120, 10)

    def user_market_clear_stock(self):
        res = self.safe_call("marketClearSystemStock", {})
        self.log("user_market_clear_stock result=%s" % json_text(res, 1800))
        self.market_enable_auto(max_concurrent=8)
        self.sample_with_event("user_market_clear_stock")

    def user_market_restock_once(self):
        res = self.safe_call("marketRestockOnce", {"market": "auction", "execute": True, "max_actions": 256, "max_concurrent": 4, "continue_on_error": True})
        self.log("user_market_restock_once result=%s" % json_text(res, 2200))
        self.sample_with_event("user_market_restock_once")

    def user_market_collect_once(self):
        res = self.safe_call("marketCollectOnce", {"market": "auction", "execute": True, "max_actions": 128, "max_concurrent": 4, "continue_on_error": True})
        self.log("user_market_collect_once result=%s" % json_text(res, 1800))
        self.sample_with_event("user_market_collect_once")

    def announcement_check(self):
        self.log("announcement_check begin")
        res = self.safe_call("systemAnnouncement", {})
        self.log("announcement_check result=%s" % json_text(res, 1600))
        self.sample_with_event("announcement_check")
        self.burst_sample("announcement_recover", self.scaled_seconds(30, 90), 10)

    def market_fault(self):
        self.log("market_fault begin")
        self.market_enable_auto(max_concurrent=8)
        self.market_fault_kill_services()
        self.market_fault_missing_iteminfo()
        self.market_fault_bad_iteminfo()
        self.market_fault_stale_db_iteminfo()
        self.market_fault_missing_catalog()
        self.market_fault_partial_catalog()
        self.market_fault_bad_config()
        self.market_enable_auto(max_concurrent=8)
        self.wait_market_services("market_fault_final_recover", 180, 10)
        self.burst_sample("market_fault_final", self.scaled_seconds(30, 90), 10)
        self.log("market_fault done")

    def market_startup_iteminfo_race(self):
        self.log("market_startup_iteminfo_race begin")
        self.sample_with_event("market_startup_race_before")
        self.shell("cd /root && (./stop >/tmp/vm_random_stop_market_race.log 2>&1 || true); sleep 12; ss -lntp | grep -E ':(10011|30303)' || true", 180)
        self.robot_restart_without_target("market_startup_race_robot_restart_game_down")
        res = self.safe_call("marketStatus", {})
        self.log("market_startup_iteminfo_race status_game_down=%s" % json_text(res, 1600))
        res = self.safe_call("marketSyncItemInfo", {})
        self.log("market_startup_iteminfo_race sync_iteminfo_game_down result=%s" % json_text(res, 2400))
        self.sample_with_event("market_startup_race_after_iteminfo")
        self.shell("cd /root && (./run >/tmp/vm_random_run_market_race.log 2>&1 || true); sleep 30; ss -lntp | grep -E ':(10011|30303|30603|30803)' || true", 240)
        self.wait_robot_api("market_startup_race_api", 90, 5)
        self.wait_market_services("market_startup_race_services", 240, 10)
        if not self.wait_market_auto_running("market_startup_race_auto", 180, 10):
            self.record_failure("market_startup_iteminfo_race_invariant", "market auto did not resume after iteminfo while game was down")
            self.market_enable_auto(max_concurrent=8)
        self.burst_sample("market_startup_race_recover", self.scaled_seconds(30, 90), 10)
        self.log("market_startup_iteminfo_race done")

    def market_enable_auto(self, max_concurrent=8):
        res = self.safe_call("marketConfigUpdate", {
            "auto_enabled": True,
            "interval_ms": 60000,
            "max_actions": 10000,
            "max_concurrent": max_concurrent,
            "continue_on_error": True,
            "markets": ["auction", "cera"],
        })
        self.log("market_enable_auto result=%s" % json_text(res, 1600))
        res = self.safe_call("marketStart", {})
        self.log("marketStart result=%s" % json_text(res, 1600))
        return res

    def market_status_result(self):
        res = self.safe_call("marketStatus", {})
        return (res.get("result") or {}) if isinstance(res, dict) else {}

    def market_services_ready(self, status=None):
        if status is None:
            status = self.market_status_result()
        services = status.get("services") or {}
        auction = services.get("auction") or {}
        point = services.get("point") or {}
        return bool(auction.get("status") == "ready" and auction.get("listening") and point.get("status") == "ready" and point.get("listening"))

    def market_auto_enabled(self, status=None):
        if status is None:
            status = self.market_status_result()
        auto = status.get("auto") or {}
        return bool(auto.get("enabled"))

    def wait_market_auto_running(self, event, timeout_sec=180, interval_sec=10):
        self.log("wait_market_auto_running start event=%s timeout=%s" % (event, timeout_sec))
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            status = self.market_status_result()
            last = status
            self.sample_with_event(event)
            if self.market_auto_enabled(status) and status.get("auto_running"):
                self.log("wait_market_auto_running ready event=%s" % event)
                return True
            time.sleep(interval_sec)
        self.log("wait_market_auto_running timeout event=%s status=%s" % (event, json_text(last, 1800)))
        return False

    def wait_market_services(self, event, timeout_sec=180, interval_sec=10):
        self.log("wait_market_services start event=%s timeout=%s" % (event, timeout_sec))
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            status = self.market_status_result()
            last = status
            self.sample_with_event(event)
            if self.market_services_ready(status):
                self.log("wait_market_services ready event=%s status=%s" % (event, json_text(status.get("services") or {}, 1400)))
                return True
            time.sleep(interval_sec)
        self.log("wait_market_services timeout event=%s status=%s" % (event, json_text((last or {}).get("services") or last, 1800)))
        return False

    def market_fault_kill_services(self):
        self.log("market_fault_kill_services begin")
        self.sample_with_event("market_kill_before")
        self.stop_market_services()
        self.sample_with_event("market_kill_down")
        self.market_enable_auto(max_concurrent=8)
        self.wait_market_services("market_kill_recover", 180, 10)

    def stop_market_services(self):
        script = "for p in $(pidof df_auction_r df_point_r 2>/dev/null); do kill -TERM $p || true; done; sleep 8; for p in $(pidof df_auction_r df_point_r 2>/dev/null); do kill -KILL $p || true; done; ss -lntp | grep -E ':(30603|30803)' || true; pgrep -af 'df_auction_r|df_point_r' || true"
        self.shell(script, 40)

    def market_fault_missing_iteminfo(self):
        self.log("market_fault_missing_iteminfo begin")
        backups = [
            self.backup_file("/home/neople/auction/iteminfo.dat"),
            self.backup_file("/home/neople/point/iteminfo.dat"),
        ]
        try:
            self.stop_market_services()
            self.shell("rm -f /home/neople/auction/iteminfo.dat /home/neople/point/iteminfo.dat; sleep 3", 30)
            self.market_enable_auto(max_concurrent=4)
            self.burst_sample("market_missing_iteminfo_down", 60, 10)
        finally:
            self.restore_file("/home/neople/auction/iteminfo.dat", backups[0])
            self.restore_file("/home/neople/point/iteminfo.dat", backups[1])
            self.market_enable_auto(max_concurrent=8)
            self.wait_market_services("market_missing_iteminfo_recover", 180, 10)

    def market_fault_bad_iteminfo(self):
        self.log("market_fault_bad_iteminfo begin")
        backups = [
            self.backup_file("/home/neople/auction/iteminfo.dat"),
            self.backup_file("/home/neople/point/iteminfo.dat"),
        ]
        try:
            bad = "printf 'bad iteminfo\\n1 broken row\\n999999999 0 x x x\\n' > /home/neople/auction/iteminfo.dat; cp -f /home/neople/auction/iteminfo.dat /home/neople/point/iteminfo.dat"
            self.stop_market_services()
            self.shell(bad + "; sleep 3", 30)
            self.market_enable_auto(max_concurrent=4)
            self.burst_sample("market_bad_iteminfo_down", 60, 10)
        finally:
            self.restore_file("/home/neople/auction/iteminfo.dat", backups[0])
            self.restore_file("/home/neople/point/iteminfo.dat", backups[1])
            self.market_enable_auto(max_concurrent=8)
            self.wait_market_services("market_bad_iteminfo_recover", 180, 10)

    def market_fault_stale_db_iteminfo(self):
        self.log("market_fault_stale_db_iteminfo begin")
        backups = [
            self.backup_file("/home/neople/auction/iteminfo.dat"),
            self.backup_file("/home/neople/point/iteminfo.dat"),
        ]
        try:
            self.market_enable_auto(max_concurrent=8)
            self.burst_sample("market_stale_before", self.scaled_seconds(20, 60), 10)
            bad = "printf '1 0 1 1 1 1 1 1 1 1 1 1 1 1 `bad` `bad` 1\\n' > /home/neople/auction/iteminfo.dat; cp -f /home/neople/auction/iteminfo.dat /home/neople/point/iteminfo.dat"
            self.stop_market_services()
            self.shell(bad + "; sleep 3", 30)
            self.market_enable_auto(max_concurrent=4)
            self.burst_sample("market_stale_db_iteminfo", self.scaled_seconds(30, 90), 10)
        finally:
            self.restore_file("/home/neople/auction/iteminfo.dat", backups[0])
            self.restore_file("/home/neople/point/iteminfo.dat", backups[1])
            self.safe_call("marketClearSystemStock", {})
            self.market_enable_auto(max_concurrent=8)
            self.wait_market_services("market_stale_db_iteminfo_recover", 180, 10)

    def market_fault_missing_catalog(self):
        self.log("market_fault_missing_catalog begin")
        paths = [
            "/root/config/pvf_equipment_catalog.json",
            "/root/config/pvf_stackable_catalog.json",
            "/root/config/pvf_iteminfo.dat",
        ]
        backups = [self.backup_file(path) for path in paths]
        try:
            self.shell("rm -f /root/config/pvf_equipment_catalog.json /root/config/pvf_stackable_catalog.json /root/config/pvf_iteminfo.dat", 20)
            self.sample_with_event("market_catalog_removed")
            res = self.safe_call("marketRestockOnce", {"market": "auction", "execute": True, "max_actions": 128, "max_concurrent": 4, "continue_on_error": True})
            self.log("market_missing_catalog restock result=%s" % json_text(res, 2200))
            self.burst_sample("market_missing_catalog_fallback", 60, 10)
        finally:
            for path, backup in zip(paths, backups):
                self.restore_file(path, backup)
            self.sample_with_event("market_catalog_restored")

    def market_fault_partial_catalog(self):
        self.log("market_fault_partial_catalog begin")
        paths = [
            "/root/config/pvf_equipment_catalog.json",
            "/root/config/pvf_stackable_catalog.json",
            "/root/config/pvf_iteminfo.dat",
        ]
        backups = [self.backup_file(path) for path in paths]
        try:
            self.shell("printf '[{\"id\":1,\"price\":1' > /root/config/pvf_equipment_catalog.json; printf '[{\"id\":2' > /root/config/pvf_stackable_catalog.json; printf '1 broken partial' > /root/config/pvf_iteminfo.dat", 20)
            self.sample_with_event("market_partial_catalog_written")
            res = self.safe_call("marketRestockOnce", {"market": "auction", "execute": True, "max_actions": 128, "max_concurrent": 4, "continue_on_error": True})
            self.log("market_partial_catalog restock result=%s" % json_text(res, 2200))
            self.burst_sample("market_partial_catalog", self.scaled_seconds(20, 60), 10)
        finally:
            for path, backup in zip(paths, backups):
                self.restore_file(path, backup)
            self.sample_with_event("market_partial_catalog_restored")

    def market_fault_bad_config(self):
        self.log("market_fault_bad_config begin")
        path = "/root/config/market_config.json"
        backup = self.backup_file(path)
        try:
            self.shell("printf '{bad market config\\n' > %s" % path, 10)
            self.sample_with_event("market_bad_config_written")
            self.robot_restart_without_target("market_bad_config_restart")
            self.burst_sample("market_bad_config_down", 40, 10)
        finally:
            self.restore_file(path, backup)
            self.robot_restart_without_target("market_bad_config_restore_restart")
            self.market_enable_auto(max_concurrent=8)
            self.wait_market_services("market_bad_config_recover", 180, 10)

    def cleanup_burst(self):
        self.log("cleanup_burst begin")
        for _ in range(3):
            self.random_cleanup()
            self.burst_sample("cleanup_burst", 20, 5)

    def market_button_flow(self):
        self.log("market_button_flow begin")
        self.market_enable_auto(max_concurrent=8)
        res = self.safe_call("marketClearSystemStock", {})
        self.log("market_button_flow clear_stock result=%s" % json_text(res, 2400))
        self.sample_with_event("market_clear_stock")
        self.market_enable_auto(max_concurrent=8)
        self.burst_sample("market_after_clear_stock", self.scaled_seconds(30, 90), 10)
        res = self.safe_call("marketSyncItemInfo", {})
        self.log("market_button_flow sync_iteminfo result=%s" % json_text(res, 2400))
        self.sample_with_event("market_sync_iteminfo")
        self.wait_market_services("market_sync_iteminfo_recover", 240, 10)
        self.market_enable_auto(max_concurrent=8)

    def pvf_file_fault(self):
        self.log("pvf_file_fault begin")
        candidates = ["/dp2/Script.pvf", "/home/neople/game/Script.pvf"]
        backups = []
        for path in candidates:
            backups.append((path, self.backup_file(path)))
        try:
            self.shell("for f in /dp2/Script.pvf /home/neople/game/Script.pvf; do [ -e \"$f\" ] && printf 'encrypted-or-broken-pvf' > \"$f\" || true; done", 30)
            self.sample_with_event("pvf_broken_written")
            res = self.safe_call("marketSyncItemInfo", {})
            self.log("pvf_file_fault sync_iteminfo result=%s" % json_text(res, 2200))
            self.burst_sample("pvf_file_fault", self.scaled_seconds(20, 60), 10)
        finally:
            for path, backup in backups:
                self.restore_file(path, backup)
            self.robot_restart_without_target("pvf_file_fault_restore_robot")
            self.market_enable_auto(max_concurrent=8)

    def db_stock_external_clear(self):
        self.log("db_stock_external_clear begin")
        dump = self.backup_market_database("before_db_stock_external_clear")
        self.sample_with_event("db_stock_clear_before")
        self.shell("mysql -ugame -puu5!^%jg -e \"DELETE FROM taiwan_cain_auction_gold.auction_main WHERE owner_id >= 90000001; DELETE FROM taiwan_cain_auction_cera.auction_main WHERE owner_id >= 90000001;\"", 60)
        self.sample_with_event("db_stock_clear_after")
        self.market_enable_auto(max_concurrent=8)
        self.burst_sample("db_stock_clear_recover", self.scaled_seconds(60, 180), 10)
        self.restore_market_database(dump, "after_db_stock_external_clear")
        self.market_enable_auto(max_concurrent=8)
        self.sample_with_event("db_stock_clear_restored")

    def db_schema_drift(self):
        self.log("db_schema_drift begin")
        self.backup_market_database("before_db_schema_drift")
        try:
            self.shell("mysql -ugame -puu5!^%jg -e \"ALTER TABLE taiwan_cain_auction_gold.auction_main ADD COLUMN vm_random_dummy INT NULL; ALTER TABLE taiwan_cain_auction_cera.auction_main ADD COLUMN vm_random_dummy INT NULL;\" || true", 120)
            self.sample_with_event("db_schema_drift_added")
            self.market_enable_auto(max_concurrent=4)
            self.burst_sample("db_schema_drift", self.scaled_seconds(20, 60), 10)
        finally:
            self.shell("mysql -ugame -puu5!^%jg -e \"ALTER TABLE taiwan_cain_auction_gold.auction_main DROP COLUMN vm_random_dummy; ALTER TABLE taiwan_cain_auction_cera.auction_main DROP COLUMN vm_random_dummy;\" || true", 120)
            self.sample_with_event("db_schema_drift_restored")

    def config_dir_fault(self):
        self.log("config_dir_fault begin")
        backup = "/root/config.vm_random_backup_%s" % int(time.time() * 1000)
        try:
            script = """
pids=$(ps -eo pid,args | awk '$2=="/root/robot" || ($2=="/root/robot" && $3=="--web-admin") {print $1}')
[ -z "$pids" ] || kill -TERM $pids || true
sleep 5
left=$(ps -eo pid,args | awk '$2=="/root/robot" || ($2=="/root/robot" && $3=="--web-admin") {print $1}')
[ -z "$left" ] || kill -KILL $left || true
cp -af /root/config %s 2>/dev/null || true
mkdir -p /root/config
find /root/config -mindepth 1 -maxdepth 1 -exec rm -rf -- {} + 2>/dev/null || true
printf '{broken config dir' > /root/config/market_config.json
""" % shell_quote(backup)
            self.shell(script, 120)
            self.sample_with_event("config_dir_fault_broken")
            self.robot_restart_without_target("config_dir_fault_restart")
            self.burst_sample("config_dir_fault", self.scaled_seconds(20, 60), 10)
        finally:
            script = """
pids=$(ps -eo pid,args | awk '$2=="/root/robot" || ($2=="/root/robot" && $3=="--web-admin") {print $1}')
[ -z "$pids" ] || kill -TERM $pids || true
sleep 5
left=$(ps -eo pid,args | awk '$2=="/root/robot" || ($2=="/root/robot" && $3=="--web-admin") {print $1}')
[ -z "$left" ] || kill -KILL $left || true
mkdir -p /root/config
find /root/config -mindepth 1 -maxdepth 1 -exec rm -rf -- {} + 2>/dev/null || true
if [ -d %s ]; then
  cp -af %s/. /root/config/ 2>/dev/null || true
  rm -rf %s
fi
""" % (shell_quote(backup), shell_quote(backup), shell_quote(backup))
            self.shell(script, 120)
            self.robot_restart_without_target("config_dir_fault_restore")
            self.market_enable_auto(max_concurrent=8)

    def web_api_fault(self):
        self.log("web_api_fault begin")
        self.sample_with_event("web_api_fault_before")
        self.shell("pkill -TERM -f '/root/robot --web-admin' || true; sleep 5; pgrep -af '/root/robot' || true; ss -lntp | grep -E ':(8111|8112)' || true", 30)
        for command in ("systemStatus", "autoStatus", "marketStatus", "databaseStatus"):
            res = self.safe_call(command, {})
            self.log("web_api_fault api command=%s result=%s" % (command, json_text(res, 1200)))
        self.robot_restart_without_target("web_api_fault_restart")
        self.burst_sample("web_api_fault_recover", self.scaled_seconds(30, 90), 10)

    def port_conflict_fault(self):
        self.log("port_conflict_fault begin")
        self.stop_market_services()
        cmd = "cat >/tmp/vm_random_port_conflict.py <<'PY'\nimport socket,time\ns=[]\nfor p in (30603,30803):\n    x=socket.socket(); x.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); x.bind(('0.0.0.0', p)); x.listen(1); s.append(x)\ntime.sleep(90)\nPY\nnohup python /tmp/vm_random_port_conflict.py >/tmp/vm_random_port_conflict.log 2>&1 &"
        self.shell(cmd, 5)
        self.sample_with_event("port_conflict_bound")
        self.market_enable_auto(max_concurrent=4)
        self.burst_sample("port_conflict_fault", self.scaled_seconds(30, 60), 10)
        self.shell("pkill -f /tmp/vm_random_port_conflict.py || true; rm -f /tmp/vm_random_port_conflict.py; sleep 3", 20)
        self.market_enable_auto(max_concurrent=8)
        self.wait_market_services("port_conflict_recover", 180, 10)

    def mysql_restart_fault(self):
        self.log("mysql_restart_fault begin")
        self.sample_with_event("mysql_restart_before")
        self.shell("(service mysql restart || service mysqld restart || /etc/init.d/mysqld restart || true); sleep 15; pgrep -af 'mysqld|mariadbd' || true", 180)
        self.wait_robot_api("mysql_restart_api", 120, 5)
        self.market_enable_auto(max_concurrent=8)
        self.burst_sample("mysql_restart_recover", self.scaled_seconds(30, 90), 10)

    def manual_collision(self):
        status = self.safe_call("robotsStatus", {"count": 100})
        rows = (((status or {}).get("result") or {}).get("robots") or [])
        uids = [int(r.get("uid") or 0) for r in rows if int(r.get("uid") or 0) > 0][:8]
        if not uids:
            self.log("manual_collision skipped no uids")
            return
        self.log("manual_collision uids=%s" % uids)
        calls = [
            ("robotsMove", {"uids": uids[:6]}),
            ("robotsShoutLocal", {"uids": uids[:6]}),
            ("robotsShoutWorld", {"uids": uids[:3]}),
            ("robotsStoreAsync", {"uids": uids[3:6]}),
            ("robotsLogoutAsync", {"uids": uids[6:8]}),
        ]
        for command, payload in calls:
            res = self.safe_call(command, payload)
            self.log("manual_collision command=%s payload=%s result=%s" % (command, payload, json_text(res, 1400)))
            self.sample_with_event("manual_collision:%s" % command)
            time.sleep(3)
        self.burst_sample("manual_collision_recover", 90, 10)

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
        time.sleep(self.scaled_seconds(45, 120))
        self.log("game_port_fault restore /root/run")
        self.shell("cd /root && (./run >/tmp/vm_random_run_game.log 2>&1 || true); sleep 30; ss -lntp | grep -E ':(10011|30303)' || true; pgrep -af 'df_game_r|df_monitor_r' || true", 240)
        self.sample_with_event("game_port_fault_restore")
        self.burst_sample("game_port_fault_recover", self.scaled_seconds(60, 120), 10)

    def backup_file(self, path):
        backup = "%s.vm_random_backup_%s" % (path, int(time.time() * 1000))
        script = "[ -e '%s' ] && cp -af '%s' '%s' && echo '%s' || true" % (path, path, backup, backup)
        out = self.shell(script, 20)
        if backup in out:
            self.log("backup_file path=%s backup=%s" % (path, backup))
            return backup
        self.log("backup_file missing path=%s" % path)
        return ""

    def restore_file(self, path, backup):
        if not backup:
            self.log("restore_file skipped path=%s backup_empty" % path)
            return
        script = "[ -e '%s' ] && cp -af '%s' '%s' && rm -f '%s' && echo RESTORED || echo MISSING_BACKUP" % (backup, backup, path, backup)
        out = self.shell(script, 20)
        self.log("restore_file path=%s backup=%s output=%s" % (path, backup, out[:400]))

    def backup_market_database(self, label):
        path = os.path.join(self.baseline_dir, "%s_market_robot_stock.sql" % label)
        latest = os.path.join(self.baseline_dir, "market_robot_stock.sql")
        command = """
OUT=%s
{
  echo 'DELETE FROM taiwan_cain_auction_gold.auction_main WHERE owner_id >= 90000001;';
  echo 'DELETE FROM taiwan_cain_auction_cera.auction_main WHERE owner_id >= 90000001;';
  echo 'USE taiwan_cain_auction_gold;';
  mysqldump -ugame -puu5!^%%jg --skip-triggers --no-create-info --replace --where='owner_id >= 90000001' taiwan_cain_auction_gold auction_main 2>/dev/null || true;
  echo 'USE taiwan_cain_auction_cera;';
  mysqldump -ugame -puu5!^%%jg --skip-triggers --no-create-info --replace --where='owner_id >= 90000001' taiwan_cain_auction_cera auction_main 2>/dev/null || true;
} > "$OUT"
cp -f "$OUT" %s
ls -l "$OUT" %s
""" % (shell_quote(path), shell_quote(latest), shell_quote(latest))
        out = self.shell(command, 120)
        self.log("backup_market_database label=%s path=%s output=%s" % (label, path, out[:800]))
        return path

    def restore_market_database(self, dump_path, label):
        if not dump_path:
            self.log("restore_market_database skipped label=%s empty_dump" % label)
            return
        command = "[ -s %s ] && mysql -ugame -puu5!^%%jg < %s && echo DB_RESTORED || echo DB_BACKUP_MISSING" % (shell_quote(dump_path), shell_quote(dump_path))
        out = self.shell(command, 120)
        self.log("restore_market_database label=%s dump=%s output=%s" % (label, dump_path, out[:800]))

    def robot_restart_without_target(self, label):
        self.log("robot_restart_without_target begin label=%s" % label)
        self.sample_with_event(label + "_stop")
        script = r"""
pids=$(ps -eo pid,args | awk '$2=="/root/robot" || ($2=="/root/robot" && $3=="--web-admin") {print $1}')
[ -z "$pids" ] || kill -TERM $pids || true
sleep 8
left=$(ps -eo pid,args | awk '$2=="/root/robot" || ($2=="/root/robot" && $3=="--web-admin") {print $1}')
[ -z "$left" ] || kill -KILL $left || true
nohup /root/robot > /root/robot_stdout.log 2>&1 &
sleep 12
pgrep -af '/root/robot|df_game_r|df_monitor_r|df_auction_r|df_point_r' || true
ss -lntp | grep -E ':(8111|8112|10011|30303|30603|30803)' || true
"""
        self.shell(script, 120)
        self.sample_with_event(label + "_started")

    def robot_restart(self):
        self.log("robot_restart begin")
        self.robot_restart_without_target("robot_restart")
        time.sleep(20)
        self.set_target(self.args.target_max)
        self.burst_sample("robot_restart_recover", self.scaled_seconds(60, 120), 10)

    def custom_key_test(self):
        self.log("custom_key_test begin")
        self.sample_with_event("custom_key_before")

        game_dir = "/home/neople/game"
        backup_priv = game_dir + "/privatekey.pem.bak"
        backup_pub = game_dir + "/publickey.pem.bak"

        script = """
GAME_DIR="%s"
# Backup original keys
cp "$GAME_DIR"/privatekey.pem "$GAME_DIR"/privatekey.pem.bak 2>/dev/null || true
cp "$GAME_DIR"/publickey.pem "$GAME_DIR"/publickey.pem.bak 2>/dev/null || true
# Generate custom RSA 2048 key pair
openssl genpkey -algorithm RSA -out "$GAME_DIR"/privatekey.pem -pkeyopt rsa_keygen_bits:2048 2>/dev/null
openssl rsa -pubout -in "$GAME_DIR"/privatekey.pem -out "$GAME_DIR"/publickey.pem 2>/dev/null
echo "KEYS_REPLACED"
""" % game_dir
        out = self.shell(script, 30)
        self.log("custom_key_test new_keys_generated output=%s" % out[:500])

        self.robot_restart()
        time.sleep(10)

        st = self.safe_call("keypairStatus")
        self.log("custom_key_test keypair_status=%s" % json_text(st, 2000))
        result = st.get("result") or {}
        is_user_key = result.get("KeyState") == "user"
        self.log("custom_key_test is_user_key=%s" % is_user_key)
        self.sample_with_event("custom_key_user_key")

        # Test smoke actions with user key
        status = self.safe_call("robotsStatus", {"count": 20})
        rows = (((status or {}).get("result") or {}).get("robots") or [])
        uids = [int(r.get("uid") or 0) for r in rows if int(r.get("uid") or 0) > 0][:2]
        if uids:
            res = self.safe_call("robotsShoutWorld", {"uids": uids[:1]})
            self.log("custom_key_test user_key_shout uids=%s result=%s" % (uids, json_text(res, 800)))
            res = self.safe_call("robotsMove", {"uids": uids[:2]})
            self.log("custom_key_test user_key_move uids=%s result=%s" % (uids[:2], json_text(res, 800)))
            self.sample_with_event("custom_key_user_ops")

        # Release default key to recover
        rel = self.safe_call("keypairReleaseDefault")
        self.log("custom_key_test release_default result=%s" % json_text(rel, 2000))
        time.sleep(5)
        st2 = self.safe_call("keypairStatus")
        result2 = st2.get("result") or {}
        is_default = result2.get("KeyState") == "default" or result2.get("UsingDefault")
        self.log("custom_key_test after_release is_default=%s state=%s" % (is_default, result2.get("KeyState")))
        self.sample_with_event("custom_key_default_restored")

        # Restore backups for next test run
        restore = """
GAME_DIR="%s"
[ -f "$GAME_DIR"/privatekey.pem.bak ] && mv "$GAME_DIR"/privatekey.pem.bak "$GAME_DIR"/privatekey.pem
[ -f "$GAME_DIR"/publickey.pem.bak ] && mv "$GAME_DIR"/publickey.pem.bak "$GAME_DIR"/publickey.pem
echo "KEYS_RESTORED"
""" % game_dir
        self.shell(restore, 10)
        self.log("custom_key_test backups_restored")
        self.log("custom_key_test done is_user=%s recovered=%s" % (is_user_key, is_default))

    def sample(self):
        row = self.sample_row()
        self.writerow(row)
        self.log(
            "sample target=%s actors=%s leased=%s running=%s connecting=%s mode=%s market_auto=%s auction=%s/%s cera=%s/%s load=%s/%s/%s top=%s hits=%s api_error=%s"
            % (
                row.get("target"),
                row.get("actors"),
                row.get("leased"),
                row.get("running"),
                row.get("connecting"),
                row.get("scheduler_mode"),
                row.get("market_auto"),
                row.get("market_auction_records"),
                row.get("market_auction_kinds"),
                row.get("market_cera_records"),
                row.get("market_cera_kinds"),
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
            "sample event=%s target=%s running=%s mode=%s market_auto=%s auction=%s/%s load=%s/%s/%s robot_cpu=%s df_game_cpu=%s goroutines=%s"
            % (
                event,
                row.get("target"),
                row.get("running"),
                row.get("scheduler_mode"),
                row.get("market_auto"),
                row.get("market_auction_records"),
                row.get("market_auction_kinds"),
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
        row["robot_pid_cpu"] = self.proc_pid_cpu("/root/robot")
        row["df_game_cpu"] = self.proc_pid_cpu("df_game_r")
        row["auction_cpu"] = self.proc_pid_cpu("df_auction_r")
        row["point_cpu"] = self.proc_pid_cpu("df_point_r")
        self.fill_database_row(row)
        self.fill_market_row(row)
        self.fill_tcp_row(row)
        self.fill_port_row(row)
        row["fd_robot"] = self.robot_fd_count()
        load1, load5, load15 = self.load_average()
        row["load1"], row["load5"], row["load15"] = load1, load5, load15
        row["top_cpu"] = self.top_cpu()
        row["keyword_hits"] = self.keyword_hits()
        return row

    def fill_database_row(self, row):
        try:
            db = (self.api.call("databaseStatus").get("result") or {})
            row["db_open"] = db.get("open_conns")
            row["db_in_use"] = db.get("in_use")
            row["db_idle"] = db.get("idle")
            row["db_latency_ms"] = db.get("latency_ms")
        except Exception as exc:
            if not row.get("api_error"):
                row["api_error"] = "databaseStatus:%r" % (exc,)

    def fill_market_row(self, row):
        try:
            market = (self.api.call("marketStatus").get("result") or {})
            row["market_auto"] = market.get("auto_running")
            last = market.get("last_job") or {}
            row["market_last_status"] = last.get("status")
            row["market_last_error"] = (last.get("error") or market.get("db_init_error") or "")[:160]
            services = market.get("services") or {}
            auction = services.get("auction") or {}
            point = services.get("point") or {}
            row["market_auction_status"] = auction.get("status")
            row["market_auction_open"] = int(bool(auction.get("listening")))
            row["market_point_status"] = point.get("status")
            row["market_point_open"] = int(bool(point.get("listening")))
            row["market_services_ready"] = int(bool(auction.get("status") == "ready" and auction.get("listening") and point.get("status") == "ready" and point.get("listening")))
            counts = self.market_db_counts()
            row["market_auction_records"] = counts.get("auction_records", "")
            row["market_auction_kinds"] = counts.get("auction_kinds", "")
            row["market_cera_records"] = counts.get("cera_records", "")
            row["market_cera_kinds"] = counts.get("cera_kinds", "")
        except Exception as exc:
            if not row.get("api_error"):
                row["api_error"] = "marketStatus:%r" % (exc,)

    def fill_tcp_row(self, row):
        try:
            out = subprocess.check_output("ss -ant", shell=True)
            if not isinstance(out, str):
                out = out.decode("utf-8", "replace")
            states = {}
            port_counts = {"8111": 0, "10011": 0, "30603": 0, "30803": 0}
            for line in out.splitlines()[1:]:
                parts = line.split()
                if not parts:
                    continue
                state = parts[0]
                states[state] = states.get(state, 0) + 1
                for port in port_counts:
                    if (":" + port) in line and state == "ESTAB":
                        port_counts[port] += 1
            row["tcp_estab"] = states.get("ESTAB", 0)
            row["tcp_time_wait"] = states.get("TIME-WAIT", 0)
            row["tcp_close_wait"] = states.get("CLOSE-WAIT", 0)
            row["tcp_8111_estab"] = port_counts["8111"]
            row["tcp_10011_estab"] = port_counts["10011"]
            row["tcp_30603_estab"] = port_counts["30603"]
            row["tcp_30803_estab"] = port_counts["30803"]
        except Exception as exc:
            if not row.get("api_error"):
                row["api_error"] = "tcp:%r" % (exc,)

    def fill_port_row(self, row):
        try:
            out = subprocess.check_output("ss -ltn", shell=True)
            if not isinstance(out, str):
                out = out.decode("utf-8", "replace")
            row["port_10011"] = int(":10011" in out)
            row["port_30603"] = int(":30603" in out)
            row["port_30803"] = int(":30803" in out)
        except Exception:
            pass

    def robot_fd_count(self):
        try:
            out = subprocess.check_output("pgrep -f '^/root/robot$' | head -1", shell=True)
            if not isinstance(out, str):
                out = out.decode("utf-8", "replace")
            pid = out.strip()
            if not pid:
                return ""
            return len(os.listdir("/proc/%s/fd" % pid))
        except Exception:
            return ""

    def collect_logs(self, label):
        path = os.path.join(self.out_dir, "collected_logs.log")
        command = """
echo '===== %s %s uptime ====='
date
uptime
echo '===== ps top ====='
ps -eo pid,ppid,pcpu,pmem,nlwp,comm,args --sort=-pcpu | head -n 25
echo '===== tcp states ====='
ss -ant | awk 'NR>1 {c[$1]++} END {for (k in c) print k,c[k]}'
echo '===== tcp hot ports ====='
ss -ant | grep -E ':(8111|8112|10011|30603|30803)' | head -n 120 || true
echo '===== fds ====='
for p in $(pgrep -f '^/root/robot$|df_game_r|df_auction_r|df_point_r' 2>/dev/null); do echo "$p $(ps -p $p -o comm=) fds=$(ls /proc/$p/fd 2>/dev/null | wc -l)"; done
echo '===== robot log filtered ====='
tail -n %s /root/config/log_robot 2>/dev/null | grep -a -E '%s' | tail -n 200 || true
echo '===== market log filtered ====='
tail -n %s /root/config/market_log.jsonl 2>/dev/null | grep -a -E 'market_service|job_end|auto_run|cannot assign requested address|too many open files|connection reset' | tail -n 160 || true
echo '===== market service logs ====='
tail -n 80 /root/config/market_auction_service.log 2>/dev/null || true
tail -n 80 /root/config/market_point_service.log 2>/dev/null || true
echo '===== market files ====='
ls -l /root/config/market_config.json /root/config/pvf_*catalog.json /root/config/pvf_iteminfo.dat /home/neople/auction/iteminfo.dat /home/neople/point/iteminfo.dat 2>/dev/null || true
echo '===== web stdout filtered ====='
tail -n %s /root/robot_stdout.log 2>/dev/null | grep -a -E '%s|request pid|auth rejected|web admin exited' | tail -n 120 || true
""" % (
            label,
            now_text(),
            self.args.log_tail_lines,
            "|".join(re.escape(k) for k in KEYWORDS),
            self.args.log_tail_lines,
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
                    self.log("shell_timeout command=%s output=%s" % (safe_text(command)[:160], safe_text(text)[:2000]))
                return text
            time.sleep(1)
        out = proc.communicate()[0] or b""
        text = out.decode("utf-8", "replace") if not isinstance(out, str) else out
        if log_output:
            self.log("shell command=%s output=%s" % (safe_text(command)[:160], safe_text(text)[:2000]))
        return text

    def write_summary(self):
        failures = [item for item in self.results if item.get("error") or not item.get("recovered")]
        summary = {
            "started_at": datetime.datetime.fromtimestamp(self.started).isoformat(),
            "finished_at": datetime.datetime.now().isoformat(),
            "duration_sec": int(time.time() - self.started),
            "deleted_total": self.deleted_total,
            "out_dir": self.out_dir,
            "args": vars(self.args),
            "events": self.results,
            "failure_count": len(failures),
        }
        path = os.path.join(self.out_dir, "summary.json")
        raw = json.dumps(summary, ensure_ascii=False, indent=2)
        if not isinstance(raw, type(u"")):
            raw = raw.decode("utf-8")
        io.open(path, "w", encoding="utf-8").write(raw)
        self.log("summary %s" % json_text(summary, 2000))

    def write_report(self):
        failures = [item for item in self.results if item.get("error") or not item.get("recovered")]
        failures_path = os.path.join(self.out_dir, "failures.json")
        raw = json.dumps(failures, ensure_ascii=False, indent=2)
        if not isinstance(raw, type(u"")):
            raw = raw.decode("utf-8")
        io.open(failures_path, "w", encoding="utf-8").write(raw)

        lines = []
        lines.append("# Stability Report")
        lines.append("")
        lines.append("- started_at: %s" % datetime.datetime.fromtimestamp(self.started).isoformat())
        lines.append("- finished_at: %s" % datetime.datetime.now().isoformat())
        lines.append("- duration_sec: %s" % int(time.time() - self.started))
        lines.append("- events: %s" % len(self.results))
        lines.append("- failures: %s" % len(failures))
        lines.append("")
        lines.append("## Events")
        lines.append("")
        for item in self.results:
            status = "FAIL" if item.get("error") or not item.get("recovered") else "OK"
            lines.append("- %s %s duration=%ss recovered=%s error=%s" % (
                status,
                item.get("name"),
                item.get("duration_sec"),
                item.get("recovered"),
                item.get("error") or "",
            ))
            lines.append("  before: %s" % item.get("before"))
            lines.append("  after: %s" % item.get("after"))
        lines.append("")
        lines.append("## Failure Details")
        lines.append("")
        if failures:
            for item in failures:
                lines.append("- %s recovered=%s error=%s" % (item.get("name"), item.get("recovered"), item.get("error") or ""))
        else:
            lines.append("No failed scenario events.")
        report_path = os.path.join(self.out_dir, "report.md")
        io.open(report_path, "w", encoding="utf-8").write(u"\n".join(lines) + u"\n")
        self.log("write_report report=%s failures=%s" % (report_path, failures_path))


def parse_args():
    parser = argparse.ArgumentParser(description="VM-local random stability pressure script")
    parser.add_argument("--hours", type=float, default=1.0)
    parser.add_argument("--robot-host", default="127.0.0.1")
    parser.add_argument("--robot-port", type=int, default=8111)
    parser.add_argument("--api-timeout", type=float, default=20.0)
    parser.add_argument("--out-dir", default="")
    parser.add_argument("--sample-interval", type=int, default=10)
    parser.add_argument("--log-snapshot-interval", type=int, default=5 * 60)
    parser.add_argument("--target-min", type=int, default=100)
    parser.add_argument("--target-max", type=int, default=600)
    parser.add_argument("--target-min-interval", type=int, default=20 * 60)
    parser.add_argument("--target-max-interval", type=int, default=40 * 60)
    parser.add_argument("--cleanup-min-interval", type=int, default=30 * 60)
    parser.add_argument("--cleanup-max-interval", type=int, default=45 * 60)
    parser.add_argument("--user-interleave-min-interval", type=int, default=90)
    parser.add_argument("--user-interleave-max-interval", type=int, default=180)
    parser.add_argument("--market-zero-grace", type=int, default=180)
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
    if args.user_interleave_min_interval > args.user_interleave_max_interval:
        args.user_interleave_min_interval, args.user_interleave_max_interval = args.user_interleave_max_interval, args.user_interleave_min_interval
    StabilityRun(args).run()
    return 0


if __name__ == "__main__":
    sys.exit(main())
