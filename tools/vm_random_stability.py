#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""
VM-local random stability pressure for the robot system.

Compatible with the VM's default Python 2.7 and modern Python 3.

Default full scenario:
- run one full shuffled round by default
- pass a round count and optional deadline, for example: `5`, `1 6h`, `3 8h`
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
import glob
import io
import json
import os
import random
import re
import shutil
import socket
import subprocess
import sys
import time

try:
    import cookielib
    import urllib
    import urllib2
except ImportError:
    import http.cookiejar as cookielib
    import urllib.parse as urllib
    import urllib.request as urllib2

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
    "PARTY_",
    "PARTY_RELAY",
    "PARTY_DUNGEON",
    "PARTY_DUNGEON_SKILL",
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
    "tcp_7200_estab",
    "tcp_30603_estab",
    "tcp_30803_estab",
    "fd_robot",
    "port_10011",
    "port_30303",
    "port_7200",
    "port_30603",
    "port_30803",
    "party_log_hits",
    "party_error_hits",
    "party_relay_error_hits",
    "party_tqos_exhausted_hits",
    "party_route_degraded_hits",
    "party_route_recovery_hits",
    "party_route_recovered_hits",
    "party_relay_connected_hits",
    "party_probe_cycle_hits",
    "party_peer_ready_hits",
    "party_self_id_refresh_hits",
    "party_udp_recycle_hits",
    "party_supervisor_panic_hits",
    "party_skill_hits",
    "party_skill_error_hits",
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
    "market_auction_candidates",
    "market_auction_special_candidates",
    "market_auction_special_records",
    "market_auction_high_addinfo",
    "market_auction_creature_records",
    "market_creature_instances",
    "market_creature_orphans",
    "market_auction_queue_normal",
    "market_auction_queue_special",
    "market_auction_queue_rejected",
    "market_auction_stagnant",
    "market_auction_policy",
    "market_auction_policy_reason",
    "market_auction_health",
    "market_auction_completion",
    "market_auction_failure_rounds",
    "market_auction_last_job",
    "market_auction_last_plan",
    "market_auction_last_results",
    "market_auction_last_failed",
    "market_cera_records",
    "market_cera_kinds",
    "market_cera_policy",
    "market_cera_policy_reason",
    "market_cera_health",
    "market_cera_completion",
    "market_cera_last_job",
    "market_cera_last_plan",
    "market_cera_last_results",
    "market_cera_last_failed",
    "load1",
    "load5",
    "load15",
    "top_cpu",
    "keyword_hits",
    "api_error",
    "event",
]

CONFIG_BACKUP_EXCLUDES = (
    "log_robot*",
    "market_log.jsonl*",
    "market_*_service.log*",
    "*.rotate.tmp",
    "*.trim.tmp",
)

STABILITY_OUTPUT_GLOB = "/root/robot_stability_*"
STABILITY_OUTPUT_KEEP = 5
CONFIG_FAULT_BACKUP_GLOBS = (
    "/root/config.vm_random_backup_*",
    "/root/config/*.vm_random_backup_*",
    "/dp2/Script.pvf.vm_random_backup_*",
    "/home/neople/game/Script.pvf.vm_random_backup_*",
    "/home/neople/auction/iteminfo.dat.vm_random_backup_*",
    "/home/neople/point/iteminfo.dat.vm_random_backup_*",
)
CONFIG_FAULT_BACKUP_KEEP = 5
CONFIG_FAULT_BACKUP_NAME_RE = re.compile(r"^(?:config\.vm_random_backup_|.+\.vm_random_backup_)\d+$")
CONFIG_FAULT_BACKUP_TEMP_RE = re.compile(r"^config\.vm_random_backup_\d+\.(?:tar\.)?tmp\.\d+$")
STABILITY_OUTPUT_NAME_RE = re.compile(r"^robot_stability_\d{8}-\d{6}$")
DEFAULT_ARTIFACT_MAX_MB = 512


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


def filtered_config_backup_script(source, destination):
    exclude_patterns = []
    for pattern in CONFIG_BACKUP_EXCLUDES:
        exclude_patterns.append(pattern)
        exclude_patterns.append("*/" + pattern)
    exclude_args = " ".join("--exclude=%s" % shell_quote(pattern) for pattern in exclude_patterns)
    return """
SOURCE=%(source)s
DESTINATION=%(destination)s
TMP="${DESTINATION}.tmp.$$"
ARCHIVE="${DESTINATION}.tar.tmp.$$"
rm -rf -- "$TMP"
rm -f -- "$ARCHIVE"
if [ ! -d "$SOURCE" ]; then
  echo CONFIG_BACKUP_FAILED source_missing
  exit 1
fi
if ! (cd "$SOURCE" && tar %(exclude_args)s -cf "$ARCHIVE" .); then
  rm -rf -- "$TMP"
  rm -f -- "$ARCHIVE"
  echo CONFIG_BACKUP_FAILED archive
  exit 1
fi
mkdir -p "$TMP"
if ! tar -xf "$ARCHIVE" -C "$TMP" || [ ! -s "$TMP/config.ini" ]; then
  rm -rf -- "$TMP"
  rm -f -- "$ARCHIVE"
  echo CONFIG_BACKUP_FAILED verify
  exit 1
fi
rm -rf -- "$DESTINATION"
mv "$TMP" "$DESTINATION"
rm -f -- "$ARCHIVE"
echo CONFIG_BACKUP_OK
""" % {
        "source": shell_quote(source),
        "destination": shell_quote(destination),
        "exclude_args": exclude_args,
    }


def sanitize_name(value):
    return re.sub(r"[^A-Za-z0-9_.-]+", "_", safe_text(value)).strip("_") or "snapshot"


def backup_source_key(path):
    marker = ".vm_random_backup_"
    name = os.path.basename(path)
    index = name.rfind(marker)
    if index < 0:
        return path
    return os.path.join(os.path.dirname(path), name[:index])


def path_size(path):
    try:
        return os.path.getsize(path)
    except OSError:
        return 0


def to_int(value, default=0):
    try:
        return int(value)
    except Exception:
        return default


