import json
import unittest
from unittest import mock

import agents.all_in_one as aio


class AllInOneItermToolTests(unittest.TestCase):
    def test_iterm_list_sessions_wrapper(self):
        with mock.patch("agents.all_in_one.list_iterm_agent_sessions", return_value={"ok": True, "count": 4}):
            text = aio.iterm_list_sessions()
        data = json.loads(text)
        self.assertTrue(data["ok"])
        self.assertEqual(data["count"], 4)

    def test_iterm_send_input_wrapper(self):
        with mock.patch("agents.all_in_one.send_iterm_input", return_value={"ok": True, "target_count": 2}):
            text = aio.iterm_send_input(text="hello", agent_id="agent_01,agent_02")
        data = json.loads(text)
        self.assertTrue(data["ok"])
        self.assertEqual(data["target_count"], 2)

    def test_iterm_read_output_wrapper(self):
        with mock.patch("agents.all_in_one.read_iterm_output", return_value={"ok": True, "results": []}):
            text = aio.iterm_read_output(all_agents=True)
        data = json.loads(text)
        self.assertTrue(data["ok"])
        self.assertEqual(data["results"], [])

    def test_shared_file_wrappers(self):
        with mock.patch("agents.all_in_one.write_shared_file", return_value={"ok": True, "path": "a.txt"}):
            write_text = aio.write_file(path="a.txt", content="x")
            write_data = json.loads(write_text)
            self.assertTrue(write_data["ok"])

        with mock.patch("agents.all_in_one.read_shared_file", return_value={"path": "a.txt", "content": "x"}):
            read_text = aio.read_file(path="a.txt")
            read_data = json.loads(read_text)
            self.assertEqual(read_data["path"], "a.txt")

        with mock.patch("agents.all_in_one.list_shared_files", return_value=[{"path": "a.txt"}]):
            list_text = aio.list_files(path="a")
            list_data = json.loads(list_text)
            self.assertEqual(list_data["count"], 1)

        with mock.patch("agents.all_in_one.delete_shared_file", return_value={"ok": True, "deleted": True}):
            del_text = aio.delete_file(path="a.txt")
            del_data = json.loads(del_text)
            self.assertTrue(del_data["deleted"])

    def test_interaction_and_prompt_wrappers(self):
        with mock.patch("agents.all_in_one.create_interaction_row", return_value={"id": 1, "sender": "master"}):
            text = aio.create_interaction(sender="master", receiver="a1", msg_type="task", content="do it")
            data = json.loads(text)
            self.assertTrue(data["ok"])
            self.assertEqual(data["interaction"]["id"], 1)

        with mock.patch("agents.all_in_one.list_interaction_rows", return_value=[{"id": 1}]):
            text = aio.list_interactions(limit=10)
            data = json.loads(text)
            self.assertEqual(data["count"], 1)

        with mock.patch("agents.all_in_one.save_prompt_template_row", return_value={"prompt_key": "k"}):
            text = aio.save_prompt_template(prompt_key="k", title="t", prompt_text="p")
            data = json.loads(text)
            self.assertTrue(data["ok"])

        with mock.patch("agents.all_in_one.get_prompt_template_row", return_value={"prompt_key": "k"}):
            text = aio.get_prompt_template(prompt_key="k")
            data = json.loads(text)
            self.assertTrue(data["ok"])


    def test_command_card_execution_wrappers(self):
        with mock.patch("agents.all_in_one.prepare_command_card_run_flow", return_value={"ok": True, "run": {"id": 11}}):
            text = aio.prepare_command_card_run(card_key="c", params_json='{"name":"x"}')
            data = json.loads(text)
            self.assertTrue(data["ok"])

        with mock.patch("agents.all_in_one.review_command_card_run_flow", return_value={"ok": True, "run": {"id": 11, "status": "ready"}}):
            text = aio.review_command_card_run(run_id=11, decision="approved")
            data = json.loads(text)
            self.assertTrue(data["ok"])

        with mock.patch("agents.all_in_one.execute_command_card_run_flow", return_value={"ok": True, "run": {"id": 11, "status": "success"}}):
            text = aio.execute_command_card_run(run_id=11)
            data = json.loads(text)
            self.assertTrue(data["ok"])

        with mock.patch("agents.all_in_one.execute_command_card_flow", return_value={"ok": True, "run": {"id": 22, "status": "success"}}):
            text = aio.execute_command_card(card_key="c", params_json='{"name":"x"}', auto_approve=True)
            data = json.loads(text)
            self.assertTrue(data["ok"])

        with mock.patch("agents.all_in_one.get_command_card_run_row", return_value={"id": 22}):
            text = aio.get_command_card_run(run_id=22)
            data = json.loads(text)
            self.assertTrue(data["ok"])

        with mock.patch("agents.all_in_one.list_command_card_run_rows", return_value=[{"id": 22}]):
            text = aio.list_command_card_runs(limit=10)
            data = json.loads(text)
            self.assertEqual(data["count"], 1)
    def test_command_card_and_db_wrappers(self):
        with mock.patch("agents.all_in_one.save_command_card_row", return_value={"card_key": "c"}):
            text = aio.save_command_card(card_key="c", title="t", command_template="echo hi")
            data = json.loads(text)
            self.assertTrue(data["ok"])

        with mock.patch("agents.all_in_one.db_query_sql", return_value=[{"v": 1}]):
            text = aio.db_query(sql="SELECT 1", limit=10)
            data = json.loads(text)
            self.assertEqual(data["count"], 1)

        with mock.patch("agents.all_in_one.db_execute_sql", return_value={"ok": True, "rowcount": 2}):
            text = aio.db_execute(sql="UPDATE x SET y=1")
            data = json.loads(text)
            self.assertEqual(data["rowcount"], 2)

    def test_orchestration_tui_wrapper(self):
        with mock.patch(
            "agents.all_in_one.publish_orchestration_tui_begin",
            return_value={"ok": True, "event": "BeginOrchestrationTaskState"},
        ):
            begin_text = aio.orchestration_tui(action="begin", run_id="run-001", status_header="Running")
            begin_data = json.loads(begin_text)
            self.assertTrue(begin_data["ok"])
            self.assertEqual(begin_data["event"], "BeginOrchestrationTaskState")

        with mock.patch(
            "agents.all_in_one.get_orchestration_tui_snapshot",
            return_value={"ok": True, "running": False, "active_count": 0},
        ):
            snap_text = aio.orchestration_tui(action="snapshot")
            snap_data = json.loads(snap_text)
            self.assertTrue(snap_data["ok"])
            self.assertFalse(snap_data["running"])


if __name__ == "__main__":
    unittest.main()
