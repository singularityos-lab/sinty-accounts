package accounts

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/singularityos-lab/sinty-accounts/internal/auth"
	"github.com/singularityos-lab/sinty-accounts/internal/store"
	"github.com/singularityos-lab/sinty-accounts/internal/users"
)

const (
	BusName      = "org.freedesktop.Accounts"
	ManagerPath  = dbus.ObjectPath("/org/freedesktop/Accounts")
	ManagerIface = "org.freedesktop.Accounts"
	Version      = "0.1.0"
)

var managerProps = []propDef{
	{"DaemonVersion", "s", false}, {"HasNoUsers", "b", false}, {"HasMultipleUsers", "b", false}, {"AutomaticLoginUsers", "ao", false},
}

var managerMethods = []introspect.Method{
	{Name: "ListCachedUsers", Args: []introspect.Arg{{Name: "users", Type: "ao", Direction: "out"}}},
	{Name: "FindUserById", Args: []introspect.Arg{{Name: "id", Type: "x", Direction: "in"}, {Name: "user", Type: "o", Direction: "out"}}},
	{Name: "FindUserByName", Args: []introspect.Arg{{Name: "name", Type: "s", Direction: "in"}, {Name: "user", Type: "o", Direction: "out"}}},
	{Name: "CreateUser", Args: []introspect.Arg{{Name: "name", Type: "s", Direction: "in"}, {Name: "fullname", Type: "s", Direction: "in"}, {Name: "accountType", Type: "i", Direction: "in"}, {Name: "user", Type: "o", Direction: "out"}}},
	{Name: "CacheUser", Args: []introspect.Arg{{Name: "name", Type: "s", Direction: "in"}, {Name: "user", Type: "o", Direction: "out"}}},
	{Name: "UncacheUser", Args: []introspect.Arg{{Name: "name", Type: "s", Direction: "in"}}},
	{Name: "DeleteUser", Args: []introspect.Arg{{Name: "id", Type: "x", Direction: "in"}, {Name: "removeFiles", Type: "b", Direction: "in"}}},
	{Name: "GetUsersLanguages", Args: []introspect.Arg{{Name: "languages", Type: "as", Direction: "out"}}},
}

type Options struct {
	Root          string
	InterfacesDir string
	Run           users.Runner
}

type Service struct {
	conn       *dbus.Conn
	db         *users.Database
	tools      *users.Tools
	store      *store.Store
	auth       *auth.Checker
	extensions []extension

	mu      sync.Mutex
	all     []*users.User
	entries []*users.User
	objects map[uint32]*User
	lastMgr map[string]dbus.Variant
	stamp   string
}

func New(conn *dbus.Conn, opt Options) *Service {
	db := users.New(opt.Root)
	run := opt.Run
	if run == nil {
		run = users.Exec
	}
	s := &Service{
		conn:       conn,
		db:         db,
		tools:      &users.Tools{Run: run, DB: db},
		store:      store.New(opt.Root),
		extensions: loadExtensions(opt.InterfacesDir),
		objects:    map[uint32]*User{},
	}
	s.auth = &auth.Checker{Conn: conn, IsAdmin: s.isAdmin}
	return s
}

func (s *Service) Export() error {
	if err := s.conn.ExportMethodTable(map[string]interface{}{
		"ListCachedUsers":   s.ListCachedUsers,
		"FindUserById":      s.FindUserById,
		"FindUserByName":    s.FindUserByName,
		"CreateUser":        s.CreateUser,
		"CacheUser":         s.CacheUser,
		"UncacheUser":       s.UncacheUser,
		"DeleteUser":        s.DeleteUser,
		"GetUsersLanguages": s.GetUsersLanguages,
	}, ManagerPath, ManagerIface); err != nil {
		return err
	}
	if err := s.conn.ExportMethodTable(map[string]interface{}{
		"Get":    s.getManagerProp,
		"GetAll": s.getManagerProps,
		"Set":    s.setManagerProp,
	}, ManagerPath, propsIface); err != nil {
		return err
	}
	node := &introspect.Node{
		Name: string(ManagerPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			propsIntrospection,
			{
				Name:       ManagerIface,
				Methods:    managerMethods,
				Signals:    []introspect.Signal{{Name: "UserAdded", Args: []introspect.Arg{{Name: "user", Type: "o"}}}, {Name: "UserDeleted", Args: []introspect.Arg{{Name: "user", Type: "o"}}}},
				Properties: propsNode(managerProps),
			},
		},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.conn.Export(introspect.NewIntrospectable(node), ManagerPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	s.reloadLocked()
	return nil
}

func (s *Service) Watch(stop <-chan struct{}) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			stamp := s.fileStamp()
			s.mu.Lock()
			if stamp != s.stamp {
				s.reloadLocked()
			}
			s.mu.Unlock()
		}
	}
}

func (s *Service) fileStamp() string {
	out := ""
	for _, name := range []string{"passwd", "shadow", "group"} {
		if info, err := os.Stat(filepath.Join(s.db.Root, "etc", name)); err == nil {
			out += fmt.Sprintf("%s:%d:%d;", name, info.ModTime().UnixNano(), info.Size())
		}
	}
	return out
}