def parse_time_limit(value):
    if value is None or safe_text(value).strip() == "":
        return 0
    text = safe_text(value).strip().lower()
    match = re.match(r"^(\d+(?:\.\d+)?)([smhd]?)$", text)
    if match:
        amount = float(match.group(1))
        unit = match.group(2) or "s"
        factors = {"s": 1, "m": 60, "h": 3600, "d": 86400}
        return int(amount * factors[unit])
    for fmt in ("%Y-%m-%dT%H:%M:%S", "%Y-%m-%d %H:%M:%S", "%Y-%m-%dT%H:%M", "%Y-%m-%d %H:%M"):
        try:
            target = datetime.datetime.strptime(text, fmt)
            return max(0, int((target - datetime.datetime.now()).total_seconds()))
        except Exception:
            pass
    raise ValueError("invalid time limit %r; use seconds, 30m, 6h, or YYYY-MM-DDTHH:MM:SS" % value)


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
        self.time_limit_sec = parse_time_limit(args.time_limit)
        self.deadline_at = self.started + self.time_limit_sec if self.time_limit_sec > 0 else 0
        self.total_sec = max(self.time_limit_sec or 3600, 600)
        self.artifact_max_bytes = max(0, args.artifact_max_mb) * 1024 * 1024
        self.artifact_limit_reported = False
        self.baseline_dir = os.path.join(self.out_dir, "baseline")
        self.snapshot_dir = os.path.join(self.out_dir, "snapshots")
        if not os.path.isdir(self.snapshot_dir):
            os.makedirs(self.snapshot_dir)
        self.results = []
        self.round_orders = []
        self.market_auto_stopped_since = 0
        self.market_zero_since = 0
        self.market_zero_last_seen = 0
        self.last_invariant_failure = {}
        self.ports = self.read_ports()
        self.web_password = self.read_web_password()
        self.web_opener = None
        self.api = RobotAPI(args.robot_host, self.port("RobotAPI"), args.api_timeout)

    def read_ports(self):
        ports = {
            "RobotAPI": int(self.args.robot_port or 8111),
            "Web": 8112,
            "Game": 10011,
            "Monitor": 30303,
            "Auction": 30803,
            "Point": 30603,
            "Relay": 7200,
            "PartyRoute0": 5063,
        }
        path = "/root/config/config.ini"
        try:
            section = ""
            for line in io.open(path, "r", encoding="utf-8"):
                text = safe_text(line).strip()
                if not text or text.startswith("#") or text.startswith(";"):
                    continue
                if text.startswith("[") and "]" in text:
                    section = text[1:text.index("]")].strip()
                    continue
                if section != "Ports" or "=" not in text:
                    continue
                key, value = text.split("=", 1)
                key = key.strip()
                if key in ports:
                    port = int(value.strip())
                    if port > 0 and port <= 65535:
                        ports[key] = port
        except Exception as exc:
            self.log("read_ports fallback path=%s err=%r ports=%s" % (path, exc, ports))
        self.args.robot_port = ports.get("RobotAPI", self.args.robot_port)
        return ports

    def port(self, name):
        return int(self.ports.get(name) or 0)

    def port_text(self, name):
        return str(self.port(name))

    def port_regex(self, names):
        values = []
        for name in names:
            port = self.port(name)
            if port > 0:
                values.append(str(port))
        return "|".join(values)

    def read_web_password(self):
        path = "/root/config/config.ini"
        password = "twadmin"
        try:
            section = ""
            for line in io.open(path, "r", encoding="utf-8"):
                text = safe_text(line).strip()
                if not text or text.startswith("#") or text.startswith(";"):
                    continue
                if text.startswith("[") and "]" in text:
                    section = text[1:text.index("]")].strip()
                    continue
                if section == "Web" and "=" in text:
                    key, value = text.split("=", 1)
                    if key.strip() == "WebPassword" and value.strip():
                        password = value.strip()
        except Exception as exc:
            self.log("read_web_password fallback path=%s err=%r" % (path, exc))
        return password

    def web_base_url(self):
        return "http://127.0.0.1:%s" % self.port_text("Web")

    def ensure_web_login(self):
        if self.web_opener is not None:
            return self.web_opener
        jar = cookielib.CookieJar()
        opener = urllib2.build_opener(urllib2.HTTPCookieProcessor(jar))
        data = urllib.urlencode({"password": self.web_password}).encode("utf-8")
        req = urllib2.Request(self.web_base_url() + "/login", data=data)
        try:
            opener.open(req, timeout=self.args.api_timeout).read()
        except Exception as exc:
            raise RuntimeError("web login failed: %r" % (exc,))
        self.web_opener = opener
        return opener

    def web_json(self, path, payload=None):
        opener = self.ensure_web_login()
        url = self.web_base_url() + path
        data = None
        headers = {}
        if payload is not None:
            data = json.dumps(payload, ensure_ascii=True, separators=(",", ":")).encode("utf-8")
            headers["Content-Type"] = "application/json"
        req = urllib2.Request(url, data=data, headers=headers)
        try:
            raw = opener.open(req, timeout=self.args.api_timeout).read()
        except Exception as exc:
            self.web_opener = None
            raise RuntimeError("web request failed path=%s err=%r" % (path, exc))
        if isinstance(raw, bytes):
            raw = raw.decode("utf-8", "replace")
        try:
            return json.loads(raw)
        except Exception as exc:
            raise RuntimeError("web json failed path=%s err=%r raw=%s" % (path, exc, raw[:1000]))

    def log(self, message):
        line = u"[%s] %s" % (now_text(), safe_text(message))
        print(line.encode("utf-8") if sys.version_info[0] < 3 else line)
        self.events.write(line + u"\n")

    def run(self):
        seed = self.args.seed or int(time.time() * 1000000)
        random.seed(seed)
        self.args.seed = seed
        self.log("start out_dir=%s args=%s seed=%s min_rounds=%s time_limit_sec=%s" % (self.out_dir, vars(self.args), seed, self.args.rounds, self.time_limit_sec))
        self.cleanup_stale_artifacts()
        if not self.prepare_baseline():
            self.log("run aborted: baseline config backup failed")
            self.events.close()
            self.samples_file.close()
            return
        self.ensure_auto()
        next_target = time.time() + random.randint(self.args.target_min_interval, self.args.target_max_interval)
        next_cleanup = time.time() + random.randint(self.args.cleanup_min_interval, self.args.cleanup_max_interval)
        next_user_interleave = time.time() + random.randint(self.args.user_interleave_min_interval, self.args.user_interleave_max_interval)
        next_log_snapshot = time.time()
        next_sample = 0
        next_invariant = 0
        scenarios = self.scenario_events()
        round_no = 0
        stop_reason = ""
        try:
            while True:
                if self.artifact_budget_exceeded():
                    stop_reason = "artifact_budget_reached"
                    break
                round_no += 1
                round_scenarios = list(scenarios)
                random.shuffle(round_scenarios)
                order = [item["name"] for item in round_scenarios]
                self.round_orders.append({"round": round_no, "order": order})
                self.log("scenario round start round=%s order=%s" % (round_no, ",".join(order)))
                for event in round_scenarios:
                    now = time.time()
                    if now >= next_sample:
                        self.sample()
                        next_sample = now + self.args.sample_interval
                    if now >= next_log_snapshot:
                        self.collect_logs("periodic")
                        next_log_snapshot = now + self.args.log_snapshot_interval
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
                    self.run_event(event, round_no)
                    self.write_report()
                    if self.artifact_budget_exceeded():
                        stop_reason = "artifact_budget_reached"
                        self.log("scenario stop after scene round=%s event=%s reason=%s" % (round_no, event["name"], stop_reason))
                        raise StopIteration
                    if self.should_stop_after_scene(round_no):
                        stop_reason = self.stop_reason(round_no)
                        self.log("scenario stop after scene round=%s event=%s reason=%s" % (round_no, event["name"], stop_reason))
                        raise StopIteration
                self.log("scenario round done round=%s" % round_no)
                self.write_report()
                if self.should_stop_after_round(round_no):
                    stop_reason = self.stop_reason(round_no)
                    self.log("scenario stop after round=%s reason=%s" % (round_no, stop_reason))
                    break
                now = time.time()
                if now >= next_sample:
                    self.sample()
                    next_sample = now + self.args.sample_interval
                if now >= next_log_snapshot:
                    self.collect_logs("periodic")
                    next_log_snapshot = now + self.args.log_snapshot_interval
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
        except StopIteration:
            pass
        except KeyboardInterrupt:
            self.log("interrupted by user")
        finally:
            if not stop_reason:
                stop_reason = self.stop_reason(round_no)
            self.log("run finishing rounds=%s stop_reason=%s" % (round_no, stop_reason))
            if not self.artifact_budget_exceeded():
                self.collect_logs("before_final_recover")
            self.final_recover_environment()
            if not self.artifact_budget_exceeded():
                self.collect_logs("final")
            self.write_report()
            self.write_summary()
            self.events.close()
            self.samples_file.close()

    def should_stop_after_scene(self, round_no):
        return round_no > self.args.rounds and self.deadline_at > 0 and time.time() >= self.deadline_at

    def should_stop_after_round(self, round_no):
        if round_no < self.args.rounds:
            return False
        if self.deadline_at <= 0:
            return True
        return time.time() >= self.deadline_at

    def stop_reason(self, round_no):
        if round_no < self.args.rounds:
            return "interrupted_before_min_rounds"
        if self.deadline_at > 0 and time.time() >= self.deadline_at:
            return "deadline_reached"
        return "min_rounds_complete"

    def run_event(self, event, round_no=1):
        name = event["name"]
        self.log("scenario event start round=%s name=%s" % (round_no, name))
        baseline_ready = self.ensure_baseline_ready("before_%s" % name)
        if not baseline_ready:
            before_path = self.write_snapshot("round%s_%s_baseline_before" % (round_no, name))
            after_path = self.write_snapshot("round%s_%s_baseline_after" % (round_no, name))
            result = {
                "name": "baseline_before_%s" % name,
                "round": round_no,
                "started_at": datetime.datetime.now().isoformat(),
                "duration_sec": 0,
                "recovered": False,
                "error": "baseline services were not ready before scenario",
                "before": before_path,
                "after": after_path,
            }
            self.results.append(result)
            self.log("scenario event skipped name=%s reason=baseline_not_ready" % name)
            return
        before_path = self.write_snapshot("round%s_%s_before" % (round_no, name))
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
        after_path = self.write_snapshot("round%s_%s_after" % (round_no, name))
        result = {
            "name": name,
            "round": round_no,
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
        sched_res = self.safe_call("schedulerStatus", {})
        sched = (sched_res.get("result") or {}) if isinstance(sched_res, dict) else {}
        market = self.market_status_result()
        ports = self.port_snapshot()
        ok = bool(isinstance(api, dict) and api.get("ok"))
        scheduler_ok = not (sched.get("mode") == "maintenance" and sched.get("operation_active"))
        game_ok = bool(ports.get(self.port_text("Game")))
        market_ok = self.market_services_ready(market)
        scaling_ok = ok and game_ok and market_ok and self.scaling_recovery_ok(event, sched)
        if scaling_ok:
            self.log("recover_check event=%s scaling_recovery_ok scheduler=%s" % (event, json_text(sched, 1400)))
            return True
        if not ok:
            self.log("recover_check event=%s failed reason=robot_api api=%s" % (event, json_text(api, 1000)))
        if not scheduler_ok:
            self.log("recover_check event=%s failed reason=scheduler_maintenance scheduler=%s" % (event, json_text(sched, 1400)))
        if not game_ok:
            self.log("recover_check event=%s failed reason=game_port ports=%s" % (event, ports))
        if not market_ok:
            self.log("recover_check event=%s failed reason=market_services services=%s" % (event, json_text((market.get("services") or {}), 1400)))
        return bool(ok and scheduler_ok and game_ok and market_ok)

    def scaling_recovery_ok(self, event, sched):
        if event not in ("target20", "target_mid", "target_high", "final_target_mid", "robot_scale_wave", "robot_restart", "robot_restart_under_load"):
            return False
        if not (sched.get("mode") == "maintenance" and sched.get("operation_active")):
            return False
        operation = str(sched.get("operation") or sched.get("recent_operation") or "")
        if operation not in ("create", "cleanup"):
            return False
        target = int(sched.get("target_online") or 0)
        actors = int(sched.get("actors") or 0)
        running = int(sched.get("running") or 0)
        connecting = int(sched.get("connecting") or 0)
        actor_online = int(sched.get("actor_online") or 0)
        return target > 0 and actors > 0 and (running > 0 or connecting > 0 or actor_online > 0)

    def check_market_invariants(self, event):
        status = self.market_status_result()
        ports = self.port_snapshot()
        counts = self.market_db_counts()
        enabled = self.market_auto_enabled(status)
        running = bool(status.get("auto_running"))
        services_ready = self.market_services_ready(status)
        game_ready = bool(ports.get(self.port_text("Game")))
        now = time.time()
        if enabled and game_ready and services_ready and not running:
            if not self.market_auto_stopped_since:
                self.market_auto_stopped_since = now
                self.safe_call("marketStart", {})
                return
            key = "market_auto_stopped:%s" % event
            if now - self.market_auto_stopped_since > 60 and now - self.last_invariant_failure.get(key, 0) > 60:
                self.last_invariant_failure[key] = now
                self.record_failure(key, "market auto enabled but not running while game and services are ready for %ss" % int(now - self.market_auto_stopped_since))
                self.safe_call("marketStart", {})
        else:
            self.market_auto_stopped_since = 0
        auction_kinds = int(counts.get("auction_kinds") or 0)
        if enabled and running and game_ready and services_ready and auction_kinds <= 0:
            if self.market_zero_last_seen and now - self.market_zero_last_seen > self.args.market_zero_grace:
                self.market_zero_since = 0
            self.market_zero_last_seen = now
            if not self.market_zero_since:
                self.market_zero_since = now
            elif now - self.market_zero_since > self.args.market_zero_grace:
                key = "market_zero_kinds:%s" % event
                if now - self.last_invariant_failure.get(key, 0) > 120:
                    recovered = self.wait_market_count(
                        "market_zero_verify:%s" % event,
                        lambda c: int(c.get("auction_kinds") or 0) > 0,
                        self.args.market_zero_grace,
                        10,
                    )
                    if int(recovered.get("auction_kinds") or 0) > 0:
                        self.market_zero_since = 0
                        self.market_zero_last_seen = 0
                    else:
                        self.last_invariant_failure[key] = now
                        self.record_failure(key, "market auto running but auction kinds stayed zero for %ss" % int(now - self.market_zero_since))
                        self.safe_call("marketStart", {})
        else:
            self.market_zero_since = 0
            self.market_zero_last_seen = 0

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
            "processes": self.shell("pgrep -af '/root/robot|df_game_r|df_monitor_r|df_auction_r|df_point_r|df_relay_r|mysqld' || true", 20, log_output=False)[:4000],
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
        return dict((str(port), int((":" + str(port)) in out)) for port in self.ports.values() if int(port or 0) > 0)

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
            "SELECT 'auction_high_addinfo',COUNT(*),COUNT(DISTINCT item_id) FROM taiwan_cain_auction_gold.auction_main WHERE owner_id>=90000001 AND add_info>=210000000;"
            "SELECT 'auction_creature',COUNT(*),COUNT(DISTINCT a.item_id) FROM taiwan_cain_auction_gold.auction_main a INNER JOIN taiwan_cain_2nd.creature_items c ON c.ui_id=a.add_info AND c.charac_no=a.owner_id WHERE a.owner_id>=90000001;"
            "SELECT 'creature_instances',COUNT(*),COUNT(DISTINCT it_id) FROM taiwan_cain_2nd.creature_items WHERE charac_no>=90000001;"
            "SHOW COLUMNS FROM taiwan_cain_auction_gold.auction_main;"
            "SHOW COLUMNS FROM taiwan_cain_auction_cera.auction_main;"
            "SHOW COLUMNS FROM taiwan_cain_2nd.creature_items;"
        )
        return self.shell("mysql -ugame -puu5!^%%jg -e %s" % shell_quote(query), 60, log_output=False)[:12000]

    def market_db_counts(self):
        query = (
            "SELECT 'auction',COUNT(*),COUNT(DISTINCT item_id) FROM taiwan_cain_auction_gold.auction_main;"
            "SELECT 'cera',COUNT(*),COUNT(DISTINCT item_id) FROM taiwan_cain_auction_cera.auction_main;"
            "SELECT 'auction_high_addinfo',COUNT(*),COUNT(DISTINCT item_id) FROM taiwan_cain_auction_gold.auction_main WHERE owner_id>=90000001 AND add_info>=210000000;"
            "SELECT 'auction_creature',COUNT(*),COUNT(DISTINCT a.item_id) FROM taiwan_cain_auction_gold.auction_main a INNER JOIN taiwan_cain_2nd.creature_items c ON c.ui_id=a.add_info AND c.charac_no=a.owner_id WHERE a.owner_id>=90000001;"
            "SELECT 'creature_instances',COUNT(*),COUNT(DISTINCT it_id) FROM taiwan_cain_2nd.creature_items WHERE charac_no>=90000001;"
            "SELECT 'creature_orphans',COUNT(*),COUNT(DISTINCT c.it_id) FROM taiwan_cain_2nd.creature_items c LEFT JOIN taiwan_cain_auction_gold.auction_main a ON a.add_info=c.ui_id AND a.owner_id=c.charac_no WHERE c.charac_no>=90000001 AND a.auction_id IS NULL;"
        )
        out = self.shell("mysql -ugame -puu5!^%%jg -N -e %s" % shell_quote(query), 30, log_output=False)
        counts = {}
        for line in safe_text(out).splitlines():
            parts = line.split()
            if len(parts) >= 3 and parts[0] in ("auction", "cera"):
                counts[parts[0] + "_records"] = parts[1]
                counts[parts[0] + "_kinds"] = parts[2]
            elif len(parts) >= 3 and parts[0] in ("auction_high_addinfo", "auction_creature", "creature_instances", "creature_orphans"):
                counts[parts[0] + "_records"] = parts[1]
                counts[parts[0] + "_kinds"] = parts[2]
        return counts

    def store_db_counts(self):
        query = (
            "SELECT 'dummy_total',COUNT(*) FROM d_starsky.Dummylist;"
            "SELECT 'dummy_store',COUNT(*) FROM d_starsky.Dummylist WHERE CAST(function_type AS UNSIGNED)=2;"
            "SELECT 'dummy_disjoint',COUNT(*) FROM d_starsky.Dummylist WHERE CAST(function_type AS UNSIGNED)=3;"
            "SELECT 'stall_rows',COUNT(*),COUNT(DISTINCT UID) FROM d_starsky.Robot_stall WHERE function_type=2 AND state=1;"
            "SELECT 'stall_config',COUNT(*),COUNT(DISTINCT UID) FROM d_starsky.Robot_stall_config WHERE function_type=2 AND state=1;"
        )
        out = self.shell("mysql -ugame -puu5!^%%jg -N -e %s" % shell_quote(query), 30, log_output=False)
        counts = {}
        for line in safe_text(out).splitlines():
            parts = line.split()
            if len(parts) >= 2:
                counts[parts[0]] = parts[1]
            if len(parts) >= 3:
                counts[parts[0] + "_uids"] = parts[2]
        return counts

    def assert_store_presence(self, label):
        counts = self.store_db_counts()
        self.log("%s store_counts=%s" % (label, json_text(counts, 1200)))
        store = to_int(counts.get("dummy_store"))
        disjoint = to_int(counts.get("dummy_disjoint"))
        stall_rows = to_int(counts.get("stall_rows"))
        if store + disjoint <= 0:
            self.record_failure(label + "_no_store_function_type", "Dummylist has no function_type=2 or function_type=3 rows after store scenario")
        if store > 0 and stall_rows <= 0:
            self.record_failure(label + "_store_without_stall_rows", "Dummylist has function_type=2 rows but Robot_stall has no active function_type=2 rows")
        return counts

    def wait_market_count(self, label, predicate, timeout, interval):
        deadline = time.time() + timeout
        last = {}
        while time.time() < deadline:
            last = self.market_db_counts()
            if predicate(last):
                self.log("wait_market_count ready label=%s counts=%s" % (label, json_text(last, 1000)))
                return last
            self.log("wait_market_count wait label=%s counts=%s" % (label, json_text(last, 1000)))
            time.sleep(interval)
        return last

    def prepare_baseline(self):
        if not os.path.isdir(self.baseline_dir):
            os.makedirs(self.baseline_dir)
        self.log("prepare_baseline begin dir=%s" % self.baseline_dir)
        backup_output = self.shell(filtered_config_backup_script("/root/config", os.path.join(self.baseline_dir, "root_config")), 120)
        if "CONFIG_BACKUP_OK" not in safe_text(backup_output):
            self.log("prepare_baseline config_backup_failed output=%s" % safe_text(backup_output)[:1000])
            return False
        self.shell("mkdir -p %s/home_neople_auction %s/home_neople_point; cp -af /home/neople/auction/iteminfo.dat %s/home_neople_auction/iteminfo.dat 2>/dev/null || true; cp -af /home/neople/point/iteminfo.dat %s/home_neople_point/iteminfo.dat 2>/dev/null || true" % (shell_quote(self.baseline_dir), shell_quote(self.baseline_dir), shell_quote(self.baseline_dir), shell_quote(self.baseline_dir)), 60)
        self.backup_market_database("baseline")
        restore_path = os.path.join(self.baseline_dir, "restore_baseline.sh")
        restore = """#!/bin/sh
set -e
BASE=%s
mkdir -p /root/config
find /root/config -mindepth 1 -maxdepth 1 \
  ! -name 'log_robot*' \
  ! -name 'market_log.jsonl*' \
  ! -name 'market_*_service.log*' \
  ! -name '*.rotate.tmp' \
  ! -name '*.trim.tmp' \
  -exec rm -rf -- {} +
cp -af "$BASE/root_config/." /root/config/
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
            return False
        self.log("prepare_baseline done restore=%s" % restore_path)
        return True

    def final_recover_environment(self):
        self.log("final_recover_environment begin")
        restore_path = os.path.join(self.baseline_dir, "restore_baseline.sh")
        if os.path.isfile(restore_path):
            self.shell("sh %s" % shell_quote(restore_path), 180)
        else:
            self.log("final_recover_environment missing restore script=%s" % restore_path)
        core_ports = self.port_regex(("Game", "Monitor", "Point", "Auction"))
        market_ports = self.port_regex(("Point", "Auction"))
        self.shell("cd /root && (./run >/dev/null 2>&1 || true); sleep 20; ss -lntp | grep -E ':(%s)' || true; pgrep -af 'df_game_r|df_monitor_r|df_auction_r|df_point_r' || true" % core_ports, 240)
        self.robot_restart_without_target("final_recover_robot")
        if not self.wait_robot_api("final_recover_api", 90, 5):
            self.record_failure("final_recover_api_timeout", "robot API was not ready after final recovery")
        self.set_target(20, settle_sec=0)
        self.market_enable_auto(max_concurrent=8)
        if not self.wait_market_services("final_recover_market", 240, 10):
            self.log("final_recover_market first attempt failed; clear system stock and retry")
            self.safe_call("marketClearSystemStock", {})
            self.stop_market_services()
            self.shell("cd /root && (./run >/dev/null 2>&1 || true); sleep 20; ss -lntp | grep -E ':(%s)' || true; pgrep -af 'df_auction_r|df_point_r' || true" % market_ports, 240)
            self.market_enable_auto(max_concurrent=8)
            if not self.wait_market_services("final_recover_market_retry", 180, 10):
                self.record_failure("final_recover_market_timeout", "market services were not ready after final recovery retry")
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

    def wait_game_port(self, event, timeout_sec=90, interval_sec=5):
        self.log("wait_game_port start event=%s timeout=%s" % (event, timeout_sec))
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            last = self.port_snapshot()
            if last.get(self.port_text("Game")):
                self.log("wait_game_port ready event=%s ports=%s" % (event, last))
                return True
            time.sleep(interval_sec)
        self.log("wait_game_port timeout event=%s ports=%s" % (event, last))
        return False

    def ensure_baseline_ready(self, event, timeout_sec=120):
        api_ok = self.wait_robot_api("%s_api" % event, 30, 5)
        scheduler_ok = self.wait_scheduler_ready("%s_scheduler" % event, 30, 5)
        game_ok = self.wait_game_port("%s_game" % event, 30, 5)
        market_ok = self.wait_market_services("%s_market" % event, 30, 5)
        if api_ok and scheduler_ok and game_ok and market_ok:
            return True
        self.log("ensure_baseline_ready recover event=%s api=%s scheduler=%s game=%s market=%s" % (event, api_ok, scheduler_ok, game_ok, market_ok))
        core_ports = self.port_regex(("Game", "Monitor", "Point", "Auction"))
        self.shell("cd /root && (./run >/dev/null 2>&1 || true); sleep 20; ss -lntp | grep -E ':(%s)' || true; pgrep -af 'df_game_r|df_monitor_r|df_auction_r|df_point_r' || true" % core_ports, 240)
        if not api_ok:
            self.robot_restart_without_target("%s_robot_restart" % event)
        api_ok = self.wait_robot_api("%s_api_retry" % event, timeout_sec, 5)
        scheduler_ok = self.wait_scheduler_ready("%s_scheduler_retry" % event, timeout_sec, 5)
        game_ok = self.wait_game_port("%s_game_retry" % event, timeout_sec, 5)
        market_ok = self.wait_market_services("%s_market_retry" % event, timeout_sec, 10)
        ready = bool(api_ok and scheduler_ok and game_ok and market_ok)
        self.log("ensure_baseline_ready done event=%s ready=%s api=%s scheduler=%s game=%s market=%s" % (event, ready, api_ok, scheduler_ok, game_ok, market_ok))
        return ready

    def wait_scheduler_ready(self, event, timeout_sec=90, interval_sec=5):
        self.log("wait_scheduler_ready start event=%s timeout=%s" % (event, timeout_sec))
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            res = self.safe_call("schedulerStatus", {})
            last = (res.get("result") or {}) if isinstance(res, dict) else {}
            if isinstance(res, dict) and res.get("ok") and not (last.get("mode") == "maintenance" and last.get("operation_active")):
                self.log("wait_scheduler_ready ready event=%s scheduler=%s" % (event, json_text(last, 1200)))
                return True
            time.sleep(interval_sec)
        self.log("wait_scheduler_ready timeout event=%s scheduler=%s" % (event, json_text(last, 1400)))
        return False

    def scenario_events(self):
        high = self.args.target_max
        mid = max(self.args.target_min, min(high, max(100, high // 2)))
        low = max(20, min(self.args.target_min, 80))
        return [
            {"name": "target20", "fn": lambda: self.set_target(20)},
            {"name": "robot_scale_wave", "fn": lambda: self.robot_scale_wave(low, mid, high)},
            {"name": "smoke_actions", "fn": self.smoke_actions},
            {"name": "robot_action_storm", "fn": self.robot_action_storm},
            {"name": "announcement_check", "fn": self.announcement_check},
            {"name": "party_relay_health", "fn": self.party_relay_health},
            {"name": "party_compat_supervisor", "fn": self.party_compat_supervisor},
            {"name": "party_observer_smoke", "fn": self.party_observer_smoke},
            {"name": "party_skill_observer", "fn": self.party_skill_observer},
            {"name": "market_fault", "fn": self.market_fault},
            {"name": "market_operation_storm", "fn": self.market_operation_storm},
            {"name": "robot_manual_mode_drill", "fn": self.robot_manual_mode_drill},
            {"name": "market_special_smoke", "fn": self.market_special_smoke},
            {"name": "market_weapon_target_smoke", "fn": self.market_weapon_target_smoke},
            {"name": "market_cera_drill", "fn": self.market_cera_drill},
            {"name": "market_startup_iteminfo_race", "fn": self.market_startup_iteminfo_race},
            {"name": "pvf_file_fault", "fn": self.pvf_file_fault},
            {"name": "market_button_flow", "fn": self.market_button_flow},
            {"name": "target_mid", "fn": lambda: self.set_target(mid)},
            {"name": "manual_collision", "fn": self.manual_collision},
            {"name": "robot_store_lifecycle_storm", "fn": self.robot_store_lifecycle_storm},
            {"name": "db_stock_external_clear", "fn": self.db_stock_external_clear},
            {"name": "db_schema_drift", "fn": self.db_schema_drift},
            {"name": "target_high", "fn": lambda: self.set_target(high, settle_sec=self.scaled_seconds(45, 90))},
            {"name": "cleanup_burst", "fn": self.cleanup_burst},
            {"name": "robot_cleanup_edge_cases", "fn": self.robot_cleanup_edge_cases},
            {"name": "config_dir_fault", "fn": self.config_dir_fault},
            {"name": "web_api_fault", "fn": self.web_api_fault},
            {"name": "port_conflict_fault", "fn": self.port_conflict_fault},
            {"name": "mysql_restart_fault", "fn": self.mysql_restart_fault},
            {"name": "monitor_fault", "fn": self.monitor_fault},
            {"name": "game_port_fault", "fn": self.game_port_fault},
            {"name": "robot_restart_under_load", "fn": lambda: self.robot_restart_under_load(high)},
            {"name": "robot_restart", "fn": self.robot_restart},
            {"name": "custom_key_test", "fn": self.custom_key_test},
            {"name": "final_target_mid", "fn": lambda: self.set_target(mid)},
        ]

    def scaled_seconds(self, low, high):
        value = int(self.total_sec / 40)
        return max(low, min(high, value))

    def ensure_auto(self):
        self.set_target(20)
        self.ensure_baseline_ready("initial")

    def set_target(self, target, settle_sec=0):
        payload = {"updates": {"auto.auto_target_online_count": str(target), "auto.auto_actions": "true"}}
        res = self.safe_call("robotConfigUpdate", payload)
        self.log("set_target target=%s config=%s" % (target, json_text(res, 1200)))
        res = self.safe_call("autoStart", {})
        self.log("autoStart result=%s" % json_text(res, 1200))
        self.sample_with_event("after_set_target:%s" % target)
        if settle_sec > 0:
            self.burst_sample("set_target:%s" % target, settle_sec)

    def random_target(self):
        self.set_target(random.randint(self.args.target_min, self.args.target_max))

    def random_user_interleave(self):
        actions = [
            self.user_robot_action_mix,
            self.user_robot_online_logout,
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

    def status_rows(self, count=None):
        status = self.safe_call("robotsStatus", {"count": count or self.args.status_count})
        rows = (((status or {}).get("result") or {}).get("robots") or [])
        if not isinstance(rows, list):
            return []
        return rows

    def select_uids(self, count, prefer_running=True):
        rows = self.status_rows(max(self.args.status_count, count * 4))
        if prefer_running:
            preferred = []
            fallback = []
            for row in rows:
                uid = int(row.get("uid") or 0)
                if uid <= 0:
                    continue
                fallback.append(uid)
                if row.get("runtime_state") in ("running", "store") and not row.get("missing_core"):
                    preferred.append(uid)
            uids = preferred or fallback
        else:
            uids = [int(r.get("uid") or 0) for r in rows if int(r.get("uid") or 0) > 0]
        random.shuffle(uids)
        return uids[:count]

    def robot_call(self, command, payload, label):
        res = self.safe_call(command, payload)
        self.log("%s command=%s payload=%s result=%s" % (label, command, payload, json_text(res, 1800)))
        self.sample_with_event("%s:%s" % (label, command))
        return res

    def user_robot_action_mix(self):
        uids = self.select_uids(12)
        if not uids:
            self.log("user_robot_action_mix skipped no uids")
            return
        random.shuffle(uids)
        self.robot_call("robotsMove", {"uids": uids[:8]}, "user_robot_action_mix")
        self.robot_call("robotsShoutLocal", {"uids": uids[2:10]}, "user_robot_action_mix")
        self.robot_call("robotsShoutWorld", {"uids": uids[:3]}, "user_robot_action_mix")
        self.robot_call("robotsStoreAsync", {"uids": uids[4:8]}, "user_robot_action_mix")

    def user_robot_online_logout(self):
        uids = self.select_uids(10, prefer_running=False)
        if uids:
            self.robot_call("robotsLogoutAsync", {"uids": uids[:5]}, "user_robot_online_logout")
            time.sleep(5)
        self.robot_call("robotsOnlineAsync", {"count": random.randint(3, 12)}, "user_robot_online_logout")

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
        self.wait_market_count("user_market_iteminfo_recover", lambda counts: int(counts.get("auction_kinds") or 0) > 0, 600, 10)

    def user_market_clear_stock(self):
        res = self.market_call_when_idle("marketClearSystemStock", {}, "user_market_clear_stock", attempts=24, delay_sec=5)
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
        if not (isinstance(res, dict) and res.get("ok")):
            raise RuntimeError("systemAnnouncement failed: %s" % json_text(res, 1000))
        self.sample_with_event("announcement_check")
        self.burst_sample("announcement_recover", self.scaled_seconds(30, 90), 10)

    def party_relay_health(self):
        relay_port = self.port("Relay")
        self.log("party_relay_health begin relay_port=%s" % relay_port)
        if relay_port <= 0:
            raise RuntimeError("relay port is not configured")
        err = ""
        sock = None
        try:
            sock = socket.create_connection(("127.0.0.1", relay_port), 3)
        except Exception as exc:
            err = repr(exc)
        finally:
            try:
                if sock:
                    sock.close()
            except Exception:
                pass
        if err:
            raise RuntimeError("relay port not ready: %s" % err)
        self.sample_with_event("party_relay_health")
        self.log("party_relay_health done")

    def party_compat_supervisor(self):
        self.log("party_compat_supervisor begin")
        account_start = 17000000
        account_end = 17001000
        before = self.web_json("/api/party-compat")
        self.log("party_compat_supervisor before=%s" % json_text(before, 1400))
        off = self.web_json("/api/party-compat", {"action": "off", "account_start": account_start, "account_end": account_end})
        self.log("party_compat_supervisor off=%s" % json_text(off, 1400))
        time.sleep(3)
        off_status = self.web_json("/api/party-compat")
        self.log("party_compat_supervisor off_status=%s" % json_text(off_status, 1400))
        off_result = off_status.get("result") or {}
        if off_result.get("desired_enabled"):
            self.record_failure("party_compat_off_desired", "party compat off did not save desired_enabled=false")

        on = self.web_json("/api/party-compat", {"action": "on", "account_start": account_start, "account_end": account_end})
        self.log("party_compat_supervisor on=%s" % json_text(on, 1400))
        if not self.wait_party_compat_desired(True, "party_compat_on", 45, 5):
            self.record_failure("party_compat_on_desired", "party compat on did not save desired_enabled=true")

        game_ports = self.port_regex(("Game", "Monitor", "Point", "Auction", "Relay"))
        self.sample_with_event("party_compat_restart_game_before")
        self.shell("cd /root && (./stop >/dev/null 2>&1 || true); sleep 10; (./run >/dev/null 2>&1 || true); sleep 25; ss -lntp | grep -E ':(%s)' || true; pgrep -af 'df_game_r|df_monitor_r|df_auction_r|df_point_r|df_relay_r' || true" % game_ports, 240)
        if not self.wait_party_compat_actual_on("party_compat_after_game_restart", 120, 5):
            self.record_failure("party_compat_supervisor_repatch", "party compat supervisor did not repatch after game restart")
        self.sample_with_event("party_compat_supervisor_done")
        self.log("party_compat_supervisor done")

    def wait_party_compat_desired(self, enabled, event, timeout_sec, interval_sec):
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            try:
                status = self.web_json("/api/party-compat")
            except Exception as exc:
                self.log("wait_party_compat_desired event=%s err=%r" % (event, exc))
                time.sleep(interval_sec)
                continue
            last = status.get("result") or {}
            if bool(last.get("desired_enabled")) == bool(enabled):
                self.log("wait_party_compat_desired ready event=%s status=%s" % (event, json_text(last, 1200)))
                return True
            time.sleep(interval_sec)
        self.log("wait_party_compat_desired timeout event=%s status=%s" % (event, json_text(last, 1400)))
        return False

    def wait_party_compat_actual_on(self, event, timeout_sec, interval_sec):
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            try:
                status = self.web_json("/api/party-compat")
            except Exception as exc:
                self.log("wait_party_compat_actual_on event=%s err=%r" % (event, exc))
                time.sleep(interval_sec)
                continue
            last = status.get("result") or {}
            if last.get("desired_enabled") and last.get("enabled") and last.get("state") == "on":
                self.log("wait_party_compat_actual_on ready event=%s status=%s" % (event, json_text(last, 1200)))
                return True
            time.sleep(interval_sec)
        self.log("wait_party_compat_actual_on timeout event=%s status=%s" % (event, json_text(last, 1400)))
        return False

    def party_observer_smoke(self):
        self.log("party_observer_smoke begin")
        before_logs = self.party_log_parts()
        before = self.party_log_counts(self.join_party_logs(before_logs))
        self.log("party_observer_smoke before=%s" % json_text(before, 1000))
        self.set_target(max(20, min(self.args.target_max, 80)))
        self.burst_sample("party_observer_window", self.scaled_seconds(45, 120), 5)
        after_logs = self.party_log_parts()
        delta_text = self.party_log_delta(before_logs, after_logs)
        delta = self.party_log_counts(delta_text)
        unresolved = self.party_unresolved_routes(delta_text)
        if unresolved:
            self.log("party_observer_smoke recovery_grace unresolved=%s" % ",".join(unresolved[:20]))
            self.burst_sample("party_recovery_grace", self.scaled_seconds(20, 60), 5)
            after_logs = self.party_log_parts()
            delta_text = self.party_log_delta(before_logs, after_logs)
            delta = self.party_log_counts(delta_text)
            unresolved = self.party_unresolved_routes(delta_text)
        self.log("party_observer_smoke delta=%s unresolved=%s" % (json_text(delta, 1200), ",".join(unresolved[:20])))
        if delta.get("party_total", 0) <= 0:
            self.log("party_observer_smoke skipped reason=no_party_activity")
            return
        relay_errors = delta.get("relay_errors", 0)
        udp_errors = delta.get("udp_errors", 0)
        tqos_exhausted = delta.get("tqos_exhausted", 0)
        route_degraded = delta.get("route_degraded", 0)
        route_recovery = delta.get("route_recovery", 0)
        route_recovered = delta.get("route_recovered", 0)
        relay_connected = delta.get("relay_connected", 0)
        probe_cycles = delta.get("probe_cycles", 0)
        peer_ready = delta.get("peer_ready", 0)
        udp_recycles = delta.get("udp_recycles", 0)
        supervisor_panics = delta.get("supervisor_panics", 0)
        recovery_events = route_recovered + peer_ready
        self.log("party_observer_smoke delta relay_errors=%s udp_errors=%s tqos_exhausted=%s route_degraded=%s route_recovery=%s route_recovered=%s relay_connected=%s probe_cycles=%s peer_ready=%s" % (
            relay_errors, udp_errors, tqos_exhausted, route_degraded, route_recovery, route_recovered, relay_connected, probe_cycles, peer_ready,
        ))
        if relay_errors > 0 and relay_connected <= 0 and recovery_events <= 0:
            self.record_failure("party_relay_errors_unrecovered", "party relay errors increased by %s without a recovery event" % relay_errors)
        if udp_errors > 5 and udp_recycles <= 0 and recovery_events <= 0:
            self.record_failure("party_udp_errors_unrecovered", "party UDP errors increased by %s without a recovery event" % udp_errors)
        if tqos_exhausted > 0 and recovery_events <= 0:
            self.record_failure("party_tqos_unrecovered", "party TQOS retries exhausted %s times without route or peer recovery" % tqos_exhausted)
        if unresolved:
            self.record_failure("party_route_unrecovered", "%s party routes remained degraded: %s" % (len(unresolved), ",".join(unresolved[:20])))
        if probe_cycles > 12 and peer_ready <= 0:
            self.record_failure("party_peer_probe_storm", "%s robot peer probe cycles completed without a ready transition" % probe_cycles)
        if supervisor_panics > 0:
            self.record_failure("party_supervisor_panic", "party supervisor recovered from %s panic(s)" % supervisor_panics)
        self.sample_with_event("party_observer_smoke_done")

    def party_skill_observer(self):
        self.log("party_skill_observer begin")
        before_logs = self.party_log_parts()
        before = self.party_log_counts(self.join_party_logs(before_logs))
        if before.get("skill_profiles", 0) <= 0:
            self.log("party_skill_observer note=no_skill_profile_logs_yet")
        self.burst_sample("party_skill_window", self.scaled_seconds(60, 150), 5)
        after_logs = self.party_log_parts()
        delta = self.party_log_counts(self.party_log_delta(before_logs, after_logs))
        self.log("party_skill_observer delta=%s" % json_text(delta, 1000))
        skill_events = delta.get("skill_casts", 0)
        skill_errors = delta.get("skill_errors", 0)
        skill_profiles = delta.get("skill_profiles", 0)
        empty_profiles = delta.get("skill_empty_profiles", 0)
        if empty_profiles > 0:
            self.record_failure("party_skill_empty_profile", "%s new skill profiles had zero effective candidates" % empty_profiles)
        if skill_errors > 0:
            self.record_failure("party_skill_errors", "party skill errors increased by %s" % skill_errors)
        if skill_events <= 0 and skill_profiles <= 0:
            self.log("party_skill_observer skipped reason=no_new_skill_activity")
        elif skill_events <= 0 and skill_errors <= 0 and empty_profiles <= 0:
            self.log("party_skill_observer note=profile_seen_without_cast profiles=%s" % skill_profiles)
        self.sample_with_event("party_skill_observer_done")

    def party_log_counts(self, text=None):
        patterns = {
            "party_total": r"\[PARTY_",
            "relay_errors": r"PARTY_RELAY_.*ERROR|PARTY_RELAY_BAD_PACKET",
            "udp_errors": r"PARTY_UDP_.*ERROR|PARTY_ROBOT_PROBE_ERROR",
            "tqos_exhausted": r"PARTY_TQOS_RETRY_EXHAUSTED",
            "route_degraded": r"PARTY_ROUTE_DEGRADED",
            "route_recovery": r"PARTY_ROUTE_RECOVERY\]",
            "route_recovered": r"PARTY_ROUTE_RECOVERED",
            "relay_connected": r"PARTY_RELAY_CONNECTED",
            "probe_cycles": r"PARTY_ROBOT_PROBE_CYCLE",
            "peer_ready": r"PARTY_ROBOT_TQOS_READY",
            "self_id_refresh": r"PARTY_SELF_ID_REFRESH",
            "udp_recycles": r"PARTY_UDP_RECYCLE",
            "supervisor_panics": r"PARTY_SUPERVISOR_PANIC",
            "skill_profiles": r"PARTY_DUNGEON_SKILL_PROFILE\]",
            "skill_empty_profiles": r"PARTY_DUNGEON_SKILL_PROFILE\][^\n]*candidates=0(?:\s|$)",
            "skill_casts": r"PARTY_DUNGEON_SKILL\]",
            "skill_errors": r"PARTY_DUNGEON_SKILL_.*ERROR|PARTY_DUNGEON_SKILL_CATALOG_ERROR",
        }
        if text is None:
            text = self.party_log_tail()
        return dict((name, len(re.findall(pattern, text))) for name, pattern in patterns.items())

    def robot_log_tail(self, max_bytes=1024 * 1024):
        return self.read_log_tail("/root/config/log_robot", max_bytes)

    def party_log_tail(self, max_bytes=2 * 1024 * 1024):
        return self.join_party_logs(self.party_log_parts(max_bytes))

    def party_log_parts(self, max_bytes=2 * 1024 * 1024):
        each = max(1, max_bytes // 2)
        return {
            "log_robot": self.read_log_tail("/root/config/log_robot", each),
            "robot_stdout": self.read_log_tail("/root/config/robot_stdout.log", each),
        }

    def join_party_logs(self, parts):
        return safe_text(parts.get("log_robot")) + u"\n" + safe_text(parts.get("robot_stdout"))

    def party_log_delta(self, before, after):
        chunks = []
        for name in ("log_robot", "robot_stdout"):
            old = safe_text(before.get(name))
            new = safe_text(after.get(name))
            if not old:
                chunks.append(new)
                continue
            if new.startswith(old):
                chunks.append(new[len(old):])
                continue
            overlap = u""
            limit = min(len(old), 65536)
            size = limit
            while size >= 256:
                marker = old[-size:]
                if new.rfind(marker) >= 0:
                    overlap = marker
                    break
                size //= 2
            if overlap:
                chunks.append(new[new.rfind(overlap) + len(overlap):])
            else:
                chunks.append(new)
        return u"\n".join(chunks)

    def party_unresolved_routes(self, text):
        route_pattern = re.compile(r"\[PARTY_ROUTE_(DEGRADED|RECOVERY|RECOVERED)\][^\n]*uid=(\d+)[^\n]*peer=(\d+)[^\n]*route=(\d+)")
        ready_pattern = re.compile(r"\[PARTY_ROBOT_TQOS_READY\][^\n]*uid=(\d+)[^\n]*peer=(\d+)[^\n]*route=(\d+)")
        states = {}
        for line in safe_text(text).splitlines():
            match = route_pattern.search(line)
            if match:
                key = (match.group(2), match.group(3), match.group(4))
                state = match.group(1).lower()
                states[key] = state
                continue
            match = ready_pattern.search(line)
            if not match:
                continue
            uid, peer = match.group(1), match.group(2)
            for key in list(states):
                if key[0] == uid and key[1] == peer and states[key] in ("degraded", "recovery"):
                    states[key] = "ready"
        unresolved = []
        for key, state in states.items():
            if state in ("degraded", "recovery"):
                unresolved.append("uid=%s/peer=%s/route=%s" % key)
        return sorted(unresolved)

    def read_log_tail(self, path, max_bytes):
        try:
            fh = open(path, "rb")
            try:
                fh.seek(0, os.SEEK_END)
                size = fh.tell()
                if size > max_bytes:
                    fh.seek(-max_bytes, os.SEEK_END)
                else:
                    fh.seek(0, os.SEEK_SET)
                return safe_text(fh.read())
            finally:
                fh.close()
        except Exception:
            return u""

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

    def market_special_smoke(self):
        self.log("market_special_smoke begin")
        self.market_enable_auto(max_concurrent=8)
        before = self.market_db_counts()
        self.log("market_special_smoke before=%s" % json_text(before, 1200))
        res = self.market_call_when_idle("marketRestockOnce", {"market": "auction", "execute": True, "max_actions": 1000, "max_concurrent": 8, "continue_on_error": True}, "market_special_smoke", attempts=24, delay_sec=5)
        self.log("market_special_smoke restock result=%s" % json_text(res, 2600))
        self.validate_market_action_prices(res, "market_special_smoke")
        special_action_ok = self.market_result_has_special_success(res)
        self.burst_sample("market_special_after_restock", self.scaled_seconds(30, 90), 10)
        after = self.wait_market_count(
            "market_special_after_restock",
            lambda counts: int(counts.get("auction_high_addinfo_records") or 0) + int(counts.get("auction_creature_records") or 0) > 0,
            300,
            10,
        )
        self.log("market_special_smoke after=%s" % json_text(after, 1200))
        special = int(after.get("auction_high_addinfo_records") or 0) + int(after.get("auction_creature_records") or 0)
        if int(after.get("auction_records") or 0) > 0 and special <= 0 and not special_action_ok:
            self.record_failure("market_special_no_records", "auction has records but no high-addinfo or creature special records after special smoke")
        res = self.market_call_when_idle("marketClearSystemStock", {}, "market_special_clear")
        self.log("market_special_smoke clear result=%s" % json_text(res, 2200))
        cleared = self.wait_market_count(
            "market_special_clear",
            lambda counts: int(counts.get("creature_instances_records") or 0) <= 0,
            120,
            5,
        )
        self.log("market_special_smoke cleared=%s" % json_text(cleared, 1200))
        if int(cleared.get("creature_instances_records") or 0) > 0:
            self.record_failure("market_creature_instances_not_cleared", "system creature instances remained after marketClearSystemStock")
        self.market_enable_auto(max_concurrent=8)
        self.sample_with_event("market_special_smoke_done")
        self.log("market_special_smoke done")

    def market_weapon_target_smoke(self):
        self.log("market_weapon_target_smoke begin")
        targets = [
            (28237, "swordman_beamsword"),
            (37603, "thief_wand"),
            (37605, "thief_dagger"),
        ]
        stop = self.safe_call("marketStop", {})
        self.log("market_weapon_target_smoke stop_auto=%s" % json_text(stop, 1200))
        self.wait_market_job_idle("market_weapon_target_pre", 300, 5)
        clear = self.market_call_when_idle("marketClearSystemStock", {}, "market_weapon_target_clear", attempts=24, delay_sec=5)
        self.log("market_weapon_target_smoke clear result=%s" % json_text(clear, 1600))
        target_ids = [item_id for item_id, _ in targets]
        iteminfo = self.auction_iteminfo_presence(target_ids)
        self.log("market_weapon_target_smoke iteminfo=%s" % json_text(iteminfo, 1200))
        missing_iteminfo = []
        for item_id, label in targets:
            if not iteminfo.get(str(item_id)):
                missing_iteminfo.append("%s:%s" % (item_id, label))
        if missing_iteminfo:
            self.record_failure("market_weapon_target_iteminfo_missing", "target auction item ids missing from auction iteminfo: %s" % ",".join(missing_iteminfo))
        res = self.market_call_when_idle("marketRestockOnce", {"market": "auction", "execute": True, "max_actions": 64, "max_concurrent": 4, "continue_on_error": True, "item_ids": target_ids}, "market_weapon_target_smoke", attempts=24, delay_sec=5)
        self.log("market_weapon_target_smoke restock result=%s" % json_text(res, 2600))
        self.validate_market_action_prices(res, "market_weapon_target_smoke")
        self.burst_sample("market_weapon_target_after_restock", self.scaled_seconds(20, 60), 10)
        counts = self.auction_item_counts(target_ids)
        self.log("market_weapon_target_smoke counts=%s" % json_text(counts, 1200))
        missing = []
        for item_id, label in targets:
            if to_int(counts.get(str(item_id))) <= 0:
                missing.append("%s:%s" % (item_id, label))
        target_actions, target_rejected = self.target_restock_rejected(res, target_ids)
        self.log("market_weapon_target_smoke target_actions=%s target_rejected=%s" % (target_actions, target_rejected))
        if missing and target_actions <= 0:
            self.record_failure("market_weapon_target_no_actions", "target auction item ids produced no restock actions: %s" % ",".join(missing))
        elif missing and not target_rejected:
            self.record_failure("market_weapon_target_missing", "target auction item ids missing from robot stock: %s" % ",".join(missing))
        elif missing:
            self.log("market_weapon_target_smoke stock_missing_but_server_rejected=%s" % ",".join(missing))
        self.market_enable_auto(max_concurrent=8)
        self.sample_with_event("market_weapon_target_smoke_done")
        self.log("market_weapon_target_smoke done")

    def auction_iteminfo_presence(self, item_ids):
        wanted = set()
        for item_id in item_ids:
            try:
                wanted.add(int(item_id))
            except Exception:
                pass
        result = dict((str(item_id), False) for item_id in wanted)
        paths = ["/home/neople/auction/iteminfo.dat", "/root/config/pvf_iteminfo.dat"]
        for path in paths:
            try:
                with io.open(path, "r", encoding="utf-8", errors="ignore") as fh:
                    for line in fh:
                        parts = line.strip().split()
                        if not parts:
                            continue
                        try:
                            item_id = int(parts[0])
                        except Exception:
                            continue
                        if item_id in wanted and "== NULL" not in line:
                            result[str(item_id)] = True
                result["_source"] = path
                return result
            except Exception:
                continue
        result["_source"] = ""
        return result

    def target_restock_rejected(self, result, item_ids):
        wanted = set([int(item_id) for item_id in item_ids])
        payload = (result.get("result") or {}) if isinstance(result, dict) else {}
        actions = payload.get("actions") or []
        target_actions = []
        for entry in actions:
            action = entry.get("action") or {}
            try:
                item_id = int(action.get("item_id") or 0)
            except Exception:
                item_id = 0
            if item_id in wanted:
                target_actions.append(entry)
        if not target_actions:
            return 0, False
        rejected = True
        for entry in target_actions:
            if entry.get("ok"):
                rejected = False
                break
        return len(target_actions), rejected

    def validate_market_action_prices(self, result, label):
        payload = (result.get("result") or {}) if isinstance(result, dict) else {}
        actions = payload.get("actions") or []
        for idx, entry in enumerate(actions):
            action = entry.get("action") or {}
            market = safe_text(action.get("market") or "")
            kind = safe_text(action.get("kind") or "")
            item_id = action.get("item_id")
            count = to_int(action.get("count"))
            unit = to_int(action.get("unit_price"))
            total = to_int(action.get("total_price"))
            start = to_int(action.get("start_price"))
            instant = to_int(action.get("instant_price"))
            prefix = "%s action[%s] item_id=%s market=%s kind=%s" % (label, idx, item_id, market, kind)
            if unit <= 0 or total <= 0 or instant <= 0:
                self.record_failure("market_action_non_positive_price", "%s unit=%s total=%s instant=%s" % (prefix, unit, total, instant))
            if market == "auction":
                if start < 0:
                    self.record_failure("market_action_negative_start_price", "%s start=%s instant=%s" % (prefix, start, instant))
                if start >= instant:
                    self.record_failure("market_action_invalid_price_order", "%s start=%s instant=%s" % (prefix, start, instant))
                if kind == "stackable" and count > 0 and unit > 0 and total != unit * count:
                    self.record_failure("market_action_total_mismatch", "%s unit=%s count=%s total=%s" % (prefix, unit, count, total))

    def auction_item_counts(self, item_ids):
        ids = []
        for item_id in item_ids:
            try:
                ids.append(str(int(item_id)))
            except Exception:
                pass
        if not ids:
            return {}
        query = "SELECT item_id,COUNT(*) FROM taiwan_cain_auction_gold.auction_main WHERE owner_id>=90000001 AND item_id IN (%s) GROUP BY item_id;" % ",".join(ids)
        out = self.shell("mysql -ugame -puu5!^%%jg -N -e %s" % shell_quote(query), 30, log_output=False)
        counts = {}
        for line in safe_text(out).splitlines():
            parts = line.split()
            if len(parts) >= 2:
                counts[parts[0]] = parts[1]
        return counts

    def market_operation_storm(self):
        self.log("market_operation_storm begin")
        self.market_enable_auto(max_concurrent=8)
        ops = [
            ("marketRestockOnce", {"market": "auction", "execute": True, "max_actions": 1536, "max_concurrent": 8, "continue_on_error": True}),
            ("marketRestockOnce", {"market": "cera", "execute": True, "max_actions": 256, "max_concurrent": 8, "continue_on_error": True}),
            ("marketCollectOnce", {"market": "auction", "execute": True, "max_actions": 512, "max_concurrent": 8, "continue_on_error": True}),
            ("marketRestockOnce", {"market": "", "execute": True, "max_actions": 2048, "max_concurrent": 8, "continue_on_error": True}),
            ("marketCollectOnce", {"market": "", "execute": True, "max_actions": 512, "max_concurrent": 8, "continue_on_error": True}),
        ]
        for idx, item in enumerate(ops):
            command, payload = item
            res = self.safe_call(command, payload)
            self.log("market_operation_storm step=%s command=%s result=%s" % (idx, command, json_text(res, 2600)))
            self.validate_market_action_prices(res, "market_operation_storm:%s:%s" % (idx, command))
            self.sample_with_event("market_operation_storm:%s:%s" % (idx, command))
            time.sleep(random.randint(2, 6))
        self.safe_call("marketStop", {})
        self.sample_with_event("market_operation_storm:stop")
        time.sleep(random.randint(3, 8))
        self.market_enable_auto(max_concurrent=8)
        self.burst_sample("market_operation_storm_recover", self.scaled_seconds(30, 90), 10)
        self.log("market_operation_storm done")

    def market_cera_drill(self):
        self.log("market_cera_drill begin")
        stop = self.safe_call("marketStop", {})
        self.log("market_cera_drill stop_auto=%s" % json_text(stop, 1200))
        self.wait_market_job_idle("market_cera_drill_pre", 300, 5)
        before = self.market_db_counts()
        self.log("market_cera_drill before=%s" % json_text(before, 1200))
        for idx in range(3):
            res = self.market_call_when_idle("marketRestockOnce", {"market": "cera", "execute": True, "max_actions": 256, "max_concurrent": 8, "continue_on_error": True}, "market_cera_drill:%s" % idx, attempts=60, delay_sec=5)
            self.log("market_cera_drill restock idx=%s result=%s" % (idx, json_text(res, 2200)))
            self.validate_market_action_prices(res, "market_cera_drill:%s" % idx)
            self.sample_with_event("market_cera_restock:%s" % idx)
            time.sleep(5)
        self.burst_sample("market_cera_drill_recover", self.scaled_seconds(20, 60), 10)
        after = self.wait_market_count("market_cera_drill", lambda counts: int(counts.get("cera_records") or 0) > 0, 420, 10)
        self.log("market_cera_drill after=%s" % json_text(after, 1200))
        if int(after.get("cera_records") or 0) <= 0:
            self.record_failure("market_cera_empty", "cera restock drill produced no cera records")
        res = self.market_call_when_idle("marketCollectOnce", {"market": "cera", "execute": True, "max_actions": 128, "max_concurrent": 8, "continue_on_error": True}, "market_cera_collect")
        self.log("market_cera_drill collect result=%s" % json_text(res, 1800))
        self.market_enable_auto(max_concurrent=8)
        self.log("market_cera_drill done")

    def market_startup_iteminfo_race(self):
        self.log("market_startup_iteminfo_race begin")
        self.market_enable_auto(max_concurrent=8)
        self.wait_market_auto_running("market_startup_race_auto_before", 120, 10)
        self.sample_with_event("market_startup_race_before")
        game_ports = self.port_regex(("Game", "Monitor"))
        core_ports = self.port_regex(("Game", "Monitor", "Point", "Auction"))
        self.shell("cd /root && (./stop >/dev/null 2>&1 || true); sleep 12; ss -lntp | grep -E ':(%s)' || true" % game_ports, 180)
        self.robot_restart_without_target("market_startup_race_robot_restart_game_down")
        res = self.safe_call("marketStatus", {})
        self.log("market_startup_iteminfo_race status_game_down=%s" % json_text(res, 1600))
        res = self.safe_call("marketSyncItemInfo", {})
        self.log("market_startup_iteminfo_race sync_iteminfo_game_down result=%s" % json_text(res, 2400))
        self.sample_with_event("market_startup_race_after_iteminfo")
        self.shell("cd /root && (./run >/dev/null 2>&1 || true); sleep 30; ss -lntp | grep -E ':(%s)' || true" % core_ports, 240)
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

    def market_call_when_idle(self, command, payload, label, attempts=12, delay_sec=5):
        last = {}
        for idx in range(attempts):
            last = self.safe_call(command, payload)
            result = (last.get("result") or {}) if isinstance(last, dict) else {}
            status = safe_text(result.get("status") or "")
            error = safe_text(last.get("error") if isinstance(last, dict) else "")
            if "timed out" in error:
                self.log("%s command=%s timeout_wait_idle result=%s" % (label, command, json_text(last, 1200)))
                self.wait_market_job_idle(label + ":" + command, 300, 5)
                return last
            if status != "busy" and "market job already running" not in error:
                if idx > 0:
                    self.log("%s command=%s accepted_after=%s result=%s" % (label, command, idx, json_text(last, 1600)))
                return last
            self.log("%s command=%s busy attempt=%s result=%s" % (label, command, idx, json_text(last, 1200)))
            time.sleep(delay_sec)
        return last

    def wait_market_job_idle(self, event, timeout_sec=300, interval_sec=5):
        self.log("wait_market_job_idle start event=%s timeout=%s" % (event, timeout_sec))
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            status = self.market_status_result()
            last = status
            job = status.get("last_job") or {}
            if job.get("status") != "running":
                self.log("wait_market_job_idle ready event=%s job=%s" % (event, json_text(job, 1200)))
                return True
            time.sleep(interval_sec)
        self.log("wait_market_job_idle timeout event=%s status=%s" % (event, json_text(last, 1600)))
        return False

    def market_status_result(self):
        res = self.safe_call("marketStatus", {})
        return (res.get("result") or {}) if isinstance(res, dict) else {}

    def market_result_has_special_success(self, result):
        payload = (result.get("result") or {}) if isinstance(result, dict) else {}
        actions = payload.get("actions") or []
        for entry in actions:
            action = entry.get("action") or {}
            if entry.get("ok") and action.get("market") == "auction" and action.get("kind") in ("title", "creature", "avatar", "artifact red", "artifact blue", "artifact green"):
                return True
        return False

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
        market_ports = self.port_regex(("Point", "Auction"))
        script = "for p in $(pidof df_auction_r df_point_r 2>/dev/null); do kill -TERM $p || true; done; sleep 8; for p in $(pidof df_auction_r df_point_r 2>/dev/null); do kill -KILL $p || true; done; pkill -TERM -f '^/root/robot --bounded-log-sink /root/config/market_(auction|point)_service.log ' 2>/dev/null || true; sleep 1; pkill -KILL -f '^/root/robot --bounded-log-sink /root/config/market_(auction|point)_service.log ' 2>/dev/null || true; ss -lntp | grep -E ':(%s)' || true; pgrep -af 'df_auction_r|df_point_r' || true" % market_ports
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
            self.validate_market_action_prices(res, "market_missing_catalog")
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
            self.validate_market_action_prices(res, "market_partial_catalog")
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
        self.shell("mysql -ugame -puu5!^%jg -e \"DELETE FROM taiwan_cain_auction_gold.auction_main WHERE owner_id >= 90000001; DELETE FROM taiwan_cain_auction_cera.auction_main WHERE owner_id >= 90000001; DELETE FROM taiwan_cain_2nd.creature_items WHERE charac_no >= 90000001;\"", 60)
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
        backup_output = self.shell(filtered_config_backup_script("/root/config", backup), 120)
        if "CONFIG_BACKUP_OK" not in safe_text(backup_output):
            self.log("config_dir_fault skipped: config backup failed output=%s" % safe_text(backup_output)[:1000])
            return
        backup_ready = True
        self.stop_market_services()
        try:
            script = """
