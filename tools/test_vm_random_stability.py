from __future__ import print_function

import os
import shutil
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(__file__))

import vm_random_stability as stability


class RobotLayoutPathTest(unittest.TestCase):
    def test_runtime_files_are_grouped_under_config_subdirectories(self):
        expected = (
            (stability.MAIN_CONFIG_PATH, "/root/config/conf/config.ini"),
            (stability.ROBOT_CONFIG_PATH, "/root/config/conf/robot_config.ini"),
            (stability.MARKET_CONFIG_PATH, "/root/config/conf/market_config.ini"),
            (stability.MARKET_PRICE_RANGES_PATH, "/root/config/conf/market_item_price_ranges.json"),
            (stability.MAILBOX_COMPAT_PATH, "/root/config/conf/compat.json"),
            (stability.PARTY_COMPAT_PATH, "/root/config/conf/party_compat.json"),
            (stability.NAME_TEMPLATES_PATH, "/root/config/templates/robot_name_templates.json"),
            (stability.SHOUT_TEMPLATES_PATH, "/root/config/templates/robot_shout_templates.json"),
            (stability.STORE_TITLES_PATH, "/root/config/templates/robot_store_titles.json"),
            (stability.PARTY_SKILLS_PATH, "/root/config/templates/party_skill_catalog.json"),
            (stability.PRIVATE_KEY_PATH, "/root/config/keys/privatekey.pem"),
            (stability.PUBLIC_KEY_PATH, "/root/config/keys/publickey.pem"),
            (stability.PVF_MANIFEST_PATH, "/root/config/pvf/pvf_manifest.json"),
            (stability.PVF_EQUIPMENT_PATH, "/root/config/pvf/equipment_catalog.json"),
            (stability.PVF_STACKABLE_PATH, "/root/config/pvf/stackable_catalog.json"),
            (stability.PVF_MAP_PATH, "/root/config/pvf/map_catalog.json"),
            (stability.PVF_SKILL_STATE_PATH, "/root/config/pvf/skill_state_catalog.json"),
            (stability.PVF_LEVEL_EXP_PATH, "/root/config/pvf/level_exp_catalog.json"),
            (stability.PVF_ITEMINFO_PATH, "/root/config/pvf/iteminfo.dat"),
            (stability.STORE_POINTS_CACHE_PATH, "/root/config/state/store_points_cache.json"),
            (stability.STORE_POINTS_ACTIVE_PATH, "/root/config/state/store_points_active.json"),
            (stability.MAIL_NOTIFY_CURSOR_PATH, "/root/config/state/mail_notify_cursor.json"),
            (stability.ROBOT_LOG_PATH, "/root/config/logs/robot.log"),
            (stability.ROBOT_STDOUT_LOG_PATH, "/root/config/logs/stdout.log"),
            (stability.ROBOT_START_ERROR_LOG_PATH, "/root/config/logs/start_error.log"),
            (stability.MARKET_LOG_PATH, "/root/config/logs/market.jsonl"),
            (stability.STABILITY_OUTPUT_ROOT, "/root/config/logs/stability"),
            (stability.CORE_START_LOG, "/root/config/logs/stability/core_start.log"),
            (stability.CONFIG_TEMP_DIR, "/root/config/tmp"),
        )
        for actual, wanted in expected:
            self.assertEqual(actual, wanted)

    def test_filtered_config_backup_preserves_layout_and_skips_runtime_output(self):
        script = stability.filtered_config_backup_script(stability.CONFIG_ROOT, "/tmp/config-backup")
        self.assertIn('$TMP/conf/config.ini', script)
        self.assertIn("--exclude='logs'", script)
        self.assertIn("--exclude='tmp'", script)
        self.assertNotIn('$TMP/config.ini', script)

    def test_nested_runtime_file_backups_are_cleanup_candidates(self):
        self.assertIn("/root/config/*/*.vm_random_backup_*", stability.CONFIG_FAULT_BACKUP_GLOBS)

    def test_stability_output_cannot_escape_config_logs(self):
        stamp = "20260803-120000"
        default_path = stability.stability_output_directory("", stamp)
        self.assertEqual(
            default_path,
            os.path.join(stability.STABILITY_OUTPUT_ROOT, "robot_stability_%s" % stamp),
        )
        custom_path = os.path.join(stability.STABILITY_OUTPUT_ROOT, "custom")
        self.assertEqual(stability.stability_output_directory(custom_path, stamp), custom_path)
        with self.assertRaises(ValueError):
            stability.stability_output_directory(tempfile.gettempdir(), stamp)

    def test_robot_process_detection_uses_fixed_deployment_path(self):
        with open(stability.__file__, "r", encoding="utf-8") as source_file:
            source = source_file.read()
        self.assertNotIn('"./robot"', source)
        self.assertNotIn("^./robot", source)


