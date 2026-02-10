import tempfile
import unittest
from pathlib import Path

from plugins.loader import discover_plugins, load_plugin_module


class PluginLoaderTests(unittest.TestCase):
    def test_discover_plugins_ignores_non_directory_entries(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            base_dir = Path(tmp_dir)
            (base_dir / "README.md").write_text("not a plugin", encoding="utf-8")

            alpha_dir = base_dir / "alpha"
            alpha_dir.mkdir()
            (alpha_dir / "plugin.py").write_text('PLUGIN_NAME = "alpha"\n', encoding="utf-8")

            discovered = discover_plugins(base_dir)
            self.assertEqual(discovered, [str(alpha_dir / "plugin.py")])

    def test_discover_plugins_ignores_directory_without_plugin_py(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            base_dir = Path(tmp_dir)

            (base_dir / "empty").mkdir()

            beta_dir = base_dir / "beta"
            beta_dir.mkdir()
            (beta_dir / "plugin.py").write_text('PLUGIN_NAME = "beta"\n', encoding="utf-8")

            discovered = discover_plugins(base_dir)
            self.assertEqual(discovered, [str(beta_dir / "plugin.py")])

    def test_discover_plugins_raises_on_duplicate_plugin_name(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            base_dir = Path(tmp_dir)

            first = base_dir / "first"
            first.mkdir()
            (first / "plugin.py").write_text('PLUGIN_NAME = "dup"\n', encoding="utf-8")

            second = base_dir / "second"
            second.mkdir()
            (second / "plugin.py").write_text('PLUGIN_NAME = "dup"\n', encoding="utf-8")

            with self.assertRaisesRegex(ValueError, "重复插件名"):
                discover_plugins(base_dir)

    def test_load_plugin_module_success(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            base_dir = Path(tmp_dir)
            hello_dir = base_dir / "hello"
            hello_dir.mkdir()
            plugin_file = hello_dir / "plugin.py"
            plugin_file.write_text(
                'PLUGIN_NAME = "hello"\nVALUE = 42\n\n'
                "def ping() -> str:\n"
                '    return "pong"\n',
                encoding="utf-8",
            )

            discovered = discover_plugins(base_dir)
            self.assertEqual(discovered, [str(plugin_file)])

            module = load_plugin_module(discovered[0])
            self.assertEqual(module.PLUGIN_NAME, "hello")
            self.assertEqual(module.VALUE, 42)
            self.assertEqual(module.ping(), "pong")


if __name__ == "__main__":
    unittest.main()