pids=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
[ -z "$pids" ] || kill -TERM $pids || true
pkill -TERM -f '^/root/robot --web-admin' 2>/dev/null || true
pkill -TERM -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true
sleep 5
left=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
[ -z "$left" ] || kill -KILL $left || true
pkill -KILL -f '^/root/robot --web-admin' 2>/dev/null || true
pkill -KILL -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true
mkdir -p /root/config
find /root/config -mindepth 1 -maxdepth 1 \
  ! -name 'log_robot*' \
  ! -name 'market_log.jsonl*' \
  ! -name 'market_*_service.log*' \
  ! -name '*.rotate.tmp' \
  ! -name '*.trim.tmp' \
  -exec rm -rf -- {} + 2>/dev/null || true
printf '{broken config dir' > /root/config/market_config.json
"""
            self.shell(script, 120)
            self.sample_with_event("config_dir_fault_broken")
            self.robot_restart_without_target("config_dir_fault_restart")
            self.burst_sample("config_dir_fault", self.scaled_seconds(20, 60), 10)
        finally:
            if backup_ready:
                script = """
pids=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
[ -z "$pids" ] || kill -TERM $pids || true
pkill -TERM -f '^/root/robot --web-admin' 2>/dev/null || true
pkill -TERM -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true
sleep 5
left=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
[ -z "$left" ] || kill -KILL $left || true
pkill -KILL -f '^/root/robot --web-admin' 2>/dev/null || true
pkill -KILL -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true
mkdir -p /root/config
find /root/config -mindepth 1 -maxdepth 1 \
  ! -name 'log_robot*' \
  ! -name 'market_log.jsonl*' \
  ! -name 'market_*_service.log*' \
  ! -name '*.rotate.tmp' \
  ! -name '*.trim.tmp' \
  -exec rm -rf -- {} + 2>/dev/null || true