class StatusRowCompatibilityTest(unittest.TestCase):
    def test_current_status_fields(self):
        row = {
            "actor_attached": True,
            "actor_state": "running",
            "robot_state": {"actual_state": "running"},
        }
        self.assertTrue(stability.status_row_is_active(row))

        row["actor_state"] = "busy"
        self.assertTrue(stability.status_row_is_active(row))

        row["robot_state"]["actual_state"] = "disconnected"
        self.assertFalse(stability.status_row_is_active(row))

    def test_legacy_status_fields(self):
        row = {"actor_attached": True, "runtime_state": "store"}
        self.assertTrue(stability.status_row_is_active(row))

        row["missing_core"] = True
        self.assertFalse(stability.status_row_is_active(row))

    def test_store_state_fields(self):
        self.assertTrue(stability.status_row_has_store({"store_display_ack": True}))
        self.assertTrue(stability.status_row_has_store({"disjoint_active": True}))
        self.assertTrue(stability.status_row_has_store({"runtime_state": "store"}))
        self.assertFalse(stability.status_row_has_store({"store_created": True}))


class KeyStatusCompatibilityTest(unittest.TestCase):
    def test_snake_case_and_legacy_fields(self):
        self.assertEqual(stability.key_state_name({"key_state": "user"}), "user")
        self.assertEqual(stability.key_state_name({"KeyState": "default"}), "default")
        self.assertTrue(stability.key_using_default({"using_default": True}))
        self.assertTrue(stability.key_using_default({"UsingDefault": True}))


class DatabaseConnectionTargetTest(unittest.TestCase):
    def test_proc_socket_and_processlist_are_joined_by_client_port(self):
        tcp = stability.parse_proc_tcp_table(
            " 89: 0100007F:9752 0100007F:0CEA 01 00000000:00000000 02:000001A9 00000000 0 0 1087173 2\n"
            " 90: 00000000:1FAF 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 1088023 1\n"
        )
        processlist = stability.parse_mysql_processlist(
            "670\tgame\tlocalhost\t\tQuery\n"
            "669\tgame\tlocalhost:38738\td_taiwan\tSleep\n"
            "611\tsupergod\t192.168.200.131:58806\td_technical_report\tSleep\n"
        )
        matched = stability.match_process_mysql_connections(set(["1087173", "1088023"]), tcp, processlist)
        self.assertEqual([row["id"] for row in matched], [669])
        self.assertEqual(matched[0]["client_port"], 38738)

    def test_database_status_requires_inner_ping_and_select(self):
        healthy = {"ok": True, "result": {"ok": True, "select_verified": True}}
        self.assertTrue(stability.database_status_ready(healthy))
        self.assertFalse(stability.database_status_ready({"ok": True, "result": {"ok": False}}))
        self.assertFalse(stability.database_status_ready({"ok": False, "result": healthy["result"]}))

    def test_targeted_drop_requires_a_replacement_connection(self):
        run = object.__new__(stability.StabilityRun)
        healthy = {"ok": True, "result": {"ok": True, "select_verified": True}}
        connection_sets = [[{"id": 669}], [{"id": 671}]]
        coverage = []
        failures = []
        samples = []
        run.wait_database_ready = lambda *args: healthy
        run.robot_mysql_connections = lambda: connection_sets.pop(0)
        run.kill_mysql_connections = lambda ids: (set([669]), "KILLED:669")
        run.mark_coverage = lambda name, observed, detail="": coverage.append((name, observed, detail))
        run.record_failure = lambda name, error: failures.append((name, error))
        run.market_enable_auto = lambda max_concurrent: None
        run.wait_market_services = lambda *args: True
        run.wait_market_auto_running = lambda *args: True
        run.sample_with_event = lambda event: samples.append(event)
        run.safe_call = lambda *args: healthy

        run.database_robot_connection_drop_probe()
        self.assertEqual(failures, [])
        self.assertTrue(dict((name, observed) for name, observed, _ in coverage)["database_robot_connection_recovery"])
        self.assertEqual(samples, ["database_robot_connection_drop_done"])


