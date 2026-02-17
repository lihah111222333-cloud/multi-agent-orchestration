import tempfile
import unittest
from pathlib import Path

from plugins.loader import discover_plugin_index, discover_plugins, load_plugin_module, load_plugins


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

    def test_discover_plugin_index_returns_name_to_file_mapping(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            base_dir = Path(tmp_dir)
            (base_dir / "alpha").mkdir()
            (base_dir / "alpha" / "plugin.py").write_text('PLUGIN_NAME = "alpha"\n', encoding="utf-8")
            (base_dir / "beta").mkdir()
            (base_dir / "beta" / "plugin.py").write_text('PLUGIN_NAME = "beta"\n', encoding="utf-8")

            index = discover_plugin_index(base_dir)

            self.assertEqual(set(index.keys()), {"alpha", "beta"})
            self.assertEqual(index["alpha"], base_dir / "alpha" / "plugin.py")
            self.assertEqual(index["beta"], base_dir / "beta" / "plugin.py")

    def test_load_plugins_success(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            base_dir = Path(tmp_dir)

            alpha_dir = base_dir / "alpha"
            alpha_dir.mkdir()
            (alpha_dir / "plugin.py").write_text(
                'PLUGIN_NAME = "alpha"\n'
                "PLUGIN_TOOLS = []\n",
                encoding="utf-8",
            )

            beta_dir = base_dir / "beta"
            beta_dir.mkdir()
            (beta_dir / "plugin.py").write_text(
                'PLUGIN_NAME = "beta"\n'
                "PLUGIN_TOOLS = []\n",
                encoding="utf-8",
            )

            loaded = load_plugins(("alpha", "beta"), base_dir=base_dir)
            self.assertEqual(set(loaded.keys()), {"alpha", "beta"})
            self.assertEqual(loaded["alpha"].PLUGIN_NAME, "alpha")
            self.assertEqual(loaded["beta"].PLUGIN_NAME, "beta")

    def test_load_plugins_rejects_unknown_plugin(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            base_dir = Path(tmp_dir)
            with self.assertRaisesRegex(KeyError, "插件不存在"):
                load_plugins(("not_found",), base_dir=base_dir)

    def test_load_plugins_rejects_duplicate_requested_names(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            base_dir = Path(tmp_dir)
            alpha_dir = base_dir / "alpha"
            alpha_dir.mkdir()
            (alpha_dir / "plugin.py").write_text(
                'PLUGIN_NAME = "alpha"\n'
                "PLUGIN_TOOLS = []\n",
                encoding="utf-8",
            )

            with self.assertRaisesRegex(ValueError, "重复插件声明"):
                load_plugins(("alpha", "alpha"), base_dir=base_dir)

    def test_load_plugins_requires_plugin_tools(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            base_dir = Path(tmp_dir)
            alpha_dir = base_dir / "alpha"
            alpha_dir.mkdir()
            (alpha_dir / "plugin.py").write_text('PLUGIN_NAME = "alpha"\n', encoding="utf-8")

            with self.assertRaisesRegex(ValueError, "缺少 PLUGIN_TOOLS"):
                load_plugins(("alpha",), base_dir=base_dir)

    def test_load_plugins_requires_plugin_tools_as_sequence(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            base_dir = Path(tmp_dir)
            alpha_dir = base_dir / "alpha"
            alpha_dir.mkdir()
            (alpha_dir / "plugin.py").write_text(
                'PLUGIN_NAME = "alpha"\n'
                'PLUGIN_TOOLS = "invalid"\n',
                encoding="utf-8",
            )

            with self.assertRaisesRegex(TypeError, "PLUGIN_TOOLS 类型非法"):
                load_plugins(("alpha",), base_dir=base_dir)

    def test_load_plugins_requires_name_match(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            base_dir = Path(tmp_dir)
            alpha_dir = base_dir / "alpha"
            alpha_dir.mkdir()
            (alpha_dir / "plugin.py").write_text(
                "# intentionally missing PLUGIN_NAME\n"
                "PLUGIN_TOOLS = []\n",
                encoding="utf-8",
            )

            with self.assertRaisesRegex(ValueError, "插件名不匹配"):
                load_plugins(("alpha",), base_dir=base_dir)


if __name__ == "__main__":
    unittest.main()
