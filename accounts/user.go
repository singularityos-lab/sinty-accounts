package accounts

import (
	"fmt"
	"os"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/singularityos-lab/sinty-accounts/internal/auth"
	"github.com/singularityos-lab/sinty-accounts/internal/store"
	"github.com/singularityos-lab/sinty-accounts/internal/users"
)

const (
	UserIface   = "org.freedesktop.Accounts.User"
	maxIconSize = 1 << 20
)

var userProps = []propDef{
	{"Uid", "t", false}, {"UserName", "s", false}, {"RealName", "s", false}, {"AccountType", "i", false},
	{"HomeDirectory", "s", false}, {"Shell", "s", false}, {"Email", "s", false}, {"Language", "s", false},
	{"FormatsLocale", "s", false}, {"Session", "s", false}, {"SessionType", "s", false}, {"XSession", "s", false},
	{"Location", "s", false}, {"LoginFrequency", "t", false}, {"LoginTime", "x", false},
	{"LoginHistory", "a(xxa{sv})", false}, {"XHasMessages", "b", false}, {"XKeyboardLayouts", "as", false},
	{"BackgroundFile", "s", false}, {"IconFile", "s", false}, {"Saved", "b", false}, {"Locked", "b", false},
	{"PasswordMode", "i", false}, {"PasswordHint", "s", false}, {"AutomaticLogin", "b", false},
	{"SystemAccount", "b", false}, {"LocalAccount", "b", false}, {"InputSources", "aa{ss}", false},
}

var userMethods = []introspect.Method{
	{Name: "SetUserName", Args: []introspect.Arg{{Name: "name", Type: "s", Direction: "in"}}},
	{Name: "SetRealName", Args: []introspect.Arg{{Name: "name", Type: "s", Direction: "in"}}},
	{Name: "SetEmail", Args: []introspect.Arg{{Name: "email", Type: "s", Direction: "in"}}},
	{Name: "SetLanguage", Args: []introspect.Arg{{Name: "language", Type: "s", Direction: "in"}}},
	{Name: "SetFormatsLocale", Args: []introspect.Arg{{Name: "formats_locale", Type: "s", Direction: "in"}}},
	{Name: "SetInputSources", Args: []introspect.Arg{{Name: "sources", Type: "aa{ss}", Direction: "in"}}},
	{Name: "SetXSession", Args: []introspect.Arg{{Name: "x_session", Type: "s", Direction: "in"}}},
	{Name: "SetSession", Args: []introspect.Arg{{Name: "session", Type: "s", Direction: "in"}}},
	{Name: "SetSessionType", Args: []introspect.Arg{{Name: "session_type", Type: "s", Direction: "in"}}},
	{Name: "SetLocation", Args: []introspect.Arg{{Name: "location", Type: "s", Direction: "in"}}},
	{Name: "SetHomeDirectory", Args: []introspect.Arg{{Name: "homedir", Type: "s", Direction: "in"}}},
	{Name: "SetShell", Args: []introspect.Arg{{Name: "shell", Type: "s", Direction: "in"}}},
	{Name: "SetXHasMessages", Args: []introspect.Arg{{Name: "has_messages", Type: "b", Direction: "in"}}},
	{Name: "SetXKeyboardLayouts", Args: []introspect.Arg{{Name: "layouts", Type: "as", Direction: "in"}}},
	{Name: "SetBackgroundFile", Args: []introspect.Arg{{Name: "filename", Type: "s", Direction: "in"}}},
	{Name: "SetIconFile", Args: []introspect.Arg{{Name: "filename", Type: "s", Direction: "in"}}},
	{Name: "SetLocked", Args: []introspect.Arg{{Name: "locked", Type: "b", Direction: "in"}}},
	{Name: "SetAccountType", Args: []introspect.Arg{{Name: "accountType", Type: "i", Direction: "in"}}},
	{Name: "SetPasswordMode", Args: []introspect.Arg{{Name: "mode", Type: "i", Direction: "in"}}},
	{Name: "SetPassword", Args: []introspect.Arg{{Name: "password", Type: "s", Direction: "in"}, {Name: "hint", Type: "s", Direction: "in"}}},
	{Name: "SetPasswordHint", Args: []introspect.Arg{{Name: "hint", Type: "s", Direction: "in"}}},
	{Name: "SetAutomaticLogin", Args: []introspect.Arg{{Name: "enabled", Type: "b", Direction: "in"}}},
	{Name: "GetPasswordExpirationPolicy", Args: []introspect.Arg{
		{Name: "expiration_time", Type: "x", Direction: "out"}, {Name: "last_change_time", Type: "x", Direction: "out"},
		{Name: "min_days_between_changes", Type: "x", Direction: "out"}, {Name: "max_days_between_changes", Type: "x", Direction: "out"},
		{Name: "days_to_warn", Type: "x", Direction: "out"}, {Name: "days_after_expiration_until_lock", Type: "x", Direction: "out"}}},
}