if [ -d %s ]; then
  if cp -af %s/. /root/config/; then
    rm -rf %s
    echo CONFIG_RESTORE_OK
  else
    echo CONFIG_RESTORE_FAILED
    exit 1
  fi
else
  echo CONFIG_RESTORE_FAILED
  exit 1
fi
""" % (shell_quote(backup), shell_quote(backup), shell_quote(backup))
                restore_output = self.shell(script, 120)
                if "CONFIG_RESTORE_OK" in safe_text(restore_output):
                    self.robot_restart_without_target("config_dir_fault_restore")
                    self.market_enable_auto(max_concurrent=8)
                else:
                    self.record_failure("config_dir_fault_restore", "config restore failed; backup retained at %s" % backup)

    def cleanup_stale_artifacts(self):
        config_backups = []
        seen_backups = set()
        now = time.time()
        for pattern in CONFIG_FAULT_BACKUP_GLOBS:
            for path in glob.glob(pattern):
                absolute = os.path.abspath(path)
                if absolute in seen_backups or os.path.islink(absolute):
                    continue
                try:
                    modified = os.path.getmtime(absolute)
                except OSError:
                    continue
                name = os.path.basename(absolute)
                if CONFIG_FAULT_BACKUP_TEMP_RE.match(name):
                    if now - modified >= 3600:
                        try:
                            if os.path.isdir(absolute):
                                shutil.rmtree(absolute)
                            else:
                                os.remove(absolute)
                        except OSError as exc:
                            self.log("cleanup_stale_artifacts temp_remove_failed path=%s err=%r" % (absolute, exc))
                    continue
                if not CONFIG_FAULT_BACKUP_NAME_RE.match(name):
                    continue
                seen_backups.add(absolute)
                config_backups.append((modified, absolute))
        config_backups.sort(key=lambda item: (item[0], item[1]), reverse=True)
        grouped_backups = {}
        for modified, path in config_backups:
            grouped_backups.setdefault(backup_source_key(path), []).append((modified, path))
        protected_backups = set()
        for entries in grouped_backups.values():
            protected_backups.update(path for _, path in entries[:CONFIG_FAULT_BACKUP_KEEP])
            largest = max(entries, key=lambda item: path_size(item[1]))
            protected_backups.add(largest[1])
        removed_config_backups = []
        for _, path in config_backups:
            if path in protected_backups:
                continue
            try:
                if os.path.isdir(path):
                    shutil.rmtree(path)
                else:
                    os.remove(path)
                removed_config_backups.append(path)
            except OSError as exc:
                self.log("cleanup_stale_artifacts backup_remove_failed path=%s err=%r" % (path, exc))

        current = os.path.realpath(os.path.abspath(self.out_dir))
        candidates = []
        for path in glob.glob(STABILITY_OUTPUT_GLOB):
            absolute = os.path.abspath(path)
            if os.path.dirname(absolute) != "/root":
                continue
            if not STABILITY_OUTPUT_NAME_RE.match(os.path.basename(absolute)):
                continue
            if os.path.islink(absolute) or not os.path.isdir(absolute):
                continue
            try:
                modified = os.path.getmtime(absolute)
            except OSError:
                continue
            candidates.append((modified, absolute))
        candidates.sort(key=lambda item: (item[0], item[1]), reverse=True)

        protected = []
        for _, path in candidates:
            real_path = os.path.realpath(path)
            if current == real_path or current.startswith(real_path + os.sep):
                protected.append(path)
                break
        for _, path in candidates:
            if path in protected:
                continue
            if len(protected) >= STABILITY_OUTPUT_KEEP:
                break
            protected.append(path)
        protected = set(protected)

        removed = []
        for _, path in candidates:
            if path in protected:
                continue
            try:
                shutil.rmtree(path)
                removed.append(path)
            except OSError as exc:
                self.log("cleanup_stale_artifacts remove_failed path=%s err=%r" % (path, exc))
        self.log("cleanup_stale_artifacts config_backups_retained=%s config_backups_removed=%s outputs_retained=%s outputs_removed=%s" % (
            len(protected_backups),
            len(removed_config_backups),
            len(protected),
            len(removed),
        ))

    def artifact_budget_exceeded(self):
        if self.artifact_max_bytes <= 0:
            return False
        total = 0
        for root, dirs, files in os.walk(self.out_dir):
            dirs[:] = [name for name in dirs if not os.path.islink(os.path.join(root, name))]
            for name in files:
                path = os.path.join(root, name)
                if os.path.islink(path):
                    continue
                try:
                    total += os.path.getsize(path)
                except OSError:
                    continue
                if total > self.artifact_max_bytes:
                    if not self.artifact_limit_reported:
                        self.artifact_limit_reported = True
                        self.log("artifact budget reached bytes=%s limit=%s" % (total, self.artifact_max_bytes))
                    return True
        return False

    def web_api_fault(self):
        self.log("web_api_fault begin")
        self.sample_with_event("web_api_fault_before")
        api_ports = self.port_regex(("RobotAPI", "Web"))
        self.shell("pkill -TERM -f '/root/robot --web-admin' || true; sleep 5; pgrep -af '/root/robot' || true; ss -lntp | grep -E ':(%s)' || true" % api_ports, 30)
        for command in ("systemStatus", "autoStatus", "marketStatus", "databaseStatus"):
            res = self.safe_call(command, {})
            self.log("web_api_fault api command=%s result=%s" % (command, json_text(res, 1200)))
        self.robot_restart_without_target("web_api_fault_restart")
        self.burst_sample("web_api_fault_recover", self.scaled_seconds(30, 90), 10)

    def port_conflict_fault(self):
        self.log("port_conflict_fault begin")
        self.stop_market_services()
        conflict_ports = sorted(set([self.port("Point"), self.port("Auction")]))
        conflict_ports = [p for p in conflict_ports if p > 0]
        if not conflict_ports:
            self.log("port_conflict_fault skipped no point/auction ports")
            return
        tuple_text = ",".join(str(p) for p in conflict_ports)
        if len(conflict_ports) == 1:
            tuple_text += ","
        cmd = "cat >/tmp/vm_random_port_conflict.py <<'PY'\nimport socket,time\ns=[]\nfor p in (%s):\n    x=socket.socket(); x.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); x.bind(('0.0.0.0', p)); x.listen(1); s.append(x)\ntime.sleep(90)\nPY\nnohup python /tmp/vm_random_port_conflict.py >/dev/null 2>&1 &" % tuple_text
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

    def robot_scale_wave(self, low, mid, high):
        self.log("robot_scale_wave begin low=%s mid=%s high=%s" % (low, mid, high))
        wave = [low, mid, high, low, max(mid, high - 50), self.args.target_min]
        for idx, target in enumerate(wave):
            self.set_target(target)
            self.burst_sample("robot_scale_wave:%s:%s" % (idx, target), self.scaled_seconds(15, 45), 5)
        self.log("robot_scale_wave done")

    def robot_action_storm(self):
        self.log("robot_action_storm begin")
        for round_idx in range(4):
            uids = self.select_uids(24)
            if not uids:
                self.robot_call("robotsOnlineAsync", {"count": 20}, "robot_action_storm")
                time.sleep(10)
                uids = self.select_uids(24)
            if not uids:
                self.log("robot_action_storm skipped round=%s no uids" % round_idx)
                continue
            random.shuffle(uids)
            calls = [
                ("robotsMove", {"uids": uids[:16]}),
                ("robotsShout", {"uids": uids[0:8]}),
                ("robotsShoutLocal", {"uids": uids[8:16]}),
                ("robotsShoutWorld", {"uids": uids[0:4]}),
                ("robotsStoreAsync", {"uids": uids[4:12]}),
                ("robotsLogoutAsync", {"uids": uids[16:20]}),
                ("robotsOnlineAsync", {"count": random.randint(6, 18)}),
            ]
            for command, payload in calls:
                self.robot_call(command, payload, "robot_action_storm:%s" % round_idx)
                time.sleep(random.randint(1, 4))
        self.burst_sample("robot_action_storm_recover", self.scaled_seconds(45, 120), 10)
        self.log("robot_action_storm done")

    def robot_manual_mode_drill(self):
        self.log("robot_manual_mode_drill begin")
        stop = self.safe_call("autoStop", {})
        self.log("robot_manual_mode_drill autoStop=%s" % json_text(stop, 1200))
        self.sample_with_event("robot_manual_mode:auto_stop")
        try:
            self.robot_call("robotsOnlineAsync", {"count": 12}, "robot_manual_mode")
            time.sleep(10)
            uids = self.select_uids(16, prefer_running=False)
            if uids:
                self.robot_call("robotsMove", {"uids": uids[:10]}, "robot_manual_mode")
                self.robot_call("robotsShoutLocal", {"uids": uids[0:8]}, "robot_manual_mode")
                self.robot_call("robotsShoutWorld", {"uids": uids[0:3]}, "robot_manual_mode")
                self.robot_call("robotsStoreAsync", {"uids": uids[4:10]}, "robot_manual_mode")
                self.robot_call("robotsLogoutAsync", {"uids": uids[10:14]}, "robot_manual_mode")
                time.sleep(10)
                self.robot_call("robotsOnlineAsync", {"uids": uids[10:14]}, "robot_manual_mode")
            else:
                self.log("robot_manual_mode_drill no uids after online")
            self.burst_sample("robot_manual_mode_hold", self.scaled_seconds(20, 60), 10)
        finally:
            start = self.safe_call("autoStart", {})
            self.log("robot_manual_mode_drill autoStart=%s" % json_text(start, 1200))
            self.burst_sample("robot_manual_mode_recover", self.scaled_seconds(30, 90), 10)
        self.log("robot_manual_mode_drill done")

    def robot_store_lifecycle_storm(self):
        self.log("robot_store_lifecycle_storm begin")
        uids = self.select_uids(36)
        if not uids:
            self.log("robot_store_lifecycle_storm skipped no uids")
            return
        self.robot_call("robotsStoreAsync", {"uids": uids[:18]}, "robot_store_lifecycle")
        self.burst_sample("robot_store_lifecycle_store", self.scaled_seconds(20, 60), 10)
        self.assert_store_presence("robot_store_lifecycle")
        self.robot_call("robotsLogoutAsync", {"uids": uids[6:14]}, "robot_store_lifecycle")
        time.sleep(10)
        if not self.args.no_cleanup:
            clean_uids = uids[10:14]
            res = self.safe_call("cleanupRobots", {"uids": clean_uids, "force": True})
            deleted = int((((res or {}).get("result") or {}).get("deleted")) or 0)
            self.deleted_total += deleted
            self.log("robot_store_lifecycle cleanup uids=%s deleted=%s result=%s" % (clean_uids, deleted, json_text(res, 1600)))
            self.sample_with_event("robot_store_lifecycle:cleanup")
        self.robot_call("robotsOnlineAsync", {"count": 12}, "robot_store_lifecycle")
        self.burst_sample("robot_store_lifecycle_recover", self.scaled_seconds(45, 120), 10)
        self.log("robot_store_lifecycle_storm done")

    def robot_cleanup_edge_cases(self):
        self.log("robot_cleanup_edge_cases begin")
        if self.args.no_cleanup:
            self.log("robot_cleanup_edge_cases skipped no_cleanup=true")
            return
        uids = self.select_uids(6, prefer_running=False)
        cases = [
            {"uids": [999999991, 999999992], "force": True},
            {"uids": ([uids[0], uids[0]] if uids else [999999993, 999999993]), "force": True},
            {"uids": (uids[:2] if len(uids) >= 2 else [999999994]), "force": False},
            {"uids": [999999995], "force": True},
        ]
        for idx, payload in enumerate(cases):
            res = self.safe_call("cleanupRobots", payload)
            deleted = int((((res or {}).get("result") or {}).get("deleted")) or 0)
            self.deleted_total += deleted
            self.log("robot_cleanup_edge_cases idx=%s payload=%s deleted=%s result=%s" % (idx, payload, deleted, json_text(res, 1800)))
            self.sample_with_event("robot_cleanup_edge:%s" % idx)
            time.sleep(4)
        self.safe_call("autoStart", {})
        self.burst_sample("robot_cleanup_edge_recover", self.scaled_seconds(30, 90), 10)
        self.log("robot_cleanup_edge_cases done")

    def robot_restart_under_load(self, high):
        self.log("robot_restart_under_load begin high=%s" % high)
        self.set_target(high)
        uids = self.select_uids(24)
        if uids:
            self.robot_call("robotsMove", {"uids": uids[:16]}, "robot_restart_under_load")
            self.robot_call("robotsStoreAsync", {"uids": uids[4:16]}, "robot_restart_under_load")
            self.robot_call("robotsShoutWorld", {"uids": uids[:4]}, "robot_restart_under_load")
        self.market_enable_auto(max_concurrent=8)
        self.robot_restart_without_target("robot_restart_under_load_restart")
        self.wait_robot_api("robot_restart_under_load_api", 120, 5)
        self.safe_call("autoStart", {})
        self.market_enable_auto(max_concurrent=8)
        self.burst_sample("robot_restart_under_load_recover", self.scaled_seconds(60, 150), 10)
        self.log("robot_restart_under_load done")

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
        monitor_port = self.port_text("Monitor")
        self.shell("pkill -TERM -f './df_monitor_r mnt_cain start' || true; sleep 8; ss -lntp | grep ':%s' || true" % monitor_port, 30)
        status = self.safe_call("robotsStatus", {"count": 20})
        rows = (((status or {}).get("result") or {}).get("robots") or [])
        uids = [int(r.get("uid") or 0) for r in rows if int(r.get("uid") or 0) > 0][:1]
        if uids:
            res = self.safe_call("robotsShoutWorld", {"uids": uids})
            self.log("monitor_fault world_shout_down uids=%s result=%s" % (uids, json_text(res, 1600)))
        self.log("monitor_fault restore /root/run")
        self.shell("cd /root && (./run >/dev/null 2>&1 || true); sleep 20; ss -lntp | grep ':%s' || true; pgrep -af 'df_monitor_r|df_game_r' || true" % monitor_port, 180)
        self.sample_with_event("monitor_fault_restore")
        self.burst_sample("monitor_fault_recover", 60, 5)

    def game_port_fault(self):
        self.log("game_port_fault stop /root/stop")
        self.sample_with_event("game_port_fault_stop")
        game_port = self.port_text("Game")
        game_ports = self.port_regex(("Game", "Monitor"))
        self.shell("cd /root && (./stop >/dev/null 2>&1 || true); sleep 15; ss -lntp | grep ':%s' || true; pgrep -af 'df_game_r' || true" % game_port, 180)
        self.sample_with_event("game_port_down")
        time.sleep(self.scaled_seconds(45, 120))
        self.log("game_port_fault restore /root/run")
        self.shell("cd /root && (./run >/dev/null 2>&1 || true); sleep 30; ss -lntp | grep -E ':(%s)' || true; pgrep -af 'df_game_r|df_monitor_r' || true" % game_ports, 240)
        self.sample_with_event("game_port_fault_restore")
        self.burst_sample("game_port_fault_recover", self.scaled_seconds(60, 120), 10)

    def backup_file(self, path):
        backup = "%s.vm_random_backup_%s" % (path, int(time.time() * 1000))
        script = """
