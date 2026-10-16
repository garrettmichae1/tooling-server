import importlib.util
from pathlib import Path
import tempfile
import time
import unittest
import shutil
import subprocess

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
    def test_java_package_lexer_skips_non_code(self):
        cases = [
            ("/*\npackage wrong.name;\n*/\npublic class Main {}", ""),
            ("// package wrong.name;\npublic class Main {}", ""),
            ('public class Main { String s="package wrong.name;"; }', ""),
            ('public class Main { String s="""\npackage wrong.name;\n"""; }', ""),
            ("/* package wrong.name; */ package real /* gap */ . app ; class Main {}", "real.app"),
            ("// skipped\rpackage real.app; class Main {}", "real.app"),
            ("@Anno(value=\"package wrong;\", kind=String.class) package real.app;", "real.app"),
            ("package $app._demo; class Main {}", "$app._demo"),
            ("package café.例; class Main {}", "café.例"),
            (r"\u0070ackage real.\u0061pp; class Main {}", "real.app"),
            (r"// ignored\u000apackage real.app; class Main {}", "real.app"),
            (r"\u002f\u002a package wrong; \u002a\u002f package real.app;", "real.app"),
            (r"package ac\u200ctual;", "actual"),
            (r"package ac\u0000tual;", "actual"),
            (r"package \ud801\udc00;", "𐐀"),
            ('@Anno("\\\\u0022 package wrong;") package real.app;', "real.app"),
            ("package broken..name;", ""),
            ("package missing;separator;", "missing"),
            ("package missing", ""),
        ]
        for source, expected in cases:
            with self.subTest(source=source):
                self.assertEqual(runner.java_package(source), expected)
    def test_java_launch_plan_uses_actual_package(self):
        for source, name in [
            ("/*\npackage wrong;\n*/\npublic class Main {}", "Main"),
            ("/* package wrong; */ package real.app; public class Main {}", "real.app.Main"),
            ("package real/* comment */.app; public class Main {}", "real.app.Main"),
        ]:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                (root / "Main.java").write_text(source)
                value = dict(language="java", entrypoint="Main.java", files=[dict(path="Main.java", content=source)])
                plan = runner.plans(value, root)
                self.assertEqual(plan[0], ["javac", "-d", ".build", "Main.java"])
                self.assertEqual(plan[1], ["java", "-cp", ".build", name])
    def test_java_launch_plan_compiles_and_runs(self):
        java = shutil.which("java")
        javac = shutil.which("javac")
        if not java:
            self.skipTest("Java runtime unavailable; actual javac/launcher qualification not executed")
        compiler = [javac] if javac else [java, "-m", "jdk.compiler/com.sun.tools.javac.Main"]
        if not javac:
            modules = subprocess.run([java, "--list-modules"], capture_output=True, text=True, timeout=10)
            if "jdk.compiler@" not in modules.stdout:
                self.skipTest("Java compiler unavailable; actual javac/launcher qualification not executed")
        for prefix in [
            "/*\npackage wrong.name;\n*/\n",
            "// package wrong.name;\n",
            "/* package wrong; */ package actual.app;\n",
            "package actual /* comment */ . app;\n",
            r"package ac\u200ctual;" + "\n",
            r"package ac\u0000tual;" + "\n",
            r"package \ud801\udc00;" + "\n",
        ]:
            with self.subTest(prefix=prefix), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                (root / ".build").mkdir()
                source = prefix + 'public class Main { public static void main(String[] args) { System.out.println("fixture"); } }'
                (root / "Main.java").write_text(source)
                plan = runner.plans(dict(language="java", entrypoint="Main.java", files=[dict(path="Main.java", content=source)]), root)
                compiled = subprocess.run(compiler + plan[0][1:], cwd=root, capture_output=True, text=True, timeout=20)
                self.assertEqual(compiled.returncode, 0, compiled.stderr)
                launched = subprocess.run([java] + plan[1][1:], cwd=root, capture_output=True, text=True, timeout=10)
                self.assertEqual(launched.returncode, 0, launched.stderr)
                self.assertEqual(launched.stdout, "fixture\n")

if __name__ == "__main__": unittest.main()