type User struct {
	svc  *Service
	path dbus.ObjectPath
	uid  uint32
	last map[string]map[string]dbus.Variant
}

func userPath(uid uint32) dbus.ObjectPath {
	return dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/Accounts/User%d", uid))
}

func (u *User) entry() (*users.User, *store.Record, *dbus.Error) {
	e := u.svc.lookup(u.uid)
	if e == nil {
		return nil, nil, notFound("the user no longer exists")
	}
	r, err := u.svc.store.Load(e.Name)
	if err != nil {
		return nil, nil, failed(err.Error())
	}
	return e, r, nil
}

func (u *User) snapshot(e *users.User, r *store.Record) map[string]map[string]dbus.Variant {
	icon := r.Get(store.UserGroup, "Icon")
	if icon == "" {
		if _, err := os.Stat(u.svc.store.IconPath(e.Name)); err == nil {
			icon = u.svc.store.IconPath(e.Name)
		}
	}
	accountType := int32(0)
	if e.Admin {
		accountType = 1
	}
	session := r.Get(store.UserGroup, "Session")
	xsession := r.Get(store.UserGroup, "XSession")
	if session == "" {
		session = xsession
	}
	main := map[string]dbus.Variant{
		"Uid":            dbus.MakeVariant(uint64(e.UID)),
		"UserName":       dbus.MakeVariant(e.Name),
		"RealName":       dbus.MakeVariant(e.RealName),
		"AccountType":    dbus.MakeVariant(accountType),
		"HomeDirectory":  dbus.MakeVariant(e.Home),
		"Shell":          dbus.MakeVariant(e.Shell),
		"Email":          dbus.MakeVariant(r.Get(store.UserGroup, "Email")),
		"Language":       dbus.MakeVariant(r.Get(store.UserGroup, "Language")),
		"FormatsLocale":  dbus.MakeVariant(r.Get(store.UserGroup, "FormatsLocale")),
		"Session":        dbus.MakeVariant(session),
		"SessionType":    dbus.MakeVariant(r.Get(store.UserGroup, "SessionType")),
		"XSession":       dbus.MakeVariant(xsession),
		"Location":       dbus.MakeVariant(r.Get(store.UserGroup, "Location")),
		"LoginFrequency": dbus.MakeVariant(uint64(0)),
		"LoginTime":      dbus.MakeVariant(int64(0)),
		"LoginHistory": dbus.MakeVariant([]struct {
			A, B int64
			C    map[string]dbus.Variant
		}{}),
		"XHasMessages":     dbus.MakeVariant(r.Get(store.UserGroup, "XHasMessages") == "true"),
		"XKeyboardLayouts": dbus.MakeVariant(splitList(r.Get(store.UserGroup, "XKeyboardLayouts"))),
		"BackgroundFile":   dbus.MakeVariant(r.Get(store.UserGroup, "BackgroundFile")),
		"IconFile":         dbus.MakeVariant(icon),
		"Saved":            dbus.MakeVariant(true),
		"Locked":           dbus.MakeVariant(e.Locked()),
		"PasswordMode":     dbus.MakeVariant(e.PasswordMode()),
		"PasswordHint":     dbus.MakeVariant(r.Get(store.UserGroup, "PasswordHint")),
		"AutomaticLogin":   dbus.MakeVariant(r.Get(store.UserGroup, "AutomaticLogin") == "true"),
		"SystemAccount":    dbus.MakeVariant(r.Get(store.UserGroup, "SystemAccount") == "true"),
		"LocalAccount":     dbus.MakeVariant(true),
		"InputSources":     dbus.MakeVariant(inputSources(r.Get(store.UserGroup, "InputSources"))),
	}
	out := map[string]map[string]dbus.Variant{UserIface: main}
	for i := range u.svc.extensions {
		ext := &u.svc.extensions[i]
		m := map[string]dbus.Variant{}
		for _, p := range ext.Props {
			m[p.Name] = ext.value(r.Get(ext.Iface, p.Name), p.Name)
		}
		out[ext.Iface] = m
	}
	return out
}

