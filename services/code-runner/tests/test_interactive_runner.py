"""Real local process/HTTP tests for ephemeral interactive console execution.

These exercise the production controller and its child launcher. Disposable
microVM networking/UID isolation remains the responsibility of image smoke
tests; no provider, Docker daemon, or hosted runner is required here.
"""
import concurrent.futures
import contextlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock


spec = importlib.util.spec_from_file_location("interactive_runner", Path(__file__).parents[1] / "runtime/runner.py")
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


def payload(source, stdin="", language="python", entrypoint="main.py"):
    return dict(language=language, entrypoint=entrypoint, files=[dict(path=entrypoint, content=source)], stdin=stdin, mode="run")


class RunningConsole:
    def __init__(self, value, deadline=3):
        self.channel = runner.InteractiveInput(value["stdin"])
        self.frames = []
        self.condition = threading.Condition()
        self.result = None
        self.error = None

        def emit(frame):
            with self.condition:
                self.frames.append(frame)
                self.condition.notify_all()

        def execute():
            try:
                self.result = runner.execute_interactive(value, emit, self.channel, deadline_seconds=deadline)
            except BaseException as error:
                self.error = error
            finally:
                with self.condition:
                    self.condition.notify_all()

        self.thread = threading.Thread(target=execute, daemon=True)
        self.thread.start()

    def wait(self, predicate, timeout=3):
        until = time.monotonic() + timeout
        with self.condition:
            while not predicate(self.frames):
                if self.error:
                    raise self.error
                remaining = until - time.monotonic()
                if remaining <= 0:
                    raise AssertionError("Expected console event was not emitted")
                self.condition.wait(timeout=remaining)
        return self.frames

    def ready(self):
        self.wait(lambda frames: any(frame["type"] == "ready" for frame in frames))

    def output(self, text, timeout=3):
        self.wait(lambda frames: text in "".join(frame.get("text", "") for frame in frames), timeout=timeout)

    def send(self, sequence, text="", eof=False):
        return self.channel.submit(dict(sequence=sequence, text=text, eof=eof))

    def finish(self, timeout=4):
        self.thread.join(timeout=timeout)
        if self.thread.is_alive():
            self.channel.cancelled.set()
            self.thread.join(timeout=2)
            raise AssertionError("Console did not terminate within its deadline")
        if self.error:
            raise self.error
        return self.result


