package users

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type Runner func(name string, args ...string) error

func Exec(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			return fmt.Errorf("%s: %w", name, err)
		}
		return fmt.Errorf("%s: %s", name, msg)
	}
	return nil
}

type Tools struct {
	Run Runner
	DB  *Database
}

func (t *Tools) Create(name, realName string, admin bool) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid user name %q", name)
	}
	if !ValidRealName(realName) {
		return fmt.Errorf("invalid full name")
	}
	args := []string{"-m", "-s", "/bin/bash"}
	if realName != "" {
		args = append(args, "-c", realName)
	}
	if admin {
		args = append(args, "-G", t.DB.AdminGroup())
	}
	return t.Run("useradd", append(args, "--", name)...)
}

func (t *Tools) Delete(name string, removeFiles bool) error {
	if removeFiles {
		return t.Run("userdel", "-r", "--", name)
	}
	return t.Run("userdel", "--", name)
}

func (t *Tools) Rename(old, name string) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid user name %q", name)
	}
	return t.Run("usermod", "-l", name, "--", old)
}

func (t *Tools) SetRealName(name, realName string) error {
	if !ValidRealName(realName) {
		return fmt.Errorf("invalid full name")
	}
	return t.Run("usermod", "-c", realName, "--", name)
}

func (t *Tools) SetShell(name, shell string) error {
	if !strings.HasPrefix(shell, "/") || strings.ContainsAny(shell, ":\n") {
		return fmt.Errorf("invalid shell")
	}
	return t.Run("usermod", "-s", shell, "--", name)
}

func (t *Tools) SetHome(name, home string) error {
	if !strings.HasPrefix(home, "/") || strings.ContainsAny(home, ":\n") {
		return fmt.Errorf("invalid home directory")
	}
	return t.Run("usermod", "-m", "-d", home, "--", name)
}

func (t *Tools) SetAdmin(name string, admin bool) error {
	group := t.DB.AdminGroup()
	if admin {
		return t.Run("usermod", "-a", "-G", group, "--", name)
	}
	return t.Run("gpasswd", "-d", name, group)
}

func (t *Tools) SetHash(name, hash string) error {
	if !ValidHash(hash) {
		return fmt.Errorf("invalid password hash")
	}
	return t.Run("usermod", "-p", hash, "--", name)
}

func (t *Tools) SetLocked(name string, locked bool) error {
	if locked {
		return t.Run("usermod", "-L", "--", name)
	}
	return t.Run("usermod", "-U", "--", name)
}

func (t *Tools) ExpireNow(name string) error {
	return t.Run("chage", "-d", "0", "--", name)
}
