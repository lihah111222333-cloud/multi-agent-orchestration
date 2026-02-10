import json
import tempfile
import unittest
from pathlib import Path

from config import settings


class DynamicConfigArchitectureTests(unittest.TestCase):
    def test_load_architecture_supports_agents_without_module(self):
        config = {
            "gateways": [
                {
                    "id": "gateway_x",
                    "name": "动态网关",
                    "agents": [
                        {
                            "id": "agent_01",
                            "name": "固定模块",
                            "module": "agents.runtime_agent",
                            "capabilities": ["collect"],
                        },
                        {
                            "id": "agent_13",
                            "name": "动态代理",
                            "capabilities": ["clean"],
                            "depends_on": ["agent_01"],
                        },
                    ],
                }
            ]
        }

        with tempfile.TemporaryDirectory() as tmpdir:
            path = Path(tmpdir) / "config.json"
            path.write_text(json.dumps(config, ensure_ascii=False), encoding="utf-8")

            old_config_file = settings.CONFIG_FILE
            try:
                settings.CONFIG_FILE = path
                gw_map = settings.load_architecture()
            finally:
                settings.CONFIG_FILE = old_config_file

        self.assertIn("gateway_x", gw_map)
        agents = gw_map["gateway_x"]["agents"]

        self.assertEqual(agents["agent_01"]["args"], ["-m", "agents.runtime_agent"])
        self.assertEqual(
            agents["agent_13"]["args"],
            ["-m", "agents.runtime_agent", "--id", "agent_13", "--name", "动态代理"],
        )

        self.assertIn("capabilities", gw_map["gateway_x"])
        self.assertIn("collect", gw_map["gateway_x"]["capabilities"])
        self.assertIn("clean", gw_map["gateway_x"]["capabilities"])

        agent_meta = gw_map["gateway_x"]["agent_meta"]
        self.assertEqual(agent_meta["agent_13"]["depends_on"], ["agent_01"])


if __name__ == "__main__":
    unittest.main()
