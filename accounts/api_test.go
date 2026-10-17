package accounts

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type fakeEtc struct {
	root string
	t    *testing.T
}

func (f *fakeEtc) path(name string) string {
	return filepath.Join(f.root, "etc", name)
}

func (f *fakeEtc) edit(name string, fn func([]string) []string) {
	data, _ := os.ReadFile(f.path(name))
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	out := fn(lines)
	os.WriteFile(f.path(name), []byte(strings.Join(out, "\n")+"\n"), 0o644)
}

func (f *fakeEtc) setField(file, user string, field int, value string) {
	f.edit(file, func(lines []string) []string {
		for i, l := range lines {
			p := strings.Split(l, ":")
			if p[0] == user && len(p) > field {
				p[field] = value
				lines[i] = strings.Join(p, ":")
			}
		}
		return lines
	})
}

func (f *fakeEtc) run(name string, args ...string) error {
	target := args[len(args)-1]
	switch name {
	case "useradd":
		uid := 1500
		f.edit("passwd", func(l []string) []string {
			return append(l, fmt.Sprintf("%s:x:%d:%d:%s:/home/%s:/bin/bash", target, uid, uid, argAfter(args, "-c"), target))
		})
		f.edit("shadow", func(l []string) []string { return append(l, target+":!:19500:0:99999:7:::") })
	case "userdel":
		for _, file := range []string{"passwd", "shadow"} {
			f.edit(file, func(l []string) []string {
				var out []string
				for _, x := range l {
					if !strings.HasPrefix(x, target+":") {
						out = append(out, x)
					}
				}
				return out
			})
		}
	case "usermod":
		if v := argAfter(args, "-c"); v != "" {
			f.setField("passwd", target, 4, v)
		}
		if v := argAfter(args, "-p"); v != "" || hasArg(args, "-p") {
			f.setField("shadow", target, 1, v)
		}
		if hasArg(args, "-L") {
			f.setField("shadow", target, 1, "!$6$x")
		}
	default:
		return fmt.Errorf("unexpected %s", name)
	}
	return nil
}

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func privateBus(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--print-address")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skip("dbus-daemon not available")
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(line)
}

func connect(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func setup(t *testing.T) (*dbus.Conn, *fakeEtc, string) {
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "etc"), 0o755)
	f := &fakeEtc{root: root, t: t}
	os.WriteFile(f.path("passwd"), []byte(fmt.Sprintf("root:x:0:0:root:/root:/bin/sh\n%s:x:%s:%s:Me:/home/me:/bin/bash\nother:x:1200:1200:Other:/home/other:/bin/bash\n", me.Username, me.Uid, me.Gid)), 0o644)
	os.WriteFile(f.path("shadow"), []byte(fmt.Sprintf("root:*:1::::::\n%s:$6$a$b:19500:0:99999:7:::\nother:$6$c$d:19500:0:99999:7:::\n", me.Username)), 0o644)
	os.WriteFile(f.path("group"), []byte(fmt.Sprintf("wheel:x:10:%s\n", me.Username)), 0o644)
	os.WriteFile(f.path("login.defs"), []byte("UID_MIN 1000\n"), 0o644)
	addr := privateBus(t)
	server := connect(t, addr)
	svc := New(server, Options{Root: root, InterfacesDir: filepath.Join(root, "none"), Run: f.run})
	if err := svc.Export(); err != nil {
		t.Fatal(err)
	}
	if _, err := server.RequestName(BusName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go svc.Watch(stop)
	return connect(t, addr), f, me.Username
}