class InteractiveRunnerTests(unittest.TestCase):
    def assert_error(self, code, function, *args):
        with self.assertRaises(runner.InputError) as caught:
            function(*args)
        self.assertEqual(caught.exception.code, code)
        return caught.exception

    def assert_transcript(self, console):
        result = console.finish()
        self.assertLessEqual(len((result["stdout"] + result["stderr"]).encode()), runner.MAX_OUTPUT)
        self.assertEqual([frame for frame in console.frames if frame["type"] == "result"], [dict(type="result", execution=result)])
        self.assertLessEqual(sum(frame["type"] != "result" for frame in console.frames), runner.MAX_STREAM_FRAMES)
        self.assertLessEqual(sum(len((json.dumps(frame, ensure_ascii=False, separators=(",", ":")) + "\n").encode()) for frame in console.frames), runner.MAX_STREAM_WIRE)
        for name in ("stdout", "stderr"):
            streamed = "".join(frame["text"] for frame in console.frames if frame["type"] == "output" and frame["stream"] == name)
            self.assertEqual(streamed, result[name])
        return result

    def test_prompt_without_newline_arrives_before_user_types(self):
        console = RunningConsole(payload('name = input("Your name? ")\nprint("Hello, " + name)\n'))
        console.ready()
        console.output("Your name? ")
        self.assertIsNone(console.result)
        self.assertEqual(console.send(1, "Zoë 漢🙂\n"), dict(accepted=True, sequence=1))
        result = self.assert_transcript(console)
        self.assertEqual(result["stdout"], "Your name? Zoë 漢🙂\nHello, Zoë 漢🙂\n")
        self.assertEqual(result["status"], "completed")

    def test_multiline_unicode_inputs_are_written_once_after_exact_retry(self):
        console = RunningConsole(payload('import sys\nprint("LINES", flush=True)\ndata = sys.stdin.read()\nprint(repr(data))\n'))
        console.output("LINES")
        self.assertEqual(console.send(1, "α\n漢🙂\n"), console.send(1, "α\n漢🙂\n"))
        self.assert_error("changed_duplicate", console.send, 1, "changed\n")
        self.assert_error("out_of_order", console.send, 3, "future\n")
        self.assertEqual(console.send(2, eof=True), dict(accepted=True, sequence=2))
        self.assertEqual(console.send(2, eof=True), dict(accepted=True, sequence=2))
        result = self.assert_transcript(console)
        self.assertEqual(result["stdout"], "LINES\nα\n漢🙂\n'α\\n漢🙂\\n'\n")
        self.assert_error("input_closed", console.send, 3, "late\n")

    def test_empty_input_does_not_mean_eof_and_explicit_eof_is_real(self):
        console = RunningConsole(payload('import sys\nprint("waiting", flush=True)\nprint("EOF=" + repr(sys.stdin.read()))\n'))
        console.output("waiting")
        self.assertEqual(console.send(1, ""), dict(accepted=True, sequence=1))
        self.assertIsNone(console.result)
        console.send(2, eof=True)
        self.assertEqual(self.assert_transcript(console)["stdout"], "waiting\nEOF=''\n")

    def test_prefilled_input_gets_final_newline_and_remains_open_for_more(self):
        console = RunningConsole(payload('a = input("first: ")\nb = input("second: ")\nprint(a + ":" + b)\n', stdin="one"))
        console.output("second: ")
        console.send(1, "two\n")
        result = self.assert_transcript(console)
        self.assertIn("one\n", result["stdout"])
        self.assertIn("second: two\n", result["stdout"])
        self.assertTrue(result["stdout"].endswith("one:two\n"))

    def test_compiler_gets_eof_program_gets_live_input_and_compiler_logs_stay(self):
        compile_stage = ["/usr/bin/python3", "-c", "import sys; assert sys.stdin.read() == ''; print('built'); print('warning', file=sys.stderr)"]
        run_stage = ["/usr/bin/python3", "-c", "print('answer=' + input('prompt: '))"]
        with mock.patch.object(runner, "plans", return_value=[compile_stage, run_stage]):
            console = RunningConsole(payload("# fixture"))
            console.output("prompt: ")
            console.send(1, "42\n")
            result = self.assert_transcript(console)
        self.assertEqual(result["stdout"], "built\nprompt: 42\nanswer=42\n")
        self.assertEqual(result["stderr"], "warning\n")
        ready_index = next(index for index, frame in enumerate(console.frames) if frame["type"] == "ready")
        self.assertTrue(any(frame.get("text") == "built\n" for frame in console.frames[:ready_index]))

    @unittest.skipUnless(shutil.which("gcc") and Path("/usr/bin/stdbuf").is_file(), "Local C compiler/stdbuf unavailable")
    def test_c_prompt_without_fflush_is_visible_before_scanf(self):
        source = '#include <stdio.h>\nint main(void) { int n; printf("number: "); if (scanf("%d", &n) != 1) return 2; printf("double=%d\\n", 2*n); return 0; }\n'
        console = RunningConsole(payload(source, language="c", entrypoint="main.c"))
        console.output("number: ")
        console.send(1, "21\n")
        self.assertEqual(self.assert_transcript(console)["stdout"], "number: 21\ndouble=42\n")

    @contextlib.contextmanager
    def java_console(self, source):
        java = shutil.which("java")
        if not java:
            self.skipTest("Local Java runtime unavailable")
        javac = shutil.which("javac")
        if not javac:
            modules = subprocess.run([java, "--list-modules"], capture_output=True, text=True, timeout=5, check=True).stdout
            if not any(line.startswith("jdk.compiler@") for line in modules.splitlines()):
                self.skipTest("Local Java compiler unavailable")
        actual_plans = runner.plans

        def local_java_plan(value, root):
            stages = actual_plans(value, root)
            if not javac:
                # Some local JDK installations include the real compiler module
                # without its javac executable. Only adapt that trusted launcher;
                # staging, javac arguments, Java execution, limits, and all console
                # transport remain the production code paths.
                stages[0] = [java, "-m", "jdk.compiler/com.sun.tools.javac.Main", *stages[0][1:]]
            return stages

        with mock.patch.object(runner, "plans", side_effect=local_java_plan):
            console = RunningConsole(payload(source, language="java", entrypoint="Main.java"), deadline=12)
            try:
                yield console
            finally:
                if console.thread.is_alive():
                    console.channel.cancelled.set()
                    console.thread.join(timeout=2)

    def test_java_scanner_accepts_sequential_numeric_and_unicode_string_responses(self):
        source = '''import java.nio.charset.StandardCharsets;
import java.util.Locale;
import java.util.Scanner;
public class Main {
  public static void main(String[] args) {
    try (Scanner scanner = new Scanner(System.in, StandardCharsets.UTF_8)) {
      scanner.useLocale(Locale.ROOT);
      System.out.print("Count? ");
      int count = scanner.nextInt(); scanner.nextLine();
      System.out.print("Name? ");
      String name = scanner.nextLine();
      System.out.print("Scale? ");
      double scale = scanner.nextDouble(); scanner.nextLine();
      System.out.print("Comment? ");
      String comment = scanner.nextLine();
      System.out.println("Result: " + count + " | " + name + " | " + scale + " | " + comment);
    }
  }
}
'''
        with self.java_console(source) as console:
            console.output("Count? ", timeout=8)
            self.assertEqual(console.channel.total, 0)
            self.assertIsNone(console.result)
            self.assertEqual(console.send(1, "7\n"), dict(accepted=True, sequence=1))
            console.output("Name? ")
            console.send(2, "Zoë 漢🙂\n")
            console.output("Scale? ")
            console.send(3, "2.5\n")
            console.output("Comment? ")
            console.send(4, "ready to learn\n")
            result = self.assert_transcript(console)
        self.assertEqual(result["status"], "completed")
        self.assertEqual(result["exitCode"], 0)
        self.assertEqual(result["stdout"], "Count? 7\nName? Zoë 漢🙂\nScale? 2.5\nComment? ready to learn\nResult: 7 | Zoë 漢🙂 | 2.5 | ready to learn\n")

    def test_java_scanner_receives_explicit_eof_with_or_without_a_response(self):
        source = '''import java.nio.charset.StandardCharsets;
import java.util.Scanner;
public class Main {
  public static void main(String[] args) {
    try (Scanner scanner = new Scanner(System.in, StandardCharsets.UTF_8)) {
      while (true) {
        System.out.print("Line? ");
        if (!scanner.hasNextLine()) { System.out.println("EOF"); break; }
        System.out.println("Read: " + scanner.nextLine());
      }
    }
  }
}
'''
        for response in (None, "Héllo 漢🙂"):
            with self.subTest(response=response), self.java_console(source) as console:
                console.output("Line? ", timeout=8)
                self.assertIsNone(console.result)
                sequence = 1
                expected = "Line? "
                if response is not None:
                    console.send(sequence, response + "\n")
                    sequence += 1
                    console.output("Read: " + response + "\nLine? ")
                    expected += response + "\nRead: " + response + "\nLine? "
                self.assertEqual(console.send(sequence, eof=True), dict(accepted=True, sequence=sequence))
                result = self.assert_transcript(console)
                self.assertEqual(result["status"], "completed")
                self.assertEqual(result["stdout"], expected + "EOF\n")

    def test_java_system_in_reads_queued_unicode_bytes_until_real_eof(self):
        source = '''import java.nio.charset.StandardCharsets;
public class Main {
  public static void main(String[] args) throws Exception {
    System.out.print("Text? ");
    byte[] data = System.in.readAllBytes();
    System.out.println("Bytes=" + data.length);
    System.out.print(new String(data, StandardCharsets.UTF_8));
  }
}
'''
        text = "漢🙂 café\n"
        with self.java_console(source) as console:
            console.output("Text? ", timeout=8)
            console.send(1, "漢🙂")
            console.send(2, " café\n")
            self.assertIsNone(console.result)
            console.send(3, eof=True)
            result = self.assert_transcript(console)
        self.assertEqual(result["status"], "completed")
        self.assertEqual(result["stdout"], "Text? " + text + "Bytes=" + str(len(text.encode())) + "\n" + text)

    def test_compile_failure_never_claims_ready_or_consumes_user_input(self):
        compiler = [["/usr/bin/python3", "-c", "import sys; print('compile error', file=sys.stderr); sys.exit(3)"], ["/usr/bin/python3", "-c", "print('must not run')"]]
        with mock.patch.object(runner, "plans", return_value=compiler):
            console = RunningConsole(payload("# fixture", stdin="private input"))
            result = self.assert_transcript(console)
        self.assertEqual(result["status"], "failed")
        self.assertEqual(result["exitCode"], 3)
        self.assertEqual(result["stdout"], "")
        self.assertEqual(result["stderr"], "compile error\n")
        self.assertNotIn("ready", [frame["type"] for frame in console.frames])

    def test_no_input_program_finishes_without_an_eof_request(self):
        console = RunningConsole(payload('print("hello")\n'))
        self.assertEqual(self.assert_transcript(console)["stdout"], "hello\n")
        self.assert_error("input_closed", console.send, 1, "late\n")

    def test_invalid_inputs_and_normalization_overflow_are_rejected(self):
        self.assert_error("limit", runner.InteractiveInput, "x" * 8192)
        channel = runner.InteractiveInput("")
        self.assert_error("not_ready", channel.submit, dict(sequence=1, text="x", eof=False))
        for value in (dict(sequence=True, text="", eof=False), dict(sequence=0, text="", eof=False), dict(sequence=129, text="", eof=False), dict(sequence=1, text="", eof=0), dict(sequence=1, text="x", eof=True), dict(sequence=1, text="\ud800", eof=False), dict(sequence=1, text="", eof=False, extra=True)):
            with self.subTest(value=repr(value)):
                self.assert_error("invalid_request", channel.submit, value)

    def test_total_utf8_input_budget_includes_prefill_and_echo(self):
        console = RunningConsole(payload('import sys\nprint("reading", flush=True)\nprint(len(sys.stdin.buffer.read()))\n', stdin="a" * 4095))
        console.output("reading")
        console.send(1, "🙂" * 1024)
        self.assert_error("limit", console.send, 2, "x")
        console.send(2, eof=True)
        result = self.assert_transcript(console)
        self.assertTrue(result["stdout"].endswith("8192\n"))
        self.assertEqual(console.channel.total, 8192)
        self.assertTrue(all(not operation["data"] for operation in console.channel.operations.values()))

    def test_concurrent_exact_duplicates_share_one_delivery(self):
        console = RunningConsole(payload('import sys\nprint("ready", flush=True)\nprint(repr(sys.stdin.read()))\n'))
        console.output("ready")
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
            acknowledgements = list(executor.map(lambda _: console.send(1, "once\n"), range(8)))
        self.assertEqual(acknowledgements, [dict(accepted=True, sequence=1)] * 8)
        console.send(2, eof=True)
        self.assertEqual(self.assert_transcript(console)["stdout"], "ready\nonce\n'once\\n'\n")

    def test_partial_pipe_writes_ack_only_after_complete_delivery_and_echo_once(self):
        console = RunningConsole(payload('import sys\nprint("ready", flush=True)\nprint(len(sys.stdin.buffer.read()))\n'))
        console.output("ready")
        actual_write = os.write
        written = []

        def partial_write(fd, data):
            count = actual_write(fd, data[:17])
            written.append(count)
            return count

        with mock.patch.object(runner.os, "write", side_effect=partial_write):
            acknowledgement = console.send(1, "漢" * 1000 + "\n")
        self.assertEqual(acknowledgement, dict(accepted=True, sequence=1))
        self.assertGreater(len(written), 1)
        self.assertEqual(sum(written), 3001)
        console.send(2, eof=True)
        self.assertEqual(self.assert_transcript(console)["stdout"], "ready\n" + "漢" * 1000 + "\n3001\n")

    def test_waiting_for_input_uses_the_same_deadline(self):
        console = RunningConsole(payload('input("waiting: ")\n'), deadline=0.35)
        console.output("waiting: ")
        result = self.assert_transcript(console)
        self.assertEqual(result["status"], "timeout")
        self.assertEqual(result["exitCode"], 124)
        self.assert_error("input_closed", console.send, 1, "late\n")

    def test_flood_is_bounded_and_combined_transcript_is_valid_utf8(self):
        console = RunningConsole(payload('import os\nos.write(1, b"\\xff" * 40000)\nos.write(2, b"e" * 40000)\n'))
        result = self.assert_transcript(console)
        self.assertEqual(result["status"], "failed")
        self.assertTrue(result["truncated"])
        (result["stdout"] + result["stderr"]).encode().decode("utf-8", "strict")

    def test_tiny_multibyte_writes_preserve_incremental_decoding(self):
        console = RunningConsole(payload('import os, time\nfor byte in "漢🙂".encode():\n os.write(1, bytes([byte])); time.sleep(.015)\nos.write(2, b"err")\n'))
        result = self.assert_transcript(console)
        self.assertEqual(result["stdout"], "漢🙂")
        self.assertEqual(result["stderr"], "err")

    def test_chatty_output_coalesces_tails_and_always_preserves_ready(self):
        emitted = []
        frames = runner.ConsoleFrames(emitted.append)
        expected = {"stdout": "", "stderr": ""}
        # More flush attempts than the real protocol frame cap, including
        # compiler progress before the program's reserved ready control.
        for index in range(1500):
            stream = "stdout" if index % 2 else "stderr"
            frames.add(stream, "漢")
            frames.flush(force=True)
            expected[stream] += "漢"
        frames.send(dict(type="ready"))
        for index in range(1500):
            stream = "stdout" if index % 2 else "stderr"
            frames.add(stream, "🙂")
            frames.flush(force=True)
            expected[stream] += "🙂"
        frames.flush(final=True)
        frames.send(dict(type="result", execution=expected), final=True)
        self.assertEqual(sum(frame["type"] == "ready" for frame in emitted), 1)
        self.assertLessEqual(len(emitted) - 1, runner.MAX_STREAM_FRAMES)
        for stream in expected:
            self.assertEqual("".join(frame["text"] for frame in emitted if frame["type"] == "output" and frame["stream"] == stream), expected[stream])

    def test_real_process_transcript_remains_exact_after_progress_cap(self):
        compiler = ["/usr/bin/python3", "-c", 'import os, time\nfor n in range(8):\n os.write(1, b"c"); os.write(2, b"w"); time.sleep(.06)']
        program = ["/usr/bin/python3", "-c", 'import sys\nprint("answer=" + input("prompt: "))']
        with mock.patch.object(runner, "MAX_STREAM_FRAMES", 6), mock.patch.object(runner, "plans", return_value=[compiler, program]):
            console = RunningConsole(payload("# fixture"))
            console.ready()
            console.send(1, "42\n")
            result = self.assert_transcript(console)
        # Input sent immediately on ready can beat the first guest write, just
        # as a user typing before a terminal prompt can. Both real orderings
        # are valid; progress/final bytes must still match exactly.
        self.assertIn(result["stdout"], ("c" * 8 + "prompt: 42\nanswer=42\n", "c" * 8 + "42\nprompt: answer=42\n"))
        self.assertEqual(result["stderr"], "w" * 8)

    def test_controller_environment_never_reaches_child(self):
        with mock.patch.dict(os.environ, {"X_EDSGER_SESSION_KEY": "controller-private", "CODE_RUNNER_INTERACTIVE_ENABLED": "true", "SECRET_TEST_TOKEN": "controller-secret"}):
            console = RunningConsole(payload('import json, os\nprint(json.dumps(sorted(os.environ)))'))
            result = self.assert_transcript(console)
        self.assertNotIn("SESSION", result["stdout"])
        self.assertNotIn("SECRET", result["stdout"])
        self.assertNotIn("INTERACTIVE", result["stdout"])

    def test_guest_home_cannot_inject_sitecustomize_into_privileged_launcher(self):
        # The first guest stage plants a Python user-site startup hook in HOME.
        # A non-isolated root launcher would execute it before dropping UID.
        compiler = [sys.executable, "-c", 'from pathlib import Path; import site; folder = Path(site.getusersitepackages()); folder.mkdir(parents=True); (folder / "sitecustomize.py").write_text("from pathlib import Path\\nPath(\\\".build/startup-injected\\\").write_text(\\\"unsafe\\\")\\n")']
        program = ["/bin/bash", "-c", 'if test -e .build/startup-injected; then printf unsafe; exit 9; fi; printf safe']
        with mock.patch.object(runner, "plans", return_value=[compiler, program]):
            console = RunningConsole(payload("# fixture"))
            result = self.assert_transcript(console)
        self.assertEqual(result["status"], "completed")
        self.assertEqual(result["stdout"], "safe")
        self.assertEqual(result["stderr"], "")

    def test_cancellation_kills_process_group_and_removes_private_files(self):
        actual_mkdtemp = tempfile.mkdtemp
        roots = []

        def private_root(*args, **kwargs):
            root = actual_mkdtemp(*args, **kwargs)
            roots.append(Path(root))
            return root

        with mock.patch.object(runner.tempfile, "mkdtemp", side_effect=private_root):
            console = RunningConsole(payload('import os, subprocess, time\np = subprocess.Popen(["/usr/bin/python3", "-c", "import time; time.sleep(20)"])\nprint(str(os.getpid()) + ":" + str(p.pid), flush=True)\ntime.sleep(20)\n'))
            console.wait(lambda frames: any(":" in frame.get("text", "") for frame in frames))
            ids = [int(value) for value in "".join(frame.get("text", "") for frame in console.frames).strip().split(":")]
            console.channel.cancelled.set()
            console.thread.join(timeout=2)
        self.assertFalse(console.thread.is_alive())
        self.assertIsInstance(console.error, ConnectionAbortedError)
        self.assertTrue(all(not root.exists() for root in roots))
        for pid in ids:
            status = Path(f"/proc/{pid}/stat")
            self.assertTrue(not status.exists() or status.read_text().split()[2] == "Z", f"process {pid} survived cancellation")

    def test_128_commands_are_bounded_and_last_operation_can_be_eof(self):
        console = RunningConsole(payload('import sys\nprint("ready", flush=True)\nprint(len(sys.stdin.read()))\n'))
        console.output("ready")
        for sequence in range(1, 128):
            console.send(sequence, "x")
        console.send(128, eof=True)
        result = self.assert_transcript(console)
        self.assertEqual(result["stdout"], "ready\n" + "x" * 127 + "127\n")
        self.assertEqual(len(console.channel.operations), 128)