class CoreFileDetectionTest(unittest.TestCase):
    def test_old_cores_are_ignored_and_new_or_replaced_cores_are_reported_once(self):
        baseline = {
            "/home/neople/bridge/core.1": {"inode": 1, "size": 100, "mtime": 10},
            "/home/neople/game/core.2": {"inode": 2, "size": 200, "mtime": 20},
        }
        current = dict(baseline)
        current["/home/neople/game/core.2"] = {"inode": 3, "size": 250, "mtime": 30}
        current["/home/neople/game/core.3"] = {"inode": 4, "size": 300, "mtime": 40}
        changed = stability.changed_core_files(baseline, current)
        self.assertEqual([item["path"] for item in changed], [
            "/home/neople/game/core.2",
            "/home/neople/game/core.3",
        ])
        self.assertEqual(stability.changed_core_files(baseline, current, set(item["path"] for item in changed)), [])


class TargetActionOutcomeTest(unittest.TestCase):
    def test_outcomes_are_tracked_per_item(self):
        result = {
            "result": {
                "actions": [
                    {"ok": True, "action": {"item_id": 100}},
                    {"ok": False, "action": {"item_id": 200}},
                    {"ok": True, "action": {"item_id": 300}},
                ]
            }
        }
        outcomes = stability.target_action_outcomes(result, [100, 200, 400])
        self.assertEqual(outcomes[100], {"actions": 1, "accepted": True})
        self.assertEqual(outcomes[200], {"actions": 1, "accepted": False})
        self.assertEqual(outcomes[400], {"actions": 0, "accepted": False})
        self.assertNotIn(300, outcomes)


class ScenarioPlanTest(unittest.TestCase):
    def test_default_plan_is_compact_and_dependency_ordered(self):
        run = object.__new__(stability.StabilityRun)
        names = [item["name"] for item in run.scenario_events()]
        self.assertEqual(
            names,
            [
                "integrated_load_data_matrix",
                "restart_recovery_matrix",
            ],
        )
        self.assertEqual(
            run.scenario_events()[0]["phases"],
            ("load_runtime_observation", "market_matrix", "database_fault_matrix"),
        )
        self.assertNotIn("target20", names)
        self.assertNotIn("robot_restart", names)
        self.assertNotIn("market_button_flow", names)

    def test_database_resume_executes_only_database_subphase(self):
        run = object.__new__(stability.StabilityRun)
        run.args = type("Args", (), {"target_min": 100, "target_max": 600})()
        calls = []
        run.log = lambda message: None
        run.set_target = lambda target: calls.append(("target", target))
        run.wait_target_running = lambda *args, **kwargs: True
        run.phase_allowed = lambda: True
        run.begin_shared_runtime_observation = lambda: calls.append(("observe", "begin"))
        run.finish_shared_runtime_observation = lambda high, stages, required, activity: calls.append(
            ("observe", tuple(sorted(stages)), required, activity)
        )
        run.announcement_and_relay_probe = lambda: calls.append(("phase", "load"))
        run.market_workflow = lambda: calls.append(("phase", "market_workflow"))
        run.market_fault_matrix = lambda: calls.append(("phase", "market_fault"))
        run.database_fault_matrix = lambda: calls.append(("phase", "database"))
        run.compact_manual_store_cleanup = lambda: calls.append(("phase", "manual"))

        def run_phase(name, fn, recover=True):
            calls.append(("step", name))
            fn()
            return True

        run.run_phase_step = run_phase
        run.integrated_load_data_matrix("database_fault_matrix")
        self.assertIn(("phase", "database"), calls)
        self.assertIn(("observe", ("database_fault",), True, False), calls)
        self.assertNotIn(("phase", "market_workflow"), calls)
        self.assertNotIn(("phase", "market_fault"), calls)
        self.assertNotIn(("phase", "manual"), calls)

    def test_runtime_resume_skips_config_fault(self):
        run = object.__new__(stability.StabilityRun)
        run.args = type("Args", (), {"target_max": 600})()
        calls = []
        run.log = lambda message: None
        run.phase_allowed = lambda: True
        run.config_dir_fault = lambda: calls.append("config")
        run.configure_compatibility_guards = lambda: calls.append("compat")
        run.set_target = lambda target: calls.append("target")
        run.wait_target_running = lambda *args, **kwargs: True
        run.combined_service_runtime_recovery = lambda high: calls.append("combined")
        run.sample_with_event = lambda event: calls.append("sample")

        def run_phase(name, fn, recover=True):
            fn()
            return True

        run.run_phase_step = run_phase
        run.restart_recovery_matrix("runtime_recovery_matrix")
        self.assertNotIn("config", calls)
        self.assertIn("compat", calls)
        self.assertIn("combined", calls)