func splitList(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ";") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func inputSources(s string) []map[string]string {
	out := []map[string]string{}
	for _, p := range splitList(s) {
		kind, id, ok := strings.Cut(p, ":")
		if ok {
			out = append(out, map[string]string{kind: id})
		}
	}
	return out
}

func (u *User) refresh() {
	e, r, err := u.entry()
	if err != nil {
		return
	}
	now := u.snapshot(e, r)
	for iface, props := range now {
		changed := diff(u.last[iface], props)
		if len(changed) > 0 && u.last != nil {
			u.svc.conn.Emit(u.path, propsIface+".PropertiesChanged", iface, changed, []string{})
		}
	}
	if u.last != nil {
		for k, v := range now[UserIface] {
			if old, ok := u.last[UserIface][k]; !ok || old.String() != v.String() {
				u.svc.conn.Emit(u.path, UserIface+".Changed")
				break
			}
		}
	}
	u.last = now
}

func (u *User) introspection() string {
	node := &introspect.Node{
		Name: string(u.path),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			propsIntrospection,
			{Name: UserIface, Methods: userMethods, Signals: []introspect.Signal{{Name: "Changed"}}, Properties: propsNode(userProps)},
		},
	}
	for _, ext := range u.svc.extensions {
		node.Interfaces = append(node.Interfaces, introspect.Interface{Name: ext.Iface, Properties: propsNode(ext.Props)})
	}
	return string(introspect.NewIntrospectable(node))
}

func (u *User) Get(iface, name string, sender dbus.Sender) (dbus.Variant, *dbus.Error) {
	all, err := u.GetAll(iface, sender)
	if err != nil {
		return dbus.Variant{}, err
	}
	v, ok := all[name]
	if !ok {
		return dbus.Variant{}, unknownProperty(iface, name)
	}
	return v, nil
}

func (u *User) GetAll(iface string, sender dbus.Sender) (map[string]dbus.Variant, *dbus.Error) {
	u.svc.mu.Lock()
	e, r, err := u.entry()
	if err != nil {
		u.svc.mu.Unlock()
		return nil, err
	}
	props, ok := u.snapshot(e, r)[iface]
	u.svc.mu.Unlock()
	if !ok {
		return nil, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []interface{}{iface})
	}
	if iface != UserIface {
		caller, cerr := u.svc.auth.Caller(sender)
		if cerr != nil || (caller.UID != e.UID && u.svc.auth.Check(caller, auth.ActionAdminister) != nil) {
			return nil, denied()
		}
	}
	return props, nil
}

func (u *User) Set(iface, name string, value dbus.Variant, sender dbus.Sender) *dbus.Error {
	if iface == UserIface {
		return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []interface{}{name})
	}
	var ext *extension
	for i := range u.svc.extensions {
		if u.svc.extensions[i].Iface == iface {
			ext = &u.svc.extensions[i]
		}
	}
	if ext == nil {
		return unknownProperty(iface, name)
	}
	def, ok := ext.def(name)
	if !ok {
		return unknownProperty(iface, name)
	}
	if !def.Writable {
		return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []interface{}{name})
	}
	if value.Signature().String() != def.Sig {
		return dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []interface{}{"wrong type for " + name})
	}
	typed, terr := normalize(value)
	if terr != nil {
		return dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []interface{}{terr.Error()})
	}
	encoded, err := encodeValue(typed)
	if err != nil {
		return failed(err.Error())
	}
	return u.mutate(sender, auth.ActionAdminister, true, func(e *users.User, r *store.Record) error {
		r.Set(iface, name, encoded)
		return nil
	})
}

func (u *User) mutate(sender dbus.Sender, action string, adminOnly bool, fn func(*users.User, *store.Record) error) *dbus.Error {
	caller, err := u.svc.auth.Caller(sender)
	if err != nil {
		return failed(err.Error())
	}
	u.svc.mu.Lock()
	defer u.svc.mu.Unlock()
	e, r, derr := u.entry()
	if derr != nil {
		return derr
	}
	if adminOnly || caller.UID != e.UID {
		action = auth.ActionAdminister
	}
	u.svc.mu.Unlock()
	aerr := u.svc.auth.Check(caller, action)
	u.svc.mu.Lock()
	if aerr != nil {
		return denied()
	}
	e, r, derr = u.entry()
	if derr != nil {
		return derr
	}
	if err := fn(e, r); err != nil {
		return failed(err.Error())
	}
	if err := u.svc.store.Save(e.Name, r); err != nil {
		return failed(err.Error())
	}
	u.svc.reloadLocked()
	return nil
}

