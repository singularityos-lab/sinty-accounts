package users

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type User struct {
	Name     string
	UID      uint32
	GID      uint32
	RealName string
	Home     string
	Shell    string
	Hash     string
	Admin    bool
	Aging    [6]int64
}

func (u *User) Locked() bool {
	return strings.HasPrefix(u.Hash, "!")
}

func (u *User) PasswordMode() int32 {
	switch {
	case u.Hash == "":
		return 2
	case strings.TrimLeft(u.Hash, "!") == "":
		return 2
	default:
		return 0
	}
}

func (u *User) Human(min, max uint32) bool {
	if u.UID < min || u.UID > max {
		return false
	}
	base := filepath.Base(u.Shell)
	return base != "nologin" && base != "false"
}

type Database struct {
	Root        string
	AdminGroups []string
	UIDMin      uint32
	UIDMax      uint32
}

func New(root string) *Database {
	d := &Database{Root: root, AdminGroups: []string{"wheel", "sudo", "admin"}, UIDMin: 1000, UIDMax: 60000}
	d.readLoginDefs()
	return d
}

func (d *Database) path(name string) string {
	return filepath.Join(d.Root, "etc", name)
}

func (d *Database) readLoginDefs() {
	f, err := os.Open(d.path("login.defs"))
	if err != nil {
		return
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) != 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 10, 32)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "UID_MIN":
			d.UIDMin = uint32(v)
		case "UID_MAX":
			d.UIDMax = uint32(v)
		}
	}
}

func readLines(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out [][]string
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, strings.Split(line, ":"))
	}
	return out, s.Err()
}

func (d *Database) Load() ([]*User, error) {
	passwd, err := readLines(d.path("passwd"))
	if err != nil {
		return nil, fmt.Errorf("read passwd: %w", err)
	}
	hashes := map[string]string{}
	aging := map[string][6]int64{}
	if shadow, err := readLines(d.path("shadow")); err == nil {
		for _, f := range shadow {
			if len(f) >= 2 {
				hashes[f[0]] = f[1]
			}
			var a [6]int64
			for i := range a {
				a[i] = -1
				if len(f) > i+2 && f[i+2] != "" {
					if v, err := strconv.ParseInt(f[i+2], 10, 64); err == nil {
						a[i] = v
					}
				}
			}
			aging[f[0]] = a
		}
	} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
		return nil, fmt.Errorf("read shadow: %w", err)
	}
	admins := map[string]bool{}
	if groups, err := readLines(d.path("group")); err == nil {
		for _, f := range groups {
			if len(f) < 4 || !d.isAdminGroup(f[0]) {
				continue
			}
			for _, m := range strings.Split(f[3], ",") {
				if m != "" {
					admins[m] = true
				}
			}
		}
	}
	var out []*User
	for _, f := range passwd {
		if len(f) < 7 {
			continue
		}
		uid, err1 := strconv.ParseUint(f[2], 10, 32)
		gid, err2 := strconv.ParseUint(f[3], 10, 32)
		if err1 != nil || err2 != nil {
			continue
		}
		hash, ok := hashes[f[0]]
		if !ok {
			hash = f[1]
		}
		out = append(out, &User{
			Name:     f[0],
			UID:      uint32(uid),
			GID:      uint32(gid),
			RealName: strings.Split(f[4], ",")[0],
			Home:     f[5],
			Shell:    f[6],
			Hash:     hash,
			Admin:    uid == 0 || admins[f[0]],
			Aging:    aging[f[0]],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	return out, nil
}

func (d *Database) isAdminGroup(name string) bool {
	for _, g := range d.AdminGroups {
		if g == name {
			return true
		}
	}
	return false
}

func (d *Database) AdminGroup() string {
	groups, err := readLines(d.path("group"))
	if err != nil {
		return d.AdminGroups[0]
	}
	for _, want := range d.AdminGroups {
		for _, f := range groups {
			if len(f) > 0 && f[0] == want {
				return want
			}
		}
	}
	return d.AdminGroups[0]
}

func (d *Database) Humans() ([]*User, error) {
	all, err := d.Load()
	if err != nil {
		return nil, err
	}
	var out []*User
	for _, u := range all {
		if u.Human(d.UIDMin, d.UIDMax) {
			out = append(out, u)
		}
	}
	return out, nil
}

func ValidName(name string) bool {
	if name == "" || len(name) > 32 || name[0] == '-' {
		return false
	}
	for i, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c == '_':
		case (c >= '0' && c <= '9') || c == '-' || c == '.':
			if i == 0 {
				return false
			}
		case c == '$' && i == len(name)-1:
		default:
			return false
		}
	}
	return true
}

func ValidRealName(name string) bool {
	return len(name) <= 256 && !strings.ContainsAny(name, ":,\n=")
}

func ValidHash(hash string) bool {
	return len(hash) <= 512 && !strings.ContainsAny(hash, ":\n")
}