func (s *Service) isAdmin(uid uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.all {
		if u.UID == uid {
			return u.Admin
		}
	}
	return false
}

func (s *Service) lookup(uid uint32) *users.User {
	for _, u := range s.all {
		if u.UID == uid {
			return u
		}
	}
	return nil
}

func (s *Service) lookupName(name string) *users.User {
	for _, u := range s.all {
		if u.Name == name {
			return u
		}
	}
	return nil
}

func (s *Service) reloadLocked() {
	s.stamp = s.fileStamp()
	all, err := s.db.Load()
	if err != nil {
		return
	}
	s.all = all
	var humans []*users.User
	keep := map[uint32]bool{}
	for _, u := range all {
		if u.Human(s.db.UIDMin, s.db.UIDMax) {
			humans = append(humans, u)
			keep[u.UID] = true
		}
	}
	for uid, obj := range s.objects {
		if r, err := s.store.Load(s.nameOf(uid)); err == nil && r.Get(store.UserGroup, "Cached") == "true" && s.lookup(uid) != nil {
			keep[uid] = true
			continue
		}
		if !keep[uid] {
			s.unexport(obj)
			delete(s.objects, uid)
			s.conn.Emit(ManagerPath, ManagerIface+".UserDeleted", obj.path)
		}
	}
	s.entries = humans
	for _, u := range humans {
		s.ensure(u.UID)
	}
	for _, obj := range s.objects {
		obj.refresh()
	}
	now := s.managerProps()
	if changed := diff(s.lastMgr, now); len(changed) > 0 && s.lastMgr != nil {
		s.conn.Emit(ManagerPath, propsIface+".PropertiesChanged", ManagerIface, changed, []string{})
	}
	s.lastMgr = now
}

func (s *Service) nameOf(uid uint32) string {
	if u := s.lookup(uid); u != nil {
		return u.Name
	}
	return ""
}

func (s *Service) ensure(uid uint32) *User {
	if obj, ok := s.objects[uid]; ok {
		return obj
	}
	obj := &User{svc: s, path: userPath(uid), uid: uid}
	userTable := map[string]interface{}{}
	for _, m := range userMethods {
		if fn := userMethod(obj, m.Name); fn != nil {
			userTable[m.Name] = fn
		}
	}
	s.conn.ExportMethodTable(userTable, obj.path, UserIface)
	s.conn.ExportMethodTable(map[string]interface{}{"Get": obj.Get, "GetAll": obj.GetAll, "Set": obj.Set}, obj.path, propsIface)
	s.conn.Export(introspect.Introspectable(obj.introspection()), obj.path, "org.freedesktop.DBus.Introspectable")
	s.objects[uid] = obj
	s.conn.Emit(ManagerPath, ManagerIface+".UserAdded", obj.path)
	return obj
}

func userMethod(u *User, name string) interface{} {
	switch name {
	case "SetUserName":
		return u.SetUserName
	case "SetRealName":
		return u.SetRealName
	case "SetEmail":
		return u.SetEmail
	case "SetLanguage":
		return u.SetLanguage
	case "SetFormatsLocale":
		return u.SetFormatsLocale
	case "SetInputSources":
		return u.SetInputSources
	case "SetXSession":
		return u.SetXSession
	case "SetSession":
		return u.SetSession
	case "SetSessionType":
		return u.SetSessionType
	case "SetLocation":
		return u.SetLocation
	case "SetHomeDirectory":
		return u.SetHomeDirectory
	case "SetShell":
		return u.SetShell
	case "SetXHasMessages":
		return u.SetXHasMessages
	case "SetXKeyboardLayouts":
		return u.SetXKeyboardLayouts
	case "SetBackgroundFile":
		return u.SetBackgroundFile
	case "SetIconFile":
		return u.SetIconFile
	case "SetLocked":
		return u.SetLocked
	case "SetAccountType":
		return u.SetAccountType
	case "SetPasswordMode":
		return u.SetPasswordMode
	case "SetPassword":
		return u.SetPassword
	case "SetPasswordHint":
		return u.SetPasswordHint
	case "SetAutomaticLogin":
		return u.SetAutomaticLogin
	case "GetPasswordExpirationPolicy":
		return u.GetPasswordExpirationPolicy
	}
	return nil
}

func (s *Service) unexport(obj *User) {
	for _, iface := range []string{UserIface, propsIface, "org.freedesktop.DBus.Introspectable"} {
		s.conn.Export(nil, obj.path, iface)
	}
}

func (s *Service) managerProps() map[string]dbus.Variant {
	var auto []dbus.ObjectPath
	for _, u := range s.entries {
		if r, err := s.store.Load(u.Name); err == nil && r.Get(store.UserGroup, "AutomaticLogin") == "true" {
			auto = append(auto, userPath(u.UID))
		}
	}
	if auto == nil {
		auto = []dbus.ObjectPath{}
	}
	return map[string]dbus.Variant{
		"DaemonVersion":       dbus.MakeVariant(Version),
		"HasNoUsers":          dbus.MakeVariant(len(s.entries) == 0),
		"HasMultipleUsers":    dbus.MakeVariant(len(s.entries) > 1),
		"AutomaticLoginUsers": dbus.MakeVariant(auto),
	}
}

