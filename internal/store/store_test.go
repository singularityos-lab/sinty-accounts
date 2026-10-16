package store

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	r := NewRecord()
	r.Set(UserGroup, "Language", "it_IT.UTF-8")
	r.Set(UserGroup, "PasswordHint", "two\nlines \\ slash")
	r.Set("com.example.Ext", "Value", "(false, @as [])")
	var buf bytes.Buffer
	if err := r.Write(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "[User]\n") {
		t.Fatalf("User group must come first:\n%s", buf.String())
	}
	back, err := Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"Language", "PasswordHint"} {
		if back.Get(UserGroup, k) != r.Get(UserGroup, k) {
			t.Fatalf("%s: %q != %q", k, back.Get(UserGroup, k), r.Get(UserGroup, k))
		}
	}
	if back.Get("com.example.Ext", "Value") != "(false, @as [])" {
		t.Fatal("extension group lost")
	}
}

func TestStoreFiles(t *testing.T) {
	s := New(t.TempDir())
	r := NewRecord()
	r.Set(UserGroup, "Email", "a@b.c")
	if err := s.Save("anna", r); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.file("anna"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state file must be private: %v %v", info, err)
	}
	if _, err := s.InstallIcon("anna", []byte("png")); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("anna", "anna2"); err != nil {
		t.Fatal(err)
	}
	back, err := s.Load("anna2")
	if err != nil || back.Get(UserGroup, "Email") != "a@b.c" {
		t.Fatalf("rename lost the record: %v", err)
	}
	if _, err := os.Stat(s.IconPath("anna2")); err != nil {
		t.Fatal("rename lost the icon")
	}
	if err := s.Remove("anna2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.IconPath("anna2")); !os.IsNotExist(err) {
		t.Fatal("remove kept the icon")
	}
}