func (u *User) setKey(sender dbus.Sender, key, value string, adminOnly bool) *dbus.Error {
	if len(value) > 4096 || strings.ContainsAny(value, "\n") {
		return failed("invalid value for " + key)
	}
	return u.mutate(sender, auth.ActionOwnData, adminOnly, func(e *users.User, r *store.Record) error {
		r.Set(store.UserGroup, key, value)
		return nil
	})
}

func (u *User) SetUserName(name string, sender dbus.Sender) *dbus.Error {
	return u.mutate(sender, auth.ActionAdminister, true, func(e *users.User, r *store.Record) error {
		if e.Name == name {
			return nil
		}
		if err := u.svc.tools.Rename(e.Name, name); err != nil {
			return err
		}
		return u.svc.store.Rename(e.Name, name)
	})
}

func (u *User) SetRealName(name string, sender dbus.Sender) *dbus.Error {
	return u.mutate(sender, auth.ActionOwnData, false, func(e *users.User, r *store.Record) error {
		if e.RealName == name {
			return nil
		}
		return u.svc.tools.SetRealName(e.Name, name)
	})
}

func (u *User) SetEmail(v string, sender dbus.Sender) *dbus.Error {
	return u.setKey(sender, "Email", v, false)
}

func (u *User) SetLanguage(v string, sender dbus.Sender) *dbus.Error {
	return u.setKey(sender, "Language", v, false)
}

func (u *User) SetFormatsLocale(v string, sender dbus.Sender) *dbus.Error {
	return u.setKey(sender, "FormatsLocale", v, false)
}

func (u *User) SetInputSources(v []map[string]string, sender dbus.Sender) *dbus.Error {
	var parts []string
	for _, m := range v {
		for k, id := range m {
			if strings.ContainsAny(k+id, ";:\n") {
				return failed("invalid input source")
			}
			parts = append(parts, k+":"+id)
		}
	}
	return u.setKey(sender, "InputSources", strings.Join(parts, ";"), false)
}

func (u *User) SetXSession(v string, sender dbus.Sender) *dbus.Error {
	return u.setKey(sender, "XSession", v, false)
}

func (u *User) SetSession(v string, sender dbus.Sender) *dbus.Error {
	return u.setKey(sender, "Session", v, false)
}

func (u *User) SetSessionType(v string, sender dbus.Sender) *dbus.Error {
	return u.setKey(sender, "SessionType", v, false)
}

func (u *User) SetLocation(v string, sender dbus.Sender) *dbus.Error {
	return u.setKey(sender, "Location", v, false)
}

func (u *User) SetXHasMessages(v bool, sender dbus.Sender) *dbus.Error {
	return u.setKey(sender, "XHasMessages", fmt.Sprint(v), false)
}

func (u *User) SetXKeyboardLayouts(v []string, sender dbus.Sender) *dbus.Error {
	for _, l := range v {
		if strings.Contains(l, ";") {
			return failed("invalid keyboard layout")
		}
	}
	return u.setKey(sender, "XKeyboardLayouts", strings.Join(v, ";"), false)
}

func (u *User) SetBackgroundFile(v string, sender dbus.Sender) *dbus.Error {
	return u.setKey(sender, "BackgroundFile", v, false)
}

func (u *User) SetPasswordHint(v string, sender dbus.Sender) *dbus.Error {
	return u.setKey(sender, "PasswordHint", v, false)
}

func (u *User) SetAutomaticLogin(enabled bool, sender dbus.Sender) *dbus.Error {
	return u.mutate(sender, auth.ActionAdminister, true, func(e *users.User, r *store.Record) error {
		if enabled {
			for _, other := range u.svc.entries {
				if other.UID == e.UID {
					continue
				}
				or, err := u.svc.store.Load(other.Name)
				if err != nil || or.Get(store.UserGroup, "AutomaticLogin") != "true" {
					continue
				}
				or.Delete(store.UserGroup, "AutomaticLogin")
				if err := u.svc.store.Save(other.Name, or); err != nil {
					return err
				}
			}
			r.Set(store.UserGroup, "AutomaticLogin", "true")
		} else {
			r.Delete(store.UserGroup, "AutomaticLogin")
		}
		return nil
	})
}

