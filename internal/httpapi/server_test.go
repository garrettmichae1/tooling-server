package httpapi_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"edsger.local/figureserver/internal/chart"
	"edsger.local/figureserver/internal/fakemcp"
	"edsger.local/figureserver/internal/handout"
	"edsger.local/figureserver/internal/httpapi"
	"edsger.local/figureserver/internal/model"
	"edsger.local/figureserver/internal/session"
)

const token = "test-token-must-be-at-least-32-bytes"

func TestMCPHelper(t *testing.T) {
	if os.Getenv("FIGURE_FAKE_MCP") != "1" {
		return
	}
	fakemcp.Run()
	os.Exit(0)
}

func fakeCommand() *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMCPHelper$")
	cmd.Env = []string{
		"FIGURE_FAKE_MCP=1",
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + os.TempDir(),
	}
	return cmd
}

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	mgr, err := session.NewManager(session.Config{
		NewCommand:  fakeCommand,
		MaxSessions: 2,
		MaxCalls:    40,
		MaxPNG:      1024 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	compiler := fakeTypst(t)
	graphs := chart.Compiler{Bin: compiler.Bin, MaxPDF: compiler.MaxPDF}
	objects := fakeOpenSCAD(t)
	handler := httpapi.New(token, 256*1024, 1000, mgr, &compiler, &graphs, &objects, "# Figure transcription guide\n\nIt does not generate photographs.\n", nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestAuthAndGuide(t *testing.T) {
	srv := newServer(t)
	res, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(body, []byte(`"status":"ok"`)) {
		t.Fatalf("health %d %s", res.StatusCode, body)
	}

	res, err = http.Get(srv.URL + "/v1/guide")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("guide without token: %d", res.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/guide", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(body, []byte("Figure")) || !bytes.Contains(body, []byte("photographs")) {
		t.Fatalf("guide %d %s", res.StatusCode, body)
	}
}

func TestDrawExportAndRejection(t *testing.T) {
	srv := newServer(t)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/sessions", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create %d %s", res.StatusCode, body)
	}
	id := between(string(body), `"id":"`, `"`)
	if len(id) != 32 {
		t.Fatalf("id %q", id)
	}

	call := func(payload string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/sessions/"+id+"/calls", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	res = call(`{"tool":"open_file","arguments":{"path":"a.svg"}}`)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("open_file %d", res.StatusCode)
	}
	res = call(`{"tool":"draw_shape","arguments":{"fill":"/etc/passwd"}}`)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("absolute path %d", res.StatusCode)
	}
	res = call(`{"tool":"draw_shape","arguments":{"note":"../secret"}}`)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("dotdot %d", res.StatusCode)
	}
	res = call(`{"tool":"draw_shape","arguments":{"shape":"rectangle"},"path":"/tmp/x"}`)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown field %d", res.StatusCode)
	}
	res = call(`{"tool":"compose_poster","arguments":{"title":"CODE AFTER DARK","subtitle":"C PYTHON JS LUA","mood":"night"}}`)
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("compose %d %s", res.StatusCode, body)
	}
	res = call(`{"tool":"draw_shape","arguments":{"shape":"rectangle","x":0,"y":0,"width":10,"height":10,"fill":"#112233"}}`)
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("draw %d %s", res.StatusCode, body)
	}

	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/sessions/"+id+"/export", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	png, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || len(png) < 8 || png[0] != 0x89 {
		t.Fatalf("export %d %d bytes", res.StatusCode, len(png))
	}
}

func TestWrongToken(t *testing.T) {
	srv := newServer(t)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/sessions", http.NoBody)
	req.Header.Set("Authorization", "Bearer not-the-right-token-but-long-enough")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func fakeTypst(t *testing.T) handout.Compiler {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "typst")
	script := "#!/bin/sh\n" +
		"if [ -n \"${FIGURE_TOKEN:-}\" ]; then\n" +
		"  exit 2\n" +
		"fi\n" +
		"out=\n" +
		"for a in \"$@\"; do out=$a; done\n" +
		"printf '%s\\n' '%PDF-1.4' '1 0 obj<<>>endobj' 'trailer<<>>' '%%EOF' > \"$out\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return handout.Compiler{Bin: path, MaxPDF: 1024 * 1024}
}

