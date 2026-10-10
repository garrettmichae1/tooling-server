#!/usr/bin/env python3
"""Run real programs in task-local Docker images; no network or paid deployment."""
import argparse
import json
import subprocess

CASES = {
    "c": ("main.c", '#include <stdio.h>\nint main(void){puts("hello c");}', "hello c"),
    "cpp": ("main.cpp", '#include <iostream>\nint main(){std::cout<<"hello cpp";}', "hello cpp"),
    "python": ("main.py", 'import numpy, pandas, matplotlib, sympy, PIL, requests\nprint("hello python", sum([1,2,3]))', "hello python 6"),
    "javascript": ("main.js", 'const _=require("lodash"); const {z}=require("zod"); console.log("hello javascript",z.number().parse(_.sum([1,2,3])))', "hello javascript 6"),
    "typescript": ("main.ts", 'import {z} from "zod"; const n:number=z.number().parse(6); console.log("hello typescript",n)', "hello typescript 6"),
    "lua": ("main.lua", 'print("hello lua")', "hello lua"),
    "java": ("Main.java", 'public class Main {public static void main(String[] args){System.out.println("hello java");}}', "hello java"),
    "csharp": ("Program.cs", 'Console.WriteLine("hello csharp");', "hello csharp"),
    "go": ("main.go", 'package main\nimport "fmt"\nfunc main(){fmt.Println("hello go")}', "hello go"),
    "rust": ("main.rs", 'fn main(){println!("hello rust");}', "hello rust"),
    "ruby": ("main.rb", 'puts "hello ruby"', "hello ruby"),
    "php": ("main.php", '<?php echo "hello php";', "hello php"),
    "kotlin": ("main.kt", 'fun main(){println("hello kotlin")}', "hello kotlin"),
    "swift": ("main.swift", 'import Foundation\nprint("hello swift")', "hello swift"),
    "r": ("main.R", 'cat("hello r\\n")', "hello r"),
    "perl": ("main.pl", 'print "hello perl\\n";', "hello perl"),
    "bash": ("main.sh", 'printf "hello bash\\n"', "hello bash"),
    "sql": ("main.sql", "SELECT 'hello sql' AS message;", "hello sql"),
    "html": ("index.html", '<h1>hello html</h1><script src="app.js"></script>', "hello html"),
}
PROFILES = {
    "core": ["python", "javascript", "typescript", "lua", "ruby", "php", "r", "perl", "bash", "sql", "html"],
    "systems": ["c", "cpp", "go", "rust"], "jvm": ["java", "kotlin"],
    "dotnet": ["csharp"], "swift": ["swift"],
}

def run(image, language, entry, source, *, files=None, stdin="", mode="run"):
    payload = dict(language=language, entrypoint=entry, files=files or [dict(path=entry, content=source)], stdin=stdin, mode=mode)
    result = subprocess.run(["docker", "--host=unix:///var/run/docker.sock", "run", "--rm", "--network=none", "--memory=6g", "--cpus=1", "--pids-limit=128", "--security-opt=no-new-privileges", "-e", "EDSGER_TEST_SECRET=must-not-reach-child", "-i", image, "--execute"], input=json.dumps(payload), text=True, capture_output=True, timeout=55)
    assert result.returncode == 0, (language, result.stderr[-3000:])
    value = json.loads(result.stdout)
    assert value["language"] == language, value
    assert len((value["stdout"] + value["stderr"]).encode()) <= 65536, value
    return value

def completed(value, text):
    assert value["status"] == "completed" and value["exitCode"] == 0, value
    assert text in value.get("previewHTML", value["stdout"]), value

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile", required=True, choices=PROFILES)
    parser.add_argument("--image")
    args = parser.parse_args()
    image = args.image or f"edsger-code-{args.profile}:review"
    for language in PROFILES[args.profile]:
        entry, source, expected = CASES[language]
        completed(run(image, language, entry, source, mode="preview" if language == "html" else "run"), expected)
        print("PASS", language, flush=True)
    if args.profile == "core":
        completed(run(image, "python", "main.py", 'import os,sys\nprint(os.getuid(),bool(os.environ.get("EDSGER_TEST_SECRET")),input())', stdin="héllo\n"), "65532 False héllo")
        failed = run(image, "python", "main.py", 'raise ValueError("intentional")')
        assert failed["status"] == "failed" and failed["exitCode"] != 0 and "intentional" in failed["stderr"], failed
        output = run(image, "python", "main.py", 'print("x"*200000)')
        assert output["truncated"], output
        for language, entry, source in [
            ("javascript", "app.jsx", 'import React from "react"; import {createRoot} from "react-dom/client"; createRoot(document.getElementById("root")).render(<h1>React works</h1>)'),
            ("typescript", "app.ts", 'import {createApp,h} from "vue"; createApp({render:()=>h("h1","Vue works")}).mount("#app")'),
        ]:
            completed(run(image, language, entry, source, mode="preview"), "works")
        completed(run(image, "html", "index.html", "", files=[dict(path="index.html",content='<link rel="stylesheet" href="style.css"><h1>inline</h1><script src="app.js"></script>'),dict(path="style.css",content="h1{color:red}"),dict(path="app.js",content='console.log("local script")')], mode="preview"), "local script")
        print("PASS stdin, UID, environment, failure, output cap, React, Vue, local HTML assets", flush=True)
    if args.profile == "systems":
        completed(run(image,"go","main.go",'package main\nimport ("crypto/sha256"; "encoding/json"; "fmt"; "net/http")\nfunc main(){b,_:=json.Marshal(http.StatusOK);fmt.Printf("stdlib %s %x\\n",b,sha256.Sum256(b))}'),"stdlib 200")
        completed(run(image,"go","main.go",'package main\nimport ("fmt"; "os")\nfunc main(){if err:=os.WriteFile("/opt/go-cache/edsger-smoke-marker",[]byte("fixed fixture"),0600);err!=nil{panic(err)};fmt.Println("marker written")}'),"marker written")
        completed(run(image,"go","main.go",'package main\nimport ("fmt"; "os")\nfunc main(){_,err:=os.Stat("/opt/go-cache/edsger-smoke-marker");fmt.Println("fresh cache",os.IsNotExist(err))}'),"fresh cache true")
        completed(run(image,"rust","src/main.rs","",files=[dict(path="Cargo.toml",content='[package]\nname="sample"\nversion="0.1.0"\nedition="2024"\n'),dict(path="src/main.rs",content='fn main(){println!("cargo works");}')]),"cargo works")
        completed(run(image,"c","main.c","",files=[dict(path="main.c",content='#include <stdio.h>\nint sum(void);int main(){printf("multi %d",sum());}'),dict(path="sum.c",content='int sum(void){return 6;}')]),"multi 6")
        print("PASS Go standard libraries and fresh cache, Cargo project and multiple C files",flush=True)

if __name__ == "__main__": main()