class MainExitCodeTest(unittest.TestCase):
    def test_main_reports_run_result(self):
        args = type("Args", (), {
            "rounds": 1,
            "time_limit": "",
            "target_min": 100,
            "target_max": 600,
            "user_interleave_min_interval": 90,
            "user_interleave_max_interval": 180,
        })()
        original_parse_args = stability.parse_args
        original_run = stability.StabilityRun

        class FakeRun(object):
            result = False

            def __init__(self, parsed):
                self.args = parsed

            def run(self):
                return self.result

        try:
            stability.parse_args = lambda: args
            stability.StabilityRun = FakeRun
            self.assertEqual(stability.main(), 1)
            FakeRun.result = True
            self.assertEqual(stability.main(), 0)
        finally:
            stability.parse_args = original_parse_args
            stability.StabilityRun = original_run


class SchedulerCommandReadinessTest(unittest.TestCase):
    def test_busy_actors_are_allowed_after_transition_states_clear(self):
        status = {
            "actors": 20,
            "actor_busy": 8,
            "actor_assigned": 0,
            "actor_online": 0,
            "actor_releasing": 0,
            "operation_active": False,
        }
        self.assertTrue(stability.scheduler_user_commands_ready(status))
        status["actor_online"] = 1
        self.assertFalse(stability.scheduler_user_commands_ready(status))
        status["actor_online"] = 0
        status["operation_active"] = True
        self.assertFalse(stability.scheduler_user_commands_ready(status))

    def test_empty_manual_scheduler_requires_explicit_opt_in(self):
        status = {"actors": 0, "operation_active": False}
        self.assertFalse(stability.scheduler_user_commands_ready(status))
        self.assertTrue(stability.scheduler_user_commands_ready(status, allow_empty=True))

    def test_scheduler_busy_result_is_retryable(self):
        result = {
            "ok": False,
            "result": {"robots": [{"state": "scheduler_busy"}, {"state": "scheduler_busy"}]},
        }
        self.assertTrue(stability.robot_command_retryable(result))
        self.assertFalse(stability.robot_command_retryable({"ok": False, "error": "invalid uid"}))

    def test_action_counter_maps_command_metrics(self):
        status = {
            "move_success": 3,
            "move_failed": 1,
            "shout_local_success": 5,
            "shout_local_failed": 2,
            "shout_world_success": 7,
            "shout_world_failed": 4,
        }
        self.assertEqual(stability.robot_action_counter(status, "robotsMove"), 4)
        self.assertEqual(stability.robot_action_counter(status, "robotsShoutLocal"), 7)
        self.assertEqual(stability.robot_action_counter(status, "robotsShout"), 18)
        self.assertIsNone(stability.robot_action_counter(status, "robotsOnlineAsync"))


class MarketObservationTest(unittest.TestCase):
    def test_timeout_uses_terminal_job_state(self):
        run = object.__new__(stability.StabilityRun)
        run.safe_call = lambda command, payload: {"ok": False, "error": "timed out"}
        run.wait_market_job_idle = lambda event, timeout, interval: True
        run.market_status_result = lambda: {"last_job": {"kind": "restock", "status": "success"}}
        run.log = lambda message: None
        result = run.market_call_when_idle("marketRestockOnce", {}, "unit", attempts=1, delay_sec=0)
        self.assertTrue(result["ok"])
        self.assertTrue(result["observed_after_timeout"])

    def test_terminal_market_states(self):
        self.assertTrue(stability.market_job_terminal_success({"status": "success"}))
        self.assertTrue(stability.market_job_terminal_success({"status": "partial_failed"}))
        self.assertFalse(stability.market_job_terminal_success({"status": "failed"}))
        self.assertTrue(stability.api_result_timed_out({"ok": False, "error": "timed out"}))

    def test_target_selection_skips_layout_missing_catalog_items(self):
        preferred = [(28237, "beam"), (37603, "wand"), (37605, "dagger")]
        selected = stability.select_supported_market_targets(
            preferred,
            set([37603, 37605]),
            {"28237": True, "37603": True, "37605": True},
        )
        self.assertEqual(selected, [(37603, "wand"), (37605, "dagger")])


class CombinedRuntimeObservationTest(unittest.TestCase):
    def test_all_recovery_gates_are_required(self):
        state = dict((name, True) for name in (
            "api_ok", "game_ok", "monitor_ok", "bridge_ok", "market_ok", "party_ok", "mailbox_ok", "key_ok", "load_ok"
        ))
        self.assertTrue(stability.combined_runtime_ready(state))
        state["bridge_ok"] = False
        self.assertFalse(stability.combined_runtime_ready(state))

    def test_monitor_probe_uid_is_required_before_fault_setup(self):
        run = object.__new__(stability.StabilityRun)
        calls = []
        run.select_uids = lambda count: calls.append("select") or []
        run.mark_coverage = lambda *args: None
        with self.assertRaises(RuntimeError):
            run.combined_service_runtime_recovery(600)
        self.assertEqual(calls, ["select"])


