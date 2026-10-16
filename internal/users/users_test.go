package users

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEtc(t *testing.T, root string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, "etc", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	writeEtc(t, root, map[string]string{
		"passwd": "root:x:0:0:root:/root:/bin/sh\nnobody:x:65534:65534:nobody:/:/usr/sbin/nologin\nanna:x:1000:1000:Anna Rossi,,,:/home/anna:/bin/bash\nbot:x:1001:1001::/home/bot:/bin/false\nbroken:line\n",
		"shadow": "root:*:19000:0:99999:7:::\nanna:$6$salt$hash:19500:0:99999:7:::\nbot:!:19500::::::\n",
		"group":  "root:x:0:\nwheel:x:10:anna\nanna:x:1000:\n",
	})
	db := New(root)
	all, err := db.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("want 4 users, got %d", len(all))
	}
	humans, err := db.Humans()
	if err != nil {
		t.Fatal(err)
	}
	if len(humans) != 1 || humans[0].Name != "anna" {
		t.Fatalf("unexpected humans %+v", humans)
	}
	anna := humans[0]
	if anna.RealName != "Anna Rossi" || !anna.Admin || anna.Locked() || anna.PasswordMode() != 0 {
		t.Fatalf("unexpected anna %+v", anna)
	}
	if anna.Aging[0] != 19500 || anna.Aging[2] != 99999 || anna.Aging[5] != -1 {
		t.Fatalf("unexpected aging %v", anna.Aging)
	}
	if db.AdminGroup() != "wheel" {
		t.Fatalf("admin group %s", db.AdminGroup())
	}
}

func TestLoginDefs(t *testing.T) {
	root := t.TempDir()
	writeEtc(t, root, map[string]string{"login.defs": "# comment\nUID_MIN 500\nUID_MAX\t2000\n"})
	db := New(root)
	if db.UIDMin != 500 || db.UIDMax != 2000 {
		t.Fatalf("got %d %d", db.UIDMin, db.UIDMax)
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"anna", "a_b", "x1", "build$", "a.b-c"} {
		if !ValidName(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "-x", "1x", "Anna", "a b", "a:b", "a/b", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "x$y"} {
		if ValidName(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
	if ValidRealName("a:b") || ValidRealName("a\nb") || !ValidRealName("Anna Rossi") {
		t.Fatal("real name validation")
	}
}

func TestTools(t *testing.T) {
	root := t.TempDir()
	writeEtc(t, root, map[string]string{"group": "sudo:x:27:\n"})
	var got [][]string
	tools := &Tools{DB: New(root), Run: func(name string, args ...string) error {
		got = append(got, append([]string{name}, args...))
		return nil
	}}
	if err := tools.Create("anna", "Anna", true); err != nil {
		t.Fatal(err)
	}
	if err := tools.Create("-rf", "", false); err == nil {
		t.Fatal("option-like names must be refused")
	}
	if err := tools.SetHash("anna", "a:b"); err == nil {
		t.Fatal("hash with a colon must be refused")
	}
	want := []string{"useradd", "-m", "-s", "/bin/bash", "-c", "Anna", "-G", "sudo", "--", "anna"}
	if len(got) != 1 || len(got[0]) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[0][i] != want[i] {
			t.Fatalf("got %v want %v", got[0], want)
		}
	}
}
