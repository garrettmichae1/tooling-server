"""Single-job console/build adapter inside a disposable Cloudflare microVM.

This is NOT the isolation boundary. Cloudflare's microVM, denied egress, hard
Worker deadline and destroy() are that boundary. No credentials enter this image.
The root controller never executes submitted source; child programs use UID 65532.
"""
import ctypes
import html
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import re
import resource
import selectors
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

LANGUAGES = ("c", "cpp", "python", "javascript", "typescript", "lua", "java", "csharp", "go", "rust", "ruby", "php", "kotlin", "swift", "r", "perl", "bash", "sql", "html")
EXTENSIONS = dict(zip(LANGUAGES, (("c",), ("cpp", "cc", "cxx"), ("py",), ("js", "mjs", "cjs", "jsx"), ("ts", "tsx"), ("lua",), ("java",), ("cs",), ("go",), ("rs",), ("rb",), ("php",), ("kt",), ("swift",), ("r", "R"), ("pl",), ("sh",), ("sql",), ("html", "htm"))))
MAX_REQUEST = 524288
MAX_SOURCE = 262144
MAX_OUTPUT = 65536
MAX_PREVIEW = 262144
DEADLINE_SECONDS = 30

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

def child_limits():
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

def command(argv, cwd, env, deadline, stdin="", maximum=MAX_OUTPUT):
    """Argv-only invocation, bounded output while reading, and whole process-group kill."""
    process = subprocess.Popen(argv, cwd=cwd, env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, preexec_fn=child_limits)
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
    # Replacing invalid UTF-8 can expand byte count. Keep the protocol's bound.
    output = {name: bytes(value).decode("utf-8", "replace") for name, value in captured.items()}
    for name in output:
        output[name] = output[name].encode()[:maximum // 2].decode("utf-8", "ignore")
    code = 124 if status == "timeout" else max(-255, min(255, process.returncode or 0))
    return dict(status=status, exitCode=code, truncated=truncated, **output)

def environment(root):
    # Never inherit the controller/process environment, proxies or credentials.
    return {"PATH": "/opt/python/bin:/opt/node/bin:/opt/go/bin:/opt/rust/bin:/opt/dotnet:/usr/local/swift/usr/bin:/usr/local/bin:/usr/bin:/bin", "HOME": str(root), "TMPDIR": str(root / ".build"), "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "PYTHONUNBUFFERED": "1", "PYTHONDONTWRITEBYTECODE": "1", "MPLBACKEND": "Agg", "OPENBLAS_NUM_THREADS": "1", "OMP_NUM_THREADS": "1", "NODE_PATH": "/opt/node-libraries/node_modules", "DOTNET_ROOT": "/opt/dotnet", "DOTNET_CLI_TELEMETRY_OPTOUT": "1", "DOTNET_SKIP_FIRST_TIME_EXPERIENCE": "1", "DOTNET_NOLOGO": "1", "DOTNET_PROCESSOR_COUNT": "1", "DOTNET_CLI_USE_MSBUILD_SERVER": "0", "MSBUILDDISABLENODEREUSE": "1", "MSBuildEnableWorkloadResolver": "false", "DOTNET_GCHeapHardLimit": "80000000", "DOTNET_gcServer": "0", "DOTNET_GCHeapCount": "1", "GOCACHE": str(root / ".build/go-cache"), "GOMODCACHE": "/opt/go-modules", "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local", "GOMEMLIMIT": "2GiB", "CARGO_HOME": "/opt/rust/cargo", "RUSTUP_HOME": "/opt/rust/rustup", "CARGO_NET_OFFLINE": "true", "JAVA_TOOL_OPTIONS": "-Xmx2g -XX:ActiveProcessorCount=1"}

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
        package = re.search(r"(?m)^\s*package\s+([A-Za-z_][\w.]*)\s*;", text)
        name = (package.group(1) + "." if package else "") + Path(entry).stem
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
        return [["go", "build", "-o", binary, package], ["./" + binary]]
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

def execute(raw, deadline_seconds=DEADLINE_SECONDS):
    value = validate(raw)
    root = Path(tempfile.mkdtemp(prefix="edsger-job-"))
    deadline = time.monotonic() + deadline_seconds
    try:
        for file in value["files"]:
            path = root / file["path"]
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(file["content"])
        (root / ".build").mkdir()
        for folder, dirs, files in os.walk(root):
            os.chmod(folder, 0o700)
            if os.getuid() == 0: os.chown(folder, 65532, 65532)
            for name in files:
                path = Path(folder) / name
                os.chmod(path, 0o600)
                if os.getuid() == 0: os.chown(path, 65532, 65532)
        env = environment(root)
        node_modules = Path("/opt/node-libraries/node_modules")
        if node_modules.exists(): (root / "node_modules").symlink_to(node_modules)
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
                stage = command(argv, root, env, deadline, stdin)
                remaining = MAX_OUTPUT - len((result["stdout"] + result["stderr"]).encode())
                for name in ("stdout", "stderr"):
                    data = stage[name].encode()
                    if len(data) > remaining: stage["truncated"] = True
                    result[name] += data[:remaining].decode("utf-8", "ignore")
                    remaining -= min(len(data), remaining)
                result.update({k: stage[k] for k in ("status", "exitCode", "truncated")})
                if stage["status"] != "completed": break
            if value["mode"] == "preview" and result["status"] == "completed":
                script = (root / ".build/preview.js").read_bytes()
                css_path = root / ".build/preview.css"
                css = css_path.read_text() if css_path.exists() else ""
                if len(script) + len(css.encode()) > MAX_PREVIEW - 1024:
                    result.update(status="failed", stderr="Preview exceeds the size limit. Use a smaller bundle.", exitCode=1)
                else:
                    result["previewHTML"] = '<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><style>' + css.replace("</style", "<\\/style") + '</style></head><body><div id="root"></div><div id="app"></div><script>' + script.decode().replace("</script", "<\\/script") + "</script></body></html>"
        if len(result.get("previewHTML", "").encode()) > MAX_PREVIEW:
            result = dict(status="failed", stdout="", stderr="Preview exceeds the size limit.", exitCode=1, truncated=False)
        return dict(language=value["language"], **result)
    finally:
        shutil.rmtree(root, ignore_errors=True)

def no_duplicates(pairs):
    result = {}
    for key, value in pairs:
        if key in result: raise ValueError("invalid_request")
        result[key] = value
    return result

class Handler(BaseHTTPRequestHandler):
    used = False
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
    def do_POST(self):
        if self.path != "/execute" or Handler.used:
            self.send_json({"error": "invalid_request"}, 400); return
        Handler.used = True
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= MAX_REQUEST: raise ValueError()
            value = json.loads(self.rfile.read(length).decode("utf-8"), object_pairs_hook=no_duplicates)
            self.send_json(execute(value))
        except (ValueError, UnicodeError): self.send_json({"error": "invalid_request"}, 400)
        except Exception: self.send_json({"error": "runner_unavailable"}, 503)

if __name__ == "__main__":
    if "--execute" in sys.argv:
        print(json.dumps(execute(json.load(sys.stdin, object_pairs_hook=no_duplicates)), ensure_ascii=False))
    else:
        # PID 1 exits promptly on shutdown; arbitrary child code has another UID.
        signal.signal(signal.SIGTERM, lambda *_: os._exit(0))
        server = HTTPServer(("0.0.0.0", 8080), Handler)
        server.timeout = 45
        server.serve_forever()