class StablePortObservationTest(unittest.TestCase):
    def test_transient_listener_does_not_count_as_recovered(self):
        run = object.__new__(stability.StabilityRun)
        snapshots = [
            {"10011": True, "30303": True},
            {"10011": False, "30303": True},
            {"10011": True, "30303": True},
            {"10011": True, "30303": True},
            {"10011": True, "30303": True},
        ]
        run.port_snapshot = lambda: snapshots.pop(0)
        run.port_text = lambda name: {"Game": "10011", "Monitor": "30303"}[name]
        run.log = lambda message: None
        self.assertTrue(run.wait_service_ports_stable("unit", ("Game", "Monitor"), True, 1, 0, 3))
        self.assertEqual(snapshots, [])

    def test_core_restart_uses_detached_session_and_checks_bridge(self):
        run = object.__new__(stability.StabilityRun)
        calls = []
        run.safe_call = lambda command, payload: {"ok": True}
        run.wait_market_job_idle = lambda *args: True
        run.wait_auto_drained = lambda *args: True
        run.port_regex = lambda names: "10011|30303|7000|30603|30803"
        run.log = lambda message: None

        def shell(command, timeout, log_output=True):
            calls.append(("shell", command))
            return "CORE_START_RC=0" if "setsid sh -c" in command else ""

        def wait_ports(event, names, expected, *args, **kwargs):
            calls.append(("ports", tuple(names), expected))
            return True

        run.shell = shell
        run.wait_service_ports = wait_ports
        run.wait_service_ports_stable = wait_ports
        self.assertTrue(run.restart_core_services("unit"))
        launch = [entry[1] for entry in calls if entry[0] == "shell" and "setsid sh -c" in entry[1]][0]
        self.assertIn("mkdir -p %s" % stability.shell_quote(stability.STABILITY_OUTPUT_ROOT), launch)
        self.assertIn("setsid sh -c 'cd /root && ./run' </dev/null", launch)
        self.assertIn(stability.CORE_START_LOG, launch)
        port_checks = [entry for entry in calls if entry[0] == "ports"]
        self.assertEqual(port_checks[0][1], ("Game", "Monitor", "Bridge"))
        self.assertEqual(port_checks[-1][1], ("Game", "Monitor", "Bridge"))

    def test_auto_drain_requires_all_activity_to_clear(self):
        run = object.__new__(stability.StabilityRun)
        statuses = [
            {"actors": 2, "running": 2},
            {"actors": 1, "actor_releasing": 1},
            {"actors": 0, "running": 0, "connecting": 0, "actor_online": 0, "actor_releasing": 0},
        ]
        run.safe_call = lambda command, payload: {"ok": True, "result": statuses.pop(0)}
        run.log = lambda message: None
        self.assertTrue(run.wait_auto_drained("unit", 1, 0))
        self.assertEqual(statuses, [])


class FailureDetectionTest(unittest.TestCase):
    def test_failure_result_stops_fail_fast_round(self):
        run = object.__new__(stability.StabilityRun)
        run.results = [{"error": "", "recovered": True}]
        self.assertFalse(run.has_failures())
        run.results.append({"error": "boom", "recovered": True})
        self.assertTrue(run.has_failures())

    def test_scenario_selection_starts_at_requested_matrix(self):
        run = object.__new__(stability.StabilityRun)
        events = run.scenario_events()
        selected = stability.select_scenario_events(events, "database_fault_matrix")
        self.assertEqual(
            [event["name"] for event in selected],
            ["integrated_load_data_matrix", "restart_recovery_matrix"],
        )
        self.assertEqual(selected[0]["start_phase"], "database_fault_matrix")

    def test_integrated_observation_stage_detection(self):
        rows = [
            {"event": "shared_runtime_observation:start"},
            {"event": "market_workflow_final"},
            {"event": "market_service_port_conflict"},
            {"event": "market_iteminfo_missing"},
            {"event": "market_source_partial"},
            {"event": "database_robot_connection_drop_done"},
        ]
        self.assertEqual(
            stability.integrated_observation_stages(rows),
            set(("load", "market_workflow", "market_service_fault", "market_source_fault", "database_fault")),
        )

    def test_integrated_runtime_health_uses_complete_port_samples(self):
        rows = [
            {
                "api_error": "",
                "game_port": "1",
                "monitor_port": "1",
                "bridge_port": "1",
                "relay_port": "1",
                "goroutines": 100,
                "memory_mb": 200,
                "fd_robot": 50,
            },
            {
                "api_error": "timeout",
                "game_port": "1",
                "monitor_port": "0",
                "bridge_port": "1",
                "relay_port": "1",
                "goroutines": 120,
                "memory_mb": 220,
                "fd_robot": 60,
            },
            {"api_error": "", "game_port": "", "monitor_port": "", "bridge_port": "", "relay_port": ""},
        ]
        stats = stability.integrated_runtime_health_stats(rows)
        self.assertEqual(stats["api_errors"], 1)
        self.assertEqual(stats["core_port_samples"], 2)
        self.assertEqual(stats["core_port_down"], 1)
        self.assertEqual(stats["goroutines"]["start"], 100)
        self.assertEqual(stats["goroutines"]["end"], 120)


