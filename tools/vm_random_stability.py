#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""
VM-local random stability pressure for the robot system.

Compatible with the VM's default Python 2.7 and modern Python 3.

Default full scenario:
- run one deterministic, dependency-aware round by default
- pass a round count and optional deadline, for example: `5`, `1 6h`, `3 8h`
- share observation windows across related checks instead of sleeping per action
- collect filtered robot/web/resource logs every 5 minutes
- run load/store, market, DB, service, config and runtime restart phases
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
import posixpath
import random
import re
import shutil
import signal
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
    "CharacterCache",
    "DISJOINT_",
    "market_service",
    "iteminfo",
    "RegistItem",
    "PARTY_",
    "PARTY_RELAY",
    "PARTY_DUNGEON",
    "PARTY_DUNGEON_SKILL",
    "SESSION_CHECK_RESPONSE_ERROR",
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
    "store_item_running",
    "store_disjoint_running",
    "store_success",
    "store_failed",
    "store_expired",
    "store_target",
    "store_concurrent",
    "store_probability_percent",
    "store_status_uids",
    "store_item_status_uids",
    "store_disjoint_status_uids",
    "store_item_display_histogram",
    "store_item_display_active",
    "store_item_display_seven",
    "store_item_display_seven_ratio",
    "store_item_display_zero",
    "store_item_display_out_of_range",
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
    "port_7000",
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
    "party_route_failover_hits",
    "party_relay_connected_hits",
    "party_probe_cycle_hits",
    "party_peer_ready_hits",
    "party_self_id_refresh_hits",
    "party_self_id_recovered_hits",
    "party_self_id_recycle_hits",
    "party_udp_recycle_hits",
    "party_transport_cleared_hits",
    "party_peer_transport_reset_hits",
    "party_supervisor_panic_hits",
    "party_skill_hits",
    "party_skill_error_hits",
    "game_ping_timeout_hits",
    "game_check_disconnect_hits",
    "game_disconnect_from5_hits",
    "game_disconnect_from10_hits",
    "store_err_0x11_hits",
    "store_failed_after_hits",
    "store_failed_after_max_tries",
    "store_restore_ok_hits",
    "store_restore_failed_hits",
    "store_restore_elapsed_ms_max",
    "store_nocache_sent_hits",
    "store_nocache_failed_hits",
    "store_nocache_elapsed_ms_max",
    "store_inventory_refresh_hits",
    "disjoint_same_session_retry_hits",
    "disjoint_session_point_failure_hits",
    "disjoint_cache_invalidation_error_hits",
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
    "/home/*/game/Script.pvf.vm_random_backup_*",
    "/home/*/auction/iteminfo.dat.vm_random_backup_*",
    "/home/*/point/iteminfo.dat.vm_random_backup_*",
)
CONFIG_FAULT_BACKUP_KEEP = 5
CONFIG_FAULT_BACKUP_NAME_RE = re.compile(r"^(?:config\.vm_random_backup_|.+\.vm_random_backup_)\d+$")
CONFIG_FAULT_BACKUP_TEMP_RE = re.compile(r"^config\.vm_random_backup_\d+\.(?:tar\.)?tmp\.\d+$")
STABILITY_OUTPUT_NAME_RE = re.compile(r"^robot_stability_\d{8}-\d{6}$")
DEFAULT_ARTIFACT_MAX_MB = 512
DEFAULT_DF_GAME_R = "/home/neople/game/df_game_r"
DEFAULT_MARKET_ACTION_BUDGET = 512
MYSQL_CLI = "mysql -ugame -puu5!^%jg"
CORE_START_LOG = "/root/vm_core_start.log"
CORE_FILE_PATTERNS = (
    "/home/neople/*/core.*",
    "/home/dxf/*/core.*",
)
STABILITY_SCENARIO_ORDER = (
    "integrated_load_data_matrix",
    "restart_recovery_matrix",
)

STABILITY_SCENARIO_PHASES = {
    "integrated_load_data_matrix": (
        "load_runtime_observation",
        "market_matrix",
        "database_fault_matrix",
    ),
    "restart_recovery_matrix": (
        "config_dir_fault",
        "runtime_recovery_matrix",
    ),
}

STABILITY_SCENARIO_CHOICES = STABILITY_SCENARIO_ORDER + tuple(
    phase
    for scenario in STABILITY_SCENARIO_ORDER
    for phase in STABILITY_SCENARIO_PHASES[scenario]
)

MARKET_SERVICE_TERMINAL_STATES = frozenset((
    "ready",
    "port_ready_process_missing",
    "process_without_port",
    "prepare_failed",
    "start_failed",
    "regist_item_failed",
    "process_exited",
    "port_ready_but_unstable",
    "start_timeout",
))

GAME_CONNECTION_CHECK_PATTERNS = {
    "ping_timeout": r"no response 2th ping",
    "check_disconnect": r"DisConnSig[^\n]*from \(8\)",
}

GAME_STORE_DISCONNECT_PATTERNS = {
    "from5": r"DisConnSig[^\n]*from \(5\)",
    "from10": r"DisConnSig[^\n]*from \(10\)",
}


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


def parse_proc_tcp_table(raw):
    rows = []
    for line in safe_text(raw).splitlines():
        parts = line.split()
        if len(parts) < 10 or parts[0] == "sl":
            continue
        try:
            local_port = int(parts[1].rsplit(":", 1)[1], 16)
            remote_port = int(parts[2].rsplit(":", 1)[1], 16)
        except (IndexError, ValueError):
            continue
        rows.append({
            "local_port": local_port,
            "remote_port": remote_port,
            "state": parts[3],
            "inode": safe_text(parts[9]),
        })
    return rows


def mysql_processlist_client_port(host):
    text = safe_text(host).strip()
    if ":" not in text:
        return 0
    return to_int(text.rsplit(":", 1)[1])


def parse_mysql_processlist(raw):
    rows = []
    for line in safe_text(raw).splitlines():
        parts = line.split("\t", 4)
        if len(parts) < 5:
            continue
        connection_id = to_int(parts[0])
        if connection_id <= 0:
            continue
        rows.append({
            "id": connection_id,
            "user": parts[1],
            "host": parts[2],
            "database": parts[3],
            "command": parts[4],
            "client_port": mysql_processlist_client_port(parts[2]),
        })
    return rows


def match_process_mysql_connections(socket_inodes, tcp_rows, processlist_rows):
    socket_inodes = set(safe_text(inode) for inode in socket_inodes)
    client_ports = set(
        row.get("local_port")
        for row in tcp_rows
        if safe_text(row.get("inode")) in socket_inodes and safe_text(row.get("state")) == "01"
    )
    return [
        row for row in processlist_rows
        if to_int(row.get("client_port")) in client_ports
    ]


def database_status_ready(status):
    result = (status.get("result") or {}) if isinstance(status, dict) else {}
    return bool(
        isinstance(status, dict)
        and status.get("ok")
        and isinstance(result, dict)
        and result.get("ok")
        and result.get("select_verified")
    )


def changed_core_files(baseline, current, reported=None):
    baseline = baseline or {}
    reported = set(reported or ())
    return [
        dict(current[path], path=path)
        for path in sorted(current)
        if path not in reported and (path not in baseline or current[path] != baseline[path])
    ]


def status_row_is_active(row):
    if not isinstance(row, dict) or not row.get("actor_attached") or row.get("missing_core"):
        return False
    runtime_state = safe_text(row.get("runtime_state")).strip().lower()
    if runtime_state:
        return runtime_state in ("running", "store")
    actor_state = safe_text(row.get("actor_state")).strip().lower()
    robot_state = row.get("robot_state") or {}
    actual_state = ""
    if isinstance(robot_state, dict):
        actual_state = safe_text(robot_state.get("actual_state")).strip().lower()
    return actor_state in ("running", "busy") and actual_state in ("", "running")


def status_row_has_store(row):
    if not isinstance(row, dict):
        return False
    if row.get("store_display_ack") or row.get("disjoint_active"):
        return True
    runtime_state = safe_text(row.get("runtime_state")).strip().lower()
    return runtime_state in ("store", "disjoint")


def scheduler_user_commands_ready(status, allow_empty=False):
    """Return whether the scheduler can accept a direct actor command now."""
    if not isinstance(status, dict):
        return False
    if status.get("operation_active"):
        return False
    if to_int(status.get("actor_assigned")) > 0:
        return False
    if to_int(status.get("actor_online")) > 0:
        return False
    if to_int(status.get("actor_releasing")) > 0:
        return False
    if not allow_empty and to_int(status.get("actors")) <= 0:
        return False
    return True


def market_job_terminal_success(job):
    if not isinstance(job, dict):
        return False
    return safe_text(job.get("status")).strip().lower() in (
        "success",
        "partial_failed",
        "pending_db_confirm",
        "planned",
    )


def api_result_timed_out(result):
    return bool(
        isinstance(result, dict)
        and not result.get("ok")
        and "timed out" in safe_text(result.get("error")).lower()
    )


def robot_command_retryable(result):
    if not isinstance(result, dict) or result.get("ok"):
        return False
    payload = result.get("result") or {}
    robots = payload.get("robots") or [] if isinstance(payload, dict) else []
    if robots and all(safe_text(row.get("state")) == "scheduler_busy" for row in robots if isinstance(row, dict)):
        return True
    text = safe_text(result.get("error")) + " " + json_text(payload, 1200)
    return "scheduler_busy" in text or "operation already running" in text


def robot_action_counter(status, command):
    if not isinstance(status, dict):
        return None
    fields = {
        "robotsMove": ("move_success", "move_failed"),
        "robotsShout": ("shout_local_success", "shout_local_failed", "shout_world_success", "shout_world_failed"),
        "robotsShoutLocal": ("shout_local_success", "shout_local_failed"),
        "robotsShoutWorld": ("shout_world_success", "shout_world_failed"),
    }.get(command)
    if not fields:
        return None
    return sum(to_int(status.get(field)) for field in fields)


def select_supported_market_targets(preferred, catalog_ids, iteminfo):
    return [
        (item_id, label)
        for item_id, label in preferred
        if item_id in catalog_ids and iteminfo.get(str(item_id))
    ]


def combined_runtime_ready(state):
    return bool(
        isinstance(state, dict)
        and all(
            state.get(name)
            for name in ("api_ok", "game_ok", "monitor_ok", "bridge_ok", "market_ok", "party_ok", "mailbox_ok", "key_ok", "load_ok")
        )
    )


def high_load_observation_stats(rows, target):
    target = int(target)
    stable = [
        row for row in rows
        if row.get("target") == target and row.get("running", 0) >= high_load_running_floor(target)
    ]
    store_uids = set()
    item_store_uids = set()
    disjoint_store_uids = set()
    for row in stable:
        store_uids.update(to_int(uid) for uid in (row.get("store_uids") or []) if to_int(uid) > 0)
        item_store_uids.update(to_int(uid) for uid in (row.get("item_store_uids") or []) if to_int(uid) > 0)
        disjoint_store_uids.update(to_int(uid) for uid in (row.get("disjoint_store_uids") or []) if to_int(uid) > 0)
    peak_stores = max([row.get("stores", 0) for row in stable] or [0])
    store_success = [row.get("store_success", 0) for row in stable]
    store_success_delta = max(0, to_int(store_success[-1]) - to_int(store_success[0])) if len(store_success) >= 2 else 0
    displayed = sum(row.get("displayed_item_stores", 0) for row in stable)
    seven = sum(row.get("seven_item_stores", 0) for row in stable)
    return {
        "stable_samples": len(stable),
        "peak_stores": peak_stores,
        "peak_item_stores": max([row.get("item_stores", 0) for row in stable] or [0]),
        "peak_disjoint_stores": max([row.get("disjoint_stores", 0) for row in stable] or [0]),
        "unique_stores": len(store_uids),
        "unique_item_stores": len(item_store_uids),
        "unique_disjoint_stores": len(disjoint_store_uids),
        "store_success_delta": store_success_delta,
        "store_activity": max(len(store_uids), peak_stores + store_success_delta),
        "displayed_item_stores": displayed,
        "seven_item_stores": seven,
        "displayed_zero": sum(row.get("displayed_zero", 0) for row in stable),
        "displayed_out_of_range": sum(row.get("displayed_out_of_range", 0) for row in stable),
        "seven_ratio": 100.0 * seven / displayed if displayed else 0.0,
    }


