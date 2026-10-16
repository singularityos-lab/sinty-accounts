package api

import (
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	propsIface = "org.freedesktop.DBus.Properties"
	errPrefix  = "org.freedesktop.Accounts.Error."
)

func failed(msg string) *dbus.Error {
	return dbus.NewError(errPrefix+"Failed", []interface{}{msg})
}

func denied() *dbus.Error {
	return dbus.NewError(errPrefix+"PermissionDenied", []interface{}{"Not authorized"})
}

func notFound(msg string) *dbus.Error {
	return dbus.NewError(errPrefix+"UserDoesNotExist", []interface{}{msg})
}

func unknownProperty(iface, name string) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []interface{}{iface + "." + name})
}

type propDef struct {
	Name     string
	Sig      string
	Writable bool
}

func propsNode(defs []propDef) []introspect.Property {
	out := make([]introspect.Property, 0, len(defs))
	for _, d := range defs {
		access := "read"
		if d.Writable {
			access = "readwrite"
		}
		out = append(out, introspect.Property{Name: d.Name, Type: d.Sig, Access: access})
	}
	return out
}

func diff(before, after map[string]dbus.Variant) map[string]dbus.Variant {
	changed := map[string]dbus.Variant{}
	for k, v := range after {
		old, ok := before[k]
		if !ok || old.String() != v.String() {
			changed[k] = v
		}
	}
	return changed
}

var propsIntrospection = introspect.Interface{
	Name: propsIface,
	Methods: []introspect.Method{
		{Name: "Get", Args: []introspect.Arg{{Name: "interface_name", Type: "s", Direction: "in"}, {Name: "property_name", Type: "s", Direction: "in"}, {Name: "value", Type: "v", Direction: "out"}}},
		{Name: "GetAll", Args: []introspect.Arg{{Name: "interface_name", Type: "s", Direction: "in"}, {Name: "properties", Type: "a{sv}", Direction: "out"}}},
		{Name: "Set", Args: []introspect.Arg{{Name: "interface_name", Type: "s", Direction: "in"}, {Name: "property_name", Type: "s", Direction: "in"}, {Name: "value", Type: "v", Direction: "in"}}},
	},
	Signals: []introspect.Signal{
		{Name: "PropertiesChanged", Args: []introspect.Arg{{Name: "interface_name", Type: "s"}, {Name: "changed_properties", Type: "a{sv}"}, {Name: "invalidated_properties", Type: "as"}}},
	},
}
