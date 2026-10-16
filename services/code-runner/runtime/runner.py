"""Single-job console/build adapter inside a disposable Cloudflare microVM.

This is NOT the isolation boundary. Cloudflare's microVM, denied egress, hard
Worker deadline and destroy() are that boundary. No credentials enter this image.
The root controller never executes submitted source; child programs use UID 65532.
"""
import ctypes
import codecs
from collections import deque
import hashlib
import hmac
import html
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import re
import resource
import selectors
import select
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unicodedata
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LANGUAGES = ("c", "cpp", "python", "javascript", "typescript", "lua", "java", "csharp", "go", "rust", "ruby", "php", "kotlin", "swift", "r", "perl", "bash", "sql", "html")
EXTENSIONS = dict(zip(LANGUAGES, (("c",), ("cpp", "cc", "cxx"), ("py",), ("js", "mjs", "cjs", "jsx"), ("ts", "tsx"), ("lua",), ("java",), ("cs",), ("go",), ("rs",), ("rb",), ("php",), ("kt",), ("swift",), ("r", "R"), ("pl",), ("sh",), ("sql",), ("html", "htm"))))
MAX_REQUEST = 524288
MAX_SOURCE = 262144
MAX_OUTPUT = 65536
MAX_PREVIEW = 262144
DEADLINE_SECONDS = 30
MAX_INPUT = 8192
MAX_INPUT_SEQUENCES = 128
MAX_STREAM_FRAMES = 1024
MAX_STREAM_WIRE = 1048576

def safe_path(value):
    return isinstance(value, str) and len(value) <= 192 and all(re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}", p) and ".." not in p and p != "node_modules" for p in value.split("/"))

def validate(value):
    if not isinstance(value, dict) or set(value) != {"language", "entrypoint", "files", "stdin", "mode"}:
        raise ValueError("invalid_request")
    language = value["language"]
    if language not in LANGUAGES or not safe_path(value["entrypoint"]) or value["entrypoint"].split(".")[-1] not in EXTENSIONS[language]:
        raise ValueError("invalid_request")
    if value["mode"] not in ("run", "preview") or (value["mode"] == "preview" and language not in ("javascript", "typescript", "html")) or (language == "html" and value["mode"] != "preview"):
        raise ValueError("invalid_request")
    if not isinstance(value["stdin"], str) or len(value["stdin"].encode()) > 8192 or not isinstance(value["files"], list) or not 1 <= len(value["files"]) <= 64:
        raise ValueError("invalid_request")
    paths = set()
    size = 0
    for file in value["files"]:
        if not isinstance(file, dict) or set(file) != {"path", "content"} or not safe_path(file["path"]) or not isinstance(file["content"], str) or "\0" in file["content"] or file["path"].lower() in paths:
            raise ValueError("invalid_request")
        paths.add(file["path"].lower())
        size += len(file["content"].encode())
    if size > MAX_SOURCE or not any(f["path"] == value["entrypoint"] for f in value["files"]) or any(a.startswith(b + "/") for a in paths for b in paths if a != b):
        raise ValueError("invalid_request")
    return value

def child_limits(new_session=True):
    if new_session:
        os.setsid()
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_CPU, (30, 31))
    resource.setrlimit(resource.RLIMIT_FSIZE, (268435456, 268435456))
    resource.setrlimit(resource.RLIMIT_NOFILE, (1024, 1024))
    resource.setrlimit(resource.RLIMIT_NPROC, (64, 64))
    if os.getuid() == 0:
        os.setgroups([])
        os.setgid(65532)
        os.setuid(65532)
    # No exec may gain privileges; the image also strips setuid/setgid files.
    ctypes.CDLL(None).prctl(38, 1, 0, 0, 0)

def terminate(process):
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    process.wait(timeout=2)

def bounded_console(captured, maximum):
    """Decode complete stream captures within one combined UTF-8 byte budget."""
    output = {}
    remaining = maximum
    truncated = False
    for name, value in captured.items():
        data = bytes(value).decode("utf-8", "replace").encode()
        output[name] = data[:remaining].decode("utf-8", "ignore")
        if len(data) > remaining:
            truncated = True
        remaining -= len(output[name].encode())
    return output, truncated

def reject_preview(result, message):
    """Keep compiler output when adding a bounded preview failure diagnostic."""
    separator = "\n" if result["stderr"] and not result["stderr"].endswith("\n") else ""
    output, truncated = bounded_console({"stdout": result["stdout"].encode(), "stderr": (result["stderr"] + separator + message).encode()}, MAX_OUTPUT)
    result.update(output, status="failed", exitCode=1)
    result["truncated"] = result["truncated"] or truncated
    result.pop("previewHTML", None)