func (u *User) SetHomeDirectory(home string, sender dbus.Sender) *dbus.Error {
	return u.mutate(sender, auth.ActionAdminister, true, func(e *users.User, r *store.Record) error {
		if e.Home == home {
			return nil
		}
		return u.svc.tools.SetHome(e.Name, home)
	})
}

func (u *User) SetShell(shell string, sender dbus.Sender) *dbus.Error {
	return u.mutate(sender, auth.ActionAdminister, true, func(e *users.User, r *store.Record) error {
		if e.Shell == shell {
			return nil
		}
		return u.svc.tools.SetShell(e.Name, shell)
	})
}

func (u *User) SetLocked(locked bool, sender dbus.Sender) *dbus.Error {
	return u.mutate(sender, auth.ActionAdminister, true, func(e *users.User, r *store.Record) error {
		if e.Locked() == locked {
			return nil
		}
		return u.svc.tools.SetLocked(e.Name, locked)
	})
}

func (u *User) SetAccountType(t int32, sender dbus.Sender) *dbus.Error {
	if t != 0 && t != 1 {
		return failed("unknown account type")
	}
	return u.mutate(sender, auth.ActionAdminister, true, func(e *users.User, r *store.Record) error {
		if e.Admin == (t == 1) {
			return nil
		}
		if e.UID == 0 {
			return fmt.Errorf("the root account is always an administrator")
		}
		return u.svc.tools.SetAdmin(e.Name, t == 1)
	})
}

func (u *User) SetPasswordMode(mode int32, sender dbus.Sender) *dbus.Error {
	return u.mutate(sender, auth.ActionAdminister, true, func(e *users.User, r *store.Record) error {
		switch mode {
		case 0:
			return nil
		case 1:
			return u.svc.tools.ExpireNow(e.Name)
		case 2:
			return u.svc.tools.SetHash(e.Name, "")
		}
		return fmt.Errorf("unknown password mode")
	})
}

func (u *User) SetPassword(hash, hint string, sender dbus.Sender) *dbus.Error {
	if strings.ContainsAny(hint, "\n") || len(hint) > 4096 {
		return failed("invalid password hint")
	}
	return u.mutate(sender, auth.ActionOwnPassword, false, func(e *users.User, r *store.Record) error {
		if err := u.svc.tools.SetHash(e.Name, hash); err != nil {
			return err
		}
		if hint == "" {
			r.Delete(store.UserGroup, "PasswordHint")
		} else {
			r.Set(store.UserGroup, "PasswordHint", hint)
		}
		return nil
	})
}

func (u *User) SetIconFile(path string, sender dbus.Sender) *dbus.Error {
	caller, err := u.svc.auth.Caller(sender)
	if err != nil {
		return failed(err.Error())
	}
	var data []byte
	if path != "" {
		d, rerr := readIcon(path, caller.UID, u.svc.auth.Check(caller, auth.ActionAdminister) == nil)
		if rerr != nil {
			return failed(rerr.Error())
		}
		data = d
	}
	return u.mutate(sender, auth.ActionOwnData, false, func(e *users.User, r *store.Record) error {
		if data == nil {
			r.Delete(store.UserGroup, "Icon")
			if err := os.Remove(u.svc.store.IconPath(e.Name)); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		installed, err := u.svc.store.InstallIcon(e.Name, data)
		if err != nil {
			return err
		}
		r.Set(store.UserGroup, "Icon", installed)
		return nil
	})
}

func (u *User) GetPasswordExpirationPolicy(sender dbus.Sender) (int64, int64, int64, int64, int64, int64, *dbus.Error) {
	caller, err := u.svc.auth.Caller(sender)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, failed(err.Error())
	}
	u.svc.mu.Lock()
	e := u.svc.lookup(u.uid)
	u.svc.mu.Unlock()
	if e == nil {
		return 0, 0, 0, 0, 0, 0, notFound("the user no longer exists")
	}
	if caller.UID != e.UID && u.svc.auth.Check(caller, auth.ActionAdminister) != nil {
		return 0, 0, 0, 0, 0, 0, denied()
	}
	a := e.Aging
	return a[5], a[0], a[1], a[2], a[3], a[4], nil
}
