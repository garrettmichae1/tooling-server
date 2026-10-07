package allowlist

import "testing"

func TestValidate(t *testing.T) {
	ok := []struct {
		tool string
		args string
	}{
		{"draw_shape", `{"shape":"rectangle","x":0,"y":0,"width":10,"height":10,"fill":"#fff"}`},
		{"add_text", `{"text":"NIGHT DRIVE","x":48,"y":72,"size":64,"color":"#ffffff"}`},
		{"undo", `{}`},
		{"inspect_document", ``},
	}
	for _, tc := range ok {
		if err := Validate(tc.tool, []byte(tc.args)); err != nil {
			t.Fatalf("%s %s: %v", tc.tool, tc.args, err)
		}
	}

	bad := []struct {
		tool string
		args string
	}{
		{"open_file", `{"path":"a.svg"}`},
		{"run_command", `{"command":"document.save"}`},
		{"export", `{"format":"png"}`},
		{"screenshot", `{}`},
		{"draw_shape", `{"fill":"/etc/passwd"}`},
		{"draw_shape", `{"note":"../secret"}`},
		{"draw_shape", `["nope"]`},
		{"draw_shape", `{"shape":"rectangle","extra":{"a":{"b":{"c":{"d":{"e":{"f":{"g":{"h":{"i":{"j":{"k":{"l":{"m":1}}}}}}}}}}}}}`},
	}
	for _, tc := range bad {
		if err := Validate(tc.tool, []byte(tc.args)); err == nil {
			t.Fatalf("accepted %s %s", tc.tool, tc.args)
		}
	}
}

func TestNamesMatchAllowlist(t *testing.T) {
	for _, name := range Names() {
		if !Allowed(name) {
			t.Fatalf("listed but not allowed: %s", name)
		}
	}
	if Allowed("run_command") || Allowed("export") {
		t.Fatal("closed tools are allowed")
	}
}
