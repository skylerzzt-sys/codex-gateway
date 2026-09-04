import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import yaml


SCRIPT = Path(__file__).with_name("merge-teamorouter-config.py")


class MergeTeamoRouterConfigTest(unittest.TestCase):
    def test_preserves_existing_config_and_is_idempotent(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / "config.yaml"
            config.write_text(
                "host: 127.0.0.1\napi-keys:\n  - gateway-secret\nopenai-compatibility:\n  - name: existing\n    base-url: https://example.test/v1\n",
                encoding="utf-8",
            )
            env = {**os.environ, "TEAMOROUTER_API_KEY": "sk-teamo-test-secret"}
            for _ in range(2):
                subprocess.run(["python", str(SCRIPT), str(config)], env=env, check=True, capture_output=True, text=True)
            result = yaml.safe_load(config.read_text(encoding="utf-8"))
            self.assertEqual(result["api-keys"], ["gateway-secret"])
            self.assertEqual([item["name"] for item in result["openai-compatibility"]], ["existing", "teamorouter"])
            teamo = result["openai-compatibility"][1]
            self.assertEqual(teamo["prefix"], "teamo")
            self.assertEqual(teamo["base-url"], "https://api.teamorouter.cn/v1")
            self.assertEqual(teamo["api-key-entries"], [{"api-key": "sk-teamo-test-secret"}])
            self.assertEqual([model["alias"] for model in teamo["models"]], ["gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"])
            self.assertGreaterEqual(len(list(Path(directory).glob("config.yaml.before-teamorouter-*"))), 1)

    def test_rejects_missing_key_without_modifying_config(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / "config.yaml"
            original = "host: 127.0.0.1\n"
            config.write_text(original, encoding="utf-8")
            env = dict(os.environ)
            env.pop("TEAMOROUTER_API_KEY", None)
            result = subprocess.run(["python", str(SCRIPT), str(config)], env=env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(config.read_text(encoding="utf-8"), original)


if __name__ == "__main__":
    unittest.main()
