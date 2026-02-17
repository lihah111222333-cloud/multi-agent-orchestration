import unittest

import dashboard


class DashboardResponsesConfigTests(unittest.TestCase):
    def test_sanitize_accepts_previous_response_id_toggle(self):
        data = dashboard._sanitize_config_updates({"OPENAI_USE_PREVIOUS_RESPONSE_ID": "1"})
        self.assertEqual(data["OPENAI_USE_PREVIOUS_RESPONSE_ID"], "1")

    def test_sanitize_accepts_conversation_id(self):
        data = dashboard._sanitize_config_updates({"OPENAI_RESPONSES_CONVERSATION_ID": "conv_123"})
        self.assertEqual(data["OPENAI_RESPONSES_CONVERSATION_ID"], "conv_123")

    def test_render_html_contains_responses_controls(self):
        html = dashboard.render_html()
        self.assertIn('name="OPENAI_USE_PREVIOUS_RESPONSE_ID"', html)
        self.assertIn('name="OPENAI_RESPONSES_CONVERSATION_ID"', html)


if __name__ == "__main__":
    unittest.main()