class HighLoadObservationTest(unittest.TestCase):
    def new_run(self, rows):
        run = object.__new__(stability.StabilityRun)
        run.sample_metrics = rows
        run.coverage = {}
        run.results = []
        run.current_round = 1
        run.current_phase = "load_runtime_observation"
        run.log = lambda message: None
        return run

    def test_healthy_store_window_passes(self):
        rows = []
        for index in range(3):
            rows.append(
                {
                    "target": 600,
                    "running": 590,
                    "stores": 64,
                    "item_stores": 31,
                    "disjoint_stores": 33,
                    "displayed_item_stores": 31,
                    "seven_item_stores": 31,
                    "displayed_zero": 0,
                    "displayed_out_of_range": 0,
                    "nocache_sent": 10 + index,
                    "nocache_failed": 0,
                }
            )
        run = self.new_run(rows)
        stats = stability.high_load_observation_stats(rows, 600)
        self.assertTrue(stability.high_load_observation_ready(stats, 600))
        run.validate_high_load_observation(0, len(rows), 600)
        self.assertEqual(run.results, [])
        self.assertTrue(run.coverage["high_load_store_observation"]["observed"])
        self.assertTrue(run.coverage["store_nocache"]["observed"])

    def test_turnover_evidence_passes_with_realistic_concurrency(self):
        rows = []
        for index in range(4):
            rows.append(
                {
                    "target": 600,
                    "running": 570,
                    "stores": 9,
                    "item_stores": 4,
                    "disjoint_stores": 5,
                    "store_uids": list(range(index * 5 + 1, index * 5 + 10)),
                    "item_store_uids": list(range(index * 5 + 1, index * 5 + 5)),
                    "disjoint_store_uids": list(range(index * 5 + 5, index * 5 + 10)),
                    "store_success": 100 + index * 4,
                    "displayed_item_stores": 4,
                    "seven_item_stores": 4,
                    "displayed_zero": 0,
                    "displayed_out_of_range": 0,
                    "nocache_sent": 10 + index,
                    "nocache_failed": 0,
                }
            )
        run = self.new_run(rows)
        stats = stability.high_load_observation_stats(rows, 600)
        self.assertEqual(stability.high_load_running_floor(600), 564)
        self.assertEqual(stability.high_load_store_requirements(600), (6, 12))
        self.assertEqual(stats["peak_stores"], 9)
        self.assertGreaterEqual(stats["store_activity"], 20)
        self.assertTrue(stability.high_load_observation_ready(stats, 600))
        run.validate_high_load_observation(0, len(rows), 600)
        self.assertEqual(run.results, [])

    def test_invalid_display_and_missing_nocache_fail(self):
        rows = [
            {
                "target": 600,
                "running": 590,
                "stores": 10,
                "item_stores": 5,
                "disjoint_stores": 5,
                "displayed_item_stores": 5,
                "seven_item_stores": 2,
                "displayed_zero": 1,
                "displayed_out_of_range": 0,
                "nocache_sent": 4,
                "nocache_failed": 0,
            }
        ]
        run = self.new_run(rows)
        stats = stability.high_load_observation_stats(rows, 600)
        self.assertFalse(stability.high_load_observation_ready(stats, 600))
        run.validate_high_load_observation(0, len(rows), 600)
        names = {item["name"] for item in run.results}
        self.assertIn("high_load_stable_samples", names)
        self.assertIn("store_count_below_expected", names)
        self.assertIn("store_display_invalid_count", names)
        self.assertIn("store_display_seven_ratio_low", names)
        self.assertIn("store_nocache_not_observed", names)


