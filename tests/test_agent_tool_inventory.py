import unittest

from agents.specs import AGENT_SPECS


class AgentToolInventoryTests(unittest.TestCase):
    def test_agent_tool_inventory(self):
        expected_tools = {
            "agent_01": ["fetch_market_data", "fetch_news"],
            "agent_02": ["clean_data", "normalize_data"],
            "agent_03": ["statistical_analysis", "trend_analysis"],
            "agent_04": ["create_chart", "create_dashboard"],
            "agent_05": ["write_article", "summarize_text"],
            "agent_06": ["translate", "detect_language"],
            "agent_07": ["generate_code", "review_code"],
            "agent_08": ["generate_report", "export_pdf"],
            "agent_09": ["check_system_health", "get_metrics"],
            "agent_10": ["search_logs", "analyze_errors"],
            "agent_11": ["deploy_service", "rollback_service"],
            "agent_12": ["create_alert", "list_active_alerts", "acknowledge_alert"],
        }

        self.assertEqual(set(AGENT_SPECS.keys()), set(expected_tools.keys()))

        total = 0
        for agent_id, expected in expected_tools.items():
            actual = [tool.name for tool in AGENT_SPECS[agent_id].tools]
            self.assertEqual(
                actual,
                expected,
                f"{agent_id} tools mismatch: expected={expected}, actual={actual}",
            )
            total += len(actual)

        self.assertEqual(total, 25)


if __name__ == "__main__":
    unittest.main()
