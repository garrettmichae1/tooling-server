"""Local subprocess regressions for the shared console UTF-8 byte budget.

These use the production command/execute paths with harmless Python and Bash
programs. They do not stand in for the disposable microVM isolation tests.
"""
import importlib.util
from pathlib import Path
import tempfile
import time
import unittest
from unittest import mock


spec = importlib.util.spec_from_file_location("output_runner", Path(__file__).parents[1] / "runtime/runner.py")
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)
PYTHON = "/usr/bin/python3"


class OutputBudgetTests(unittest.TestCase):
    def emit(self, stdout=b"", stderr=b"", maximum=runner.MAX_OUTPUT):
        source = "import os\nos.write(1, " + repr(stdout) + ")\nos.write(2, " + repr(stderr) + ")\n"
        with tempfile.TemporaryDirectory(prefix="edsger-output-test-") as directory:
            root = Path(directory)
            root.chmod(0o755)
            fixture = root / "emit.py"
            fixture.write_text(source)
            fixture.chmod(0o644)
            return runner.command([PYTHON, "emit.py"], root, {"PATH": "/usr/bin:/bin"}, time.monotonic() + 5, maximum=maximum)

    def assert_output(self, result, stdout, stderr, *, truncated=False, status="completed", maximum=runner.MAX_OUTPUT):
        self.assertEqual(result["stdout"], stdout)
        self.assertEqual(result["stderr"], stderr)
        self.assertEqual(result["truncated"], truncated)
        self.assertEqual(result["status"], status)
        self.assertLessEqual(len((result["stdout"] + result["stderr"]).encode()), maximum)
        # The serialized protocol must not contain split UTF-8 or surrogates.
        (result["stdout"] + result["stderr"]).encode().decode("utf-8", "strict")

    def payload(self, source, language="python"):
        entrypoint = "main.py" if language == "python" else "main.sh"
        return dict(language=language, entrypoint=entrypoint, files=[dict(path=entrypoint, content=source)], stdin="", mode="run")

    def staged_execute(self, sources):
        # Replace only the fixed compiler plan with local argv fixtures. All
        # staging, child limits, capture, aggregation, and cleanup remain real.
        plans = [[PYTHON, "-c", source] for source in sources]
        with mock.patch.object(runner, "plans", return_value=plans), mock.patch.object(runner, "command", wraps=runner.command) as calls:
            result = runner.execute(self.payload("# harmless fixture\n"), deadline_seconds=5)
        return result, calls

    def preview_execute(self, build_source):
        actual_command = runner.command

        def local_build(argv, cwd, env, deadline, stdin, maximum):
            self.assertEqual(argv[0], "/opt/node-libraries/node_modules/.bin/esbuild")
            return actual_command([PYTHON, "-c", build_source], cwd, env, deadline, stdin, maximum=maximum)

        value = dict(language="javascript", entrypoint="main.js", files=[dict(path="main.js", content="console.log(1);\n")], stdin="", mode="preview")
        with mock.patch.object(runner, "command", side_effect=local_build):
            return runner.execute(value, deadline_seconds=5)

    def test_one_sided_ascii_keeps_the_entire_shared_budget(self):
        for name in ("stdout", "stderr"):
            for size in (33000, 50000, runner.MAX_OUTPUT):
                with self.subTest(stream=name, size=size):
                    outputs = {"stdout": b"", "stderr": b""}
                    outputs[name] = b"x" * size
                    self.assert_output(self.emit(**outputs), outputs["stdout"].decode(), outputs["stderr"].decode())

    def test_asymmetric_streams_preserve_exact_text_at_the_combined_limit(self):
        for stdout_size, stderr_size in ((50000, 15536), (15536, 50000), (65535, 1), (1, 65535)):
            with self.subTest(stdout=stdout_size, stderr=stderr_size):
                self.assert_output(self.emit(b"o" * stdout_size, b"e" * stderr_size), "o" * stdout_size, "e" * stderr_size)

    def test_multibyte_text_is_preserved_across_pipe_read_boundaries(self):
        stdout = "🙂é漢x" * 5000
        stderr = "λ diagnostics\n" * 500
        self.assert_output(self.emit(stdout.encode(), stderr.encode()), stdout, stderr)

    def test_one_sided_multibyte_text_at_the_exact_limit(self):
        for name in ("stdout", "stderr"):
            with self.subTest(stream=name):
                outputs = {"stdout": b"", "stderr": b""}
                outputs[name] = ("🙂" * (runner.MAX_OUTPUT // 4)).encode()
                self.assert_output(self.emit(**outputs), outputs["stdout"].decode(), outputs["stderr"].decode())

    def test_raw_overflow_is_visible_on_either_stream(self):
        for name in ("stdout", "stderr"):
            with self.subTest(stream=name):
                outputs = {"stdout": b"", "stderr": b""}
                outputs[name] = b"x" * (runner.MAX_OUTPUT + 10000)
                expected = {"stdout": "", "stderr": ""}
                expected[name] = "x" * runner.MAX_OUTPUT
                self.assert_output(self.emit(**outputs), **expected, truncated=True, status="failed")

    def test_both_streams_share_the_limit_when_combined_output_overflows(self):
        result = self.emit(b"o" * 40000, b"e" * 40000)
        self.assertEqual(len((result["stdout"] + result["stderr"]).encode()), runner.MAX_OUTPUT)
        self.assertTrue(set(result["stdout"]) <= {"o"})
        self.assertTrue(set(result["stderr"]) <= {"e"})
        self.assertTrue(result["truncated"])
        self.assertEqual(result["status"], "failed")

    def test_small_and_zero_remaining_budgets_are_enforced(self):
        for maximum in (0, 1, 7, 33):
            with self.subTest(maximum=maximum):
                self.assert_output(self.emit(b"x" * (maximum + 1), maximum=maximum), "x" * maximum, "", truncated=True, status="failed", maximum=maximum)
        self.assert_output(self.emit(maximum=0), "", "", maximum=0)

    def test_truncated_multibyte_character_is_not_emitted_partially(self):
        self.assert_output(self.emit("éééé".encode(), maximum=7), "ééé", "", truncated=True, status="failed", maximum=7)

    def test_invalid_utf8_replacement_fits_without_a_false_truncation(self):
        for name in ("stdout", "stderr"):
            with self.subTest(stream=name):
                outputs = {"stdout": b"", "stderr": b""}
                outputs[name] = b"\xff" * 21845
                expected = {"stdout": "", "stderr": ""}
                expected[name] = "�" * 21845
                self.assert_output(self.emit(**outputs), **expected)

    def test_replacement_expansion_sets_truncation_and_stays_within_limit(self):
        for name in ("stdout", "stderr"):
            with self.subTest(stream=name):
                outputs = {"stdout": b"", "stderr": b""}
                outputs[name] = b"\xff" * 21846
                expected = {"stdout": "", "stderr": ""}
                expected[name] = "�" * 21845
                self.assert_output(self.emit(**outputs), **expected, truncated=True, status="failed")

    def test_replacement_expansion_still_leaves_only_shared_room_for_stderr(self):
        self.assert_output(self.emit(b"\xff" * 24000, b"TAIL"), "�" * 21845, "T", truncated=True, status="failed")

    def test_invalid_and_valid_sequences_keep_their_decoded_text_when_it_fits(self):
        raw = b"start:" + "é🙂".encode() + b"\xff\xc0\xaf" + b":end"
        self.assert_output(self.emit(raw, b"error\xe2\x82"), raw.decode("utf-8", "replace"), "error�")

    def test_execute_python_preserves_one_sided_console_output(self):
        source = "import os\nos.write(1, b'x' * 50000)\n"
        result = runner.execute(self.payload(source), deadline_seconds=5)
        self.assert_output(result, "x" * 50000, "")

    def test_execute_bash_preserves_large_stderr(self):
        result = runner.execute(self.payload("printf '%50000s' '' >&2\n", language="bash"), deadline_seconds=5)
        self.assert_output(result, "", " " * 50000)

    def test_execute_reports_utf8_replacement_expansion(self):
        source = "import os\nos.write(1, b'\\xff' * 24000)\n"
        self.assert_output(runner.execute(self.payload(source), deadline_seconds=5), "�" * 21845, "", truncated=True, status="failed")

    def test_compiler_and_program_share_one_combined_budget(self):
        result, calls = self.staged_execute([
            "import os; os.write(1, b'c' * 30000); os.write(2, b'w' * 5000)",
            "import os; os.write(1, b'p' * 30536)",
            "# a final silent stage is still allowed with no output room",
        ])
        self.assert_output(result, "c" * 30000 + "p" * 30536, "w" * 5000)
        self.assertEqual([call.kwargs["maximum"] for call in calls.call_args_list], [65536, 30536, 0])

    def test_program_overflow_after_compiler_diagnostics_is_flagged_and_stops(self):
        result, calls = self.staged_execute([
            "import os; os.write(2, b'w' * 50000)",
            "import os; os.write(1, b'p' * 20000)",
            "print('must not execute')",
        ])
        self.assert_output(result, "p" * 15536, "w" * 50000, truncated=True, status="failed")
        self.assertEqual(calls.call_count, 2)

    def test_stage_boundary_drops_a_partial_codepoint_with_visible_truncation(self):
        result, calls = self.staged_execute([
            "import os; os.write(1, b'c' * 65535)",
            "import os; os.write(2, 'é'.encode())",
            "print('must not execute')",
        ])
        self.assert_output(result, "c" * 65535, "", truncated=True, status="failed")
        self.assertEqual(calls.call_count, 2)

    def test_failed_compiler_preserves_diagnostics_and_does_not_run_program(self):
        result, calls = self.staged_execute([
            "import os, sys; os.write(2, b'compile error\\n' * 3000); sys.exit(9)",
            "print('must not execute')",
        ])
        self.assert_output(result, "", "compile error\n" * 3000, status="failed")
        self.assertEqual(result["exitCode"], 9)
        self.assertEqual(calls.call_count, 1)

    def test_stage_replacement_expansion_uses_the_remaining_budget(self):
        result, calls = self.staged_execute([
            "import os; os.write(1, b'c' * 60000)",
            "import os; os.write(2, b'\\xff' * 2000)",
            "print('must not execute')",
        ])
        self.assert_output(result, "c" * 60000, "�" * 1845, truncated=True, status="failed")
        self.assertEqual(calls.call_count, 2)

    def test_oversized_preview_keeps_compiler_output_and_adds_diagnostic(self):
        result = self.preview_execute("from pathlib import Path; import os; Path('.build/preview.js').write_bytes(b'x' * 262144); os.write(1, b'c' * 40000); os.write(2, b'w' * 20000)")
        self.assert_output(result, "c" * 40000, "w" * 20000 + "\nPreview exceeds the size limit. Use a smaller bundle.", status="failed")
        self.assertNotIn("previewHTML", result)

    def test_preview_rejection_at_full_console_budget_marks_dropped_diagnostic(self):
        result = self.preview_execute("from pathlib import Path; import os; Path('.build/preview.js').write_bytes(b'x' * 262144); os.write(1, b'c' * 65536)")
        self.assert_output(result, "c" * 65536, "", truncated=True, status="failed")
        self.assertNotIn("previewHTML", result)

    def test_preview_diagnostic_uses_remaining_shared_room_without_losing_logs(self):
        result = self.preview_execute("from pathlib import Path; import os; Path('.build/preview.js').write_bytes(b'x' * 262144); os.write(1, b'c' * 35000); os.write(2, b'w' * 30530)")
        self.assert_output(result, "c" * 35000, "w" * 30530 + "\nPrevi", truncated=True, status="failed")

    def test_final_html_expansion_rejection_preserves_actual_console(self):
        result = self.preview_execute("from pathlib import Path; import os; Path('.build/preview.js').write_bytes(b'</script>' * 29000); os.write(1, b'c' * 40000); os.write(2, b'w' * 10000)")
        self.assert_output(result, "c" * 40000, "w" * 10000 + "\nPreview exceeds the size limit.", status="failed")
        self.assertNotIn("previewHTML", result)

    def test_oversized_html_is_resource_rejection_without_console_truncation(self):
        value = dict(language="html", entrypoint="index.html", files=[dict(path="index.html", content='<link rel="stylesheet" href="style.css">' * 3), dict(path="style.css", content="x" * 100000)], stdin="", mode="preview")
        result = runner.execute(value, deadline_seconds=5)
        self.assert_output(result, "", "Preview exceeds the size limit.", status="failed")
        self.assertNotIn("previewHTML", result)

    def test_valid_preview_preserves_console_and_document(self):
        result = self.preview_execute("from pathlib import Path; import os; Path('.build/preview.js').write_text('console.log(1);'); os.write(1, b'compiler output'); os.write(2, b'warning\\n')")
        self.assert_output(result, "compiler output", "warning\n")
        self.assertIn("console.log(1);", result["previewHTML"])


if __name__ == "__main__":
    unittest.main()
