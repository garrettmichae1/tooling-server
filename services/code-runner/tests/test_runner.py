import importlib.util
from pathlib import Path
import tempfile
import time
import unittest

spec = importlib.util.spec_from_file_location("runner", Path(__file__).parents[1] / "runtime/runner.py")
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)

class RunnerTests(unittest.TestCase):
    def payload(self, **extra):
        return dict(language="python", entrypoint="main.py",files=[dict(path="main.py",content='print("hello")')], stdin="",mode="run",**extra)
    def test_no_traversal_or_reserved_paths(self):
        for path in ["../main.py","/main.py","a/../main.py",".ssh/key","node_modules/a.py","a\\b.py","a//b.py"]:
            self.assertFalse(runner.safe_path(path), path)
    def test_shape_size_and_path_collisions(self):
        for value in [dict(self.payload(),command="sh"),dict(self.payload(),files=[dict(path="main.py",content="x"*262145)]),dict(self.payload(),files=[dict(path="main.py",content=""),dict(path="MAIN.py",content="")]),dict(self.payload(),files=[dict(path="main.py",content=""),dict(path="main.py/a",content="")])]:
            with self.assertRaises(ValueError): runner.validate(value)
    def test_no_inherited_credentials_or_proxy(self):
        env=runner.environment(Path("/tmp/project"))
        self.assertFalse(any("secret" in k.lower() or k.lower() in ("http_proxy", "https_proxy", "all_proxy") for k in env))
        self.assertEqual(env["GOPROXY"], "off")
    def test_deadline_and_output_bounded(self):
        with tempfile.TemporaryDirectory() as temp:
            # Tests run unprivileged; Docker smoke verifies UID demotion separately.
            root=Path(temp)
            result=runner.command(["/usr/bin/python3","-c","import time; time.sleep(3)"],root,{"PATH":"/usr/bin:/bin"},time.monotonic()+0.2)
            self.assertEqual(result["status"],"timeout")
            result=runner.command(["/usr/bin/python3","-c",'print("x"*200000)'],root,{"PATH":"/usr/bin:/bin"},time.monotonic()+3)
            self.assertTrue(result["truncated"])
            self.assertLessEqual(len((result["stdout"]+result["stderr"]).encode()),65536)
    def test_html_only_inlines_submitted_assets(self):
        parser=runner.OfflineHTML({"app.js":"hello","style.css":"color:red"},"index.html")
        parser.feed('<link rel="stylesheet" href="style.css"><script src="app.js"></script><script src="https://evil.test/x"></script>')
        text="".join(parser.parts)
        self.assertIn("hello",text);self.assertIn("color:red",text);self.assertNotIn("evil.test",text)

if __name__ == "__main__": unittest.main()