func (s *Service) getManagerProp(iface, name string) (dbus.Variant, *dbus.Error) {
	all, err := s.getManagerProps(iface)
	if err != nil {
		return dbus.Variant{}, err
	}
	v, ok := all[name]
	if !ok {
		return dbus.Variant{}, unknownProperty(iface, name)
	}
	return v, nil
}

func (s *Service) getManagerProps(iface string) (map[string]dbus.Variant, *dbus.Error) {
	if iface != ManagerIface {
		return nil, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []interface{}{iface})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.managerProps(), nil
}

func (s *Service) setManagerProp(iface, name string, v dbus.Variant) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []interface{}{name})
}

func (s *Service) ListCachedUsers() ([]dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []dbus.ObjectPath
	uids := make([]int, 0, len(s.objects))
	for uid := range s.objects {
		uids = append(uids, int(uid))
	}
	sort.Ints(uids)
	for _, uid := range uids {
		out = append(out, userPath(uint32(uid)))
	}
	if out == nil {
		out = []dbus.ObjectPath{}
	}
	return out, nil
}

func (s *Service) FindUserById(id int64) (dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id < 0 || id > int64(^uint32(0)) || s.lookup(uint32(id)) == nil {
		return "", notFound(fmt.Sprintf("Failed to look up user with uid %d.", id))
	}
	return s.ensure(uint32(id)).path, nil
}

func (s *Service) FindUserByName(name string) (dbus.ObjectPath, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.lookupName(name)
	if u == nil {
		return "", notFound(fmt.Sprintf("Failed to look up user with name %s.", name))
	}
	obj := s.ensure(u.UID)
	obj.refresh()
	return obj.path, nil
}

func (s *Service) CacheUser(name string, sender dbus.Sender) (dbus.ObjectPath, *dbus.Error) {
	if err := s.require(sender, auth.ActionAdminister); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.lookupName(name)
	if u == nil {
		return "", notFound(fmt.Sprintf("Failed to look up user with name %s.", name))
	}
	r, err := s.store.Load(u.Name)
	if err != nil {
		return "", failed(err.Error())
	}
	r.Set(store.UserGroup, "Cached", "true")
	if err := s.store.Save(u.Name, r); err != nil {
		return "", failed(err.Error())
	}
	obj := s.ensure(u.UID)
	obj.refresh()
	return obj.path, nil
}

func (s *Service) UncacheUser(name string, sender dbus.Sender) *dbus.Error {
	if err := s.require(sender, auth.ActionAdminister); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.lookupName(name)
	if u == nil {
		return notFound(fmt.Sprintf("Failed to look up user with name %s.", name))
	}
	r, err := s.store.Load(u.Name)
	if err != nil {
		return failed(err.Error())
	}
	r.Delete(store.UserGroup, "Cached")
	if err := s.store.Save(u.Name, r); err != nil {
		return failed(err.Error())
	}
	s.reloadLocked()
	return nil
}

func (s *Service) require(sender dbus.Sender, action string) *dbus.Error {
	caller, err := s.auth.Caller(sender)
	if err != nil {
		return failed(err.Error())
	}
	if s.auth.Check(caller, action) != nil {
		return denied()
	}
	return nil
}

func (s *Service) CreateUser(name, fullname string, accountType int32, sender dbus.Sender) (dbus.ObjectPath, *dbus.Error) {
	if err := s.require(sender, auth.ActionAdminister); err != nil {
		return "", err
	}
	if accountType != 0 && accountType != 1 {
		return "", failed("unknown account type")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lookupName(name) != nil {
		return "", dbus.NewError(errPrefix+"UserExists", []interface{}{fmt.Sprintf("A user with name '%s' already exists", name)})
	}
	if err := s.tools.Create(name, fullname, accountType == 1); err != nil {
		return "", failed(err.Error())
	}
	s.reloadLocked()
	u := s.lookupName(name)
	if u == nil {
		return "", failed("the new user did not appear in the user database")
	}
	return s.ensure(u.UID).path, nil
}

func (s *Service) DeleteUser(id int64, removeFiles bool, sender dbus.Sender) *dbus.Error {
	if err := s.require(sender, auth.ActionAdminister); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id <= 0 || id > int64(^uint32(0)) {
		return failed("refusing to delete this account")
	}
	u := s.lookup(uint32(id))
	if u == nil {
		return notFound(fmt.Sprintf("Failed to look up user with uid %d.", id))
	}
	if err := s.tools.Delete(u.Name, removeFiles); err != nil {
		return failed(err.Error())
	}
	if err := s.store.Remove(u.Name); err != nil {
		return failed(err.Error())
	}
	s.reloadLocked()
	return nil
}

func (s *Service) GetUsersLanguages() ([]string, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	out := []string{}
	for _, u := range s.entries {
		r, err := s.store.Load(u.Name)
		if err != nil {
			continue
		}
		if l := r.Get(store.UserGroup, "Language"); l != "" && !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out, nil
}