if [ -e %(path)s ]; then
  if mv -f %(path)s %(backup)s && : > %(path)s; then
    echo BACKUP_OK
  else
    [ ! -e %(path)s ] && mv -f %(backup)s %(path)s 2>/dev/null || true
    echo BACKUP_FAILED
  fi
fi
""" % {"path": shell_quote(path), "backup": shell_quote(backup)}
        out = self.shell(script, 20)
        if "BACKUP_OK" in [line.strip() for line in safe_text(out).splitlines()]:
            self.log("backup_file path=%s backup=%s" % (path, backup))
            return backup
        self.log("backup_file missing path=%s" % path)
        return ""

    def restore_file(self, path, backup):
        if not backup:
            self.log("restore_file skipped path=%s backup_empty" % path)
            return
        script = "[ -e %s ] && rm -f %s && mv -f %s %s && echo RESTORED || echo MISSING_BACKUP" % (
            shell_quote(backup), shell_quote(path), shell_quote(backup), shell_quote(path)
        )
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
  echo 'DELETE FROM taiwan_cain_2nd.creature_items WHERE charac_no >= 90000001;';
  echo 'USE taiwan_cain_auction_gold;';
  mysqldump -ugame -puu5!^%%jg --skip-triggers --no-create-info --replace --where='owner_id >= 90000001' taiwan_cain_auction_gold auction_main 2>/dev/null || true;
  echo 'USE taiwan_cain_auction_cera;';
  mysqldump -ugame -puu5!^%%jg --skip-triggers --no-create-info --replace --where='owner_id >= 90000001' taiwan_cain_auction_cera auction_main 2>/dev/null || true;
  echo 'USE taiwan_cain_2nd;';
  mysqldump -ugame -puu5!^%%jg --skip-triggers --no-create-info --replace --where='charac_no >= 90000001' taiwan_cain_2nd creature_items 2>/dev/null || true;
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
        hot_ports = self.port_regex(("RobotAPI", "Web", "Game", "Monitor", "Point", "Auction", "Relay", "PartyRoute0"))
        script = r"""