def command(argv, cwd, env, deadline, stdin="", maximum=MAX_OUTPUT):
    """Argv-only invocation, bounded output while reading, and whole process-group kill."""
    process = launch_child(argv, cwd, env)
    captured = {"stdout": bytearray(), "stderr": bytearray()}
    selector = selectors.DefaultSelector()
    for stream, name in ((process.stdout, "stdout"), (process.stderr, "stderr")):
        os.set_blocking(stream.fileno(), False)
        selector.register(stream, selectors.EVENT_READ, name)
    pending = memoryview(stdin.encode())
    if pending:
        os.set_blocking(process.stdin.fileno(), False)
        selector.register(process.stdin, selectors.EVENT_WRITE, "stdin")
    else:
        process.stdin.close()
    status = "completed"
    truncated = False
    try:
        while selector.get_map():
            if time.monotonic() >= deadline:
                status = "timeout"
                break
            for key, _ in selector.select(timeout=min(0.1, max(0, deadline - time.monotonic()))):
                if key.data == "stdin":
                    try:
                        count = os.write(key.fd, pending)
                        pending = pending[count:]
                    except BrokenPipeError:
                        pending = memoryview(b"")
                    if not pending:
                        selector.unregister(key.fileobj)
                        key.fileobj.close()
                    continue
                chunk = os.read(key.fd, 4096)
                if not chunk:
                    selector.unregister(key.fileobj)
                    key.fileobj.close()
                    continue
                room = maximum - sum(map(len, captured.values()))
                captured[key.data].extend(chunk[:max(room, 0)])
                if len(chunk) > room:
                    truncated = True
                    status = "failed"
                    break
            if truncated:
                break
        if status == "completed":
            try:
                process.wait(timeout=max(0.01, deadline - time.monotonic()))
                if process.returncode:
                    status = "failed"
            except subprocess.TimeoutExpired:
                status = "timeout"
    finally:
        # Also remove background descendants after a normal parent exit.
        terminate(process)
        selector.close()
        for stream in (process.stdin, process.stdout, process.stderr):
            if not stream.closed:
                stream.close()
    # Replacement characters can expand the captured bytes. Both streams share
    # the protocol's UTF-8 budget; a quiet stream leaves its room to the other.
    output, decoded_truncated = bounded_console(captured, maximum)
    truncated = truncated or decoded_truncated
    if truncated and status == "completed":
        status = "failed"
    code = 124 if status == "timeout" else max(-255, min(255, process.returncode or 0))
    return dict(status=status, exitCode=code, truncated=truncated, **output)