class ManualStoreObservationTest(unittest.TestCase):
    def test_targeted_store_observation_accumulates_uids(self):
        run = object.__new__(stability.StabilityRun)
        run.args = type("Args", (), {"status_count": 1000})()
        batches = [
            [{"uid": 11, "store_display_ack": True}],
            [{"uid": 12, "disjoint_active": True}],
        ]
        run.status_rows = lambda count: batches.pop(0)
        coverage = []
        failures = []
        run.mark_coverage = lambda label, observed, detail: coverage.append((label, observed, detail))
        run.record_failure = lambda label, error: failures.append((label, error))

        self.assertTrue(run.wait_uids_store_active("targeted", [11, 12], 2, 5, 0))
        self.assertEqual(failures, [])
        self.assertTrue(coverage[-1][1])
        self.assertIn("observed=2/2", coverage[-1][2])


class MarketFaultObservationTest(unittest.TestCase):
    def test_terminal_service_states_settle(self):
        status = {
            "services": {
                "auction": {"status": "start_failed"},
                "point": {"status": "ready"},
            }
        }
        self.assertTrue(stability.market_fault_state_settled(status))

    def test_down_or_missing_service_does_not_settle(self):
        self.assertFalse(
            stability.market_fault_state_settled(
                {"services": {"auction": {"status": "down"}, "point": {"status": "ready"}}}
            )
        )
        self.assertFalse(stability.market_fault_state_settled({"services": {}}))

    def test_stable_nonterminal_fault_is_observed(self):
        status = {
            "services": {
                "auction": {"status": "down", "listening": False},
                "point": {"status": "down", "listening": False},
            }
        }
        self.assertTrue(stability.market_fault_signature(status))
        self.assertFalse(stability.market_fault_state_observed(status))
        status["_stable_fault_observed"] = True
        self.assertTrue(stability.market_fault_state_observed(status))

    def test_wait_accepts_three_stable_fault_samples(self):
        run = object.__new__(stability.StabilityRun)
        status = {
            "services": {
                "auction": {"status": "down", "listening": False},
                "point": {"status": "down", "listening": False},
            }
        }
        run.market_status_result = lambda: dict(status, services=dict(status["services"]))
        run.market_services_ready = lambda value: False
        run.log = lambda message: None
        observed = run.wait_market_fault_state("unit", 5, 0)
        self.assertTrue(observed["_stable_fault_observed"])
        self.assertEqual(observed["_stable_fault_samples"], 3)


class LogCursorTest(unittest.TestCase):
    def test_reads_only_appended_and_rotated_content(self):
        directory = tempfile.mkdtemp()
        try:
            path = os.path.join(directory, "robot.log")
            with open(path, "wb") as fh:
                fh.write(b"old\n")
            run = object.__new__(stability.StabilityRun)
            cursor = run.capture_log_cursor(path)
            with open(path, "ab") as fh:
                fh.write(b"new\n")
            self.assertEqual(run.read_log_since(cursor).strip(), "new")

            cursor = run.capture_log_cursor(path)
            os.rename(path, path + ".1")
            with open(path, "wb") as fh:
                fh.write(b"rotated\n")
            self.assertEqual(run.read_log_since(cursor).strip(), "rotated")
        finally:
            shutil.rmtree(directory)


class LogCollectionTest(unittest.TestCase):
    def test_collection_is_bounded_and_skips_full_game_log_scan(self):
        directory = tempfile.mkdtemp()
        try:
            run = object.__new__(stability.StabilityRun)
            run.out_dir = directory
            run.args = type("Args", (), {"log_tail_lines": 100})()
            run.auction_iteminfo = "/tmp/auction/iteminfo.dat"
            run.point_iteminfo = "/tmp/point/iteminfo.dat"
            run.port_regex = lambda names: "8111|10011"
            run.log = lambda message: None
            calls = []

            def shell(command, timeout, log_output=True):
                calls.append((command, timeout, log_output))
                return "ok\n"

            run.shell = shell
            run.collect_logs("unit")
            self.assertEqual(calls[0][1], 30)
            self.assertNotIn("find ", calls[0][0])
            self.assertIn(stability.ROBOT_LOG_PATH, calls[0][0])
            self.assertIn(stability.ROBOT_STDOUT_LOG_PATH, calls[0][0])
            self.assertIn(stability.ROBOT_START_ERROR_LOG_PATH, calls[0][0])
            self.assertIn(stability.MARKET_LOG_PATH, calls[0][0])
            self.assertNotIn("/root/config/log_robot", calls[0][0])
            self.assertNotIn("market_auction_service.log", calls[0][0])
            self.assertNotIn("market_point_service.log", calls[0][0])
        finally:
            shutil.rmtree(directory)