pids=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
[ -z "$pids" ] || kill -TERM $pids || true
pkill -TERM -f '^/root/robot --web-admin' 2>/dev/null || true
pkill -TERM -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true
sleep 8
left=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
[ -z "$left" ] || kill -KILL $left || true
pkill -KILL -f '^/root/robot --web-admin' 2>/dev/null || true
pkill -KILL -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true
mkdir -p /root/config
nohup sh -c '/root/robot 2>&1 | /root/robot --bounded-log-sink /root/config/robot_stdout.log' >/dev/null 2>/root/config/robot_start_error.log &
sleep 12
pgrep -af '/root/robot|df_game_r|df_monitor_r|df_auction_r|df_point_r|df_relay_r' || true
ss -lntp | grep -E ':(%s)' || true
""" % hot_ports
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
            "sample target=%s actors=%s leased=%s running=%s connecting=%s mode=%s market_auto=%s auction=%s/%s cand=%s special=%s specialdb=%s high=%s creature=%s inst=%s orphan=%s q=%s/%s/%s health=%s/%s policy=%s stg=%s failr=%s act=%s/%s/%s cera=%s/%s health=%s/%s policy=%s act=%s/%s/%s load=%s/%s/%s top=%s hits=%s api_error=%s"
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
                row.get("market_auction_candidates"),
                row.get("market_auction_special_candidates"),
                row.get("market_auction_special_records"),
                row.get("market_auction_high_addinfo"),
                row.get("market_auction_creature_records"),
                row.get("market_creature_instances"),
                row.get("market_creature_orphans"),
                row.get("market_auction_queue_normal"),
                row.get("market_auction_queue_special"),
                row.get("market_auction_queue_rejected"),
                row.get("market_auction_health"),
                row.get("market_auction_completion"),
                row.get("market_auction_policy"),
                row.get("market_auction_stagnant"),
                row.get("market_auction_failure_rounds"),
                row.get("market_auction_last_plan"),
                row.get("market_auction_last_results"),
                row.get("market_auction_last_failed"),
                row.get("market_cera_records"),
                row.get("market_cera_kinds"),
                row.get("market_cera_health"),
                row.get("market_cera_completion"),
                row.get("market_cera_policy"),
                row.get("market_cera_last_plan"),
                row.get("market_cera_last_results"),
                row.get("market_cera_last_failed"),
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
        for path in ("/root/config/log_robot", "/root/config/robot_stdout.log"):
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
        row["robot_pid_cpu"] = self.proc_pid_cpu("^/root/robot$|^./robot$")
        row["df_game_cpu"] = self.proc_pid_cpu("df_game_r")
        row["auction_cpu"] = self.proc_pid_cpu("df_auction_r")
        row["point_cpu"] = self.proc_pid_cpu("df_point_r")
        self.fill_database_row(row)
        self.fill_market_row(row)
        self.fill_tcp_row(row)
        self.fill_port_row(row)
        self.fill_party_row(row)
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

    def sum_ints(self, *values):
        total = 0
        for value in values:
            try:
                total += int(value or 0)
            except Exception:
                pass
        return total

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
            row["market_auction_high_addinfo"] = counts.get("auction_high_addinfo_records", "")
            row["market_auction_creature_records"] = counts.get("auction_creature_records", "")
            row["market_auction_special_records"] = self.sum_ints(row.get("market_auction_high_addinfo"), row.get("market_auction_creature_records"))
            row["market_creature_instances"] = counts.get("creature_instances_records", "")
            row["market_creature_orphans"] = counts.get("creature_orphans_records", "")
            row["market_cera_records"] = counts.get("cera_records", "")
            row["market_cera_kinds"] = counts.get("cera_kinds", "")
            policy = market.get("policy") or {}
            auction_policy = policy.get("auction") or {}
            cera_policy = policy.get("cera") or {}
            row["market_auction_candidates"] = auction_policy.get("candidates", "")
            row["market_auction_special_candidates"] = auction_policy.get("special_candidates", "")
            row["market_auction_queue_normal"] = auction_policy.get("queue_normal", "")
            row["market_auction_queue_special"] = auction_policy.get("queue_special", "")
            row["market_auction_queue_rejected"] = auction_policy.get("queue_rejected", "")
            row["market_auction_stagnant"] = auction_policy.get("stagnant_rounds", "")
            row["market_auction_policy"] = auction_policy.get("mode", "")
            row["market_auction_policy_reason"] = (auction_policy.get("reason") or "")[:160]
            row["market_auction_health"] = auction_policy.get("health", "")
            row["market_auction_completion"] = auction_policy.get("completion", "")
            row["market_auction_failure_rounds"] = auction_policy.get("action_failure_rounds", "")
            row["market_auction_last_job"] = auction_policy.get("last_job_status", "")
            row["market_auction_last_plan"] = auction_policy.get("last_plan_actions", "")
            row["market_auction_last_results"] = auction_policy.get("last_action_results", "")
            row["market_auction_last_failed"] = auction_policy.get("last_action_failed", "")
            row["market_cera_policy"] = cera_policy.get("mode", "")
            row["market_cera_policy_reason"] = (cera_policy.get("reason") or "")[:160]
            row["market_cera_health"] = cera_policy.get("health", "")
            row["market_cera_completion"] = cera_policy.get("completion", "")
            row["market_cera_last_job"] = cera_policy.get("last_job_status", "")
            row["market_cera_last_plan"] = cera_policy.get("last_plan_actions", "")
            row["market_cera_last_results"] = cera_policy.get("last_action_results", "")
            row["market_cera_last_failed"] = cera_policy.get("last_action_failed", "")
        except Exception as exc:
            if not row.get("api_error"):
                row["api_error"] = "marketStatus:%r" % (exc,)

    def fill_tcp_row(self, row):
        try:
            out = subprocess.check_output("ss -ant", shell=True)
            if not isinstance(out, str):
                out = out.decode("utf-8", "replace")
            states = {}
            port_fields = {
                "tcp_8111_estab": self.port("RobotAPI"),
                "tcp_10011_estab": self.port("Game"),
                "tcp_7200_estab": self.port("Relay"),
                "tcp_30603_estab": self.port("Point"),
                "tcp_30803_estab": self.port("Auction"),
            }
            port_counts = dict((key, 0) for key in port_fields)
            for line in out.splitlines()[1:]:
                parts = line.split()
                if not parts:
                    continue
                state = parts[0]
                states[state] = states.get(state, 0) + 1
                for field, port in port_fields.items():
                    if port > 0 and (":" + str(port)) in line and state == "ESTAB":
                        port_counts[field] += 1
            row["tcp_estab"] = states.get("ESTAB", 0)
            row["tcp_time_wait"] = states.get("TIME-WAIT", 0)
            row["tcp_close_wait"] = states.get("CLOSE-WAIT", 0)
            for field, count in port_counts.items():
                row[field] = count
        except Exception as exc:
            if not row.get("api_error"):
                row["api_error"] = "tcp:%r" % (exc,)

    def fill_port_row(self, row):
        try:
            out = subprocess.check_output("ss -ltn", shell=True)
            if not isinstance(out, str):
                out = out.decode("utf-8", "replace")
            row["port_10011"] = int((":" + self.port_text("Game")) in out)
            row["port_30303"] = int((":" + self.port_text("Monitor")) in out)
            row["port_7200"] = int((":" + self.port_text("Relay")) in out)
            row["port_30603"] = int((":" + self.port_text("Point")) in out)
            row["port_30803"] = int((":" + self.port_text("Auction")) in out)
        except Exception:
            pass

    def fill_party_row(self, row):
        try:
            counts = self.party_log_counts()
            row["party_log_hits"] = counts.get("party_total", "")
            row["party_error_hits"] = self.sum_ints(counts.get("relay_errors"), counts.get("udp_errors"), counts.get("skill_errors"), counts.get("supervisor_panics"))
            row["party_relay_error_hits"] = counts.get("relay_errors", "")
            row["party_tqos_exhausted_hits"] = counts.get("tqos_exhausted", "")
            row["party_route_degraded_hits"] = counts.get("route_degraded", "")
            row["party_route_recovery_hits"] = counts.get("route_recovery", "")
            row["party_route_recovered_hits"] = counts.get("route_recovered", "")
            row["party_relay_connected_hits"] = counts.get("relay_connected", "")
            row["party_probe_cycle_hits"] = counts.get("probe_cycles", "")
            row["party_peer_ready_hits"] = counts.get("peer_ready", "")
            row["party_self_id_refresh_hits"] = counts.get("self_id_refresh", "")
            row["party_udp_recycle_hits"] = counts.get("udp_recycles", "")
            row["party_supervisor_panic_hits"] = counts.get("supervisor_panics", "")
            row["party_skill_hits"] = counts.get("skill_casts", "")
            row["party_skill_error_hits"] = counts.get("skill_errors", "")
        except Exception as exc:
            if not row.get("api_error"):
                row["api_error"] = "party:%r" % (exc,)

    def robot_fd_count(self):
        try:
            out = subprocess.check_output("pgrep -f '^/root/robot$|^./robot$' | head -1", shell=True)
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
        hot_ports = self.port_regex(("RobotAPI", "Web", "Game", "Monitor", "Point", "Auction", "Relay", "PartyRoute0")) or "8111|8112|10011|30303|30603|30803|7200|5063"
        command = """