@contextlib.contextmanager
def http_server(enabled=True):
    with mock.patch.dict(os.environ, {"CODE_RUNNER_INTERACTIVE_ENABLED": "true" if enabled else "false"}):
        server = runner.RunnerHTTPServer(("127.0.0.1", 0), runner.Handler)
        thread = threading.Thread(target=server.serve_forever, kwargs=dict(poll_interval=0.02), daemon=True)
        thread.start()
        try:
            yield server
        finally:
            with server.job_lock:
                if server.console:
                    server.console.cancelled.set()
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)


class InteractiveHTTPTests(unittest.TestCase):
    key = "01234567-89ab-cdef-0123-456789abcdef"

    def request(self, server, path, value, key=None):
        connection = http.client.HTTPConnection("127.0.0.1", server.server_address[1], timeout=4)
        connection.request("POST", path, json.dumps(value), {"Content-Type": "application/json", "X-Edsger-Session-Key": key or self.key})
        return connection, connection.getresponse()

    def input(self, server, sequence, text="", eof=False, key=None):
        connection, response = self.request(server, "/input", dict(sequence=sequence, text=text, eof=eof), key)
        result = response.status, json.load(response)
        connection.close()
        return result

    def test_real_http_can_send_input_while_stream_request_is_open(self):
        with http_server() as server:
            connection, response = self.request(server, "/execute-stream", payload('name = input("name: ")\nprint("hello=" + name)\n'))
            self.assertEqual(response.status, 200)
            self.assertEqual(response.getheader("Content-Type"), "application/x-ndjson")
            frames = [json.loads(response.readline())]
            self.assertEqual(frames[0], {"type": "ready"})
            frames.append(json.loads(response.readline()))
            self.assertEqual(frames[-1]["text"], "name: ")
            self.assertEqual(self.input(server, 1, "wrong\n", key="different-session-key-0123456789012345"), (400, dict(error="invalid_request")))
            self.assertEqual(self.input(server, 2, "future\n"), (409, dict(error="out_of_order")))
            self.assertEqual(self.input(server, 1, "Mina\n"), (200, dict(accepted=True, sequence=1)))
            frames.extend(json.loads(line) for line in response)
            connection.close()
            self.assertEqual(frames[-1]["type"], "result")
            self.assertEqual(frames[-1]["execution"]["stdout"], "name: Mina\nhello=Mina\n")
            self.assertTrue(server.used)
            self.assertIsNone(server.console)
            second, denied = self.request(server, "/execute-stream", payload('print("no replay")'))
            self.assertEqual(denied.status, 400)
            denied.read()
            second.close()

    def test_gate_is_default_closed_and_invalid_key_does_not_claim_job(self):
        with http_server(enabled=False) as server:
            connection, response = self.request(server, "/execute-stream", payload("print(1)"))
            self.assertEqual(response.status, 400)
            self.assertEqual(json.load(response), dict(error="invalid_request"))
            connection.close()
            self.assertFalse(server.used)
        with http_server() as server:
            connection, response = self.request(server, "/execute-stream", payload("print(1)"), key="short")
            self.assertEqual(response.status, 400)
            response.read()
            connection.close()
            self.assertFalse(server.used)

    def test_disconnected_client_terminates_waiting_program(self):
        with http_server() as server:
            connection, response = self.request(server, "/execute-stream", payload('input("wait: ")'))
            self.assertEqual(json.loads(response.readline()), dict(type="ready"))
            response.readline()
            response.close()
            connection.close()
            until = time.monotonic() + 2
            while server.console is not None and time.monotonic() < until:
                time.sleep(0.02)
            self.assertIsNone(server.console)


if __name__ == "__main__":
    unittest.main()
