package auth

import (
	"errors"

	"github.com/godbus/dbus/v5"
)

const (
	ActionAdminister  = "org.freedesktop.accounts.user-administration"
	ActionOwnData     = "org.freedesktop.accounts.change-own-user-data"
	ActionOwnPassword = "org.freedesktop.accounts.change-own-password"
	polkitName        = "org.freedesktop.PolicyKit1"
	polkitPath        = "/org/freedesktop/PolicyKit1/Authority"
	polkitIface       = "org.freedesktop.PolicyKit1.Authority"
)

var ErrDenied = errors.New("not authorized")

type Caller struct {
	Sender string
	UID    uint32
}

type Checker struct {
	Conn    *dbus.Conn
	IsAdmin func(uid uint32) bool
}

func (c *Checker) Caller(sender dbus.Sender) (Caller, error) {
	var uid uint32
	err := c.Conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixUser", 0, string(sender)).Store(&uid)
	if err != nil {
		return Caller{}, err
	}
	return Caller{Sender: string(sender), UID: uid}, nil
}

func (c *Checker) polkit() bool {
	var owned bool
	if err := c.Conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, polkitName).Store(&owned); err == nil && owned {
		return true
	}
	var names []string
	if err := c.Conn.BusObject().Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&names); err != nil {
		return false
	}
	for _, n := range names {
		if n == polkitName {
			return true
		}
	}
	return false
}

type polkitSubject struct {
	Kind    string
	Details map[string]dbus.Variant
}

type polkitResult struct {
	Authorized bool
	Challenge  bool
	Details    map[string]string
}

func (c *Checker) Check(caller Caller, action string) error {
	if caller.UID == 0 {
		return nil
	}
	if c.polkit() {
		subject := polkitSubject{Kind: "system-bus-name", Details: map[string]dbus.Variant{"name": dbus.MakeVariant(caller.Sender)}}
		var res polkitResult
		obj := c.Conn.Object(polkitName, polkitPath)
		err := obj.Call(polkitIface+".CheckAuthorization", 0, subject, action, map[string]string{}, uint32(1), "").Store(&res)
		if err == nil {
			if res.Authorized {
				return nil
			}
			return ErrDenied
		}
	}
	if action == ActionOwnData {
		return nil
	}
	if c.IsAdmin != nil && c.IsAdmin(caller.UID) {
		return nil
	}
	return ErrDenied
}
