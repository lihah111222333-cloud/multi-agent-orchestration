import unittest
from unittest import mock

import utils
from utils import extract_text


class UtilsTests(unittest.TestCase):
    def _build_with_dummy_llm(
        self,
        *,
        env: dict[str, str] | None = None,
        base_url: str = "https://example.com/v1",
        payload: dict | None = None,
    ) -> tuple[dict, object]:
        captured: dict = {}
        payload_to_return = payload if payload is not None else {"input": []}

        class _DummyChatOpenAI:
            def __init__(self, **kwargs):
                captured["kwargs"] = kwargs

            def _get_request_payload(self, input_, *, stop=None, **kwargs):
                return payload_to_return

        with (
            mock.patch("utils._load_chat_openai_class", return_value=_DummyChatOpenAI),
            mock.patch.dict("os.environ", env or {}, clear=False),
        ):
            llm = utils.build_chat_openai(
                model="gpt-5.2-codex",
                temperature=0.2,
                base_url=base_url,
                max_retries=1,
                request_timeout=30,
            )

        return captured["kwargs"], llm

    def test_extract_text_handles_none(self):
        self.assertEqual(extract_text(None), "")

    def test_build_chat_openai_auto_enables_responses_for_gpt5(self):
        kwargs, _ = self._build_with_dummy_llm()
        self.assertTrue(kwargs["use_responses_api"])
        self.assertFalse(kwargs["use_previous_response_id"])
        self.assertTrue(kwargs["store"])

    def test_build_chat_openai_respects_explicit_toggle(self):
        kwargs, _ = self._build_with_dummy_llm(env={"OPENAI_USE_RESPONSES_API": "0"})
        self.assertFalse(kwargs["use_responses_api"])

    def test_build_chat_openai_allows_explicit_previous_response_toggle(self):
        kwargs, _ = self._build_with_dummy_llm(
            env={"OPENAI_USE_RESPONSES_API": "1", "OPENAI_USE_PREVIOUS_RESPONSE_ID": "1"}
        )
        self.assertTrue(kwargs["use_responses_api"])
        self.assertTrue(kwargs["use_previous_response_id"])

    def test_build_chat_openai_responses_store_can_be_disabled(self):
        kwargs, _ = self._build_with_dummy_llm(
            env={"OPENAI_USE_RESPONSES_API": "1", "OPENAI_RESPONSES_STORE": "0"}
        )
        self.assertTrue(kwargs["use_responses_api"])
        self.assertFalse(kwargs["store"])

    def test_build_chat_openai_sets_responses_model_kwargs(self):
        kwargs, _ = self._build_with_dummy_llm(
            env={
                "OPENAI_USE_RESPONSES_API": "1",
                "OPENAI_RESPONSES_BACKGROUND": "1",
                "OPENAI_RESPONSES_CONVERSATION_ID": "conv_123",
                "OPENAI_RESPONSES_VERBOSITY": "low",
                "OPENAI_RESPONSES_CONTEXT_MANAGEMENT": '[{"type":"clear"}]',
            }
        )
        model_kwargs = kwargs.get("model_kwargs")
        self.assertIsInstance(model_kwargs, dict)
        self.assertTrue(model_kwargs["background"])
        self.assertEqual(model_kwargs["conversation"], {"id": "conv_123"})
        self.assertEqual(model_kwargs["text"], {"verbosity": "low"})
        self.assertEqual(model_kwargs["context_management"], [{"type": "clear"}])

    def test_build_chat_openai_sanitizes_responses_input_for_third_party_by_default(self):
        payload = {
            "input": [
                {"type": "reasoning", "id": "rs_123", "summary": []},
                {
                    "type": "message",
                    "role": "assistant",
                    "id": "msg_111",
                    "content": [
                        {"type": "output_text", "text": "hello", "id": "msg_111"},
                        {"type": "reasoning", "id": "rs_456", "summary": []},
                    ],
                },
            ]
        }

        _, llm = self._build_with_dummy_llm(payload=payload, base_url="https://example.com/v1")
        sanitized = llm._get_request_payload("hi")
        self.assertEqual(
            sanitized["input"],
            [
                {
                    "type": "message",
                    "role": "assistant",
                    "content": [{"type": "output_text", "text": "hello"}],
                }
            ],
        )

    def test_build_chat_openai_can_disable_responses_input_sanitizing(self):
        payload = {
            "input": [{"type": "reasoning", "id": "rs_123", "summary": []}],
        }

        _, llm = self._build_with_dummy_llm(
            payload=payload,
            base_url="https://example.com/v1",
        )
        with mock.patch.dict("os.environ", {"OPENAI_RESPONSES_SANITIZE_INPUT": "0"}, clear=False):
            sanitized = llm._get_request_payload("hi")
        self.assertEqual(sanitized["input"][0]["id"], "rs_123")


if __name__ == "__main__":
    unittest.main()