echo '===== %s %s uptime ====='
date
uptime
echo '===== ps top ====='
ps -eo pid,ppid,pcpu,pmem,nlwp,comm,args --sort=-pcpu | head -n 25
echo '===== tcp states ====='
ss -ant | awk 'NR>1 {c[$1]++} END {for (k in c) print k,c[k]}'
echo '===== tcp hot ports ====='
ss -ant | grep -E ':(%s)' | head -n 120 || true
echo '===== fds ====='
for p in $(pgrep -f '^/root/robot$|^./robot$|df_game_r|df_auction_r|df_point_r|df_relay_r' 2>/dev/null); do echo "$p $(ps -p $p -o comm=) fds=$(ls /proc/$p/fd 2>/dev/null | wc -l)"; done
echo '===== robot log filtered ====='
tail -n %s /root/config/log_robot 2>/dev/null | grep -a -E '%s' | tail -n 200 || true
echo '===== market log filtered ====='
tail -n %s /root/config/market_log.jsonl 2>/dev/null | grep -a -E 'market_service|job_end|auto_run|special|creature|iteminfo|cannot assign requested address|too many open files|connection reset' | tail -n 200 || true
echo '===== market service logs ====='
tail -n 80 /root/config/market_auction_service.log 2>/dev/null || true
tail -n 80 /root/config/market_point_service.log 2>/dev/null || true
echo '===== market special db ====='
mysql -ugame -puu5!^%%jg -e "SELECT 'auction_high_addinfo',COUNT(*),COUNT(DISTINCT item_id) FROM taiwan_cain_auction_gold.auction_main WHERE owner_id>=90000001 AND add_info>=210000000; SELECT 'auction_creature',COUNT(*),COUNT(DISTINCT a.item_id) FROM taiwan_cain_auction_gold.auction_main a INNER JOIN taiwan_cain_2nd.creature_items c ON c.ui_id=a.add_info AND c.charac_no=a.owner_id WHERE a.owner_id>=90000001; SELECT 'creature_instances',COUNT(*),COUNT(DISTINCT it_id) FROM taiwan_cain_2nd.creature_items WHERE charac_no>=90000001; SELECT 'creature_orphans',COUNT(*),COUNT(DISTINCT c.it_id) FROM taiwan_cain_2nd.creature_items c LEFT JOIN taiwan_cain_auction_gold.auction_main a ON a.add_info=c.ui_id AND a.owner_id=c.charac_no WHERE c.charac_no>=90000001 AND a.auction_id IS NULL;" 2>/dev/null || true
echo '===== market files ====='
ls -l /root/config/market_config.json /root/config/pvf_*catalog.json /root/config/pvf_iteminfo.dat /home/neople/auction/iteminfo.dat /home/neople/point/iteminfo.dat 2>/dev/null || true
echo '===== web stdout filtered ====='
tail -n %s /root/config/robot_stdout.log 2>/dev/null | grep -a -E '%s|request pid|auth rejected|web admin exited' | tail -n 120 || true
""" % (
            label,
            now_text(),
            hot_ports,
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
            "round_orders": self.round_orders,
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
        lines.append("- seed: %s" % self.args.seed)
        lines.append("- min_rounds: %s" % self.args.rounds)
        lines.append("- time_limit_sec: %s" % self.time_limit_sec)
        lines.append("- completed_rounds_seen: %s" % len(self.round_orders))
        lines.append("- events: %s" % len(self.results))
        lines.append("- failures: %s" % len(failures))
        lines.append("")
        lines.append("## Round Orders")
        lines.append("")
        for item in self.round_orders:
            lines.append("- round %s: %s" % (item.get("round"), ", ".join(item.get("order") or [])))
        lines.append("")
        lines.append("## Events")
        lines.append("")
        for item in self.results:
            status = "FAIL" if item.get("error") or not item.get("recovered") else "OK"
            lines.append("- %s round=%s %s duration=%ss recovered=%s error=%s" % (
                status,
                item.get("round", ""),
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
    parser.add_argument("rounds", nargs="?", type=int, default=1, help="minimum complete shuffled rounds to run; default: 1")
    parser.add_argument("time_limit", nargs="?", default="", help="optional deadline duration, for example 30m, 6h, 1d")
    parser.add_argument("--robot-host", default="127.0.0.1", help=argparse.SUPPRESS)
    parser.add_argument("--robot-port", type=int, default=8111, help=argparse.SUPPRESS)
    parser.add_argument("--api-timeout", type=float, default=20.0, help=argparse.SUPPRESS)
    parser.add_argument("--out-dir", default="", help=argparse.SUPPRESS)
    parser.add_argument("--sample-interval", type=int, default=10, help=argparse.SUPPRESS)
    parser.add_argument("--log-snapshot-interval", type=int, default=5 * 60, help=argparse.SUPPRESS)
    parser.add_argument("--target-min", type=int, default=100, help=argparse.SUPPRESS)
    parser.add_argument("--target-max", type=int, default=600, help=argparse.SUPPRESS)
    parser.add_argument("--target-min-interval", type=int, default=20 * 60, help=argparse.SUPPRESS)
    parser.add_argument("--target-max-interval", type=int, default=40 * 60, help=argparse.SUPPRESS)
    parser.add_argument("--cleanup-min-interval", type=int, default=30 * 60, help=argparse.SUPPRESS)
    parser.add_argument("--cleanup-max-interval", type=int, default=45 * 60, help=argparse.SUPPRESS)
    parser.add_argument("--user-interleave-min-interval", type=int, default=90, help=argparse.SUPPRESS)
    parser.add_argument("--user-interleave-max-interval", type=int, default=180, help=argparse.SUPPRESS)
    parser.add_argument("--market-zero-grace", type=int, default=180, help=argparse.SUPPRESS)
    parser.add_argument("--cleanup-min-count", type=int, default=1, help=argparse.SUPPRESS)
    parser.add_argument("--cleanup-max-count", type=int, default=3, help=argparse.SUPPRESS)
    parser.add_argument("--cleanup-max-total", type=int, default=30, help=argparse.SUPPRESS)
    parser.add_argument("--cleanup-logout-wait", type=int, default=15, help=argparse.SUPPRESS)
    parser.add_argument("--status-count", type=int, default=1000, help=argparse.SUPPRESS)
    parser.add_argument("--log-tail-lines", type=int, default=2000, help=argparse.SUPPRESS)
    parser.add_argument("--artifact-max-mb", type=int, default=DEFAULT_ARTIFACT_MAX_MB, help=argparse.SUPPRESS)
    parser.add_argument("--no-cleanup", action="store_true", help=argparse.SUPPRESS)
    parser.add_argument("--allow-online-cleanup", dest="allow_online_cleanup", action="store_true", default=True, help=argparse.SUPPRESS)
    parser.add_argument("--no-allow-online-cleanup", dest="allow_online_cleanup", action="store_false", help=argparse.SUPPRESS)
    parser.add_argument("--seed", type=int, default=0, help=argparse.SUPPRESS)
    return parser.parse_args()


def main():
    args = parse_args()
    if args.rounds < 1:
        args.rounds = 1
    try:
        parse_time_limit(args.time_limit)
    except ValueError as exc:
        print(safe_text(exc))
        return 2
    if args.target_min > args.target_max:
        args.target_min, args.target_max = args.target_max, args.target_min
    if args.user_interleave_min_interval > args.user_interleave_max_interval:
        args.user_interleave_min_interval, args.user_interleave_max_interval = args.user_interleave_max_interval, args.user_interleave_min_interval
    StabilityRun(args).run()
    return 0


if __name__ == "__main__":
    sys.exit(main())