func TestManagerAndUser(t *testing.T) {
	client, _, me := setup(t)
	mgr := client.Object(BusName, ManagerPath)

	var cached []dbus.ObjectPath
	if err := mgr.Call(ManagerIface+".ListCachedUsers", 0).Store(&cached); err != nil {
		t.Fatal(err)
	}
	if len(cached) != 2 {
		t.Fatalf("want 2 cached users, got %v", cached)
	}

	var path dbus.ObjectPath
	if err := mgr.Call(ManagerIface+".FindUserByName", 0, me).Store(&path); err != nil {
		t.Fatal(err)
	}
	u := client.Object(BusName, path)
	name, err := u.GetProperty(UserIface + ".RealName")
	if err != nil || name.Value().(string) != "Me" {
		t.Fatalf("RealName %v %v", name, err)
	}
	accountType, _ := u.GetProperty(UserIface + ".AccountType")
	if accountType.Value().(int32) != 1 {
		t.Fatalf("the wheel member must be an administrator, got %v", accountType)
	}

	if err := u.Call(UserIface+".SetRealName", 0, "Mirko B").Err; err != nil {
		t.Fatal(err)
	}
	if err := u.Call(UserIface+".SetLanguage", 0, "it_IT.UTF-8").Err; err != nil {
		t.Fatal(err)
	}
	if err := u.Call(UserIface+".SetRealName", 0, "bad:name").Err; err == nil {
		t.Fatal("a colon in the real name must be refused")
	}
	name, _ = u.GetProperty(UserIface + ".RealName")
	lang, _ := u.GetProperty(UserIface + ".Language")
	if name.Value().(string) != "Mirko B" || lang.Value().(string) != "it_IT.UTF-8" {
		t.Fatalf("got %v %v", name, lang)
	}

	if err := u.Call(UserIface+".SetPassword", 0, "$6$new$hash", "hint").Err; err != nil {
		t.Fatal(err)
	}
	if err := u.Call(UserIface+".SetPassword", 0, "bad:hash", "").Err; err == nil {
		t.Fatal("a colon in the hash must be refused")
	}

	var newPath dbus.ObjectPath
	if err := mgr.Call(ManagerIface+".CreateUser", 0, "guest", "Guest", int32(0)).Store(&newPath); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Call(ManagerIface+".CreateUser", 0, "guest", "Guest", int32(0)).Err; err == nil {
		t.Fatal("creating a duplicate must fail")
	}
	multi, _ := mgr.GetProperty(ManagerIface + ".HasMultipleUsers")
	if !multi.Value().(bool) {
		t.Fatal("HasMultipleUsers")
	}
	if err := mgr.Call(ManagerIface+".DeleteUser", 0, int64(1500), false).Err; err != nil {
		t.Fatal(err)
	}
	if err := mgr.Call(ManagerIface+".FindUserByName", 0, "guest").Err; err == nil {
		t.Fatal("deleted user must be gone")
	}
	if err := mgr.Call(ManagerIface+".DeleteUser", 0, int64(0), false).Err; err == nil {
		t.Fatal("root must never be deleted")
	}

	var intro string
	if err := u.Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&intro); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(intro, "com.endlessm.ParentalControls.AppFilter") || !strings.Contains(intro, "SetIconFile") {
		t.Fatal("introspection is missing interfaces")
	}

	filter := dbus.MakeVariant(struct {
		Allow bool
		Apps  []string
	}{false, []string{"org.example.Game"}})
	if err := u.Call(propsIface+".Set", 0, "com.endlessm.ParentalControls.AppFilter", "AppFilter", filter).Err; err != nil {
		t.Fatal(err)
	}
	got, err := u.GetProperty("com.endlessm.ParentalControls.AppFilter.AppFilter")
	if err != nil || !strings.Contains(got.String(), "org.example.Game") {
		t.Fatalf("AppFilter round trip: %v %v", got, err)
	}
	if err := u.Call(propsIface+".Set", 0, "com.endlessm.ParentalControls.AppFilter", "AppFilter", dbus.MakeVariant("wrong")).Err; err == nil {
		t.Fatal("a wrong type must be refused")
	}
}

func TestExternalEdit(t *testing.T) {
	client, f, _ := setup(t)
	mgr := client.Object(BusName, ManagerPath)
	added := make(chan dbus.ObjectPath, 4)
	client.AddMatchSignal(dbus.WithMatchInterface(ManagerIface), dbus.WithMatchMember("UserAdded"))
	sig := make(chan *dbus.Signal, 8)
	client.Signal(sig)
	go func() {
		for s := range sig {
			if s.Name == ManagerIface+".UserAdded" {
				added <- s.Body[0].(dbus.ObjectPath)
			}
		}
	}()
	f.edit("passwd", func(l []string) []string { return append(l, "late:x:1300:1300:Late:/home/late:/bin/bash") })
	select {
	case p := <-added:
		if p != userPath(1300) {
			t.Fatalf("unexpected path %s", p)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("an external passwd edit was not noticed")
	}
	var path dbus.ObjectPath
	if err := mgr.Call(ManagerIface+".FindUserByName", 0, "late").Store(&path); err != nil {
		t.Fatal(err)
	}
}

func TestIcon(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "face.png")
	os.WriteFile(png, append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, 64)...), 0o644)
	if _, err := readIcon(png, uint32(os.Getuid()), false); err != nil {
		t.Fatal(err)
	}
	text := filepath.Join(dir, "notes.txt")
	os.WriteFile(text, []byte("secret"), 0o644)
	if _, err := readIcon(text, uint32(os.Getuid()), false); err == nil {
		t.Fatal("a non-image must be refused")
	}
	link := filepath.Join(dir, "link.png")
	os.Symlink(png, link)
	if _, err := readIcon(link, uint32(os.Getuid()), false); err == nil {
		t.Fatal("a symlink must be refused")
	}
	if _, err := readIcon(png, uint32(os.Getuid())+1, false); err == nil {
		t.Fatal("a file of another user must be refused")
	}
	if _, err := readIcon("relative.png", uint32(os.Getuid()), false); err == nil {
		t.Fatal("a relative path must be refused")
	}
}