def environment(root):
    # Never inherit the controller/process environment, proxies or credentials.
    return {"PATH": "/opt/python/bin:/opt/node/bin:/opt/go/bin:/opt/rust/bin:/opt/dotnet:/usr/local/swift/usr/bin:/usr/local/bin:/usr/bin:/bin", "HOME": str(root), "TMPDIR": str(root / ".build"), "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "PYTHONUNBUFFERED": "1", "PYTHONDONTWRITEBYTECODE": "1", "MPLBACKEND": "Agg", "OPENBLAS_NUM_THREADS": "1", "OMP_NUM_THREADS": "1", "NODE_PATH": "/opt/node-libraries/node_modules", "DOTNET_ROOT": "/opt/dotnet", "DOTNET_CLI_TELEMETRY_OPTOUT": "1", "DOTNET_SKIP_FIRST_TIME_EXPERIENCE": "1", "DOTNET_NOLOGO": "1", "DOTNET_PROCESSOR_COUNT": "1", "DOTNET_CLI_USE_MSBUILD_SERVER": "0", "MSBUILDDISABLENODEREUSE": "1", "MSBuildEnableWorkloadResolver": "false", "DOTNET_GCHeapHardLimit": "80000000", "DOTNET_gcServer": "0", "DOTNET_GCHeapCount": "1", "GOCACHE": str(root / ".build/go-cache"), "GOMODCACHE": "/opt/go-modules", "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local", "GOMEMLIMIT": "2GiB", "CARGO_HOME": "/opt/rust/cargo", "RUSTUP_HOME": "/opt/rust/rustup", "CARGO_NET_OFFLINE": "true", "JAVA_TOOL_OPTIONS": "-Xmx2g -XX:ActiveProcessorCount=1"}

def java_package(source):
    """Read a package declaration from Java tokens, after Unicode translation.

    Comments, string/character literals and text blocks cannot supply launcher
    names. javac remains responsible for validating the complete compilation unit.
    """
    translated = []
    index = backslashes = 0
    escaped = False
    while index < len(source):
        char = source[index]
        if char == "\\":
            end = index + 1
            while end < len(source) and source[end] == "u":
                end += 1
            digits = source[end:end + 4]
            if (escaped or backslashes % 2 == 0) and end > index + 1 and len(digits) == 4 and all(c in "0123456789abcdefABCDEF" for c in digits):
                char = chr(int(digits, 16))
                index = end + 4
                escaped = True
                backslashes = backslashes + 1 if char == "\\" else 0
                translated.append(char)
                continue
            backslashes += 1
        else:
            backslashes = 0
        escaped = False
        translated.append(char)
        index += 1
    # Java identifiers are Unicode code points, including surrogate pairs
    # produced by separate \u escapes in the raw source.
    source = "".join(translated).encode("utf-16-le", "surrogatepass").decode("utf-16-le", "surrogatepass")

    def identifier_start(char):
        return char in "_$" or unicodedata.category(char) in ("Lu", "Ll", "Lt", "Lm", "Lo", "Nl", "Sc", "Pc")

    def identifier_ignorable(char):
        point = ord(char)
        return point <= 8 or 14 <= point <= 27 or 127 <= point <= 159 or unicodedata.category(char) == "Cf"

    def identifier_part(char):
        return identifier_start(char) or unicodedata.category(char) in ("Mn", "Mc", "Nd") or identifier_ignorable(char)

    tokens = []
    index = 0
    while index < len(source):
        char = source[index]
        if char.isspace():
            index += 1
        elif source.startswith("//", index):
            endings = [position for ending in ("\r", "\n") if (position := source.find(ending, index + 2)) >= 0]
            index = min(endings) + 1 if endings else len(source)
        elif source.startswith("/*", index):
            end = source.find("*/", index + 2)
            index = len(source) if end < 0 else end + 2
        elif char in "\"'":
            delimiter = '"""' if source.startswith('"""', index) else char
            index += len(delimiter)
            while index < len(source):
                if source[index] == "\\":
                    index += 2
                elif source.startswith(delimiter, index):
                    index += len(delimiter)
                    break
                else:
                    index += 1
            tokens.append("<literal>")
        elif identifier_start(char):
            end = index + 1
            while end < len(source) and identifier_part(source[end]):
                end += 1
            # javac removes ignorable characters when it emits binary names.
            tokens.append("".join(c for c in source[index:end] if not identifier_ignorable(c)))
            index = end
        else:
            tokens.append(char)
            index += 1
    depth = 0
    for index, token in enumerate(tokens):
        if token in ("(", "{", "["):
            depth += 1
        elif token in (")", "}", "]"):
            depth -= 1
        elif depth == 0 and token == "package":
            parts = []
            name_expected = True
            for part in tokens[index + 1:]:
                if part == ";":
                    return ".".join(parts) if parts and not name_expected else ""
                if name_expected and part and identifier_start(part[0]) and all(identifier_part(c) for c in part):
                    parts.append(part)
                elif not name_expected and part == ".":
                    pass
                else:
                    return ""
                name_expected = not name_expected
            return ""
        elif depth == 0 and token in ("import", "class", "interface", "enum", "record", "module"):
            return ""
    return ""

def plans(value, root):
    language = value["language"]
    entry = value["entrypoint"]
    files = [f["path"] for f in value["files"]]
    sources = lambda *exts: sorted(f for f in files if f.split(".")[-1] in exts)
    binary = ".build/program"
    if language == "c":
        return [["gcc", "-std=c17", "-O0", "-Wall", "-Wextra", "-I.", *sources("c"), "-lm", "-o", binary], ["./" + binary]]
    if language == "cpp":
        return [["g++", "-std=c++20", "-O0", "-Wall", "-Wextra", "-I.", *sources("cpp", "cc", "cxx"), "-o", binary], ["./" + binary]]
    if language == "python": return [["python3", entry]]
    if (language == "javascript" and entry.endswith(".jsx")) or language == "typescript":
        return [["/opt/node-libraries/node_modules/.bin/esbuild", entry, "--bundle", "--platform=node", "--format=cjs", "--outfile=.build/program.cjs"], ["node", "--max-old-space-size=1024", ".build/program.cjs"]]
    if language == "javascript": return [["node", "--max-old-space-size=1024", entry]]
    if language == "lua": return [["lua5.4", entry]]
    if language == "java":
        text = (root / entry).read_text()
        package = java_package(text)
        name = (package + "." if package else "") + Path(entry).stem
        return [["javac", "-d", ".build", *sources("java")], ["java", "-cp", ".build", name]]
    if language == "csharp":
        projects = sources("csproj")
        if not projects:
            (root / "execution.csproj").write_text('<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net10.0</TargetFramework><ImplicitUsings>enable</ImplicitUsings><Nullable>enable</Nullable></PropertyGroup></Project>')
            projects = ["execution.csproj"]
        (root / "NuGet.Config").write_text('<configuration><packageSources><clear /></packageSources></configuration>')
        return [["dotnet", "build", projects[0], "--nologo", "--verbosity", "quiet", "--output", ".build/dotnet", "--configfile", "NuGet.Config", "-maxcpucount:1", "-p:UseSharedCompilation=false"], ["dotnet", ".build/dotnet/" + Path(projects[0]).stem + ".dll"]]
    if language == "go":
        if "go.mod" not in files:
            (root / "go.mod").write_text("module edsger.execution\n\ngo 1.27\n")
        package = "./" + str(Path(entry).parent)
        return [["go", "build", "-p", "1", "-o", binary, package], ["./" + binary]]
    if language == "rust":
        if "Cargo.toml" in files:
            return [["cargo", "build", "--offline", "--target-dir", ".build/target"], ["cargo", "run", "--offline", "--target-dir", ".build/target", "--quiet"]]
        return [["rustc", "--edition=2024", entry, "-o", binary], ["./" + binary]]
    if language == "kotlin": return [["kotlinc", *sources("kt"), "-include-runtime", "-d", ".build/program.jar"], ["java", "-jar", ".build/program.jar"]]
    if language == "swift": return [["swiftc", *sources("swift"), "-o", binary], ["./" + binary]]
    direct = {"ruby": "ruby", "php": "php", "r": "Rscript", "perl": "perl", "bash": "bash"}
    if language in direct: return [[direct[language], entry]]
    if language == "sql": return [["sqlite3", "-batch", "-json", ":memory:"]]
    raise ValueError("invalid_request")

class OfflineHTML(HTMLParser):
    """Inline only supplied relative CSS/scripts; CSP rejects all remote assets."""
    def __init__(self, files, entry):
        super().__init__(convert_charrefs=False)
        self.files = files
        self.parent = str(Path(entry).parent)
        self.parts = []
        self.skip_script = False
    def asset(self, path):
        if not path or not safe_path(path): return None
        key = path if self.parent == "." else self.parent + "/" + path
        return self.files.get(key)
    def handle_starttag(self, tag, attrs):
        values = dict(attrs)
        if tag == "link" and values.get("rel") == "stylesheet":
            content = self.asset(values.get("href"))
            if content is not None: self.parts.append("<style>" + content.replace("</style", "<\\/style") + "</style>")
            return
        if tag == "script" and values.get("src"):
            content = self.asset(values["src"])
            if content is not None: self.parts.append("<script>" + content.replace("</script", "<\\/script") + "</script>")
            self.skip_script = True
            return
        self.parts.append(self.get_starttag_text())
    def handle_endtag(self, tag):
        if tag == "script" and self.skip_script: self.skip_script = False; return
        self.parts.append("</" + tag + ">")
    def handle_data(self, data):
        if not self.skip_script: self.parts.append(data)
    def handle_entityref(self, name): self.parts.append("&" + name + ";")
    def handle_charref(self, name): self.parts.append("&#" + name + ";")
    def handle_decl(self, decl): self.parts.append("<!" + decl + ">")

def prepare_project(root, value):
    """Stage one private project identically for batch and interactive runs."""
    for file in value["files"]:
        path = root / file["path"]
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(file["content"])
    (root / ".build").mkdir()
    for folder, _, files in os.walk(root):
        os.chmod(folder, 0o700)
        if os.getuid() == 0:
            os.chown(folder, 65532, 65532)
        for name in files:
            path = Path(folder) / name
            os.chmod(path, 0o600)
            if os.getuid() == 0:
                os.chown(path, 65532, 65532)
    env = environment(root)
    if value["language"] == "go" and Path("/opt/go-cache").is_dir():
        # A per-VM image seed, never another user's persisted build cache.
        env["GOCACHE"] = "/opt/go-cache"
        env["GOMAXPROCS"] = "1"
    node_modules = Path("/opt/node-libraries/node_modules")
    if node_modules.exists():
        (root / "node_modules").symlink_to(node_modules)
    return env


def execute(raw, deadline_seconds=DEADLINE_SECONDS):
    value = validate(raw)
    root = Path(tempfile.mkdtemp(prefix="edsger-job-"))
    deadline = time.monotonic() + deadline_seconds
    try:
        env = prepare_project(root, value)
        if value["language"] == "html":
            parser = OfflineHTML({f["path"]: f["content"] for f in value["files"]}, value["entrypoint"])
            parser.feed((root / value["entrypoint"]).read_text())
            result = dict(status="completed", stdout="", stderr="", exitCode=0, truncated=False, previewHTML="".join(parser.parts))
        else:
            stages = plans(value, root) if value["mode"] == "run" else [["/opt/node-libraries/node_modules/.bin/esbuild", value["entrypoint"], "--bundle", "--platform=browser", "--format=iife", "--minify", '--define:process.env.NODE_ENV="production"', "--loader:.js=jsx", "--outfile=.build/preview.js"]]
            result = dict(status="completed", stdout="", stderr="", exitCode=0, truncated=False)
            for index, argv in enumerate(stages):
                stdin = value["stdin"] if index == len(stages) - 1 else ""
                if value["language"] == "sql": stdin = (root / value["entrypoint"]).read_text() + "\n" + stdin
                remaining = MAX_OUTPUT - len((result["stdout"] + result["stderr"]).encode())
                stage = command(argv, root, env, deadline, stdin, maximum=remaining)
                for name in ("stdout", "stderr"):
                    result[name] += stage[name]
                result.update({k: stage[k] for k in ("status", "exitCode")})
                result["truncated"] = result["truncated"] or stage["truncated"]
                if stage["status"] != "completed": break
            if value["mode"] == "preview" and result["status"] == "completed":
                script = (root / ".build/preview.js").read_bytes()
                css_path = root / ".build/preview.css"
                css = css_path.read_text() if css_path.exists() else ""
                if len(script) + len(css.encode()) > MAX_PREVIEW - 1024:
                    reject_preview(result, "Preview exceeds the size limit. Use a smaller bundle.")
                else:
                    result["previewHTML"] = '<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><style>' + css.replace("</style", "<\\/style") + '</style></head><body><div id="root"></div><div id="app"></div><script>' + script.decode().replace("</script", "<\\/script") + "</script></body></html>"
        if len(result.get("previewHTML", "").encode()) > MAX_PREVIEW:
            reject_preview(result, "Preview exceeds the size limit.")
        return dict(language=value["language"], **result)
    finally:
        shutil.rmtree(root, ignore_errors=True)


class InputError(ValueError):
    """Fixed, non-sensitive controller errors safe to forward to the caller."""
    def __init__(self, code, status=409):
        super().__init__(code)
        self.code = code
        self.status = status


class InteractiveInput:
    """One ephemeral, ordered input channel; an ACK means the pipe was written.

    The execution thread alone touches the child pipe. HTTP request threads only
    enqueue bounded operations and wait for delivery, so retries cannot duplicate
    a line and cancellation cannot leave an acknowledged-but-unwritten queue.
    """
    def __init__(self, initial):
        if initial and not initial.endswith("\n"):
            initial += "\n"
        initial = initial.encode("utf-8")
        if len(initial) > MAX_INPUT:
            raise InputError("limit", 413)
        self.condition = threading.Condition()
        self.initial = initial
        self.total = len(initial)
        self.operations = {}
        self.pending = deque()
        self.ready = False
        self.finished = False
        self.eof = False
        self.cancelled = threading.Event()

    def start(self):
        with self.condition:
            self.ready = True
            if self.initial:
                self.pending.append(dict(sequence=0, data=self.initial, offset=0, eof=False, delivered=False))
                self.initial = b""
            self.condition.notify_all()

    def submit(self, value):
        if not isinstance(value, dict) or set(value) != {"sequence", "text", "eof"}:
            raise InputError("invalid_request", 400)
        sequence, text, eof = value["sequence"], value["text"], value["eof"]
        if type(sequence) is not int or not 1 <= sequence <= MAX_INPUT_SEQUENCES or not isinstance(text, str) or type(eof) is not bool or (eof and text):
            raise InputError("invalid_request", 400)
        try:
            data = text.encode("utf-8")
        except UnicodeError:
            raise InputError("invalid_request", 400) from None
        if len(data) > MAX_INPUT:
            raise InputError("limit", 413)
        digest = hashlib.sha256((b"1" if eof else b"0") + data).digest()
        with self.condition:
            operation = self.operations.get(sequence)
            if operation:
                if operation["digest"] != digest:
                    raise InputError("changed_duplicate")
            else:
                if self.finished or self.eof:
                    raise InputError("input_closed")
                if not self.ready:
                    raise InputError("not_ready")
                if sequence != len(self.operations) + 1:
                    raise InputError("out_of_order")
                if self.total + len(data) > MAX_INPUT:
                    raise InputError("limit", 413)
                operation = dict(sequence=sequence, data=data, offset=0, eof=eof, digest=digest, delivered=False)
                self.total += len(data)
                self.operations[sequence] = operation
                self.pending.append(operation)
                self.eof = eof
            while not operation["delivered"] and not self.finished:
                self.condition.wait(timeout=0.1)
            if not operation["delivered"]:
                raise InputError("input_closed")
            return {"accepted": True, "sequence": sequence}

    def next_operation(self):
        with self.condition:
            return self.pending[0] if self.pending else None

    def delivered(self, operation):
        with self.condition:
            if self.pending and self.pending[0] is operation:
                self.pending.popleft()
            operation["delivered"] = True
            # Keep only bounded idempotency metadata, never submitted input.
            operation["data"] = b""
            self.condition.notify_all()

    def finish(self):
        with self.condition:
            self.finished = True
            self.initial = b""
            for operation in self.pending:
                operation["data"] = b""
            self.pending.clear()
            self.condition.notify_all()


class ConsoleFrames:
    """Coalesce tiny writes and reserve wire/frame room for the trusted final."""
    def __init__(self, emit):
        self.emit = emit
        self.frames = 0
        self.ready_sent = False
        self.wire = 0
        self.pending = {"stdout": "", "stderr": ""}
        self.last_flush = time.monotonic()

    def send(self, value, final=False):
        payload = json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode() + b"\n"
        if len(payload) > MAX_REQUEST or self.wire + len(payload) > MAX_STREAM_WIRE:
            raise RuntimeError("stream_limit")
        if not final:
            if self.frames >= MAX_STREAM_FRAMES:
                raise RuntimeError("stream_limit")
            self.frames += 1
            if value["type"] == "ready":
                self.ready_sent = True
        self.wire += len(payload)
        self.emit(value)
        return True

    def add(self, stream, text):
        self.pending[stream] += text
        self.flush()

    def flush(self, force=False, final=False):
        now = time.monotonic()
        if not force and not final and now - self.last_flush < 0.1:
            return
        # Preserve the ready control and two terminal output frames. Near the
        # limit we retain a bounded per-stream tail, then flush every accepted
        # byte before the final result so stream and final transcripts agree.
        limit = MAX_STREAM_FRAMES if final else MAX_STREAM_FRAMES - 2 - (not self.ready_sent)
        for stream, text in self.pending.items():
            if text and self.frames < limit:
                self.send({"type": "output", "stream": stream, "text": text})
                self.pending[stream] = ""
        self.last_flush = now


def launch_child(argv, cwd, env, unbuffered=False):
    """Apply guest limits outside the multi-threaded controller's fork child."""
    # A previous guest build can write HOME user-site .pth/sitecustomize files.
    # The still-privileged launcher must never load them during Python startup.
    # Guest Python itself starts normally only after the UID has been dropped.
    guest_argv = argv
    if unbuffered and Path("/usr/bin/stdbuf").is_file():
        guest_argv = ["/usr/bin/stdbuf", "-o0", "-e0", *argv]
    launcher = [sys.executable, "-I", "-S", str(Path(__file__).resolve()), "--child", *guest_argv]
    return subprocess.Popen(launcher, cwd=cwd, env=env, stdin=subprocess.PIPE,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            start_new_session=True)


def interactive_command(argv, cwd, env, deadline, channel, frames, captured, live, disconnected):
    """Pipes only, incremental UTF-8, one combined budget, always kill the group."""
    # preexec_fn is unsafe in a threaded server. The trusted launcher applies
    # limits and demotes itself before exec; no controller secrets enter env.
    process = launch_child(argv, cwd, env, unbuffered=live)
    selector = selectors.DefaultSelector()
    decoders = {name: codecs.getincrementaldecoder("utf-8")("replace") for name in ("stdout", "stderr")}
    for stream, name in ((process.stdout, "stdout"), (process.stderr, "stderr")):
        os.set_blocking(stream.fileno(), False)
        selector.register(stream, selectors.EVENT_READ, name)
    os.set_blocking(process.stdin.fileno(), False)
    stdin_registered = False
    status = "completed"
    truncated = False

    def capture(name, text):
        nonlocal truncated
        data = text.encode()
        remaining = MAX_OUTPUT - sum(len(value.encode()) for value in captured.values())
        accepted = data[:remaining].decode("utf-8", "ignore")
        captured[name] += accepted
        frames.add(name, accepted)
        if len(data) > remaining:
            truncated = True

    try:
        if live:
            channel.start()
            frames.flush(force=True)
            frames.send({"type": "ready"})
        else:
            # Build stages must never consume the user's program input.
            process.stdin.close()
        while True:
            if channel.cancelled.is_set() or disconnected():
                raise ConnectionAbortedError("console_disconnected")
            if time.monotonic() >= deadline:
                status = "timeout"
                break
            operation = channel.next_operation() if live and not process.stdin.closed else None
            if operation and not stdin_registered:
                selector.register(process.stdin, selectors.EVENT_WRITE, "stdin")
                stdin_registered = True
            if not selector.get_map():
                if process.poll() is not None:
                    break
                time.sleep(min(0.02, max(0, deadline - time.monotonic())))
                continue
            for key, _ in selector.select(timeout=min(0.02, max(0, deadline - time.monotonic()))):
                if key.data == "stdin":
                    operation = channel.next_operation()
                    if not operation:
                        selector.unregister(process.stdin)
                        stdin_registered = False
                        continue
                    try:
                        if operation["offset"] < len(operation["data"]):
                            operation["offset"] += os.write(key.fd, operation["data"][operation["offset"]:])
                        if operation["offset"] == len(operation["data"]):
                            selector.unregister(process.stdin)
                            stdin_registered = False
                            if operation["eof"]:
                                process.stdin.close()
                            elif operation["data"]:
                                # A real terminal echoes delivered input. Keep
                                # that echo authoritative in the final capture
                                # too; clients must never invent a local echo.
                                capture("stdout", operation["data"].decode("utf-8"))
                            channel.delivered(operation)
                            if truncated:
                                status = "failed"
                                break
                    except (BrokenPipeError, OSError):
                        selector.unregister(process.stdin)
                        stdin_registered = False
                        process.stdin.close()
                        channel.finish()
                    continue
                try:
                    chunk = os.read(key.fd, 4096)
                except BlockingIOError:
                    continue
                if chunk:
                    capture(key.data, decoders[key.data].decode(chunk))
                else:
                    capture(key.data, decoders[key.data].decode(b"", final=True))
                    selector.unregister(key.fileobj)
                    key.fileobj.close()
                if truncated:
                    status = "failed"
                    break
            frames.flush()
            if truncated:
                break
            if process.poll() is not None and not any(key.data in ("stdout", "stderr") for key in selector.get_map().values()):
                break
        if status == "completed":
            try:
                process.wait(timeout=max(0.01, deadline - time.monotonic()))
                if process.returncode:
                    status = "failed"
            except subprocess.TimeoutExpired:
                status = "timeout"
    finally:
        if live:
            channel.finish()
        terminate(process)
        selector.close()
        for stream in (process.stdin, process.stdout, process.stderr):
            if not stream.closed:
                stream.close()
    frames.flush(force=True)
    code = 124 if status == "timeout" else max(-255, min(255, process.returncode or 0))
    return dict(status=status, exitCode=code, truncated=truncated)


def execute_interactive(raw, emit, channel=None, deadline_seconds=DEADLINE_SECONDS, disconnected=lambda: False):
    value = validate(raw)
    if value["mode"] != "run" or value["language"] in ("html", "sql"):
        raise InputError("invalid_request", 400)
    channel = channel or InteractiveInput(value["stdin"])
    root = Path(tempfile.mkdtemp(prefix="edsger-job-"))
    deadline = time.monotonic() + deadline_seconds
    frames = ConsoleFrames(emit)
    captured = {"stdout": "", "stderr": ""}
    result = dict(status="completed", exitCode=0, truncated=False)
    try:
        env = prepare_project(root, value)
        stages = plans(value, root)
        for index, argv in enumerate(stages):
            result = interactive_command(argv, root, env, deadline, channel, frames, captured,
                                         live=index == len(stages) - 1, disconnected=disconnected)
            if result["status"] != "completed":
                break
        execution = dict(language=value["language"], **result, **captured)
        frames.flush(final=True)
        frames.send({"type": "result", "execution": execution}, final=True)
        return execution
    finally:
        channel.finish()
        shutil.rmtree(root, ignore_errors=True)

def no_duplicates(pairs):
    result = {}
    for key, value in pairs:
        if key in result: raise ValueError("invalid_request")
        result[key] = value
    return result

class RunnerHTTPServer(ThreadingHTTPServer):
    daemon_threads = True
    block_on_close = False

    def __init__(self, address, handler):
        super().__init__(address, handler)
        self.job_lock = threading.Lock()
        self.handler_slots = threading.BoundedSemaphore(8)
        self.used = False
        self.console = None
        self.session_key = None

    def process_request(self, request, address):
        if not self.handler_slots.acquire(blocking=False):
            self.shutdown_request(request)
            return
        try:
            super().process_request(request, address)
        except BaseException:
            self.handler_slots.release()
            raise

    def process_request_thread(self, request, address):
        try:
            super().process_request_thread(request, address)
        finally:
            self.handler_slots.release()


class Handler(BaseHTTPRequestHandler):
    def setup(self):
        self.request.settimeout(5)
        super().setup()

    def log_message(self, *args): pass
    def send_json(self, value, status=200):
        payload = json.dumps(value, ensure_ascii=False).encode()
        if len(payload) > MAX_REQUEST:
            value = dict(language=value.get("language", "python"), status="failed", stdout="", stderr="Result exceeds the transfer limit.", exitCode=1, truncated=True)
            payload = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)
    def do_GET(self):
        self.send_json({"ok": True}, 200 if self.path == "/health" else 404)

    def read_json(self):
        self.connection.settimeout(5)
        if self.headers.get("Transfer-Encoding"):
            raise InputError("invalid_request", 400)
        try:
            if len(self.headers.get_all("Content-Length", [])) != 1:
                raise ValueError()
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= MAX_REQUEST:
                raise ValueError()
            payload = self.rfile.read(length)
            if len(payload) != length:
                raise ValueError()
            return json.loads(payload.decode("utf-8"), object_pairs_hook=no_duplicates)
        except (ValueError, UnicodeError, OSError):
            raise InputError("invalid_request", 400) from None

    def disconnected(self):
        try:
            if select.select([self.connection], [], [], 0)[0]:
                return self.connection.recv(1, socket.MSG_PEEK | socket.MSG_DONTWAIT) == b""
        except (OSError, ValueError):
            return True
        return False

    def send_frame(self, value):
        payload = json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode() + b"\n"
        self.wfile.write(payload)
        self.wfile.flush()

    def do_POST(self):
        if self.path not in ("/execute", "/execute-stream", "/input"):
            self.send_json({"error": "invalid_request"}, 400); return
        streaming = False
        owned_channel = None
        try:
            if self.path != "/execute" and os.environ.get("CODE_RUNNER_INTERACTIVE_ENABLED") != "true":
                raise InputError("invalid_request", 400)
            value = self.read_json()
            key = self.headers.get("X-Edsger-Session-Key", "")
            if self.path == "/input":
                with self.server.job_lock:
                    channel = self.server.console
                    actual_key = self.server.session_key
                if channel is None or not actual_key or not re.fullmatch(r"[A-Za-z0-9_-]{32,128}", key) or not hmac.compare_digest(key, actual_key):
                    raise InputError("invalid_request", 400)
                self.send_json(channel.submit(value))
                return
            validate(value)
            channel = None
            if self.path == "/execute-stream":
                if not re.fullmatch(r"[A-Za-z0-9_-]{32,128}", key) or value["mode"] != "run" or value["language"] in ("html", "sql"):
                    raise InputError("invalid_request", 400)
                channel = InteractiveInput(value["stdin"])
            with self.server.job_lock:
                if self.server.used:
                    raise InputError("invalid_request", 400)
                self.server.used = True
                self.server.console = channel
                self.server.session_key = key if channel else None
                owned_channel = channel
            if channel is None:
                self.send_json(execute(value))
            else:
                self.send_response(200)
                self.send_header("Content-Type", "application/x-ndjson")
                self.send_header("Cache-Control", "no-store")
                self.send_header("X-Content-Type-Options", "nosniff")
                self.send_header("Connection", "close")
                self.end_headers()
                streaming = True
                self.connection.settimeout(2)
                execute_interactive(value, self.send_frame, channel=channel, disconnected=self.disconnected)
        except InputError as error:
            if not streaming:
                self.send_json({"error": error.code}, error.status)
        except (ValueError, UnicodeError):
            if not streaming:
                self.send_json({"error": "invalid_request"}, 400)
        except (BrokenPipeError, ConnectionAbortedError, ConnectionResetError, TimeoutError):
            pass
        except Exception:
            if not streaming:
                self.send_json({"error": "runner_unavailable"}, 503)
        finally:
            if owned_channel:
                owned_channel.finish()
                with self.server.job_lock:
                    self.server.console = None
                    self.server.session_key = None
            if streaming:
                self.close_connection = True

if __name__ == "__main__":
    if len(sys.argv) > 2 and sys.argv[1] == "--child":
        child_limits(new_session=False)
        os.execvpe(sys.argv[2], sys.argv[2:], os.environ)
    elif "--execute" in sys.argv:
        print(json.dumps(execute(json.load(sys.stdin, object_pairs_hook=no_duplicates)), ensure_ascii=False))
    else:
        # PID 1 exits promptly on shutdown; arbitrary child code has another UID.
        signal.signal(signal.SIGTERM, lambda *_: os._exit(0))
        server = RunnerHTTPServer(("0.0.0.0", 8080), Handler)
        server.timeout = 45
        server.serve_forever()