def high_load_running_floor(target):
    return max(1, int(target) * 94 // 100)


def high_load_store_requirements(target):
    target = max(1, int(target))
    minimum_peak = max(3, target // 100)
    return minimum_peak, max(10, minimum_peak * 2)


def high_load_observation_ready(stats, target):
    minimum_peak, minimum_activity = high_load_store_requirements(target)
    return bool(
        stats.get("stable_samples", 0) >= 3
        and stats.get("peak_stores", 0) >= minimum_peak
        and stats.get("store_activity", 0) >= minimum_activity
        and stats.get("peak_item_stores", 0) > 0
        and stats.get("peak_disjoint_stores", 0) > 0
        and stats.get("displayed_item_stores", 0) > 0
        and stats.get("displayed_zero", 0) == 0
        and stats.get("displayed_out_of_range", 0) == 0
        and stats.get("seven_ratio", 0.0) >= 90.0
    )


def integrated_observation_stages(rows):
    stages = set()
    for row in rows:
        event = safe_text(row.get("event")).strip()
        if event.startswith("shared_runtime_observation") or event.startswith("announcement_check"):
            stages.add("load")
        if event.startswith("market_workflow"):
            stages.add("market_workflow")
        if event.startswith("market_service_port_conflict") or event.startswith("market_iteminfo_"):
            stages.add("market_service_fault")
        if event.startswith("market_source_"):
            stages.add("market_source_fault")
        if event.startswith("database_"):
            stages.add("database_fault")
    return stages


def integrated_runtime_health_stats(rows):
    def values(key):
        return [to_int(row.get(key)) for row in rows if safe_text(row.get(key)).strip() != ""]

    def envelope(key):
        series = values(key)
        return {
            "start": series[0] if series else 0,
            "end": series[-1] if series else 0,
            "peak": max(series) if series else 0,
            "samples": len(series),
        }

    core_samples = 0
    core_down = 0
    for row in rows:
        ports = [safe_text(row.get(key)).strip() for key in ("game_port", "monitor_port", "bridge_port", "relay_port")]
        if not all(value != "" for value in ports):
            continue
        core_samples += 1
        if not all(to_int(value) > 0 for value in ports):
            core_down += 1
    return {
        "samples": len(rows),
        "api_errors": sum(1 for row in rows if safe_text(row.get("api_error")).strip()),
        "core_port_samples": core_samples,
        "core_port_down": core_down,
        "goroutines": envelope("goroutines"),
        "memory_mb": envelope("memory_mb"),
        "fd_robot": envelope("fd_robot"),
    }


def market_fault_state_settled(status):
    services = status.get("services") or {} if isinstance(status, dict) else {}
    for name in ("auction", "point"):
        service = services.get(name) or {}
        if safe_text(service.get("status")) not in MARKET_SERVICE_TERMINAL_STATES:
            return False
    return True


def market_fault_signature(status):
    services = status.get("services") or {} if isinstance(status, dict) else {}
    signature = []
    for name in ("auction", "point"):
        service = services.get(name) or {}
        state = safe_text(service.get("status")).strip()
        if not state:
            return ()
        signature.append((name, state, bool(service.get("listening")), bool(service.get("pid"))))
    return tuple(signature)


def market_fault_state_observed(status):
    return bool(
        market_fault_state_settled(status)
        or isinstance(status, dict) and status.get("_stable_fault_observed")
    )


def select_scenario_events(events, start_scenario):
    selected = list(events)
    if not start_scenario:
        return selected
    names = [event.get("name") for event in selected]
    if start_scenario in names:
        return selected[names.index(start_scenario):]
    for index, event in enumerate(selected):
        if start_scenario not in (event.get("phases") or ()):
            continue
        resumed = [dict(item) for item in selected[index:]]
        resumed[0]["start_phase"] = start_scenario
        return resumed
    raise ValueError("unknown stability scenario %s" % start_scenario)


def key_state_name(result):
    if not isinstance(result, dict):
        return ""
    return safe_text(result.get("key_state") or result.get("KeyState")).strip().lower()


def key_using_default(result):
    if not isinstance(result, dict):
        return False
    value = result.get("using_default")
    if value is None:
        value = result.get("UsingDefault")
    return bool(value)


def target_action_outcomes(result, item_ids):
    outcomes = dict((to_int(item_id), {"actions": 0, "accepted": False}) for item_id in item_ids)
    payload = (result.get("result") or {}) if isinstance(result, dict) else {}
    for entry in payload.get("actions") or []:
        action = entry.get("action") or {}
        item_id = to_int(action.get("item_id"))
        if item_id not in outcomes:
            continue
        outcomes[item_id]["actions"] += 1
        if entry.get("ok"):
            outcomes[item_id]["accepted"] = True
    return outcomes


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


def service_layout_from_df_game(df_game_r):
    path = posixpath.normpath(safe_text(df_game_r).strip() or DEFAULT_DF_GAME_R)
    game_dir = posixpath.dirname(path)
    service_root = posixpath.dirname(game_dir)
    if posixpath.basename(game_dir) != "game" or service_root in ("", "/", "."):
        path = DEFAULT_DF_GAME_R
        game_dir = posixpath.dirname(path)
        service_root = posixpath.dirname(game_dir)
    return {
        "df_game_r": path,
        "game_dir": game_dir,
        "service_root": service_root,
        "script_pvf": posixpath.join(game_dir, "Script.pvf"),
        "auction_dir": posixpath.join(service_root, "auction"),
        "point_dir": posixpath.join(service_root, "point"),
        "auction_iteminfo": posixpath.join(service_root, "auction", "iteminfo.dat"),
        "point_iteminfo": posixpath.join(service_root, "point", "iteminfo.dat"),
    }


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
        self.artifact_max_bytes = max(0, args.artifact_max_mb) * 1024 * 1024
        self.artifact_limit_reported = False
        self.baseline_dir = os.path.join(self.out_dir, "baseline")
        self.snapshot_dir = os.path.join(self.out_dir, "snapshots")
        if not os.path.isdir(self.snapshot_dir):
            os.makedirs(self.snapshot_dir)
        self.results = []
        self.round_orders = []
        self.sample_metrics = []
        self.coverage = {}
        self.current_round = 0
        self.current_phase = ""
        self.market_auto_stopped_since = 0
        self.market_zero_since = 0
        self.market_zero_last_seen = 0
        self.last_invariant_failure = {}
        self.reported_core_files = set()
        self.new_core_dumps = []
        self.core_file_baseline = self.core_file_snapshot()
        self.service_layout = self.read_service_layout()
        self.df_game_r = self.service_layout["df_game_r"]
        self.game_dir = self.service_layout["game_dir"]
        self.service_root = self.service_layout["service_root"]
        self.script_pvf = self.service_layout["script_pvf"]
        self.auction_dir = self.service_layout["auction_dir"]
        self.point_dir = self.service_layout["point_dir"]
        self.auction_iteminfo = self.service_layout["auction_iteminfo"]
        self.point_iteminfo = self.service_layout["point_iteminfo"]
        self.game_log_offsets = {}
        self.game_connection_counts = dict((name, 0) for name in GAME_CONNECTION_CHECK_PATTERNS)
        self.game_store_disconnect_counts = dict((name, 0) for name in GAME_STORE_DISCONNECT_PATTERNS)
        self.game_connection_failure_reported = set()
        self.prime_game_connection_logs()
        self.ports = self.read_ports()
        self.web_password = self.read_web_password()
        self.web_opener = None
        self.api = RobotAPI(args.robot_host, self.port("RobotAPI"), args.api_timeout)

    def read_service_layout(self):
        path = "/root/config/config.ini"
        df_game_r = DEFAULT_DF_GAME_R
        try:
            section = ""
            for line in io.open(path, "r", encoding="utf-8"):
                text = safe_text(line).strip()
                if not text or text.startswith("#") or text.startswith(";"):
                    continue
                if text.startswith("[") and "]" in text:
                    section = text[1:text.index("]")].strip()
                    continue
                if section != "Robot" or "=" not in text:
                    continue
                key, value = text.split("=", 1)
                if key.strip() == "DfGameR" and value.strip():
                    df_game_r = value.strip()
        except Exception as exc:
            self.log("read_service_layout fallback path=%s err=%r" % (path, exc))
        layout = service_layout_from_df_game(df_game_r)
        self.log("service_layout df_game_r=%s root=%s" % (layout["df_game_r"], layout["service_root"]))
        return layout

    def read_ports(self):
        ports = {
            "RobotAPI": int(self.args.robot_port or 8111),
            "Web": 8112,
            "Game": 10011,
            "Monitor": 30303,
            "Bridge": 7000,
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
        url = self.web_base_url() + path
        last_exc = None
        for attempt in range(2):
            opener = self.ensure_web_login()
            data = None
            headers = {}
            if payload is not None:
                data = json.dumps(payload, ensure_ascii=True, separators=(",", ":")).encode("utf-8")
                headers["Content-Type"] = "application/json"
            req = urllib2.Request(url, data=data, headers=headers)
            try:
                raw = opener.open(req, timeout=self.args.api_timeout).read()
                break
            except urllib2.HTTPError as exc:
                last_exc = exc
                if getattr(exc, "code", 0) in (401, 403) and attempt == 0:
                    self.web_opener = None
                    continue
                self.web_opener = None
                raise RuntimeError("web request failed path=%s status=%s err=%r" % (path, getattr(exc, "code", ""), exc))
            except Exception as exc:
                last_exc = exc
                self.web_opener = None
                raise RuntimeError("web request failed path=%s err=%r" % (path, exc))
        else:
            raise RuntimeError("web request failed path=%s err=%r" % (path, last_exc))
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
        self.mark_coverage("new_core_files_absent", True, "baseline=%s" % len(self.core_file_baseline))
        if not self.prepare_baseline():
            self.log("run aborted: baseline config backup failed")
            self.events.close()
            self.samples_file.close()
            return False
        self.ensure_auto()
        fuzz_enabled = self.args.rounds > 1 or self.deadline_at > 0
        next_target = time.time() + random.randint(self.args.target_min_interval, self.args.target_max_interval)
        next_cleanup = time.time() + random.randint(self.args.cleanup_min_interval, self.args.cleanup_max_interval)
        next_user_interleave = time.time() + random.randint(self.args.user_interleave_min_interval, self.args.user_interleave_max_interval)
        next_log_snapshot = time.time() + self.args.log_snapshot_interval
        next_sample = 0
        next_invariant = 0
        scenarios = select_scenario_events(self.scenario_events(), self.args.start_scenario)
        if self.args.start_scenario:
            self.log("scenario selection start=%s order=%s" % (
                self.args.start_scenario,
                ",".join(event["name"] for event in scenarios),
            ))
        round_no = 0
        stop_reason = ""
        try:
            while True:
                if self.artifact_budget_exceeded():
                    stop_reason = "artifact_budget_reached"
                    break
                round_no += 1
                round_scenarios = list(scenarios)
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
                    if fuzz_enabled and now >= next_target:
                        self.random_target()
                        next_target = now + random.randint(self.args.target_min_interval, self.args.target_max_interval)
                    if fuzz_enabled and now >= next_cleanup:
                        self.random_cleanup()
                        next_cleanup = now + random.randint(self.args.cleanup_min_interval, self.args.cleanup_max_interval)
                    if fuzz_enabled and now >= next_user_interleave:
                        self.random_user_interleave()
                        next_user_interleave = now + random.randint(self.args.user_interleave_min_interval, self.args.user_interleave_max_interval)
                    if now >= next_invariant:
                        self.check_market_invariants("main_loop")
                        next_invariant = now + self.args.sample_interval
                    self.run_event(event, round_no)
                    self.write_report()
                    if self.args.fail_fast and self.has_failures():
                        stop_reason = "failure_detected"
                        self.log("scenario stop after failure round=%s event=%s" % (round_no, event["name"]))
                        raise StopIteration
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
                if fuzz_enabled and now >= next_target:
                    self.random_target()
                    next_target = now + random.randint(self.args.target_min_interval, self.args.target_max_interval)
                if fuzz_enabled and now >= next_cleanup:
                    self.random_cleanup()
                    next_cleanup = now + random.randint(self.args.cleanup_min_interval, self.args.cleanup_max_interval)
                if fuzz_enabled and now >= next_user_interleave:
                    self.random_user_interleave()
                    next_user_interleave = now + random.randint(self.args.user_interleave_min_interval, self.args.user_interleave_max_interval)
                if now >= next_invariant:
                    self.check_market_invariants("main_loop")
                    next_invariant = now + self.args.sample_interval
                time.sleep(1)
        except StopIteration:
            pass
        except KeyboardInterrupt:
            stop_reason = "interrupted"
            self.log("interrupted by user")
        finally:
            if not stop_reason:
                stop_reason = self.stop_reason(round_no)
            self.log("run finishing rounds=%s stop_reason=%s" % (round_no, stop_reason))
            if not self.artifact_budget_exceeded():
                self.collect_logs("before_final_recover")
            self.final_recover_environment()
            core_error = self.core_file_error("final_recover")
            if core_error:
                self.record_failure("final_recover_core_dump", core_error)
            if not self.artifact_budget_exceeded():
                self.collect_logs("final")
            self.write_report()
            self.write_summary()
            self.events.close()
            self.samples_file.close()
        return bool(
            not self.has_failures()
            and stop_reason in ("min_rounds_complete", "deadline_reached")
        )

    def has_failures(self):
        return any(item.get("error") or not item.get("recovered") for item in self.results)

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
        previous_round = self.current_round
        previous_phase = self.current_phase
        self.current_round = round_no
        self.current_phase = name
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
            self.current_round = previous_round
            self.current_phase = previous_phase
            return
        before_path = self.write_snapshot("round%s_%s_before" % (round_no, name))
        started = time.time()
        err = ""
        recovered = False
        try:
            event["fn"](event.get("start_phase") or "")
        except Exception as exc:
            err = repr(exc)
            self.log("scenario event error name=%s err=%s" % (name, err))
        core_error = self.core_file_error(name)
        if core_error:
            err = (err + "; " if err else "") + core_error
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
        self.current_round = previous_round
        self.current_phase = previous_phase

    def record_failure(self, name, error):
        now = datetime.datetime.now().isoformat()
        item = {
            "name": name,
            "round": self.current_round,
            "phase": self.current_phase,
            "started_at": now,
            "duration_sec": 0,
            "recovered": False,
            "error": error,
            "before": "",
            "after": "",
        }
        self.results.append(item)
        self.log("invariant failure name=%s error=%s" % (name, error))

    def mark_coverage(self, name, observed, detail=""):
        item = {
            "observed": bool(observed),
            "detail": safe_text(detail),
            "updated_at": datetime.datetime.now().isoformat(),
        }
        self.coverage[name] = item
        self.log("coverage name=%s observed=%s detail=%s" % (name, item["observed"], item["detail"]))

    def check_recovered(self, event):
        return self.check_recovered_once(event) or self.wait_recovered(event, 120, 10)

    def check_recovered_once(self, event):
        state = self.baseline_state()
        api = state["api"]
        sched = state["scheduler"]
        market = state["market"]
        ports = state["ports"]
        ok = state["api_ok"]
        scheduler_ok = state["scheduler_api_ok"] and not (
            sched.get("mode") == "maintenance" and sched.get("operation_active")
        )
        game_ok = state["game_ok"]
        monitor_ok = state["monitor_ok"]
        bridge_ok = state["bridge_ok"]
        market_ok = state["market_ok"]
        scaling_ok = ok and game_ok and monitor_ok and bridge_ok and market_ok and self.scaling_recovery_ok(event, sched)
        if scaling_ok:
            self.log("recover_check event=%s scaling_recovery_ok scheduler=%s" % (event, json_text(sched, 1400)))
            return True
        if not ok:
            self.log("recover_check event=%s failed reason=robot_api api=%s" % (event, json_text(api, 1000)))
        if not state["scheduler_api_ok"]:
            self.log("recover_check event=%s failed reason=scheduler_api scheduler=%s" % (event, json_text(sched, 1400)))
        elif not scheduler_ok:
            self.log("recover_check event=%s failed reason=scheduler_maintenance scheduler=%s" % (event, json_text(sched, 1400)))
        if not game_ok:
            self.log("recover_check event=%s failed reason=game_port ports=%s" % (event, ports))
        if not monitor_ok:
            self.log("recover_check event=%s failed reason=monitor_port ports=%s" % (event, ports))
        if not bridge_ok:
            self.log("recover_check event=%s failed reason=bridge_port ports=%s" % (event, ports))
        if not market_ok:
            self.log("recover_check event=%s failed reason=market_services services=%s" % (event, json_text((market.get("services") or {}), 1400)))
        return bool(ok and scheduler_ok and game_ok and monitor_ok and bridge_ok and market_ok)

    def wait_recovered(self, event, timeout_sec, interval_sec):
        self.log("wait_recovered start event=%s timeout=%s" % (event, timeout_sec))
        deadline = time.time() + timeout_sec
        while time.time() < deadline:
            if self.check_recovered_once(event):
                self.log("wait_recovered ready event=%s" % event)
                return True
            time.sleep(interval_sec)
        self.log("wait_recovered timeout event=%s" % event)
        return False

    def scaling_recovery_ok(self, event, sched):
        if event not in ("integrated_load_data_matrix", "restart_recovery_matrix"):
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
            "processes": self.shell("pgrep -af '/root/robot|df_game_r|df_monitor_r|df_bridge_r|df_auction_r|df_point_r|df_relay_r|mysqld' || true", 20, log_output=False)[:4000],
            "files": self.file_snapshot(),
            "db": self.db_snapshot(),
            "tcp": self.shell("ss -ant | awk 'NR>1 {c[$1]++} END {for (k in c) print k,c[k]}'", 20, log_output=False)[:2000],
            "disk": self.shell("df -h / /root /home 2>/dev/null || df -h", 20, log_output=False)[:2000],
        }

    def core_file_snapshot(self):
        files = {}
        for pattern in CORE_FILE_PATTERNS:
            for path in glob.glob(pattern):
                try:
                    stat = os.stat(path)
                except OSError:
                    continue
                files[path] = {
                    "inode": int(stat.st_ino),
                    "size": int(stat.st_size),
                    "mtime": int(stat.st_mtime),
                }
        return files

    def consume_new_core_files(self, event):
        current = self.core_file_snapshot()
        details = changed_core_files(self.core_file_baseline, current, self.reported_core_files)
        for detail in details:
            path = detail.get("path")
            self.reported_core_files.add(path)
            recorded = dict(detail)
            recorded["event"] = event
            recorded["detected_at"] = now_text()
            self.new_core_dumps.append(recorded)
        if details:
            self.mark_coverage("new_core_files_absent", False, json_text(details, 1800))
            self.log("new core files event=%s details=%s" % (event, json_text(details, 1800)))
        return details

    def core_file_error(self, event):
        details = self.consume_new_core_files(event)
        if not details:
            return ""
        return "new core file detected: %s" % ", ".join(detail.get("path") or "" for detail in details)

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
            "/root/config/pvf_level_exp_catalog.json",
            "/root/config/pvf_iteminfo.dat",
            "/root/config/store_points_cache.json",
            "/root/config/store_points_active.json",
            self.auction_iteminfo,
            self.point_iteminfo,
            self.script_pvf,
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
            self.sample_shared_progress("market_count:%s" % label, max(5, interval))
            time.sleep(interval)
        return last

    def prepare_baseline(self):
        if not os.path.isdir(self.baseline_dir):
            os.makedirs(self.baseline_dir)
        self.log("prepare_baseline begin dir=%s" % self.baseline_dir)
        self.safe_call("marketStop", {})
        self.wait_market_job_idle("prepare_baseline", 300, 5)
        backup_output = self.shell(filtered_config_backup_script("/root/config", os.path.join(self.baseline_dir, "root_config")), 120)
        if "CONFIG_BACKUP_OK" not in safe_text(backup_output):
            self.log("prepare_baseline config_backup_failed output=%s" % safe_text(backup_output)[:1000])
            return False
        auction_backup = os.path.join(self.baseline_dir, "auction_iteminfo.dat")
        point_backup = os.path.join(self.baseline_dir, "point_iteminfo.dat")
        self.shell("cp -af %s %s 2>/dev/null || true; cp -af %s %s 2>/dev/null || true" % (
            shell_quote(self.auction_iteminfo), shell_quote(auction_backup),
            shell_quote(self.point_iteminfo), shell_quote(point_backup),
        ), 60)
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
cp -af "$BASE/auction_iteminfo.dat" %s 2>/dev/null || true
cp -af "$BASE/point_iteminfo.dat" %s 2>/dev/null || true
if [ -s "$BASE/baseline_market_robot_stock.sql" ]; then mysql -ugame -puu5!^%%jg < "$BASE/baseline_market_robot_stock.sql"; fi
echo RESTORED
""" % (shell_quote(self.baseline_dir), shell_quote(self.auction_iteminfo), shell_quote(self.point_iteminfo))
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
        self.safe_call("marketStop", {})
        self.wait_market_job_idle("final_recover_prepare", 300, 5)
        restore_path = os.path.join(self.baseline_dir, "restore_baseline.sh")
        if os.path.isfile(restore_path):
            self.shell("sh %s" % shell_quote(restore_path), 180)
        else:
            self.log("final_recover_environment missing restore script=%s" % restore_path)
        core_ready = self.restart_core_services("final_recover_core", 60, 90)
        robot_ready = self.robot_restart_without_target("final_recover_robot")
        if not robot_ready:
            self.record_failure("final_recover_api_timeout", "robot API was not ready after final recovery")
        if not core_ready:
            self.record_failure("final_recover_core_unstable", "core ports did not remain stable during final recovery")
        if core_ready and robot_ready:
            before_result = self.safe_call("autoStatus", {})
            before = (before_result.get("result") or {}) if isinstance(before_result, dict) else {}
            self.set_target(20, settle_sec=0)
            if not self.wait_target_progress(20, before, 60, 5):
                self.record_failure("final_recover_scale_down", "target 20 did not produce scheduler progress during final recovery")
            self.market_enable_auto(max_concurrent=8)
        else:
            self.log("final_recover_environment skip automation core_ready=%s robot_ready=%s" % (core_ready, robot_ready))
        if core_ready and robot_ready and not self.wait_market_services("final_recover_market", 180, 5):
            self.record_failure("final_recover_market_timeout", "market services were not ready after the controlled final recovery")
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

    def baseline_state(self):
        api = self.safe_call("systemStatus", {})
        scheduler_result = self.safe_call("schedulerStatus", {})
        scheduler = (scheduler_result.get("result") or {}) if isinstance(scheduler_result, dict) else {}
        market = self.market_status_result()
        ports = self.port_snapshot()
        return {
            "api": api,
            "scheduler": scheduler,
            "market": market,
            "ports": ports,
            "api_ok": bool(isinstance(api, dict) and api.get("ok")),
            "scheduler_api_ok": bool(isinstance(scheduler_result, dict) and scheduler_result.get("ok")),
            "game_ok": bool(ports.get(self.port_text("Game"))),
            "monitor_ok": bool(ports.get(self.port_text("Monitor"))),
            "bridge_ok": bool(ports.get(self.port_text("Bridge"))),
            "market_ok": self.market_services_ready(market),
        }

    def baseline_state_ready(self, state, require_idle=True):
        scheduler = state.get("scheduler") or {}
        scheduler_ok = bool(state.get("scheduler_api_ok")) and (
            not require_idle or not (
                scheduler.get("mode") == "maintenance" and scheduler.get("operation_active")
            )
        )
        return bool(
            state.get("api_ok")
            and state.get("game_ok")
            and state.get("monitor_ok")
            and state.get("bridge_ok")
            and state.get("market_ok")
            and scheduler_ok
        )

    def wait_baseline_ready(self, event, timeout_sec=30, interval_sec=5):
        self.log("wait_baseline_ready start event=%s timeout=%s" % (event, timeout_sec))
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            last = self.baseline_state()
            if self.baseline_state_ready(last):
                self.log("wait_baseline_ready ready event=%s" % event)
                return True
            time.sleep(interval_sec)
        self.log(
            "wait_baseline_ready timeout event=%s api=%s game=%s monitor=%s bridge=%s market=%s scheduler=%s"
            % (
                event,
                last.get("api_ok"),
                last.get("game_ok"),
                last.get("monitor_ok"),
                last.get("bridge_ok"),
                last.get("market_ok"),
                json_text({"api_ok": last.get("scheduler_api_ok"), "status": last.get("scheduler") or {}}, 1000),
            )
        )
        return False

    def wait_service_ports(self, event, names, expected, timeout_sec=90, interval_sec=3):
        expected = bool(expected)
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            ports = self.port_snapshot()
            last = dict((name, bool(ports.get(self.port_text(name)))) for name in names)
            if all(value == expected for value in last.values()):
                self.log("wait_service_ports ready event=%s expected=%s ports=%s" % (event, expected, last))
                return True
            time.sleep(interval_sec)
        self.log("wait_service_ports timeout event=%s expected=%s ports=%s" % (event, expected, last))
        return False

    def wait_service_ports_stable(self, event, names, expected, timeout_sec=90, interval_sec=3, stable_samples=3):
        expected = bool(expected)
        stable_samples = max(1, int(stable_samples))
        deadline = time.time() + timeout_sec
        last = {}
        stable = 0
        while time.time() < deadline:
            ports = self.port_snapshot()
            last = dict((name, bool(ports.get(self.port_text(name)))) for name in names)
            if all(value == expected for value in last.values()):
                stable += 1
                if stable >= stable_samples:
                    self.log("wait_service_ports_stable ready event=%s expected=%s samples=%s ports=%s" % (
                        event, expected, stable, last,
                    ))
                    return True
            else:
                stable = 0
            time.sleep(interval_sec)
        self.log("wait_service_ports_stable timeout event=%s expected=%s samples=%s ports=%s" % (
            event, expected, stable, last,
        ))
        return False

    def restart_core_services(self, event, timeout_sec=60, retry_timeout_sec=90):
        market_stop = self.safe_call("marketStop", {})
        if isinstance(market_stop, dict) and market_stop.get("ok"):
            self.wait_market_job_idle("%s_market_stop" % event, 90, 3)
        auto_stop = self.safe_call("autoStop", {})
        if isinstance(auto_stop, dict) and auto_stop.get("ok"):
            self.wait_auto_drained("%s_auto_stop" % event, 90, 3)
        self.shell("cd /root && (./stop >/dev/null 2>&1 || true)", 180)
        if not self.wait_service_ports("%s_down" % event, ("Game", "Monitor", "Bridge"), False, 45, 3):
            self.log("restart_core_services stop failed event=%s" % event)
            return False
        core_ports = self.port_regex(("Game", "Monitor", "Bridge", "Point", "Auction"))
        command = (
            "if ! command -v setsid >/dev/null 2>&1; then echo CORE_START_NO_SETSID; exit 1; fi; "
            "setsid sh -c 'cd /root && ./run' </dev/null >%s 2>&1; "
            "rc=$?; echo CORE_START_RC=$rc; sleep 2; "
            "ss -lntp | grep -E ':(%s)' || true; "
            "pgrep -af 'df_game_r|df_monitor_r|df_bridge_r|df_auction_r|df_point_r' || true"
        ) % (shell_quote(CORE_START_LOG), core_ports)
        output = self.shell(command, 240)
        if "CORE_START_RC=0" not in safe_text(output):
            self.log("restart_core_services launch failed event=%s output=%s" % (event, safe_text(output)[:1600]))
            return False
        return self.wait_service_ports_stable(
            "%s_core" % event,
            ("Game", "Monitor", "Bridge"),
            True,
            max(timeout_sec, retry_timeout_sec, 45),
            3,
            stable_samples=3,
        )

    def ensure_baseline_ready(self, event, timeout_sec=120):
        if self.wait_baseline_ready(event, 30, 5):
            return True
        state = self.baseline_state()
        self.log(
            "ensure_baseline_ready recover event=%s api=%s game=%s monitor=%s bridge=%s market=%s scheduler=%s"
            % (
                event,
                state.get("api_ok"),
                state.get("game_ok"),
                state.get("monitor_ok"),
                state.get("bridge_ok"),
                state.get("market_ok"),
                json_text({"api_ok": state.get("scheduler_api_ok"), "status": state.get("scheduler") or {}}, 1000),
            )
        )
        if not state.get("game_ok") or not state.get("monitor_ok") or not state.get("bridge_ok"):
            self.restart_core_services("%s_core" % event, 60, 90)
        if not state.get("api_ok") or not state.get("scheduler_api_ok"):
            self.robot_restart_without_target("%s_robot_restart" % event)
        if not state.get("market_ok"):
            self.market_enable_auto(max_concurrent=8)
        ready = self.wait_baseline_ready("%s_retry" % event, timeout_sec, 5)
        self.log("ensure_baseline_ready done event=%s ready=%s" % (event, ready))
        return ready

    def scenario_events(self):
        return [
            {
                "name": "integrated_load_data_matrix",
                "fn": self.integrated_load_data_matrix,
                "phases": STABILITY_SCENARIO_PHASES["integrated_load_data_matrix"],
            },
            {
                "name": "restart_recovery_matrix",
                "fn": self.restart_recovery_matrix,
                "phases": STABILITY_SCENARIO_PHASES["restart_recovery_matrix"],
            },
        ]

    def phase_allowed(self):
        return not (self.args.fail_fast and self.has_failures())

    def integrated_load_data_matrix(self, start_phase=""):
        high = self.args.target_max
        mid = max(self.args.target_min, min(high, max(100, high // 2)))
        self.log("integrated_load_data_matrix begin mid=%s high=%s start_phase=%s" % (mid, high, start_phase or "start"))
        if start_phase in ("market_matrix", "database_fault_matrix"):
            self.set_target(high)
            scaled = self.wait_target_running(high, 300, 10, sample_interval=30, minimum_ratio=0.94)
            if not scaled:
                self.record_failure("resume_high_load_not_ready", "target %s did not reach 94%% before %s" % (high, start_phase))
        else:
            scaled = self.run_phase_step(
                "load_scale_profile",
                lambda: self.compact_scale_profile(mid, high),
                recover=False,
            )
        if not scaled or not self.phase_allowed():
            self.log("integrated_load_data_matrix stop reason=scale_failed")
            return

        self.begin_shared_runtime_observation()
        validate_activity = start_phase in ("", "load_runtime_observation")
        required_stages = set(("load", "market_workflow", "market_service_fault", "market_source_fault", "database_fault"))
        phases = [
            ("load_runtime_observation", "runtime_connectivity_probe", self.announcement_and_relay_probe),
            ("market_matrix", "market_workflow", self.market_workflow),
            ("market_matrix", "market_fault_matrix", self.market_fault_matrix),
            ("database_fault_matrix", "database_fault_matrix", self.database_fault_matrix),
        ]
        start_index = 0
        if start_phase:
            phase_names = [legacy for legacy, _, _ in phases]
            start_index = phase_names.index(start_phase)
            required_stages = set()
            for legacy, _, _ in phases[start_index:]:
                if legacy == "load_runtime_observation":
                    required_stages.add("load")
                elif legacy == "market_matrix":
                    required_stages.update(("market_workflow", "market_service_fault", "market_source_fault"))
                elif legacy == "database_fault_matrix":
                    required_stages.add("database_fault")
        try:
            for _, name, fn in phases[start_index:]:
                if not self.phase_allowed():
                    break
                self.run_phase_step(name, fn, recover=False)
        finally:
            require_all_stages = self.phase_allowed()
            self.run_phase_step(
                "runtime_observation_assertions",
                lambda: self.finish_shared_runtime_observation(
                    high,
                    required_stages,
                    require_all_stages,
                    validate_activity and require_all_stages,
                ),
                recover=False,
            )

        if self.phase_allowed() and start_phase in ("", "load_runtime_observation"):
            self.run_phase_step("manual_robot_workflow", self.compact_manual_store_cleanup, recover=False)
        self.log("integrated_load_data_matrix done")

    def run_phase_step(self, name, fn, recover=True):
        started = time.time()
        error = ""
        recovered = True
        self.log("phase step start phase=%s name=%s" % (self.current_phase, name))
        try:
            fn()
        except Exception as exc:
            error = repr(exc)
            self.log("phase step error phase=%s name=%s err=%s" % (self.current_phase, name, error))
        core_error = self.core_file_error(name)
        if core_error:
            error = (error + "; " if error else "") + core_error
        if recover:
            recovered = self.check_recovered(name)
        elif error:
            recovered = False
        result = {
            "name": name,
            "round": self.current_round,
            "phase": self.current_phase,
            "started_at": datetime.datetime.fromtimestamp(started).isoformat(),
            "duration_sec": int(time.time() - started),
            "recovered": recovered,
            "error": error,
            "before": "",
            "after": "",
        }
        self.results.append(result)
        self.log("phase step done phase=%s name=%s recovered=%s error=%s" % (self.current_phase, name, recovered, error))
        if not recovered:
            self.ensure_baseline_ready("after_%s" % name)
        return recovered and not error

    def begin_shared_runtime_observation(self):
        self._party_observation_cursors = {
            "log_robot": self.capture_log_cursor("/root/config/log_robot"),
            "robot_stdout": self.capture_log_cursor("/root/config/robot_stdout.log"),
        }
        self._store_observation_cursor = self._party_observation_cursors["log_robot"]
        self._natural_store_metrics_start = len(self.sample_metrics)
        self._shared_observation_started = time.time()
        self.sample_with_event("shared_runtime_observation:start")
        self._shared_last_sample = time.time()

    def sample_shared_progress(self, event, minimum_interval=10):
        if not getattr(self, "_shared_observation_started", 0):
            return
        now = time.time()
        if now - getattr(self, "_shared_last_sample", 0) < minimum_interval:
            return
        self._shared_last_sample = now
        self.sample_with_event("shared:%s" % event)

    def finish_shared_runtime_observation(self, high, required_stages, require_all_stages=True, validate_activity=True):
        required_stages = set(required_stages)
        deadline = time.time() + 30
        while True:
            rows = self.sample_metrics[self._natural_store_metrics_start:]
            stats = high_load_observation_stats(rows, high)
            stages = integrated_observation_stages(rows)
            stages_ready = not require_all_stages or required_stages.issubset(stages)
            activity_ready = not validate_activity or high_load_observation_ready(stats, high)
            if activity_ready and stages_ready:
                break
            if time.time() >= deadline:
                break
            self.sample_with_event("shared_runtime_observation:settle")
            time.sleep(5)
        self._natural_store_metrics_end = len(self.sample_metrics)
        rows = self.sample_metrics[self._natural_store_metrics_start:self._natural_store_metrics_end]
        stages = integrated_observation_stages(rows)
        missing = sorted(required_stages - stages) if require_all_stages else []
        detail = "samples=%s elapsed=%s stages=%s missing=%s" % (
            len(rows),
            int(time.time() - self._shared_observation_started),
            ",".join(sorted(stages)),
            ",".join(missing),
        )
        self.mark_coverage("integrated_observation_stages", not missing, detail)
        if missing:
            self.record_failure("integrated_observation_stages", "missing observation stages: %s" % ",".join(missing))
        self.assert_shared_runtime_observation(validate_activity)

    def compact_scale_profile(self, mid, high):
        targets = [mid, high]
        for index, target in enumerate(targets):
            before_result = self.safe_call("autoStatus", {})
            before = (before_result.get("result") or {}) if isinstance(before_result, dict) else {}
            self.set_target(target)
            if not self.wait_target_progress(target, before, 45, 5):
                self.record_failure(
                    "scale_target_progress_%s" % index,
                    "target %s did not produce scheduler progress" % target,
                )
        if not self.wait_target_running(high, 300, 10, sample_interval=30, minimum_ratio=0.90):
            self.record_failure("target_high_not_reached", "target %s did not reach 90%% running" % high)

    def wait_target_progress(self, target, before=None, timeout_sec=60, interval_sec=5):
        before = before or {}
        before_target = to_int(before.get("target_online"))
        before_actors = to_int(before.get("actors"))
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            result = self.safe_call("autoStatus", {})
            last = (result.get("result") or {}) if isinstance(result, dict) else {}
            scheduler_result = self.safe_call("schedulerStatus", {})
            scheduler = (scheduler_result.get("result") or {}) if isinstance(scheduler_result, dict) else {}
            actual_target = to_int(last.get("target_online"))
            actors = to_int(last.get("actors"))
            running = to_int(last.get("running"))
            connecting = to_int(last.get("connecting"))
            releasing = to_int(last.get("actor_releasing"))
            operation = safe_text(scheduler.get("operation") or scheduler.get("recent_operation"))
            if int(target) > before_target:
                progressed = connecting > 0 or actors > before_actors or running >= int(target) * 75 // 100 or operation == "create"
            elif int(target) < before_target:
                progressed = releasing > 0 or actors < before_actors or actors <= int(target) * 110 // 100 or operation == "cleanup"
            else:
                progressed = actors + running + connecting > 0 or int(target) <= 20
            if actual_target == int(target) and progressed:
                self.log("wait_target_progress ready target=%s status=%s scheduler=%s" % (target, json_text(last, 1000), json_text(scheduler, 1000)))
                return True
            time.sleep(interval_sec)
        self.log("wait_target_progress timeout target=%s status=%s" % (target, json_text(last, 1000)))
        return False

    def wait_target_running(self, target, timeout_sec=300, interval_sec=10, sample_interval=30, minimum_ratio=0.95):
        deadline = time.time() + timeout_sec
        minimum_running = max(1, int(int(target) * minimum_ratio))
        next_sample = 0
        last = {}
        while time.time() < deadline:
            result = self.safe_call("autoStatus", {})
            last = (result.get("result") or {}) if isinstance(result, dict) else {}
            actual_target = to_int(last.get("target_online"))
            running = to_int(last.get("running"))
            if actual_target == int(target) and running >= minimum_running:
                self.log("wait_target_running ready target=%s running=%s status=%s" % (target, running, json_text(last, 1000)))
                self.sample_with_event("target_running:%s" % target)
                return True
            now = time.time()
            if now >= next_sample:
                self.sample_with_event("target_wait:%s" % target)
                next_sample = now + sample_interval
            time.sleep(interval_sec)
        self.log("wait_target_running timeout target=%s minimum=%s status=%s" % (target, minimum_running, json_text(last, 1200)))
        return False

    def wait_auto_drained(self, event, timeout_sec=120, interval_sec=3):
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            result = self.safe_call("autoStatus", {})
            last = (result.get("result") or {}) if isinstance(result, dict) else {}
            active = self.sum_ints(
                last.get("actors"),
                last.get("running"),
                last.get("connecting"),
                last.get("actor_online"),
                last.get("actor_releasing"),
            )
            if active <= 0:
                self.log("wait_auto_drained ready event=%s status=%s" % (event, json_text(last, 1200)))
                return True
            time.sleep(interval_sec)
        self.log("wait_auto_drained timeout event=%s status=%s" % (event, json_text(last, 1200)))
        return False

    def announcement_and_relay_probe(self):
        self.announcement_check(observe_sec=0)
        self.party_relay_health()
        self.mark_coverage("announcement_api", True, "systemAnnouncement returned successfully")
        self.mark_coverage("party_relay_port", True, "relay TCP probe succeeded")

    def compact_manual_store_cleanup(self):
        self.robot_manual_mode_drill(hold_sec=5, recover_sec=0)

    def assert_shared_runtime_observation(self, validate_activity=True):
        self.mark_coverage(
            "integrated_activity_scope",
            True,
            "validated" if validate_activity else "skipped for resumed subphase",
        )
        if validate_activity:
            self.evaluate_party_observation(self._party_observation_cursors)
            self.validate_high_load_observation(
                getattr(self, "_natural_store_metrics_start", 0),
                getattr(self, "_natural_store_metrics_end", len(self.sample_metrics)),
                self.args.target_max,
            )
        self.validate_integrated_runtime_health(
            getattr(self, "_natural_store_metrics_start", 0),
            getattr(self, "_natural_store_metrics_end", len(self.sample_metrics)),
        )

    def validate_integrated_runtime_health(self, start_index, end_index):
        rows = self.sample_metrics[start_index:end_index]
        stats = integrated_runtime_health_stats(rows)
        healthy = bool(
            stats["core_port_samples"] > 0
            and stats["core_port_down"] == 0
            and stats["api_errors"] == 0
        )
        self.mark_coverage("integrated_runtime_health", healthy, json_text(stats, 1400))
        if stats["core_port_samples"] <= 0:
            self.record_failure("integrated_runtime_ports_unobserved", "no complete Game/Monitor/Bridge/Relay port samples were recorded")
        if stats["core_port_down"] > 0:
            self.record_failure("integrated_runtime_core_port_down", "core ports were absent in %s samples" % stats["core_port_down"])
        if stats["api_errors"] > 0:
            self.record_failure("integrated_runtime_api_errors", "Robot API failed in %s samples" % stats["api_errors"])

        growth_limits = {
            "goroutines": lambda start: max(500, start),
            "memory_mb": lambda start: max(1024, start),
            "fd_robot": lambda start: max(1024, start),
        }
        excessive = []
        for name in sorted(growth_limits):
            limit_fn = growth_limits[name]
            envelope = stats[name]
            if envelope["samples"] < 2:
                continue
            growth = envelope["end"] - envelope["start"]
            if growth > limit_fn(envelope["start"]):
                excessive.append("%s=%s->%s" % (name, envelope["start"], envelope["end"]))
        self.mark_coverage("integrated_resource_envelope", not excessive, json_text(stats, 1400))
        if excessive:
            self.record_failure("integrated_resource_growth", "catastrophic resource growth: %s" % ",".join(excessive))

    def evaluate_party_observation(self, cursors):
        delta_text = self.join_party_logs(dict(
            (name, self.read_log_since(cursor)) for name, cursor in cursors.items()
        ))
        delta = self.party_log_counts(delta_text)
        unresolved = self.party_unresolved_routes(delta_text)
        if unresolved:
            self.log("party observation recovery grace unresolved=%s" % ",".join(unresolved[:20]))
            self.burst_sample("party_recovery_grace", 30, 15)
            delta_text = self.join_party_logs(dict(
                (name, self.read_log_since(cursor)) for name, cursor in cursors.items()
            ))
            delta = self.party_log_counts(delta_text)
            unresolved = self.party_unresolved_routes(delta_text)
        party_activity = delta.get("party_total", 0) > 0
        self.mark_coverage("party_activity", party_activity, json_text(delta, 1200))
        if not party_activity:
            self.mark_coverage("party_skill_activity", False, "no party activity in shared window")
            self.log("party observation not covered: no party activity in shared window")
            return
        recovery_events = self.sum_ints(
            delta.get("route_recovered"),
            delta.get("route_failover"),
            delta.get("peer_ready"),
        )
        if delta.get("relay_errors", 0) > 0 and delta.get("relay_connected", 0) <= 0 and recovery_events <= 0:
            self.record_failure("party_relay_errors_unrecovered", "party relay errors increased without recovery")
        if delta.get("udp_errors", 0) > 5 and delta.get("udp_recycles", 0) <= 0 and recovery_events <= 0:
            self.record_failure("party_udp_errors_unrecovered", "party UDP errors increased without recovery")
        if delta.get("tqos_exhausted", 0) > 0 and recovery_events <= 0:
            self.record_failure("party_tqos_unrecovered", "party TQOS retries exhausted without recovery")
        if unresolved:
            self.record_failure("party_route_unrecovered", "party routes remained degraded: %s" % ",".join(unresolved[:20]))
        if delta.get("supervisor_panics", 0) > 0:
            self.record_failure("party_supervisor_panic", "party supervisor panicked during shared observation")
        if delta.get("skill_empty_profiles", 0) > 0:
            self.record_failure("party_skill_empty_profile", "party skill profile had zero effective candidates")
        if delta.get("skill_errors", 0) > 0:
            self.record_failure("party_skill_errors", "party skill errors increased during shared observation")
        self.mark_coverage(
            "party_skill_activity",
            delta.get("skill_profiles", 0) > 0 or delta.get("skill_casts", 0) > 0,
            "profiles=%s casts=%s" % (delta.get("skill_profiles", 0), delta.get("skill_casts", 0)),
        )

    def validate_high_load_observation(self, start_index, end_index, target):
        rows = self.sample_metrics[start_index:end_index]
        stats = high_load_observation_stats(rows, target)
        stable_count = stats["stable_samples"]
        peak_stores = stats["peak_stores"]
        unique_stores = stats["unique_stores"]
        store_success_delta = stats["store_success_delta"]
        store_activity = stats["store_activity"]
        peak_item = stats["peak_item_stores"]
        peak_disjoint = stats["peak_disjoint_stores"]
        displayed = stats["displayed_item_stores"]
        seven = stats["seven_item_stores"]
        zero = stats["displayed_zero"]
        out_of_range = stats["displayed_out_of_range"]
        if hasattr(self, "_store_observation_cursor"):
            store_delta = self.read_log_since(self._store_observation_cursor)
            sent_delta = len(re.findall(r"\[CharacterCache\][^\n]*native_nocache_sent=1", store_delta))
            failed_delta = len(re.findall(r"\[CharacterCache\][^\n]*nocache_failed", store_delta))
        else:
            sent = [row.get("nocache_sent", 0) for row in rows]
            failed = [row.get("nocache_failed", 0) for row in rows]
            sent_delta = (sent[-1] - sent[0]) if len(sent) >= 2 else 0
            failed_delta = (failed[-1] - failed[0]) if len(failed) >= 2 else 0
        ratio = stats["seven_ratio"]
        minimum_peak, minimum_activity = high_load_store_requirements(target)
        detail = "stable=%s peak=%s unique=%s started=%s activity=%s item=%s disjoint=%s seven_ratio=%.2f nocache=%s/%s" % (
            stable_count, peak_stores, unique_stores, store_success_delta, store_activity,
            peak_item, peak_disjoint, ratio, sent_delta, failed_delta,
        )
        self.mark_coverage("high_load_store_observation", high_load_observation_ready(stats, target), detail)
        if stable_count < 3:
            self.record_failure("high_load_stable_samples", "only %s stable samples at target %s" % (stable_count, target))
        if peak_stores < minimum_peak or store_activity < minimum_activity:
            self.record_failure(
                "store_count_below_expected",
                "peak=%s/%s activity=%s/%s unique=%s started=%s target=%s" % (
                    peak_stores, minimum_peak, store_activity, minimum_activity,
                    unique_stores, store_success_delta, target,
                ),
            )
        if peak_item <= 0 or peak_disjoint <= 0:
            self.record_failure("store_type_not_both_observed", "peak item=%s disjoint=%s" % (peak_item, peak_disjoint))
        if displayed <= 0:
            self.record_failure("store_display_not_observed", "no active item stalls in stable samples")
        if zero > 0 or out_of_range > 0:
            self.record_failure("store_display_invalid_count", "zero=%s out_of_range=%s" % (zero, out_of_range))
        if displayed > 0 and ratio < 90.0:
            self.record_failure("store_display_seven_ratio_low", "seven-item ratio=%.2f%%" % ratio)
        if sent_delta <= 0:
            self.mark_coverage("store_nocache", False, "NoCache counter did not increase during shared observation")
            self.record_failure("store_nocache_not_observed", "NoCache was not exercised during shared observation")
        else:
            self.mark_coverage("store_nocache", True, "sent_delta=%s" % sent_delta)
        if failed_delta > 0:
            self.record_failure("store_nocache_failed", "NoCache failure counter increased by %s" % failed_delta)

    def ensure_auto(self):
        desired_max = max(1000, self.args.target_max)
        current_max = 0
        try:
            current_result = self.web_json("/api/max-user")
            current_max = to_int(current_result.get("max_user_num")) if isinstance(current_result, dict) else 0
            self.log("read_max_user result=%s" % json_text(current_result, 1200))
        except Exception as exc:
            self.log("read_max_user failed err=%r" % (exc,))
        max_updated = False
        max_running = False
        try:
            max_result = self.web_json("/api/max-user", {"max_user_num": desired_max})
            self.log("set_max_user target=%s result=%s" % (desired_max, json_text(max_result, 1200)))
            max_ok = bool(
                isinstance(max_result, dict)
                and max_result.get("ok")
                and to_int(max_result.get("max_user_num")) == desired_max
                and bool(max_result.get("files"))
            )
            max_updated = max_ok and current_max != desired_max
            max_running = bool(max_result.get("running")) if isinstance(max_result, dict) else False
            self.mark_coverage("max_user_1000", max_ok, json_text(max_result, 1000))
            if not max_ok:
                self.record_failure("max_user_update_failed", "max user endpoint rejected target %s" % desired_max)
        except Exception as exc:
            self.log("set_max_user failed target=%s err=%r" % (desired_max, exc))
            self.record_failure("max_user_update_failed", "max user endpoint failed for target %s: %r" % (desired_max, exc))
        if max_updated and max_running:
            self.log("max_user changed from=%s to=%s; restart core services to apply" % (current_max, desired_max))
            if not self.restart_core_services("max_user_restart", 90, 120):
                self.record_failure("max_user_restart_core", "core services did not remain stable after max user update")
            self.web_opener = None
            if not self.wait_robot_api("max_user_restart_robot", 90, 3):
                self.robot_restart_without_target("max_user_restart_robot")
            self.wait_port_state(self.port("Web"), True, 90, 3)
        try:
            verified = self.web_json("/api/max-user")
            verified_ok = bool(
                isinstance(verified, dict)
                and verified.get("ok")
                and to_int(verified.get("max_user_num")) == desired_max
                and bool(verified.get("files"))
            )
            self.mark_coverage("max_user_verified", verified_ok, json_text(verified, 1000))
            if not verified_ok:
                self.record_failure("max_user_verify_failed", "max_user_num did not verify as %s" % desired_max)
        except Exception as exc:
            self.record_failure("max_user_verify_failed", "max user verification failed: %r" % (exc,))
        if not self.ensure_baseline_ready("initial"):
            self.record_failure("initial_baseline_unavailable", "baseline services were not ready before enabling automation")
            return
        self.set_target(20)

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

    def select_uids(self, count, prefer_running=True, exclude_store=False):
        rows = self.status_rows(max(self.args.status_count, count * 4))
        if prefer_running:
            preferred = []
            for row in rows:
                uid = int(row.get("uid") or 0)
                if uid <= 0:
                    continue
                if exclude_store and status_row_has_store(row):
                    continue
                if status_row_is_active(row):
                    preferred.append(uid)
            uids = preferred
        else:
            uids = [
                int(row.get("uid") or 0)
                for row in rows
                if int(row.get("uid") or 0) > 0 and not (exclude_store and status_row_has_store(row))
            ]
        random.shuffle(uids)
        return uids[:count]

    def wait_select_uids(self, count, timeout_sec=30, prefer_running=True, exclude_store=False, minimum=1):
        deadline = time.time() + timeout_sec
        uids = []
        minimum = max(1, min(int(minimum), int(count)))
        while time.time() < deadline:
            uids = self.select_uids(count, prefer_running=prefer_running, exclude_store=exclude_store)
            if len(uids) >= minimum:
                return uids
            time.sleep(3)
        return uids

    def wait_user_actor_commands_ready(self, event, timeout_sec=60, interval_sec=2, allow_empty=False):
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            result = self.safe_call("schedulerStatus", {})
            last = (result.get("result") or {}) if isinstance(result, dict) else {}
            if scheduler_user_commands_ready(last, allow_empty=allow_empty):
                self.log("wait_user_actor_commands_ready ready event=%s status=%s" % (event, json_text(last, 1200)))
                return True
            time.sleep(interval_sec)
        self.log("wait_user_actor_commands_ready timeout event=%s status=%s" % (event, json_text(last, 1200)))
        return False

    def robot_call_when_ready(self, command, payload, label, allow_empty=False, sample=False, wait_ready=True):
        structural = command in ("robotsOnlineAsync", "robotsLogoutAsync")
        if not structural and wait_ready:
            self.wait_user_actor_commands_ready(label + "_before", 60, 2, allow_empty=allow_empty)
        before_result = self.safe_call("autoStatus", {})
        before_status = (before_result.get("result") or {}) if isinstance(before_result, dict) else {}
        result = self.robot_call(command, payload, label, sample=sample, require_ok=False)
        if robot_command_retryable(result):
            self.log("%s retry_after_scheduler_busy" % label)
            if structural:
                self.wait_scheduler_operation_idle(label + "_retry", 60, 2)
            else:
                self.wait_user_actor_commands_ready(label + "_retry", 60, 2, allow_empty=allow_empty)
            result = self.robot_call(command, payload, label + "_retry", sample=sample, require_ok=False)
        if api_result_timed_out(result) and self.wait_robot_action_effect(command, before_status, 30, 2):
            result = {
                "ok": True,
                "result": {"state": "observed_after_timeout", "command": command},
                "observed_after_timeout": True,
                "transport_error": result.get("error"),
            }
            self.log("%s observed_after_timeout command=%s" % (label, command))
        if not (isinstance(result, dict) and result.get("ok")):
            self.record_failure("%s_%s" % (sanitize_name(label), command), "%s failed: %s" % (command, json_text(result, 1200)))
        return result

    def wait_scheduler_operation_idle(self, event, timeout_sec=60, interval_sec=2):
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            result = self.safe_call("schedulerStatus", {})
            last = (result.get("result") or {}) if isinstance(result, dict) else {}
            active = bool(last.get("operation_active")) or safe_text(last.get("recent_operation_state")) == "running"
            if not active:
                self.log("wait_scheduler_operation_idle ready event=%s status=%s" % (event, json_text(last, 1000)))
                return True
            time.sleep(interval_sec)
        self.log("wait_scheduler_operation_idle timeout event=%s status=%s" % (event, json_text(last, 1200)))
        return False

    def wait_robot_action_effect(self, command, before_status, timeout_sec=30, interval_sec=2):
        before = robot_action_counter(before_status, command)
        if before is None:
            return False
        deadline = time.time() + timeout_sec
        last = before
        while time.time() < deadline:
            result = self.safe_call("autoStatus", {})
            status = (result.get("result") or {}) if isinstance(result, dict) else {}
            last = robot_action_counter(status, command)
            if last is not None and last > before:
                self.log("wait_robot_action_effect ready command=%s before=%s after=%s" % (command, before, last))
                return True
            time.sleep(interval_sec)
        self.log("wait_robot_action_effect timeout command=%s before=%s after=%s" % (command, before, last))
        return False

    def wait_uids_inactive(self, label, uids, timeout_sec=30, interval_sec=3):
        wanted = set(int(uid) for uid in uids if int(uid) > 0)
        deadline = time.time() + timeout_sec
        remaining = set(wanted)
        while wanted and time.time() < deadline:
            rows = self.status_rows(max(self.args.status_count, len(wanted) * 4))
            remaining = set(
                int(row.get("uid") or 0)
                for row in rows
                if int(row.get("uid") or 0) in wanted and status_row_is_active(row)
            )
            if not remaining:
                self.log("wait_uids_inactive ready label=%s count=%s" % (label, len(wanted)))
                return True
            time.sleep(interval_sec)
        self.log("wait_uids_inactive timeout label=%s remaining=%s" % (label, sorted(remaining)))
        return False

    def wait_uids_store_active(self, label, uids, minimum=1, timeout_sec=60, interval_sec=5):
        wanted = set(int(uid) for uid in uids if int(uid) > 0)
        minimum = max(1, min(int(minimum), len(wanted))) if wanted else 1
        deadline = time.time() + timeout_sec
        observed = set()
        peak = 0
        while wanted and time.time() < deadline:
            rows = self.status_rows(max(self.args.status_count, len(wanted) * 4))
            current = set(
                int(row.get("uid") or 0)
                for row in rows
                if int(row.get("uid") or 0) in wanted and status_row_has_store(row)
            )
            observed.update(current)
            peak = max(peak, len(current))
            if len(observed) >= minimum:
                detail = "observed=%s/%s peak=%s minimum=%s" % (len(observed), len(wanted), peak, minimum)
                self.mark_coverage(label, True, detail)
                return True
            time.sleep(interval_sec)
        detail = "observed=%s/%s peak=%s minimum=%s" % (len(observed), len(wanted), peak, minimum)
        self.mark_coverage(label, False, detail)
        self.record_failure(label, "targeted store action did not become active: %s" % detail)
        return False

    def robot_call(self, command, payload, label, sample=True, require_ok=False):
        res = self.safe_call(command, payload)
        self.log("%s command=%s payload=%s result=%s" % (label, command, payload, json_text(res, 1800)))
        if require_ok and not (isinstance(res, dict) and res.get("ok")):
            self.record_failure("%s_%s" % (sanitize_name(label), command), "%s failed: %s" % (command, json_text(res, 1200)))
        if sample:
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

    def announcement_check(self, observe_sec=None):
        self.log("announcement_check begin")
        res = self.safe_call("systemAnnouncement", {})
        self.log("announcement_check result=%s" % json_text(res, 1600))
        if not (isinstance(res, dict) and res.get("ok")):
            raise RuntimeError("systemAnnouncement failed: %s" % json_text(res, 1000))
        self.sample_with_event("announcement_check")
        if observe_sec:
            self.burst_sample("announcement_recover", observe_sec, 15)

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

    def party_log_counts(self, text=None):
        patterns = {
            "party_total": r"\[PARTY_",
            "relay_errors": r"PARTY_RELAY_.*ERROR|PARTY_RELAY_BAD_PACKET",
            "udp_errors": r"PARTY_UDP_.*ERROR|PARTY_ROBOT_PROBE_ERROR",
            "tqos_exhausted": r"PARTY_TQOS_RETRY_EXHAUSTED",
            "route_degraded": r"PARTY_ROUTE_DEGRADED",
            "route_recovery": r"PARTY_ROUTE_RECOVERY\]",
            "route_recovered": r"PARTY_ROUTE_RECOVERED",
            "route_failover": r"PARTY_ROUTE_FAILOVER",
            "relay_connected": r"PARTY_RELAY_CONNECTED",
            "probe_cycles": r"PARTY_ROBOT_PROBE_CYCLE",
            "peer_ready": r"PARTY_ROBOT_TQOS_READY",
            "self_id_refresh": r"PARTY_SELF_ID_REFRESH",
            "self_id_recovered": r"PARTY_SELF_ID_RECOVERED",
            "self_id_recycle": r"PARTY_SELF_ID_RECYCLE",
            "udp_recycles": r"PARTY_UDP_(?:RECYCLE|RECOVERED)",
            "transport_cleared": r"PARTY_TRANSPORT_CLEARED",
            "peer_transport_reset": r"PARTY_PEER_TRANSPORT_RESET",
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

    def capture_log_cursor(self, path):
        try:
            stat = os.stat(path)
            return {
                "path": path,
                "inode": int(stat.st_ino),
                "offset": int(stat.st_size),
                "captured_at": time.time(),
            }
        except OSError:
            return {"path": path, "inode": 0, "offset": 0, "captured_at": time.time()}

    def read_log_segment(self, path, offset, max_bytes):
        try:
            with open(path, "rb") as fh:
                fh.seek(max(0, int(offset)), os.SEEK_SET)
                data = fh.read(max_bytes + 1)
            if len(data) > max_bytes:
                data = data[-max_bytes:]
            return safe_text(data)
        except Exception:
            return u""

    def read_log_since(self, cursor, max_bytes=16 * 1024 * 1024):
        path = cursor.get("path") or ""
        inode = to_int(cursor.get("inode"))
        captured_at = float(cursor.get("captured_at") or 0)
        entries = []
        for candidate in glob.glob(path + "*"):
            try:
                stat = os.stat(candidate)
            except OSError:
                continue
            if not os.path.isfile(candidate):
                continue
            entries.append((float(stat.st_mtime), candidate, int(stat.st_ino), int(stat.st_size)))
        entries.sort(key=lambda item: (item[0], item[1]))
        parts = []
        matched_old = False
        for _, candidate, candidate_inode, candidate_size in entries:
            if inode and candidate_inode == inode:
                offset = cursor.get("offset") or 0
                if candidate_size < to_int(offset):
                    offset = 0
                parts.append(self.read_log_segment(candidate, offset, max_bytes))
                matched_old = True
                break
        for modified, candidate, candidate_inode, _ in entries:
            if inode and candidate_inode == inode:
                continue
            if modified + 1 < captured_at:
                continue
            parts.append(self.read_log_segment(candidate, 0, max_bytes))
        if not matched_old and not parts and os.path.isfile(path):
            parts.append(self.read_log_segment(path, 0, max_bytes))
        text = u"\n".join(part for part in parts if part)
        return text[-max_bytes:]

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
        failover_pattern = re.compile(r"\[PARTY_ROUTE_FAILOVER\][^\n]*uid=(\d+)[^\n]*peer=(\d+)[^\n]*failed_route=(\d+)")
        reset_pattern = re.compile(r"\[PARTY_PEER_TRANSPORT_RESET\][^\n]*uid=(\d+)[^\n]*peer=(\d+)")
        ready_pattern = re.compile(r"\[PARTY_ROBOT_TQOS_READY\][^\n]*uid=(\d+)[^\n]*peer=(\d+)[^\n]*route=(\d+)")
        states = {}
        for line in safe_text(text).splitlines():
            match = route_pattern.search(line)
            if match:
                key = (match.group(2), match.group(3), match.group(4))
                state = match.group(1).lower()
                states[key] = state
                continue
            match = failover_pattern.search(line)
            if match:
                states[(match.group(1), match.group(2), match.group(3))] = "failover"
                continue
            match = reset_pattern.search(line)
            if match:
                uid, peer = match.group(1), match.group(2)
                for key in list(states):
                    if key[0] == uid and key[1] == peer:
                        states[key] = "reset"
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

    def market_workflow(self):
        self.log("market_workflow begin")
        preferred_targets = [
            (28237, "swordman_beamsword"),
            (37603, "thief_wand"),
            (37605, "thief_dagger"),
        ]
        try:
            stop = self.safe_call("marketStop", {})
            self.log("market_workflow stop_auto=%s" % json_text(stop, 1200))
            if not self.wait_market_job_idle("market_workflow_prepare", 300, 5):
                self.record_failure("market_workflow_busy", "market job did not become idle before workflow")

            clear = self.market_call_when_idle("marketClearSystemStock", {}, "market_workflow_clear", attempts=24, delay_sec=5)
            self.log("market_workflow clear=%s" % json_text(clear, 1800))
            cleared = self.wait_market_count(
                "market_workflow_clear",
                lambda counts: (
                    to_int(counts.get("auction_records")) <= 0
                    and to_int(counts.get("cera_records")) <= 0
                    and to_int(counts.get("creature_instances_records")) <= 0
                ),
                120,
                5,
            )
            if (
                to_int(cleared.get("auction_records")) > 0
                or to_int(cleared.get("cera_records")) > 0
                or to_int(cleared.get("creature_instances_records")) > 0
            ):
                self.record_failure("market_clear_incomplete", "system market stock remained after clear: %s" % json_text(cleared, 1200))

            sync = self.safe_call("marketSyncItemInfo", {})
            self.log("market_workflow sync_iteminfo=%s" % json_text(sync, 2200))
            services_ready = self.wait_market_services("market_workflow_sync_services", 240, 10)
            if not services_ready:
                self.record_failure("market_sync_services_failed", "market services did not recover after iteminfo sync")

            targets, target_detail = self.available_market_targets(preferred_targets)
            target_ids = [item_id for item_id, _ in targets]
            self.mark_coverage("market_weapon_target_catalog", len(target_ids) >= 2, target_detail)
            if len(target_ids) < 2:
                self.record_failure("market_weapon_target_catalog", "fewer than two preferred target items are supported: %s" % target_detail)
                return
            iteminfo = self.auction_iteminfo_presence(target_ids)
            self.log("market_workflow target_iteminfo=%s" % json_text(iteminfo, 1200))
            missing_iteminfo = [
                "%s:%s" % (item_id, label)
                for item_id, label in targets
                if not iteminfo.get(str(item_id))
            ]
            sync_observed = bool(
                (isinstance(sync, dict) and sync.get("ok"))
                or (api_result_timed_out(sync) and services_ready and not missing_iteminfo)
            )
            self.mark_coverage(
                "market_iteminfo_sync",
                sync_observed,
                "transport_ok=%s timed_out=%s services=%s missing=%s" % (
                    bool(isinstance(sync, dict) and sync.get("ok")),
                    api_result_timed_out(sync),
                    services_ready,
                    ",".join(missing_iteminfo),
                ),
            )
            if not sync_observed:
                self.record_failure("market_sync_iteminfo_failed", "marketSyncItemInfo did not reach a valid final state: %s" % json_text(sync, 1200))
            if missing_iteminfo:
                self.record_failure("market_weapon_target_iteminfo_missing", "target iteminfo missing: %s" % ",".join(missing_iteminfo))

            weapon = self.market_call_when_idle(
                "marketRestockOnce",
                {
                    "market": "auction",
                    "execute": True,
                    "max_actions": 64,
                    "max_concurrent": 4,
                    "continue_on_error": True,
                    "item_ids": target_ids,
                },
                "market_workflow_weapon",
                attempts=24,
                delay_sec=5,
            )
            self.log("market_workflow weapon=%s" % json_text(weapon, 2600))
            self.validate_market_action_prices(weapon, "market_workflow_weapon")
            outcomes = target_action_outcomes(weapon, target_ids)
            accepted_ids = [item_id for item_id in target_ids if (outcomes.get(item_id) or {}).get("accepted")]
            counts = self.wait_auction_item_counts(accepted_ids, 90, 5)
            labels = dict(targets)
            no_actions = []
            accepted_missing = []
            rejected = []
            for item_id in target_ids:
                label = "%s:%s" % (item_id, labels[item_id])
                outcome = outcomes.get(item_id) or {}
                if to_int(outcome.get("actions")) <= 0:
                    no_actions.append(label)
                elif outcome.get("accepted") and to_int(counts.get(str(item_id))) <= 0:
                    accepted_missing.append(label)
                elif not outcome.get("accepted"):
                    rejected.append(label)
            if no_actions:
                self.record_failure("market_weapon_target_no_actions", "target item ids produced no actions: %s" % ",".join(no_actions))
            if accepted_missing:
                self.record_failure("market_weapon_target_missing", "accepted target item ids missing from stock: %s" % ",".join(accepted_missing))
            if rejected:
                self.log("market_workflow weapon_server_rejected=%s" % ",".join(rejected))
            self.mark_coverage("market_weapon_targets", not no_actions and not accepted_missing, json_text(outcomes, 1200))

            special_result = self.market_call_when_idle(
                "marketRestockOnce",
                {"market": "auction", "execute": True, "max_actions": 1000, "max_concurrent": 8, "continue_on_error": True},
                "market_workflow_special",
                attempts=24,
                delay_sec=5,
            )
            self.log("market_workflow special=%s" % json_text(special_result, 2600))
            self.validate_market_action_prices(special_result, "market_workflow_special")
            special_action_ok = self.market_result_has_special_success(special_result)
            special_counts = self.wait_market_count(
                "market_workflow_special",
                lambda value: to_int(value.get("auction_high_addinfo_records")) + to_int(value.get("auction_creature_records")) > 0,
                180,
                10,
            )
            special = to_int(special_counts.get("auction_high_addinfo_records")) + to_int(special_counts.get("auction_creature_records"))
            if to_int(special_counts.get("auction_records")) > 0 and special <= 0 and not special_action_ok:
                self.record_failure("market_special_no_records", "special auction restock produced no special records")
            self.mark_coverage("market_special_stock", special > 0 or special_action_ok, json_text(special_counts, 1200))

            cera_result = self.market_call_when_idle(
                "marketRestockOnce",
                {"market": "cera", "execute": True, "max_actions": 256, "max_concurrent": 8, "continue_on_error": True},
                "market_workflow_cera",
                attempts=36,
                delay_sec=5,
            )
            self.log("market_workflow cera=%s" % json_text(cera_result, 2400))
            self.validate_market_action_prices(cera_result, "market_workflow_cera")
            cera_counts = self.wait_market_count(
                "market_workflow_cera",
                lambda value: to_int(value.get("cera_records")) > 0,
                180,
                10,
            )
            if to_int(cera_counts.get("cera_records")) <= 0:
                self.record_failure("market_cera_empty", "CERA restock produced no records")
            self.mark_coverage("market_cera_stock", to_int(cera_counts.get("cera_records")) > 0, json_text(cera_counts, 1000))

            collect = self.market_call_when_idle(
                "marketCollectOnce",
                {"market": "", "execute": True, "max_actions": 384, "max_concurrent": 8, "continue_on_error": True},
                "market_workflow_collect",
                attempts=24,
                delay_sec=5,
            )
            self.log("market_workflow collect=%s" % json_text(collect, 2200))
            self.validate_market_action_prices(collect, "market_workflow_collect")
            collect_ok = bool(isinstance(collect, dict) and collect.get("ok"))
            if not collect_ok:
                self.record_failure("market_workflow_collect", "mixed auction/CERA collect failed: %s" % json_text(collect, 1200))
            self.mark_coverage("market_operation_mix", collect_ok, "targeted auction, general auction, CERA restock and mixed collect executed")
        finally:
            self.market_enable_auto(max_concurrent=8)
            if not self.wait_market_auto_running("market_workflow_auto", 180, 10):
                self.record_failure("market_workflow_auto_not_running", "market auto did not resume")
            final_counts = self.wait_market_count(
                "market_workflow_final",
                lambda value: to_int(value.get("auction_records")) > 0 and to_int(value.get("cera_records")) > 0,
                180,
                10,
            )
            if to_int(final_counts.get("creature_orphans_records")) > 0:
                self.record_failure("market_creature_orphans", "creature instance orphan rows=%s" % final_counts.get("creature_orphans_records"))
            self.sample_with_event("market_workflow_final")
        self.log("market_workflow done")

    def wait_auction_item_counts(self, item_ids, timeout_sec=90, interval_sec=5):
        if not item_ids:
            return {}
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            last = self.auction_item_counts(item_ids)
            if all(to_int(last.get(str(item_id))) > 0 for item_id in item_ids):
                self.log("wait_auction_item_counts ready ids=%s counts=%s" % (item_ids, json_text(last, 1000)))
                return last
            self.sample_shared_progress("auction_item_counts", max(5, interval_sec))
            time.sleep(interval_sec)
        self.log("wait_auction_item_counts timeout ids=%s counts=%s" % (item_ids, json_text(last, 1000)))
        return last

    def market_fault_matrix(self):
        self.log("market_fault_matrix begin")
        self.run_phase_step("market_service_faults", self.market_service_fault_matrix, recover=False)
        if self.phase_allowed():
            self.run_phase_step("market_source_faults", self.market_source_fault_matrix, recover=False)
        self.market_enable_auto(max_concurrent=8, max_actions=512)
        if not self.wait_market_services("market_fault_matrix_final", 120, 5):
            self.record_failure("market_fault_matrix_services", "market services did not recover after fault matrix")
        if not self.wait_market_auto_running("market_fault_matrix_auto", 120, 5):
            self.record_failure("market_fault_matrix_auto", "market auto did not resume after fault matrix")
        self.sample_with_event("market_fault_matrix_done")
        self.log("market_fault_matrix done")

    def market_service_fault_matrix(self):
        conflict_ports = sorted(set([self.port("Point"), self.port("Auction")]))
        conflict_ports = [port for port in conflict_ports if port > 0]
        if not conflict_ports:
            raise RuntimeError("point/auction ports are not configured")
        self.safe_call("marketStop", {})
        self.wait_market_job_idle("market_service_faults_prepare", 300, 5)
        self.stop_market_services()
        tuple_text = ",".join(str(port) for port in conflict_ports)
        if len(conflict_ports) == 1:
            tuple_text += ","
        command = "cat >/tmp/vm_random_port_conflict.py <<'PY'\nimport socket,time\nsockets=[]\nfor port in (%s):\n    sock=socket.socket()\n    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)\n    sock.bind(('0.0.0.0', port))\n    sock.listen(1)\n    sockets.append(sock)\ntime.sleep(90)\nPY\nnohup python /tmp/vm_random_port_conflict.py >/dev/null 2>&1 &" % tuple_text
        helper_running = False
        backups = []
        try:
            self.shell(command, 10)
            helper_running = True
            process = ""
            for _ in range(10):
                process = self.shell("pgrep -af '[/]tmp/vm_random_port_conflict.py' || true", 10, log_output=False)
                if "vm_random_port_conflict.py" in safe_text(process):
                    break
                time.sleep(1)
            if "vm_random_port_conflict.py" not in safe_text(process):
                raise RuntimeError("port conflict helper did not start")
            ensure = self.market_ensure_services()
            status = self.wait_market_fault_state("market_service_port_conflict", 45, 3)
            self.sample_with_event("market_service_port_conflict")
            false_ready = self.market_services_ready(status)
            settled = market_fault_state_observed(status)
            detail = "ensure=%s services=%s" % (json_text(ensure, 700), json_text(status.get("services") or {}, 1200))
            self.mark_coverage("market_port_conflict", settled and not false_ready, detail)
            if not settled:
                self.record_failure("market_port_conflict_unobserved", "market service state did not settle during port conflict")
            if false_ready:
                self.record_failure("market_port_conflict_false_ready", "market services reported ready while external listeners owned their ports")
            api = self.safe_call("systemStatus", {})
            if not (isinstance(api, dict) and api.get("ok")):
                self.record_failure("market_port_conflict_robot_api", "robot API failed during port conflict")

            self.shell("pkill -f '[/]tmp/vm_random_port_conflict.py' || true; rm -f /tmp/vm_random_port_conflict.py", 20)
            helper_running = False
            self.stop_market_services()

            backups = [self.backup_file(self.auction_iteminfo), self.backup_file(self.point_iteminfo)]
            if not all(backups):
                raise RuntimeError("iteminfo fault matrix backup failed")
            for mode in ("missing", "malformed", "stale"):
                self.stop_market_services()
                self.apply_iteminfo_fault(mode)
                injected = self.iteminfo_fault_applied(mode)
                ensure = self.market_ensure_services()
                status = self.wait_market_fault_state("market_iteminfo_%s" % mode, 45, 3)
                api = self.safe_call("systemStatus", {})
                healthy = bool(isinstance(api, dict) and api.get("ok"))
                settled = market_fault_state_observed(status)
                observed = healthy and settled and injected
                detail = "injected=%s ensure=%s services=%s" % (
                    injected,
                    json_text(ensure, 700),
                    json_text(status.get("services") or {}, 1100),
                )
                self.mark_coverage("market_iteminfo_%s" % mode, observed, detail)
                if not healthy:
                    self.record_failure("market_iteminfo_%s_robot_api" % mode, "robot API failed during iteminfo fault")
                if not settled:
                    self.record_failure("market_iteminfo_%s_unobserved" % mode, "market service state did not settle")
                if not injected:
                    self.record_failure("market_iteminfo_%s_injection" % mode, "iteminfo mutation was not present on both service paths")
                self.sample_with_event("market_iteminfo_%s" % mode)
        finally:
            if helper_running:
                self.shell("pkill -f '[/]tmp/vm_random_port_conflict.py' || true; rm -f /tmp/vm_random_port_conflict.py", 20)
            self.stop_market_services()
            fallback_paths = [
                os.path.join(self.baseline_dir, "auction_iteminfo.dat"),
                os.path.join(self.baseline_dir, "point_iteminfo.dat"),
            ]
            for path, backup, fallback in zip(
                [self.auction_iteminfo, self.point_iteminfo],
                backups,
                fallback_paths,
            ):
                if backup:
                    self.restore_file(path, backup)
                else:
                    self.shell("cp -af %s %s" % (shell_quote(fallback), shell_quote(path)), 20)
            self.market_ensure_services()
            if not self.wait_market_services("market_service_faults_recover", 120, 5):
                self.record_failure("market_service_faults_recovery", "market services did not recover")

    def wait_market_fault_state(self, event, timeout_sec=45, interval_sec=3):
        deadline = time.time() + timeout_sec
        last = {}
        previous_signature = ()
        stable_samples = 0
        while time.time() < deadline:
            last = self.market_status_result()
            if market_fault_state_settled(last):
                self.log("wait_market_fault_state ready event=%s services=%s" % (event, json_text(last.get("services") or {}, 1200)))
                return last
            signature = market_fault_signature(last)
            if signature and not self.market_services_ready(last):
                stable_samples = stable_samples + 1 if signature == previous_signature else 1
                previous_signature = signature
                if stable_samples >= 3:
                    last["_stable_fault_observed"] = True
                    last["_stable_fault_samples"] = stable_samples
                    self.log(
                        "wait_market_fault_state stable event=%s samples=%s services=%s"
                        % (event, stable_samples, json_text(last.get("services") or {}, 1200))
                    )
                    return last
            else:
                previous_signature = ()
                stable_samples = 0
            time.sleep(interval_sec)
        self.log("wait_market_fault_state timeout event=%s services=%s" % (event, json_text(last.get("services") or {}, 1200)))
        return last

    def apply_iteminfo_fault(self, mode):
        if mode == "missing":
            command = "rm -f %s %s" % (shell_quote(self.auction_iteminfo), shell_quote(self.point_iteminfo))
        elif mode == "malformed":
            command = "printf 'bad iteminfo\\n1 broken row\\n999999999 0 x x x\\n' > %s; cp -f %s %s" % (
                shell_quote(self.auction_iteminfo), shell_quote(self.auction_iteminfo), shell_quote(self.point_iteminfo),
            )
        elif mode == "stale":
            command = "printf '1 0 1 1 1 1 1 1 1 1 1 1 1 1 `bad` `bad` 1\\n' > %s; cp -f %s %s" % (
                shell_quote(self.auction_iteminfo), shell_quote(self.auction_iteminfo), shell_quote(self.point_iteminfo),
            )
        else:
            raise RuntimeError("unknown iteminfo fault mode %s" % mode)
        self.shell(command, 20)

    def iteminfo_fault_applied(self, mode):
        paths = (self.auction_iteminfo, self.point_iteminfo)
        if mode == "missing":
            return not any(os.path.exists(path) for path in paths)
        prefix = "bad iteminfo" if mode == "malformed" else "1 0 1 1"
        for path in paths:
            try:
                with open(path, "rb") as fh:
                    head = safe_text(fh.read(64))
                if not head.startswith(prefix):
                    return False
            except Exception:
                return False
        return True

    def market_source_fault_applied(self, mode, paths):
        if mode == "missing":
            return not any(os.path.exists(path) for path in paths)
        if mode == "partial":
            prefixes = ("[{\"id\":1", "[{\"id\":2", "1 broken partial")
            for path, prefix in zip(paths, prefixes):
                try:
                    with open(path, "rb") as fh:
                        if not safe_text(fh.read(64)).startswith(prefix):
                            return False
                except Exception:
                    return False
            return True
        if mode == "broken_pvf":
            try:
                with open(paths[0], "rb") as fh:
                    return safe_text(fh.read(64)) == "encrypted-or-broken-pvf"
            except Exception:
                return False
        return False

    def observe_market_source_fault(self, mode, result, injected, log_cursor=None):
        self.log("market_source_fault mode=%s result=%s" % (mode, json_text(result, 2400)))
        self.validate_market_action_prices(result, "market_source_%s" % mode)
        api = self.safe_call("systemStatus", {})
        healthy = bool(isinstance(api, dict) and api.get("ok"))
        if mode in ("missing", "partial"):
            delta = self.read_log_since(log_cursor or {}, 2 * 1024 * 1024)
            fault_observed = "pvf_catalog" in delta and "fallback" in delta
        else:
            fault_observed = isinstance(result, dict) and not result.get("ok") and bool(result.get("error"))
        observed = healthy and injected and fault_observed
        detail = "healthy=%s injected=%s fault_observed=%s result=%s" % (healthy, injected, fault_observed, json_text(result, 850))
        self.mark_coverage("market_source_%s" % mode, observed, detail)
        if not healthy:
            self.record_failure("market_source_%s_robot_api" % mode, "robot API failed during source fault")
        if not injected:
            self.record_failure("market_source_%s_injection" % mode, "source mutation was not present before the probe")
        if not fault_observed:
            self.record_failure("market_source_%s_unobserved" % mode, "fault path was not observed by market logic")
        self.sample_with_event("market_source_%s" % mode)

    def market_source_fault_matrix(self):
        self.safe_call("marketStop", {})
        if not self.wait_market_job_idle("market_source_faults_prepare", 300, 5):
            self.record_failure("market_source_faults_busy", "market job did not become idle before source faults")
        paths = [
            "/root/config/pvf_equipment_catalog.json",
            "/root/config/pvf_stackable_catalog.json",
            "/root/config/pvf_iteminfo.dat",
        ]
        backups = [self.backup_file(path) for path in paths]
        pvf_backup = ""
        pvf_modified = False
        if not all(backups):
            for path, backup in zip(paths, backups):
                if backup:
                    self.restore_file(path, backup)
            self.market_enable_auto(max_concurrent=8, max_actions=256)
            self.wait_market_services("market_source_backup_failure_recover", 180, 10)
            raise RuntimeError("market source fault matrix backup failed")
        try:
            for mode in ("missing", "partial"):
                log_cursor = self.capture_log_cursor("/root/config/market_log.jsonl")
                if mode == "missing":
                    self.shell("rm -f %s" % " ".join(shell_quote(path) for path in paths), 20)
                else:
                    self.shell(
                        "printf '[{\"id\":1,\"price\":1' > /root/config/pvf_equipment_catalog.json; "
                        "printf '[{\"id\":2' > /root/config/pvf_stackable_catalog.json; "
                        "printf '1 broken partial' > /root/config/pvf_iteminfo.dat",
                        20,
                    )
                injected = self.market_source_fault_applied(mode, paths)
                result = self.market_call_when_idle(
                    "marketRestockOnce",
                    {"market": "auction", "execute": True, "max_actions": 16, "max_concurrent": 4, "continue_on_error": True},
                    "market_source_%s" % mode,
                    attempts=6,
                    delay_sec=2,
                )
                self.observe_market_source_fault(mode, result, injected, log_cursor)

            for path, backup in zip(paths, backups):
                self.restore_file(path, backup)
            backups = ["", "", ""]

            pvf_backup = self.backup_file(self.script_pvf)
            if not pvf_backup:
                raise RuntimeError("Script.pvf backup failed before broken PVF probe")
            self.shell("printf 'encrypted-or-broken-pvf' > %s" % shell_quote(self.script_pvf), 20)
            pvf_modified = True
            injected = self.market_source_fault_applied("broken_pvf", [self.script_pvf])
            result = self.safe_call("marketSyncItemInfo", {})
            self.observe_market_source_fault("broken_pvf", result, injected)
        finally:
            for path, backup in zip(paths, backups):
                if backup:
                    self.restore_file(path, backup)
            if pvf_backup:
                self.restore_file(self.script_pvf, pvf_backup)
            restored = {}
            if pvf_modified:
                restored = self.safe_call("marketSyncItemInfo", {})
                self.log("market_source_broken_pvf restored_sync=%s" % json_text(restored, 1800))
            self.market_ensure_services()
            services_ready = self.wait_market_services("market_source_faults_recover", 120, 5)
            if pvf_modified and not (
                (isinstance(restored, dict) and restored.get("ok"))
                or (api_result_timed_out(restored) and services_ready)
            ):
                self.record_failure("market_source_broken_pvf_restore_sync", "iteminfo sync did not recover after Script.pvf restore")
            if not services_ready:
                self.record_failure("market_source_faults_recovery", "market services did not recover")

    def available_market_targets(self, preferred):
        catalog_path = "/root/config/pvf_equipment_catalog.json"
        catalog_ids = set()
        try:
            with io.open(catalog_path, "r", encoding="utf-8") as fh:
                catalog = json.load(fh)
            if isinstance(catalog, dict):
                catalog = catalog.get("items") or catalog.get("equipment") or []
            for item in catalog if isinstance(catalog, list) else []:
                if not isinstance(item, dict):
                    continue
                item_id = to_int(item.get("id") or item.get("item_id"))
                if item_id > 0:
                    catalog_ids.add(item_id)
        except Exception as exc:
            self.log("available_market_targets catalog_error err=%r" % (exc,))
        iteminfo = self.auction_iteminfo_presence([item_id for item_id, _ in preferred])
        selected = select_supported_market_targets(preferred, catalog_ids, iteminfo)
        unavailable = [
            "%s:%s%s" % (
                item_id,
                label,
                "(catalog)" if item_id not in catalog_ids else "(iteminfo)" if not iteminfo.get(str(item_id)) else "",
            )
            for item_id, label in preferred
            if (item_id, label) not in selected
        ]
        return selected, "selected=%s unavailable=%s catalog_ids=%s iteminfo=%s" % (
            ",".join(str(item_id) for item_id, _ in selected),
            ",".join(unavailable) or "none",
            len(catalog_ids),
            json_text(iteminfo, 800),
        )

    def auction_iteminfo_presence(self, item_ids):
        wanted = set()
        for item_id in item_ids:
            try:
                wanted.add(int(item_id))
            except Exception:
                pass
        result = dict((str(item_id), False) for item_id in wanted)
        paths = [self.auction_iteminfo, "/root/config/pvf_iteminfo.dat"]
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
                # Stackable listings use -1 as the protocol's no-bidding
                # sentinel so AUCTION_BUY_ITEM_APIECE can buy part of a stack.
                # Other auction kinds still require a normal non-negative bid.
                if start < -1 or (start == -1 and kind != "stackable"):
                    self.record_failure("market_action_negative_start_price", "%s start=%s instant=%s" % (prefix, start, instant))
                if start != -1 and start >= instant:
                    self.record_failure("market_action_invalid_price_order", "%s start=%s instant=%s" % (prefix, start, instant))
                if kind == "stackable" and start != -1:
                    self.record_failure("market_action_invalid_stackable_start_price", "%s start=%s want=-1" % (prefix, start))
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

    def market_enable_auto(self, max_concurrent=8, max_actions=DEFAULT_MARKET_ACTION_BUDGET):
        res = self.safe_call("marketConfigUpdate", {
            "auto_enabled": True,
            "interval_ms": 60000,
            "max_actions": max_actions,
            "max_concurrent": max_concurrent,
            "continue_on_error": True,
            "markets": ["auction", "cera"],
        })
        self.log("market_enable_auto result=%s" % json_text(res, 1600))
        res = self.safe_call("marketStart", {})
        self.log("marketStart result=%s" % json_text(res, 1600))
        return res

    def market_ensure_services(self, markets=None):
        payload = {}
        if markets:
            payload["markets"] = list(markets)
        try:
            api = RobotAPI(self.args.robot_host, self.port("RobotAPI"), max(75.0, self.args.api_timeout))
            res = api.call("marketEnsureServices", payload)
        except Exception as exc:
            self.log("api_error command=marketEnsureServices err=%r" % (exc,))
            res = {"ok": False, "error": repr(exc)}
        result = (res.get("result") or {}) if isinstance(res, dict) else {}
        self.log("marketEnsureServices payload=%s result=%s" % (json_text(payload, 500), json_text(res, 1600)))
        return result

    def market_call_when_idle(self, command, payload, label, attempts=12, delay_sec=5):
        last = {}
        for idx in range(attempts):
            last = self.safe_call(command, payload)
            result = (last.get("result") or {}) if isinstance(last, dict) else {}
            status = safe_text(result.get("status") or "")
            error = safe_text(last.get("error") if isinstance(last, dict) else "")
            if "timed out" in error:
                self.log("%s command=%s timeout_wait_idle result=%s" % (label, command, json_text(last, 1200)))
                idle = self.wait_market_job_idle(label + ":" + command, 300, 5)
                status_snapshot = self.market_status_result()
                job = status_snapshot.get("last_job") or {}
                expected_kind = {
                    "marketRestockOnce": "restock",
                    "marketCollectOnce": "collect",
                }.get(command, "")
                observed = bool(
                    idle
                    and market_job_terminal_success(job)
                    and (not expected_kind or safe_text(job.get("kind")) == expected_kind)
                )
                return {
                    "ok": observed,
                    "error": safe_text(job.get("error")) if not observed else "",
                    "result": job,
                    "observed_after_timeout": True,
                    "transport_error": error,
                }
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
            self.sample_shared_progress("market_job:%s" % event, max(5, interval_sec))
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

    def stop_market_services(self):
        market_ports = self.port_regex(("Point", "Auction"))
        script = r"""
for p in $(pidof df_auction_r df_point_r 2>/dev/null); do kill -TERM $p || true; done
pkill -TERM -f '^/root/robot --bounded-log-sink /root/config/market_(auction|point)_service.log ' 2>/dev/null || true
for i in 1 2 3 4 5 6 7 8; do
  procs="$(pidof df_auction_r df_point_r 2>/dev/null)$(pgrep -f '^/root/robot --bounded-log-sink /root/config/market_(auction|point)_service.log ' 2>/dev/null || true)"
  listeners=$(ss -lnt 2>/dev/null | grep -E ':(%s)' || true)
  [ -z "$procs$listeners" ] && break
  sleep 1
done
for p in $(pidof df_auction_r df_point_r 2>/dev/null); do kill -KILL $p || true; done
pkill -KILL -f '^/root/robot --bounded-log-sink /root/config/market_(auction|point)_service.log ' 2>/dev/null || true
ss -lntp | grep -E ':(%s)' || true
pgrep -af 'df_auction_r|df_point_r' || true
""" % (market_ports, market_ports)
        self.shell(script, 40)

    def database_fault_matrix(self):
        self.log("database_fault_matrix begin")
        self.safe_call("marketStop", {})
        self.wait_market_job_idle("database_fault_matrix_prepare", 300, 5)
        dump = os.path.join(self.baseline_dir, "baseline_market_robot_stock.sql")
        if not os.path.isfile(dump) or os.path.getsize(dump) <= 0:
            raise RuntimeError("baseline market database backup is missing")
        try:
            self.run_phase_step("database_stock_schema", self.database_stock_schema_probe, recover=False)
            if self.phase_allowed():
                self.run_phase_step("database_robot_connection_drop", self.database_robot_connection_drop_probe, recover=False)
        finally:
            self.safe_call("marketStop", {})
            self.wait_market_job_idle("database_fault_matrix_restore", 300, 5)
            self.restore_market_database(dump, "database_fault_matrix_restore")
            self.market_enable_auto(max_concurrent=8)
            if not self.wait_market_services("database_fault_matrix_final", 120, 5):
                self.record_failure("database_fault_matrix_market", "market services did not recover after database matrix")
            if not self.wait_market_auto_running("database_fault_matrix_auto", 120, 5):
                self.record_failure("database_fault_matrix_auto", "market auto did not recover after database matrix")
            self.sample_with_event("database_fault_matrix_done")
        self.log("database_fault_matrix done")

    def database_stock_schema_probe(self):
        try:
            self.shell(
                "mysql -ugame -puu5!^%jg -e \"ALTER TABLE taiwan_cain_auction_gold.auction_main ADD COLUMN vm_random_dummy INT NULL; "
                "ALTER TABLE taiwan_cain_auction_cera.auction_main ADD COLUMN vm_random_dummy INT NULL;\" || true",
                120,
            )
            self.shell(
                "mysql -ugame -puu5!^%jg -e \"DELETE FROM taiwan_cain_auction_gold.auction_main WHERE owner_id >= 90000001; "
                "DELETE FROM taiwan_cain_auction_cera.auction_main WHERE owner_id >= 90000001; "
                "DELETE FROM taiwan_cain_2nd.creature_items WHERE charac_no >= 90000001;\"",
                60,
            )
            cleared = self.market_db_counts()
            self.log("database_stock_schema cleared=%s" % json_text(cleared, 1200))
            clear_ok = to_int(cleared.get("auction_records")) <= 0 and to_int(cleared.get("cera_records")) <= 0
            if not clear_ok:
                self.record_failure("database_external_clear_incomplete", "system stock remained after external delete")

            auction = self.market_call_when_idle(
                "marketRestockOnce",
                {"market": "auction", "execute": True, "max_actions": 256, "max_concurrent": 8, "continue_on_error": True},
                "database_stock_schema_auction",
                attempts=24,
                delay_sec=5,
            )
            cera = self.market_call_when_idle(
                "marketRestockOnce",
                {"market": "cera", "execute": True, "max_actions": 128, "max_concurrent": 8, "continue_on_error": True},
                "database_stock_schema_cera",
                attempts=24,
                delay_sec=5,
            )
            self.log("database_stock_schema auction=%s cera=%s" % (json_text(auction, 1800), json_text(cera, 1800)))
            self.validate_market_action_prices(auction, "database_stock_schema_auction")
            self.validate_market_action_prices(cera, "database_stock_schema_cera")
            operations_ok = bool(auction.get("ok") and cera.get("ok"))
            recovered = self.wait_market_count(
                "database_stock_schema_recover",
                lambda counts: to_int(counts.get("auction_records")) > 0 and to_int(counts.get("cera_records")) > 0,
                180,
                5,
            )
            stock_ok = to_int(recovered.get("auction_records")) > 0 and to_int(recovered.get("cera_records")) > 0
            self.mark_coverage("database_external_restock", clear_ok and stock_ok, json_text(recovered, 1200))
            if not stock_ok:
                self.record_failure("database_external_clear_not_restocked", "targeted auction and CERA restock did not repopulate stock")
            api = self.safe_call("databaseStatus", {})
            healthy = bool(isinstance(api, dict) and api.get("ok") and operations_ok and stock_ok)
            self.mark_coverage("database_schema_drift", healthy, "operations=%s status=%s" % (operations_ok, json_text(api, 1000)))
            if not healthy:
                self.record_failure("database_schema_drift_status", "stock operations or databaseStatus failed after additive schema drift")
            self.sample_with_event("database_stock_schema")
        finally:
            self.shell(
                "mysql -ugame -puu5!^%jg -e \"ALTER TABLE taiwan_cain_auction_gold.auction_main DROP COLUMN vm_random_dummy; "
                "ALTER TABLE taiwan_cain_auction_cera.auction_main DROP COLUMN vm_random_dummy;\" || true",
                120,
            )

    def robot_main_pids(self):
        output = self.shell("pgrep -f '^/root/robot$|^./robot$' || true", 10, log_output=False)
        return sorted(set(to_int(line.strip()) for line in safe_text(output).splitlines() if to_int(line.strip()) > 0))

    def process_socket_inodes(self, pids):
        inodes = set()
        for pid in pids:
            for path in glob.glob("/proc/%s/fd/*" % int(pid)):
                try:
                    target = os.readlink(path)
                except OSError:
                    continue
                match = re.match(r"^socket:\[(\d+)\]$", safe_text(target))
                if match:
                    inodes.add(match.group(1))
        return inodes

    def proc_tcp_rows(self):
        rows = []
        for path in ("/proc/net/tcp", "/proc/net/tcp6"):
            try:
                raw = io.open(path, "r", encoding="ascii").read()
            except (IOError, OSError):
                continue
            rows.extend(parse_proc_tcp_table(raw))
        return rows

    def mysql_processlist_rows(self):
        query = "SELECT ID,USER,HOST,IFNULL(DB,''),COMMAND FROM information_schema.PROCESSLIST"
        output = self.shell(MYSQL_CLI + " -B -N -e " + shell_quote(query), 20, log_output=False)
        return parse_mysql_processlist(output)

    def robot_mysql_connections(self):
        pids = self.robot_main_pids()
        inodes = self.process_socket_inodes(pids)
        connections = match_process_mysql_connections(inodes, self.proc_tcp_rows(), self.mysql_processlist_rows())
        self.log("robot_mysql_connections pids=%s inodes=%s connections=%s" % (
            pids,
            len(inodes),
            json_text(connections, 1600),
        ))
        return connections

    def kill_mysql_connections(self, connection_ids):
        commands = []
        for connection_id in sorted(set(to_int(value) for value in connection_ids if to_int(value) > 0)):
            query = "KILL CONNECTION %s" % connection_id
            commands.append(
                "%s -e %s >/dev/null 2>&1 && echo KILLED:%s || echo KILL_FAILED:%s"
                % (MYSQL_CLI, shell_quote(query), connection_id, connection_id)
            )
        output = self.shell("\n".join(commands), 30, log_output=False) if commands else ""
        killed = set(to_int(value) for value in re.findall(r"KILLED:(\d+)", safe_text(output)))
        return killed, output

    def wait_database_ready(self, event, timeout_sec=120, interval_sec=5):
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            last = self.safe_call("databaseStatus", {})
            if database_status_ready(last):
                self.log("wait_database_ready ready event=%s status=%s" % (event, json_text(last, 1200)))
                return last
            time.sleep(interval_sec)
        self.log("wait_database_ready timeout event=%s status=%s" % (event, json_text(last, 1200)))
        return last

    def database_robot_connection_drop_probe(self):
        initial = self.wait_database_ready("database_robot_connection_prepare", 30, 2)
        if not database_status_ready(initial):
            raise RuntimeError("database was not healthy before targeted connection drop")
        before = self.robot_mysql_connections()
        before_ids = set(to_int(row.get("id")) for row in before if to_int(row.get("id")) > 0)
        targeted = bool(before_ids)
        self.mark_coverage("database_robot_connections_targeted", targeted, json_text(before, 1600))
        if not targeted:
            raise RuntimeError("no MySQL connection owned by the robot process was found")

        killed, output = self.kill_mysql_connections(before_ids)
        killed_all = before_ids.issubset(killed)
        self.mark_coverage(
            "database_robot_connection_kill_acknowledged",
            bool(killed),
            "targeted=%s killed=%s all=%s output=%s" % (
                sorted(before_ids), sorted(killed), killed_all, safe_text(output)[:800],
            ),
        )
        status = self.wait_database_ready("database_robot_connection_recovery", 120, 3)
        after = self.robot_mysql_connections()
        after_ids = set(to_int(row.get("id")) for row in after if to_int(row.get("id")) > 0)
        replacement_ids = after_ids - before_ids
        old_connections_gone = before_ids.isdisjoint(after_ids)
        healthy = bool(database_status_ready(status) and killed and old_connections_gone and replacement_ids)
        detail = {
            "before_ids": sorted(before_ids),
            "killed_ids": sorted(killed),
            "after_ids": sorted(after_ids),
            "replacement_ids": sorted(replacement_ids),
            "old_connections_gone": old_connections_gone,
            "status": status,
        }
        self.mark_coverage("database_robot_connection_recovery", healthy, json_text(detail, 1800))
        if not healthy:
            self.record_failure("database_robot_connection_recovery", "robot database pool did not establish a verified replacement connection")
            api = self.safe_call("systemStatus", {})
            if not (isinstance(api, dict) and api.get("ok")):
                self.record_failure("database_robot_connection_robot_api", "robot API failed after targeted database connection drop")
        self.market_enable_auto(max_concurrent=8)
        if not self.wait_market_services("database_robot_connection_market", 180, 10):
            self.record_failure("database_robot_connection_market", "market services did not remain healthy after targeted database connection drop")
        if not self.wait_market_auto_running("database_robot_connection_auto", 180, 10):
            self.record_failure("database_robot_connection_auto", "market auto did not recover after targeted database connection drop")
        self.sample_with_event("database_robot_connection_drop_done")

    def config_dir_fault(self):
        self.log("config_dir_fault begin")
        backup = "/root/config.vm_random_backup_%s" % int(time.time() * 1000)
        backup_output = self.shell(filtered_config_backup_script("/root/config", backup), 120)
        if "CONFIG_BACKUP_OK" not in safe_text(backup_output):
            raise RuntimeError("config directory backup failed: %s" % safe_text(backup_output)[:1000])
        backup_ready = True
        try:
            self.safe_call("marketStop", {})
            self.wait_market_job_idle("config_fault_prepare", 300, 5)
            self.stop_market_services()
            script = """
pids=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
[ -z "$pids" ] || kill -TERM $pids || true
pkill -TERM -f '^/root/robot --web-admin' 2>/dev/null || true
pkill -TERM -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true
for i in 1 2 3 4 5; do
  left=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
  web=$(pgrep -f '^/root/robot --web-admin' 2>/dev/null || true)
  sink=$(pgrep -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true)
  [ -z "$left$web$sink" ] && break
  sleep 1
done
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
            api_ready = self.robot_restart_without_target("config_dir_fault_restart")
            market_status = self.safe_call("marketStatus", {})
            market_fallback = api_ready and isinstance(market_status, dict) and market_status.get("ok")
            detail = "api_ready=%s market=%s" % (api_ready, json_text(market_status, 1200))
            self.mark_coverage("market_bad_config", market_fallback, detail)
            self.mark_coverage("config_dir_fallback", market_fallback, detail)
            if not market_fallback:
                self.record_failure("config_dir_fault_api", "robot API or marketStatus did not start with missing/broken config directory")
            self.sample_with_event("config_dir_fault_running")
        finally:
            if backup_ready:
                script = """
pids=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
[ -z "$pids" ] || kill -TERM $pids || true
pkill -TERM -f '^/root/robot --web-admin' 2>/dev/null || true
pkill -TERM -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true
for i in 1 2 3 4 5; do
  left=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
  web=$(pgrep -f '^/root/robot --web-admin' 2>/dev/null || true)
  sink=$(pgrep -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true)
  [ -z "$left$web$sink" ] && break
  sleep 1
done
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
                    restore_api_ready = self.robot_restart_without_target("config_dir_fault_restore")
                    self.market_enable_auto(max_concurrent=8)
                    if not restore_api_ready:
                        self.record_failure("config_dir_fault_restore_api", "robot API did not recover after config restore")
                    if not self.wait_market_services("config_dir_fault_restore_market", 180, 10):
                        self.record_failure("config_dir_fault_restore_market", "market services did not recover after config restore")
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

    def restart_recovery_matrix(self, start_phase=""):
        self.log("restart_recovery_matrix begin start_phase=%s" % (start_phase or "start"))
        if start_phase != "runtime_recovery_matrix":
            self.run_phase_step("config_dir_fault", self.config_dir_fault, recover=False)
            if not self.phase_allowed():
                self.log("restart_recovery_matrix stop reason=config_fault_failed")
                return
        self.run_phase_step("compatibility_desired_state", self.configure_compatibility_guards, recover=False)
        if not self.phase_allowed():
            return
        high = self.args.target_max
        self.set_target(high)
        if not self.wait_target_running(high, 300, 10, sample_interval=30, minimum_ratio=0.75):
            self.record_failure("runtime_recovery_load_not_ready", "target %s did not reach 75%% running before combined recovery" % high)
        if self.phase_allowed():
            self.run_phase_step(
                "combined_service_runtime_recovery",
                lambda: self.combined_service_runtime_recovery(high),
                recover=False,
            )
        self.sample_with_event("restart_recovery_matrix_done")
        self.log("restart_recovery_matrix done")

    def combined_service_runtime_recovery(self, high):
        monitor_probe_uids = self.select_uids(1)
        self.mark_coverage("monitor_fault_probe_uid", bool(monitor_probe_uids), "uids=%s" % monitor_probe_uids)
        if not monitor_probe_uids:
            raise RuntimeError("no active UID was available before combined fault injection")

        private_key = self.game_dir + "/privatekey.pem"
        public_key = self.game_dir + "/publickey.pem"
        private_backup = ""
        public_backup = ""
        try:
            web_port = self.port("Web")
            self.shell("pkill -TERM -f '^/root/robot --web-admin' 2>/dev/null || true", 20)
            web_down = self.wait_port_state(web_port, False, 30, 3)
            self.mark_coverage("web_admin_process_fault", web_down, "web_port=%s" % web_port)
            if not web_down:
                self.record_failure("web_admin_fault_not_observed", "web admin port stayed open after process termination")
            for command in ("systemStatus", "autoStatus", "marketStatus", "databaseStatus"):
                result = self.safe_call(command, {})
                self.log("runtime_recovery web_down command=%s result=%s" % (command, json_text(result, 1200)))
                if not (isinstance(result, dict) and result.get("ok")):
                    self.record_failure("web_admin_down_%s" % command, "%s failed while only web admin was down" % command)

            monitor_port = self.port("Monitor")
            monitor_pids = self.shell("pidof df_monitor_r || true", 10, log_output=False).strip()
            if not monitor_pids:
                raise RuntimeError("df_monitor_r is not running before combined recovery")
            self.shell("for pid in $(pidof df_monitor_r 2>/dev/null); do kill -TERM $pid || true; done", 20)
            monitor_down = self.wait_port_state(monitor_port, False, 30, 3)
            self.mark_coverage("monitor_isolated_down", monitor_down, "monitor_port=%s" % monitor_port)
            if not monitor_down:
                self.record_failure("monitor_fault_not_observed", "monitor port stayed open after terminating df_monitor_r")
            result = self.safe_call("robotsShoutWorld", {"uids": monitor_probe_uids})
            action_observed = isinstance(result, dict) and "ok" in result
            self.mark_coverage("monitor_down_robot_action", action_observed, json_text(result, 1000))
            if not action_observed:
                self.record_failure("monitor_down_robot_action", "robot action API did not respond while monitor was down")

            auto_stop = self.safe_call("autoStop", {})
            if not (isinstance(auto_stop, dict) and auto_stop.get("ok")):
                raise RuntimeError("autoStop failed before core shutdown: %s" % json_text(auto_stop, 1000))
            if not self.wait_auto_drained("combined_core_shutdown", 120, 3):
                raise RuntimeError("automation did not drain before core shutdown")
            market_stop = self.safe_call("marketStop", {})
            if not (isinstance(market_stop, dict) and market_stop.get("ok")):
                raise RuntimeError("marketStop failed before core shutdown: %s" % json_text(market_stop, 1000))
            if not self.wait_market_job_idle("combined_core_shutdown", 120, 3):
                raise RuntimeError("market job did not stop before core shutdown")
            self.shell("cd /root && (./stop >/dev/null 2>&1 || true)", 180)
            core_down = self.wait_service_ports("combined_core_down", ("Game", "Monitor", "Bridge"), False, 45, 3)
            self.mark_coverage("core_services_down", core_down, "game_monitor_bridge_ports_down=%s" % core_down)
            if not core_down:
                self.record_failure("core_stop_not_observed", "game, monitor, or bridge port stayed open during combined recovery")

            private_backup = self.backup_file(private_key)
            public_backup = self.backup_file(public_key)
            if not private_backup or not public_backup:
                self.record_failure("runtime_recovery_key_backup", "custom key backup failed")
                raise RuntimeError("custom key backup failed")
            generate = "openssl genpkey -algorithm RSA -out %s -pkeyopt rsa_keygen_bits:2048 2>/dev/null && " \
                "openssl rsa -pubout -in %s -out %s 2>/dev/null && echo KEYS_REPLACED" % (
                    shell_quote(private_key), shell_quote(private_key), shell_quote(public_key),
                )
            output = self.shell(generate, 30)
            if "KEYS_REPLACED" not in safe_text(output):
                raise RuntimeError("custom key generation failed: %s" % safe_text(output)[:500])

            if not self.robot_restart_without_target("combined_recovery_robot_down_core"):
                self.record_failure("combined_recovery_robot_api", "robot API did not start while game services were down")
            status = self.safe_call("marketStatus", {})
            sync = self.safe_call("marketSyncItemInfo", {})
            self.log("combined_recovery game_down_status=%s sync=%s" % (json_text(status, 1400), json_text(sync, 2200)))
            api_alive = bool(isinstance(status, dict) and status.get("ok"))
            self.mark_coverage("iteminfo_sync_while_game_down", api_alive, json_text(sync, 1200))
            if not api_alive:
                self.record_failure("iteminfo_game_down_robot_api", "marketStatus failed while game services were down")

            key_result = self.wait_keypair_state("combined_recovery_user_key", "user", 45, 3)
            user_key = key_state_name(key_result) == "user"
            self.mark_coverage("custom_key_active", user_key, json_text(key_result, 1200))
            if not user_key:
                self.record_failure("custom_key_user_state", "custom key was not active after robot restart: %s" % json_text(key_result, 1200))

            if not self.restart_core_services("combined_recovery_core", 90, 120):
                raise RuntimeError("core services did not remain stable during combined recovery")
            self.market_enable_auto(max_concurrent=8)
            self.set_target(high)
            state = self.wait_combined_runtime_recovery(high, 300, 5)
            self.mark_coverage("monitor_isolated_restore", state.get("monitor_ok"), "combined recovery")
            self.mark_coverage("bridge_restore", state.get("bridge_ok"), "combined recovery")
            self.mark_coverage("party_compat_patch", state.get("party_ok"), "combined recovery")
            self.mark_coverage("party_compat_repatch", state.get("party_ok"), "combined recovery")
            self.mark_coverage("mailbox_guard_active", state.get("mailbox_ok"), "combined recovery")
            self.mark_coverage("mailbox_guard_repatch", state.get("mailbox_ok"), "combined recovery")
            self.mark_coverage("runtime_restart_recovery_window", state.get("recovery_ready"), json_text(state, 1200))
            if not state.get("monitor_ok"):
                self.record_failure("monitor_fault_restore", "monitor port did not recover after combined core restart")
            if not state.get("bridge_ok"):
                self.record_failure("bridge_fault_restore", "bridge port did not recover after combined core restart")
            if not state.get("market_ok"):
                self.record_failure("combined_recovery_market", "market services or auto did not recover after combined core restart")
            if not state.get("party_ok"):
                self.record_failure("party_compat_supervisor_repatch", "party compatibility patch did not recover after combined core restart")
            if not state.get("mailbox_ok"):
                self.record_failure("mailbox_guard_supervisor_repatch", "mailbox bad-node guard did not recover after combined core restart")
            if not state.get("recovery_ready"):
                self.record_failure("runtime_restart_recovery_window", "combined recovery did not hold all gates for three stable samples")

            release = self.safe_call("keypairReleaseDefault", {})
            self.log("combined_recovery release_default=%s" % json_text(release, 1800))
            default_result = self.wait_keypair_state("combined_recovery_default_key", "default", 45, 3)
            default_key = key_state_name(default_result) == "default" or key_using_default(default_result)
            self.mark_coverage("default_key_restored", default_key, json_text(default_result, 1200))
            if not default_key:
                self.record_failure("custom_key_default_state", "default key was not restored after release: %s" % json_text(default_result, 1200))
        finally:
            self.restore_file(private_key, private_backup)
            self.restore_file(public_key, public_backup)

    def wait_combined_runtime_recovery(self, high, timeout_sec=300, interval_sec=5):
        started = time.time()
        deadline = time.time() + timeout_sec
        stable = 0
        last = {}
        next_sample = 0
        while time.time() < deadline:
            last = self.combined_runtime_state(high)
            ready = combined_runtime_ready(last)
            stable = stable + 1 if ready else 0
            now = time.time()
            if now >= next_sample or ready and stable == 1:
                self.sample_with_event("combined_runtime_recovery")
                next_sample = now + 15
            if ready and stable >= 3 and now - started >= 15:
                last["stable_samples"] = stable
                last["elapsed_sec"] = int(now - started)
                last["recovery_ready"] = True
                self.log("combined_runtime_recovery evidence_ready state=%s" % json_text(last, 1600))
                return last
            time.sleep(interval_sec)
        last["stable_samples"] = stable
        last["elapsed_sec"] = int(time.time() - started)
        last["recovery_ready"] = False
        self.log("combined_runtime_recovery timeout state=%s" % json_text(last, 1600))
        return last

    def combined_runtime_state(self, high):
        api = self.safe_call("systemStatus", {})
        auto = self.safe_call("autoStatus", {})
        scheduler = self.safe_call("schedulerStatus", {})
        market = self.market_status_result()
        ports = self.port_snapshot()
        try:
            party = (self.web_json("/api/party-compat").get("result") or {})
        except Exception:
            party = {}
        try:
            mailbox = (self.web_json("/api/compat").get("result") or {})
        except Exception:
            mailbox = {}
        try:
            key = (self.safe_call("keypairStatus", {}).get("result") or {})
        except Exception:
            key = {}
        auto_result = (auto.get("result") or {}) if isinstance(auto, dict) else {}
        scheduler_result = (scheduler.get("result") or {}) if isinstance(scheduler, dict) else {}
        market_ok = self.market_services_ready(market) and self.market_auto_enabled(market) and bool(market.get("auto_running"))
        return {
            "api_ok": bool(isinstance(api, dict) and api.get("ok")),
            "game_ok": bool(ports.get(self.port_text("Game"))),
            "monitor_ok": bool(ports.get(self.port_text("Monitor"))),
            "bridge_ok": bool(ports.get(self.port_text("Bridge"))),
            "market_ok": market_ok,
            "party_ok": bool(party.get("desired_enabled") and party.get("enabled") and party.get("state") == "on"),
            "mailbox_ok": bool(mailbox.get("desired_enabled") and mailbox.get("enabled") and mailbox.get("state") == "on"),
            "key_ok": key_state_name(key) == "user",
            "load_ok": to_int(auto_result.get("target_online")) == int(high) and to_int(auto_result.get("running")) >= int(high) * 75 // 100,
            "target": to_int(auto_result.get("target_online")),
            "running": to_int(auto_result.get("running")),
            "scheduler": scheduler_result,
            "ports": ports,
        }

    def configure_compatibility_guards(self):
        mailbox = self.web_json("/api/compat")
        self.log("compatibility mailbox_before=%s" % json_text(mailbox, 1400))
        mailbox_result = mailbox.get("result") or {}
        if not mailbox_result.get("desired_enabled"):
            mailbox = self.web_json("/api/compat", {"mailbox_bad_node_guard": True})
            self.log("compatibility mailbox_enable=%s" % json_text(mailbox, 1400))
            mailbox_result = mailbox.get("result") or {}
        mailbox_desired = bool(mailbox_result.get("desired_enabled"))
        self.mark_coverage("mailbox_guard_desired", mailbox_desired, json_text(mailbox_result, 1000))
        if not mailbox_desired:
            self.record_failure("mailbox_guard_desired", "mailbox bad-node guard could not be enabled")

        before = self.web_json("/api/party-compat")
        self.log("compatibility party_before=%s" % json_text(before, 1400))
        party = before.get("result") or {}
        account_start = to_int(party.get("account_start"), 17000000)
        account_end = to_int(party.get("account_end"), 17001000)
        if account_start <= 0 or account_end <= account_start:
            account_start, account_end = 17000000, 17001000
        skill_enabled = bool(party.get("skill_enabled"))
        off = self.web_json(
            "/api/party-compat",
            {
                "action": "off",
                "account_start": account_start,
                "account_end": account_end,
                "skill_enabled": skill_enabled,
            },
        )
        self.log("compatibility party_off=%s" % json_text(off, 1400))
        off_result = off.get("result") or {}
        off_desired = off_result.get("desired_enabled") is False or self.wait_party_compat_desired(False, "compatibility_party_off", 15, 3)
        if not off_desired:
            self.record_failure("party_compat_off_desired", "party compatibility desired state did not turn off")
        on = self.web_json(
            "/api/party-compat",
            {
                "action": "on",
                "account_start": account_start,
                "account_end": account_end,
                "skill_enabled": skill_enabled,
            },
        )
        self.log("compatibility party_on=%s" % json_text(on, 1400))
        on_result = on.get("result") or {}
        party_desired = bool(on_result.get("desired_enabled")) or self.wait_party_compat_desired(True, "compatibility_party_on", 30, 3)
        self.mark_coverage("party_compat_desired", party_desired, json_text(on_result, 1000))
        if not party_desired:
            self.record_failure("party_compat_on_desired", "party compatibility desired state did not turn on")
        baseline_config = os.path.join(self.baseline_dir, "root_config")
        self.shell(
            "mkdir -p %s; cp -af /root/config/compat.json %s/compat.json 2>/dev/null || true; "
            "cp -af /root/config/party_compat.json %s/party_compat.json 2>/dev/null || true" % (
                shell_quote(baseline_config),
                shell_quote(baseline_config),
                shell_quote(baseline_config),
            ),
            20,
        )

    def wait_port_state(self, port, expected, timeout_sec=60, interval_sec=3):
        deadline = time.time() + timeout_sec
        last = False
        while time.time() < deadline:
            last = bool(self.port_snapshot().get(str(port)))
            if last == bool(expected):
                self.log("wait_port_state ready port=%s expected=%s" % (port, expected))
                return True
            time.sleep(interval_sec)
        self.log("wait_port_state timeout port=%s expected=%s actual=%s" % (port, expected, last))
        return False

    def robot_manual_mode_drill(self, hold_sec=20, recover_sec=None):
        self.log("robot_manual_mode_drill begin")
        stop = self.safe_call("autoStop", {})
        self.log("robot_manual_mode_drill autoStop=%s" % json_text(stop, 1200))
        if not (isinstance(stop, dict) and stop.get("ok")):
            self.record_failure("robot_manual_mode_autostop", "autoStop failed")
        self.sample_with_event("robot_manual_mode:auto_stop")
        try:
            if not self.wait_user_actor_commands_ready("robot_manual_mode_stopped", 60, 2, allow_empty=True):
                self.record_failure("robot_manual_mode_stop_busy", "scheduler stayed busy after autoStop")
            self.robot_call_when_ready(
                "robotsOnlineAsync",
                {"count": 12},
                "robot_manual_mode_online",
                allow_empty=True,
            )
            self.wait_user_actor_commands_ready("robot_manual_mode_online_ready", 60, 2)
            uids = self.wait_select_uids(12, 30, prefer_running=True, exclude_store=True, minimum=7)
            if len(uids) < 7:
                self.record_failure("robot_manual_mode_uid_shortage", "only %s active non-store UIDs available" % len(uids))
            if len(uids) >= 7:
                self.robot_call_when_ready("robotsMove", {"uids": uids[:1]}, "robot_manual_mode_move")
                self.robot_call_when_ready("robotsShout", {"uids": uids[1:2]}, "robot_manual_mode_shout", wait_ready=False)
                self.robot_call_when_ready("robotsShoutLocal", {"uids": uids[2:3]}, "robot_manual_mode_shout_local", wait_ready=False)
                self.robot_call_when_ready("robotsShoutWorld", {"uids": uids[3:4]}, "robot_manual_mode_shout_world", wait_ready=False)

                store_uids = uids[4:7]
                self.robot_call_when_ready("robotsStoreAsync", {"uids": store_uids}, "robot_manual_mode_store")
                self.wait_uids_store_active("robot_manual_mode_targeted_store", store_uids, 1, 45, 2)
                if not self.wait_scheduler_operation_idle("robot_manual_mode_store_done", 60, 2):
                    self.record_failure("robot_manual_mode_store_busy", "targeted store operation did not finish")
                self.assert_store_presence("robot_manual_mode_store")

                store_logout = store_uids[-2:]
                self.robot_call_when_ready("robotsLogoutAsync", {"uids": store_logout}, "robot_manual_mode_store_logout")
                self.wait_uids_inactive("robot_manual_mode_store_logout", store_logout, 30, 2)
                if not self.args.no_cleanup:
                    cleanup_uids = store_logout[-1:]
                    cleanup = self.safe_call("cleanupRobots", {"uids": cleanup_uids, "force": True})
                    deleted = to_int((((cleanup or {}).get("result") or {}).get("deleted")))
                    self.deleted_total += deleted
                    self.log("robot_manual_mode cleanup uids=%s deleted=%s result=%s" % (cleanup_uids, deleted, json_text(cleanup, 1600)))
                self.robot_call_when_ready("robotsOnlineAsync", {"uids": store_logout[:1]}, "robot_manual_mode_store_reonline", allow_empty=True)
                self.wait_scheduler_operation_idle("robot_manual_mode_store_reonline_done", 45, 2)
                self.robot_cleanup_edge_cases(recover_sec=0, restart_auto=False)
            else:
                self.log("robot_manual_mode_drill no uids after online")
                self.record_failure("robot_manual_mode_no_uids", "no UIDs after manual online")
            time.sleep(hold_sec)
            self.sample_with_event("robot_manual_mode_hold")
        finally:
            start = self.safe_call("autoStart", {})
            self.log("robot_manual_mode_drill autoStart=%s" % json_text(start, 1200))
            if not (isinstance(start, dict) and start.get("ok")):
                self.record_failure("robot_manual_mode_autostart", "autoStart failed")
            if recover_sec:
                self.burst_sample("robot_manual_mode_recover", recover_sec, 15)
        self.log("robot_manual_mode_drill done")

    def robot_cleanup_edge_cases(self, recover_sec=None, restart_auto=True):
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
            time.sleep(1)
        if restart_auto:
            self.safe_call("autoStart", {})
        self.sample_with_event("robot_cleanup_edge_done")
        if recover_sec:
            self.burst_sample("robot_cleanup_edge_recover", recover_sec, 15)
        self.log("robot_cleanup_edge_cases done")

    def wait_keypair_state(self, event, expected, timeout_sec=45, interval_sec=3):
        deadline = time.time() + timeout_sec
        last = {}
        while time.time() < deadline:
            status = self.safe_call("keypairStatus", {})
            last = (status.get("result") or {}) if isinstance(status, dict) else {}
            state = key_state_name(last)
            if state == expected or (expected == "default" and key_using_default(last)):
                self.log("wait_keypair_state ready event=%s expected=%s status=%s" % (event, expected, json_text(last, 1200)))
                return last
            time.sleep(interval_sec)
        self.log("wait_keypair_state timeout event=%s expected=%s status=%s" % (event, expected, json_text(last, 1200)))
        return last

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
                or not status_row_is_active(row)
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
        hot_ports = self.port_regex(("RobotAPI", "Web", "Game", "Monitor", "Bridge", "Point", "Auction", "Relay", "PartyRoute0"))
        script = r"""
pids=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
[ -z "$pids" ] || kill -TERM $pids || true
pkill -TERM -f '^/root/robot --web-admin' 2>/dev/null || true
pkill -TERM -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true
for i in 1 2 3 4 5 6 7 8; do
  left=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
  web=$(pgrep -f '^/root/robot --web-admin' 2>/dev/null || true)
  sink=$(pgrep -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true)
  [ -z "$left$web$sink" ] && break
  sleep 1
done
left=$(ps -eo pid,args | awk '($2=="/root/robot" || $2=="./robot") && NF==2 {print $1}')
[ -z "$left" ] || kill -KILL $left || true
pkill -KILL -f '^/root/robot --web-admin' 2>/dev/null || true
pkill -KILL -f '^/root/robot --bounded-log-sink .*/robot_stdout.log' 2>/dev/null || true
mkdir -p /root/config
nohup sh -c '/root/robot 2>&1 | /root/robot --bounded-log-sink /root/config/robot_stdout.log' >/dev/null 2>/root/config/robot_start_error.log &
pgrep -af '/root/robot|df_game_r|df_monitor_r|df_bridge_r|df_auction_r|df_point_r|df_relay_r' || true
ss -lntp | grep -E ':(%s)' || true
""" % hot_ports
        self.shell(script, 120)
        self.web_opener = None
        ready = self.wait_robot_api(label + "_api", 90, 3)
        self.sample_with_event(label + "_started")
        return ready

    def sample(self):
        row = self.sample_row()
        self.writerow(row)
        self.log(
            "sample target=%s actors=%s leased=%s running=%s connecting=%s stores=%s/%s items7=%s/%s%% zero=%s hist=%s mode=%s market_auto=%s auction=%s/%s cand=%s special=%s specialdb=%s high=%s creature=%s inst=%s orphan=%s q=%s/%s/%s health=%s/%s policy=%s stg=%s failr=%s act=%s/%s/%s cera=%s/%s health=%s/%s policy=%s act=%s/%s/%s load=%s/%s/%s top=%s hits=%s api_error=%s"
            % (
                row.get("target"),
                row.get("actors"),
                row.get("leased"),
                row.get("running"),
                row.get("connecting"),
                row.get("store_item_running"),
                row.get("store_disjoint_running"),
                row.get("store_item_display_seven"),
                row.get("store_item_display_seven_ratio"),
                row.get("store_item_display_zero"),
                row.get("store_item_display_histogram"),
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
        self.record_sample_metrics(row)
        encoded = {}
        for key in SAMPLE_FIELDS:
            value = safe_text(row.get(key, ""))
            if sys.version_info[0] < 3:
                encoded[key] = value.encode("utf-8")
            else:
                encoded[key] = value
        self.samples.writerow(encoded)
        self.samples_file.flush()

    def record_sample_metrics(self, row):
        target = to_int(row.get("target"))
        running = to_int(row.get("running"))
        item_stores = to_int(row.get("store_item_running"))
        disjoint_stores = to_int(row.get("store_disjoint_running"))
        active_item_stores = to_int(row.get("store_item_display_active"))
        self.sample_metrics.append({
            "event": safe_text(row.get("event")),
            "target": target,
            "running": running,
            "stores": item_stores + disjoint_stores,
            "item_stores": item_stores,
            "disjoint_stores": disjoint_stores,
            "store_uids": list(row.get("_store_uids") or []),
            "item_store_uids": list(row.get("_item_store_uids") or []),
            "disjoint_store_uids": list(row.get("_disjoint_store_uids") or []),
            "store_success": to_int(row.get("store_success")),
            "store_failed": to_int(row.get("store_failed")),
            "store_expired": to_int(row.get("store_expired")),
            "displayed_item_stores": active_item_stores,
            "seven_item_stores": to_int(row.get("store_item_display_seven")),
            "displayed_zero": to_int(row.get("store_item_display_zero")),
            "displayed_out_of_range": to_int(row.get("store_item_display_out_of_range")),
            "nocache_sent": to_int(row.get("store_nocache_sent_hits")),
            "nocache_failed": to_int(row.get("store_nocache_failed_hits")),
            "nocache_elapsed_ms_max": to_int(row.get("store_nocache_elapsed_ms_max")),
            "disjoint_same_session_retry": to_int(row.get("disjoint_same_session_retry_hits")),
            "robot_cpu": float(row.get("robot_cpu_api") or 0),
            "robot_memory_mb": to_int(row.get("robot_mem_mb")),
            "api_error": safe_text(row.get("api_error")),
            "game_port": safe_text(row.get("port_10011")),
            "monitor_port": safe_text(row.get("port_30303")),
            "bridge_port": safe_text(row.get("port_7000")),
            "relay_port": safe_text(row.get("port_7200")),
            "goroutines": to_int(row.get("goroutines")) if safe_text(row.get("goroutines")).strip() else "",
            "memory_mb": to_int(row.get("robot_mem_mb")) if safe_text(row.get("robot_mem_mb")).strip() else "",
            "fd_robot": to_int(row.get("fd_robot")) if safe_text(row.get("fd_robot")).strip() else "",
        })

    def sample_metrics_summary(self):
        rows = self.sample_metrics
        high = [row for row in rows if row["target"] == self.args.target_max]
        high_stable = [row for row in high if row["running"] >= high_load_running_floor(self.args.target_max)]
        high_stats = high_load_observation_stats(high_stable, self.args.target_max)

        def maximum(source, key):
            return max([row[key] for row in source] or [0])

        def average(source, key):
            return round(float(sum(row[key] for row in source)) / len(source), 2) if source else 0

        displayed = sum(row["displayed_item_stores"] for row in high_stable)
        seven = sum(row["seven_item_stores"] for row in high_stable)
        return {
            "samples": len(rows),
            "target_high": self.args.target_max,
            "target_high_samples": len(high),
            "target_high_stable_samples": len(high_stable),
            "target_high_running_max": maximum(high, "running"),
            "target_high_running_avg": average(high_stable, "running"),
            "target_high_store_max": maximum(high_stable, "stores"),
            "target_high_store_avg": average(high_stable, "stores"),
            "target_high_store_unique": high_stats["unique_stores"],
            "target_high_store_started": high_stats["store_success_delta"],
            "target_high_store_activity": high_stats["store_activity"],
            "target_high_item_store_max": maximum(high_stable, "item_stores"),
            "target_high_disjoint_store_max": maximum(high_stable, "disjoint_stores"),
            "target_high_item_seven_ratio": round(100.0 * seven / displayed, 2) if displayed else 0,
            "nocache_sent_max": maximum(rows, "nocache_sent"),
            "nocache_failed_max": maximum(rows, "nocache_failed"),
            "nocache_elapsed_ms_max": maximum(rows, "nocache_elapsed_ms_max"),
            "disjoint_same_session_retry_max": maximum(rows, "disjoint_same_session_retry"),
            "robot_cpu_percent_max": maximum(rows, "robot_cpu"),
            "robot_memory_mb_max": maximum(rows, "robot_memory_mb"),
        }

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
                    "store_item_running": auto.get("store_item_running"),
                    "store_disjoint_running": auto.get("store_disjoint_running"),
                    "store_success": auto.get("store_success"),
                    "store_failed": auto.get("store_failed"),
                    "store_expired": auto.get("store_expired"),
                    "store_target": sched.get("store_target"),
                    "store_concurrent": sched.get("store_concurrent"),
                    "store_probability_percent": sched.get("store_probability_percent"),
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
        self.fill_game_connection_row(row)
        self.fill_store_status_row(row)
        self.fill_store_row(row)
        row["fd_robot"] = self.robot_fd_count()
        load1, load5, load15 = self.load_average()
        row["load1"], row["load5"], row["load15"] = load1, load5, load15
        row["top_cpu"] = self.top_cpu()
        row["keyword_hits"] = self.keyword_hits()
        return row

    def fill_store_status_row(self, row):
        try:
            counts = dict((item_count, 0) for item_count in range(1, 8))
            active_item_stores = 0
            zero_items = 0
            out_of_range = 0
            store_uids = set()
            item_store_uids = set()
            disjoint_store_uids = set()
            for status in self.status_rows():
                uid = to_int(status.get("uid"))
                has_store = status_row_has_store(status)
                if uid > 0 and has_store:
                    store_uids.add(uid)
                runtime_state = safe_text(status.get("runtime_state")).strip().lower()
                if uid > 0 and has_store and (
                    to_int(status.get("robot_type")) == 3 or status.get("disjoint_active") or runtime_state == "disjoint"
                ):
                    disjoint_store_uids.add(uid)
                if to_int(status.get("robot_type")) != 2 or not status.get("store_display_ack"):
                    continue
                if uid > 0:
                    item_store_uids.add(uid)
                active_item_stores += 1
                item_count = to_int(status.get("store_display_items"), -1)
                if item_count == 0:
                    zero_items += 1
                elif item_count in counts:
                    counts[item_count] += 1
                else:
                    out_of_range += 1

            row["_store_uids"] = sorted(store_uids)
            row["_item_store_uids"] = sorted(item_store_uids)
            row["_disjoint_store_uids"] = sorted(disjoint_store_uids)
            row["store_status_uids"] = ",".join(str(uid) for uid in sorted(store_uids))
            row["store_item_status_uids"] = ",".join(str(uid) for uid in sorted(item_store_uids))
            row["store_disjoint_status_uids"] = ",".join(str(uid) for uid in sorted(disjoint_store_uids))
            if row.get("store_item_running") in (None, ""):
                row["store_item_running"] = active_item_stores
            seven_items = counts[7]
            row["store_item_display_active"] = active_item_stores
            row["store_item_display_histogram"] = ";".join(
                "%s=%s" % (item_count, counts[item_count]) for item_count in range(1, 8)
            )
            row["store_item_display_seven"] = seven_items
            row["store_item_display_seven_ratio"] = (
                "%.2f" % (100.0 * seven_items / active_item_stores) if active_item_stores else "0.00"
            )
            row["store_item_display_zero"] = zero_items
            row["store_item_display_out_of_range"] = out_of_range
        except Exception as exc:
            if not row.get("api_error"):
                row["api_error"] = "robotsStatus:%r" % (exc,)

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
            row["port_7000"] = int((":" + self.port_text("Bridge")) in out)
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
            row["party_route_failover_hits"] = counts.get("route_failover", "")
            row["party_relay_connected_hits"] = counts.get("relay_connected", "")
            row["party_probe_cycle_hits"] = counts.get("probe_cycles", "")
            row["party_peer_ready_hits"] = counts.get("peer_ready", "")
            row["party_self_id_refresh_hits"] = counts.get("self_id_refresh", "")
            row["party_self_id_recovered_hits"] = counts.get("self_id_recovered", "")
            row["party_self_id_recycle_hits"] = counts.get("self_id_recycle", "")
            row["party_udp_recycle_hits"] = counts.get("udp_recycles", "")
            row["party_transport_cleared_hits"] = counts.get("transport_cleared", "")
            row["party_peer_transport_reset_hits"] = counts.get("peer_transport_reset", "")
            row["party_supervisor_panic_hits"] = counts.get("supervisor_panics", "")
            row["party_skill_hits"] = counts.get("skill_casts", "")
            row["party_skill_error_hits"] = counts.get("skill_errors", "")
        except Exception as exc:
            if not row.get("api_error"):
                row["api_error"] = "party:%r" % (exc,)

    def game_connection_log_paths(self):
        today = datetime.datetime.now().strftime("%Y%m%d")
        pattern = posixpath.join(self.game_dir, "log", "*", "Log%s.log" % today)
        return sorted(path for path in glob.glob(pattern) if os.path.isfile(path))

    def prime_game_connection_logs(self):
        for path in self.game_connection_log_paths():
            try:
                stat = os.stat(path)
                self.game_log_offsets[path] = (getattr(stat, "st_ino", 0), stat.st_size)
            except (IOError, OSError):
                pass

    def read_game_connection_updates(self):
        for path in self.game_connection_log_paths():
            try:
                stat = os.stat(path)
                inode = getattr(stat, "st_ino", 0)
                previous = self.game_log_offsets.get(path)
                start = 0
                if previous and previous[0] == inode and stat.st_size >= previous[1]:
                    start = previous[1]
                if stat.st_size <= start:
                    self.game_log_offsets[path] = (inode, stat.st_size)
                    continue
                fh = open(path, "rb")
                try:
                    fh.seek(start, os.SEEK_SET)
                    raw = fh.read()
                    end = fh.tell()
                finally:
                    fh.close()
                self.game_log_offsets[path] = (inode, end)
                text = safe_text(raw)
                for name, pattern in GAME_CONNECTION_CHECK_PATTERNS.items():
                    self.game_connection_counts[name] += len(re.findall(pattern, text))
                for name, pattern in GAME_STORE_DISCONNECT_PATTERNS.items():
                    self.game_store_disconnect_counts[name] += len(re.findall(pattern, text))
            except (IOError, OSError):
                continue

    def fill_game_connection_row(self, row):
        self.read_game_connection_updates()
        row["game_ping_timeout_hits"] = self.game_connection_counts.get("ping_timeout", 0)
        row["game_check_disconnect_hits"] = self.game_connection_counts.get("check_disconnect", 0)
        row["game_disconnect_from5_hits"] = self.game_store_disconnect_counts.get("from5", 0)
        row["game_disconnect_from10_hits"] = self.game_store_disconnect_counts.get("from10", 0)
        for name, count in self.game_connection_counts.items():
            if count <= 0 or name in self.game_connection_failure_reported:
                continue
            self.game_connection_failure_reported.add(name)
            self.record_failure(
                "game_connection_%s" % name,
                "df_game_r reported %s new %s event(s) after stability test start" % (count, name),
            )

    def fill_store_row(self, row):
        try:
            text = self.robot_log_tail(2 * 1024 * 1024)
            failed = re.findall(r"\[AutoStore\][^\n]*failed_after=(\d+)[^\n]*reason=([^\s]+)", text)
            restore_elapsed = [
                int(value)
                for value in re.findall(r"\[AutoStore\][^\n]*restore_normal_online_(?:ok|failed)[^\n]*elapsed_ms=(\d+)", text)
            ]
            nocache_elapsed = [
                int(value)
                for value in re.findall(r"\[CharacterCache\][^\n]*native_nocache_sent=1[^\n]*elapsed_ms=(\d+)", text)
            ]
            row["store_err_0x11_hits"] = sum(1 for _, reason in failed if reason == "store_err_0x11")
            row["store_failed_after_hits"] = len(failed)
            row["store_failed_after_max_tries"] = max([int(tries) for tries, _ in failed] or [0])
            row["store_restore_ok_hits"] = len(re.findall(r"\[AutoStore\][^\n]*restore_normal_online_ok", text))
            row["store_restore_failed_hits"] = len(re.findall(r"\[AutoStore\][^\n]*restore_normal_online_failed", text))
            row["store_restore_elapsed_ms_max"] = max(restore_elapsed or [0])
            row["store_nocache_sent_hits"] = len(nocache_elapsed)
            row["store_nocache_failed_hits"] = len(re.findall(r"\[CharacterCache\][^\n]*nocache_failed", text))
            row["store_nocache_elapsed_ms_max"] = max(nocache_elapsed or [0])
            row["store_inventory_refresh_hits"] = len(re.findall(r"\[AutoStore\][^\n]*inventory_refresh mode=character_select", text))
            row["disjoint_same_session_retry_hits"] = len(re.findall(r"\[DISJOINT_RETRY_SENT\]", text))
            row["disjoint_session_point_failure_hits"] = len(re.findall(r"\[DISJOINT_SESSION_POINT_FAILURE\]", text))
            row["disjoint_cache_invalidation_error_hits"] = len(re.findall(r"\[DISJOINT_CACHE_INVALIDATION_ERROR\]", text))
        except Exception as exc:
            if not row.get("api_error"):
                row["api_error"] = "store:%r" % (exc,)

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
        hot_ports = self.port_regex(("RobotAPI", "Web", "Game", "Monitor", "Bridge", "Point", "Auction", "Relay", "PartyRoute0")) or "8111|8112|10011|30303|7000|30603|30803|7200|5063"
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
for p in $(pgrep -f '^/root/robot$|^./robot$|df_game_r|df_monitor_r|df_bridge_r|df_auction_r|df_point_r|df_relay_r' 2>/dev/null); do echo "$p $(ps -p $p -o comm=) fds=$(ls /proc/$p/fd 2>/dev/null | wc -l)"; done
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
ls -l /root/config/market_config.json /root/config/pvf_*catalog.json /root/config/pvf_iteminfo.dat %s %s 2>/dev/null || true
echo '===== web stdout filtered ====='
tail -n %s /root/config/robot_stdout.log 2>/dev/null | grep -a -E '%s|request pid|auth rejected|web admin exited' | tail -n 120 || true
""" % (
            label,
            now_text(),
            hot_ports,
            self.args.log_tail_lines,
            "|".join(re.escape(k) for k in KEYWORDS),
            self.args.log_tail_lines,
            shell_quote(self.auction_iteminfo),
            shell_quote(self.point_iteminfo),
            self.args.log_tail_lines,
            "|".join(re.escape(k) for k in KEYWORDS),
        )
        out = self.shell(command, 30, log_output=False)
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
            "coverage": self.coverage,
            "new_core_dumps": self.new_core_dumps,
            "sample_metrics": self.sample_metrics_summary(),
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
        lines.append("## Sample Metrics")
        lines.append("")
        metrics = self.sample_metrics_summary()
        for key in sorted(metrics):
            lines.append("- %s: %s" % (key, metrics[key]))
        lines.append("")
        lines.append("## Coverage")
        lines.append("")
        if self.coverage:
            for name in sorted(self.coverage):
                item = self.coverage[name]
                lines.append("- %s observed=%s detail=%s" % (name, item.get("observed"), item.get("detail") or ""))
        else:
            lines.append("No explicit coverage markers were recorded.")
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
    parser.add_argument("rounds", nargs="?", type=int, default=1, help="minimum complete dependency-ordered rounds to run; default: 1")
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
    parser.add_argument("--continue-after-failure", dest="fail_fast", action="store_false", default=True, help=argparse.SUPPRESS)
    parser.add_argument("--start-scenario", choices=STABILITY_SCENARIO_CHOICES, default="", help=argparse.SUPPRESS)
    return parser.parse_args()


def interrupt_run(signum, frame):
    raise KeyboardInterrupt


def main():
    args = parse_args()
    signal.signal(signal.SIGTERM, interrupt_run)
    signal.signal(signal.SIGINT, interrupt_run)
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
    return 0 if StabilityRun(args).run() else 1


if __name__ == "__main__":
    sys.exit(main())
