package accounts

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/singularityos-lab/libsingularity/src/system/core/go/gvariant"
)

type extension struct {
	Iface string
	Props []propDef
	Defs  map[string]dbus.Variant
}

type appFilter struct {
	Allow bool
	Apps  []string
}

type oarsFilter struct {
	Kind   string
	Values map[string]string
}

type schedule struct {
	Start uint32
	End   uint32
}

var builtinExtensions = []extension{
	{
		Iface: "com.endlessm.ParentalControls.AppFilter",
		Props: []propDef{{"AppFilter", "(bas)", true}, {"OarsFilter", "(sa{ss})", true}, {"AllowUserInstallation", "b", true}, {"AllowSystemInstallation", "b", true}},
		Defs: map[string]dbus.Variant{
			"AppFilter":               dbus.MakeVariant(appFilter{false, []string{}}),
			"OarsFilter":              dbus.MakeVariant(oarsFilter{"oars-1.1", map[string]string{}}),
			"AllowUserInstallation":   dbus.MakeVariant(true),
			"AllowSystemInstallation": dbus.MakeVariant(false),
		},
	},
	{
		Iface: "com.endlessm.ParentalControls.SessionLimits",
		Props: []propDef{{"LimitType", "u", true}, {"DailySchedule", "(uu)", true}, {"DailyLimit", "u", true}},
		Defs: map[string]dbus.Variant{
			"LimitType":     dbus.MakeVariant(uint32(0)),
			"DailySchedule": dbus.MakeVariant(schedule{0, 86400}),
			"DailyLimit":    dbus.MakeVariant(uint32(86400)),
		},
	},
	{
		Iface: "com.endlessm.ParentalControls.AccountInfo",
		Props: []propDef{{"IsParent", "b", true}},
		Defs:  map[string]dbus.Variant{"IsParent": dbus.MakeVariant(false)},
	},
}

type xmlNode struct {
	Interfaces []struct {
		Name  string `xml:"name,attr"`
		Props []struct {
			Name   string `xml:"name,attr"`
			Type   string `xml:"type,attr"`
			Access string `xml:"access,attr"`
		} `xml:"property"`
	} `xml:"interface"`
}

func loadExtensions(dir string) []extension {
	out := append([]extension(nil), builtinExtensions...)
	seen := map[string]bool{}
	for _, e := range out {
		seen[e.Iface] = true
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.xml"))
	sort.Strings(files)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil || len(data) > 1<<20 {
			continue
		}
		var n xmlNode
		if xml.Unmarshal(data, &n) != nil {
			continue
		}
		for _, i := range n.Interfaces {
			if seen[i.Name] || !strings.Contains(i.Name, ".") {
				continue
			}
			e := extension{Iface: i.Name, Defs: map[string]dbus.Variant{}}
			for _, p := range i.Props {
				if _, err := dbus.ParseSignature(p.Type); err != nil || p.Name == "" {
					continue
				}
				e.Props = append(e.Props, propDef{Name: p.Name, Sig: p.Type, Writable: strings.Contains(p.Access, "write")})
			}
			if len(e.Props) > 0 {
				seen[i.Name] = true
				out = append(out, e)
			}
		}
	}
	return out
}

func (e *extension) def(name string) (propDef, bool) {
	for _, p := range e.Props {
		if p.Name == name {
			return p, true
		}
	}
	return propDef{}, false
}

const encodedPrefix = "dbus:"

func encodeValue(v dbus.Variant) (string, error) {
	msg := &dbus.Message{
		Type: dbus.TypeSignal,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldPath:      dbus.MakeVariant(dbus.ObjectPath("/v")),
			dbus.FieldInterface: dbus.MakeVariant("v.v"),
			dbus.FieldMember:    dbus.MakeVariant("v"),
			dbus.FieldSignature: dbus.MakeVariant(dbus.SignatureOf(v)),
		},
		Body: []interface{}{v},
	}
	var buf bytes.Buffer
	if err := msg.EncodeTo(&buf, binary.LittleEndian); err != nil {
		return "", err
	}
	return encodedPrefix + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func decodeValue(s string) (dbus.Variant, bool) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, encodedPrefix))
	if err != nil || !strings.HasPrefix(s, encodedPrefix) {
		return dbus.Variant{}, false
	}
	msg, err := dbus.DecodeMessage(bytes.NewReader(raw))
	if err != nil || len(msg.Body) != 1 {
		return dbus.Variant{}, false
	}
	v, ok := msg.Body[0].(dbus.Variant)
	if !ok {
		return dbus.Variant{}, false
	}
	n, err := normalize(v)
	return n, err == nil
}

func (e *extension) value(stored, name string) dbus.Variant {
	p, _ := e.def(name)
	sig := dbus.ParseSignatureMust(p.Sig)
	if v, ok := decodeValue(stored); ok && v.Signature().String() == p.Sig {
		return v
	}
	if stored != "" {
		if v, err := gvariant.Parse(stored, sig); err == nil {
			return v
		}
	}
	if d, ok := e.Defs[name]; ok {
		return d
	}
	return zero(sig)
}

func zero(sig dbus.Signature) dbus.Variant {
	switch sig.String() {
	case "b":
		return dbus.MakeVariant(false)
	case "s":
		return dbus.MakeVariant("")
	case "u":
		return dbus.MakeVariant(uint32(0))
	case "i":
		return dbus.MakeVariant(int32(0))
	case "t":
		return dbus.MakeVariant(uint64(0))
	case "x":
		return dbus.MakeVariant(int64(0))
	case "d":
		return dbus.MakeVariant(float64(0))
	case "as":
		return dbus.MakeVariant([]string{})
	}
	if v, err := dbus.ParseVariant("@"+sig.String()+" []", sig); err == nil {
		return v
	}
	return dbus.MakeVariant("")
}