func TestHandout(t *testing.T) {
	srv := newServer(t)
	post := func(payload string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/handouts", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	res := post(`{"subtitle":"no title","sections":[{"body":[{"type":"paragraph","parts":[{"text":"Hi"}]}]}]}`)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing title %d", res.StatusCode)
	}
	res = post(`{"title":"Pointers","sections":[{"body":[{"type":"math","latex":"\\input{secret}"}]}]}`)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("input command %d", res.StatusCode)
	}
	res = post(`{"title":"Pointers","kicker":"C","sections":[{"heading":"A pointer","body":[{"type":"paragraph","parts":[{"text":"A pointer stores "},{"math":"&x"}]},{"type":"code","lang":"c","text":"int x = 3;\nint *p = &x;\n"}]}]}`)
	pdf, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !bytes.HasPrefix(pdf, []byte("%PDF")) || res.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("handout %d %q %d bytes", res.StatusCode, res.Header.Get("Content-Type"), len(pdf))
	}
}

func TestChart(t *testing.T) {
	srv := newServer(t)
	post := func(payload string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/charts", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	res := post(`{"title":"Hours","type":"pie","categories":["A"],"series":[1]}`)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("one slice %d", res.StatusCode)
	}
	res = post(`{"title":"Study hours","type":"column","xlabel":"Language","ylabel":"Hours","categories":["C","Python","JS","Lua"],"series":[4,7,5,3]}`)
	pdf, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !bytes.HasPrefix(pdf, []byte("%PDF")) || res.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("chart %d %q %d bytes", res.StatusCode, res.Header.Get("Content-Type"), len(pdf))
	}
	if res.Header.Get("Content-Disposition") != `attachment; filename="chart.pdf"` {
		t.Fatalf("disposition %q", res.Header.Get("Content-Disposition"))
	}
}

func fakeOpenSCAD(t *testing.T) model.Compiler {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "openscad")
	script := "#!/bin/sh\n" +
		"if [ -n \"${FIGURE_TOKEN:-}\" ]; then\n" +
		"  exit 2\n" +
		"fi\n" +
		"out=\n" +
		"prev=\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"-o\" ]; then out=$a; fi\n" +
		"  prev=$a\n" +
		"done\n" +
		"python3 -c 'import sys; sys.stdout.buffer.write(b\"\\0\"*80 + (1).to_bytes(4, \"little\") + b\"\\0\"*50)' > \"$out\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return model.Compiler{Bin: path}
}

func TestModel(t *testing.T) {
	t.Setenv("FIGURE_TOKEN", token)
	srv := newServer(t)
	post := func(payload string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/models", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	res := post(`{"script":"include <other.scad> cube(20);"}`)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("include %d", res.StatusCode)
	}
	res = post(`{"script":"cube(20);"}`)
	stl, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "model/stl" || len(stl) != 84+50 {
		t.Fatalf("model %d %q %d bytes", res.StatusCode, res.Header.Get("Content-Type"), len(stl))
	}
	if res.Header.Get("Content-Disposition") != `attachment; filename="model.stl"` {
		t.Fatalf("disposition %q", res.Header.Get("Content-Disposition"))
	}
}

func TestMolecule(t *testing.T) {
	t.Setenv("FIGURE_TOKEN", token)
	srv := newServer(t)
	post := func(payload string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/molecules", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := post(`{"formula":"NaCl"}`)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("salt %d", res.StatusCode)
	}
	res = post(`{"formula":"H2O"}`)
	stl, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "model/stl" || len(stl) != 84+50 {
		t.Fatalf("water %d %q %d bytes", res.StatusCode, res.Header.Get("Content-Type"), len(stl))
	}
	if res.Header.Get("Content-Disposition") != `attachment; filename="molecule.stl"` {
		t.Fatalf("disposition %q", res.Header.Get("Content-Disposition"))
	}
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return ""
	}
	return s[:j]
}