class RobotRestartCommandTest(unittest.TestCase):
    def test_restart_uses_classified_log_paths(self):
        run = object.__new__(stability.StabilityRun)
        commands = []
        events = []
        run.log = lambda message: None
        run.sample_with_event = lambda event: events.append(event)
        run.port_regex = lambda names: "8111|8112"
        run.shell = lambda command, timeout: commands.append(command) or ""
        run.wait_robot_api = lambda *args: True
        run.web_opener = object()

        self.assertTrue(run.robot_restart_without_target("unit"))
        self.assertEqual(len(commands), 1)
        self.assertIn("mkdir -p /root/config/logs", commands[0])
        self.assertIn("--bounded-log-sink /root/config/logs/stdout.log", commands[0])
        self.assertIn("2>/root/config/logs/start_error.log", commands[0])
        self.assertNotIn("robot_stdout.log", commands[0])
        self.assertEqual(events, ["unit_stop", "unit_started"])


class ConfigDirectoryFaultCommandTest(unittest.TestCase):
    def test_fault_and_restore_preserve_logs_and_use_conf_directory(self):
        run = object.__new__(stability.StabilityRun)
        commands = []
        failures = []
        run.log = lambda message: None
        run.safe_call = lambda command, payload: {"ok": True}
        run.wait_market_job_idle = lambda *args: True
        run.stop_market_services = lambda: None
        run.sample_with_event = lambda event: None
        run.robot_restart_without_target = lambda label: True
        run.mark_coverage = lambda *args: None
        run.record_failure = lambda name, error: failures.append((name, error))
        run.market_enable_auto = lambda **kwargs: None
        run.wait_market_services = lambda *args: True

        def shell(command, timeout, log_output=True):
            commands.append(command)
            if "CONFIG_BACKUP_OK" in command:
                return "CONFIG_BACKUP_OK"
            if "CONFIG_RESTORE_OK" in command:
                return "CONFIG_RESTORE_OK"
            return ""

        run.shell = shell
        run.config_dir_fault()

        fault = [command for command in commands if "equip_inflate_min = broken" in command][0]
        restore = [command for command in commands if "CONFIG_RESTORE_OK" in command][0]
        for command in (fault, restore):
            self.assertIn("! -name 'logs'", command)
            self.assertNotIn("log_robot", command)
            self.assertNotIn("market_log.jsonl", command)
        self.assertIn(stability.MARKET_CONFIG_PATH, fault)
        self.assertIn('cp -af', restore)
        self.assertEqual(failures, [])


class ShellExecutionTest(unittest.TestCase):
    def test_large_output_does_not_fill_a_pipe(self):
        fd, path = tempfile.mkstemp(suffix=".py")
        os.close(fd)
        try:
            with open(path, "w") as script:
                script.write("import sys\nsys.stdout.write('x' * (512 * 1024))\n")
            command = '"%s" "%s"' % (sys.executable.replace('"', '\\"'), path.replace('"', '\\"'))
            run = object.__new__(stability.StabilityRun)
            run.log = lambda message: None
            output = run.shell(command, 10, log_output=False)
            self.assertEqual(len(output), 512 * 1024)
        finally:
            os.remove(path)

    def test_timeout_terminates_the_subprocess_group_without_pipe_communication(self):
        class FakeProcess(object):
            pid = 123

            def __init__(self):
                self.stopped = False

            def poll(self):
                return -9 if self.stopped else None

            def kill(self):
                self.stopped = True

        proc = FakeProcess()
        popen_calls = []
        kill_calls = []
        original_popen = stability.subprocess.Popen
        original_kill = stability.kill_subprocess_group

        def fake_popen(command, **kwargs):
            popen_calls.append((command, kwargs))
            self.assertIsNot(kwargs["stdout"], stability.subprocess.PIPE)
            kwargs["stdout"].write(b"partial output")
            return proc

        def fake_kill(target):
            kill_calls.append(target)
            target.stopped = True

        stability.subprocess.Popen = fake_popen
        stability.kill_subprocess_group = fake_kill
        try:
            run = object.__new__(stability.StabilityRun)
            run.log = lambda message: None
            output = run.shell("blocked", 0.01, log_output=False)
        finally:
            stability.subprocess.Popen = original_popen
            stability.kill_subprocess_group = original_kill

        self.assertEqual(output, "partial output")
        self.assertEqual(kill_calls, [proc])
        self.assertEqual(len(popen_calls), 1)


if __name__ == "__main__":
    unittest.main()
