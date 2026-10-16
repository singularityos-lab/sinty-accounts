package store

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const UserGroup = "User"

type Record struct {
	groups map[string]map[string]string
}

func NewRecord() *Record {
	return &Record{groups: map[string]map[string]string{}}
}

func (r *Record) Get(group, key string) string {
	return r.groups[group][key]
}

func (r *Record) Has(group, key string) bool {
	_, ok := r.groups[group][key]
	return ok
}

func (r *Record) Set(group, key, value string) {
	g := r.groups[group]
	if g == nil {
		g = map[string]string{}
		r.groups[group] = g
	}
	g[key] = value
}

func (r *Record) Delete(group, key string) {
	delete(r.groups[group], key)
}

func (r *Record) Keys(group string) []string {
	var out []string
	for k := range r.groups[group] {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *Record) Groups() []string {
	var out []string
	for g := range r.groups {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

func Parse(in io.Reader) (*Record, error) {
	r := NewRecord()
	group := ""
	s := bufio.NewScanner(in)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			group = line[1 : len(line)-1]
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || group == "" {
			continue
		}
		r.Set(group, strings.TrimSpace(k), unescape(strings.TrimSpace(v)))
	}
	return r, s.Err()
}

func (r *Record) Write(w io.Writer) error {
	bw := bufio.NewWriter(w)
	groups := r.Groups()
	sort.SliceStable(groups, func(i, j int) bool { return groups[i] == UserGroup && groups[j] != UserGroup })
	for i, g := range groups {
		if len(r.groups[g]) == 0 {
			continue
		}
		if i > 0 {
			fmt.Fprintln(bw)
		}
		fmt.Fprintf(bw, "[%s]\n", g)
		for _, k := range r.Keys(g) {
			fmt.Fprintf(bw, "%s=%s\n", k, escape(r.groups[g][k]))
		}
	}
	return bw.Flush()
}

func escape(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

func unescape(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] == '\\' && i+1 < len(v) {
			i++
			switch v[i] {
			case 'n':
				b.WriteByte('\n')
			case 's':
				b.WriteByte(' ')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(v[i])
			}
			continue
		}
		b.WriteByte(v[i])
	}
	return b.String()
}

type Store struct {
	Dir   string
	Icons string
}

func New(root string) *Store {
	base := filepath.Join(root, "var", "lib", "AccountsService")
	return &Store{Dir: filepath.Join(base, "users"), Icons: filepath.Join(base, "icons")}
}

func (s *Store) file(name string) string {
	return filepath.Join(s.Dir, name)
}

func (s *Store) Load(name string) (*Record, error) {
	f, err := os.Open(s.file(name))
	if os.IsNotExist(err) {
		return NewRecord(), nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

func (s *Store) Save(name string, r *Record) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, "."+name+".")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := r.Write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.file(name))
}

func (s *Store) Rename(old, name string) error {
	for _, dir := range []string{s.Dir, s.Icons} {
		err := os.Rename(filepath.Join(dir, old), filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *Store) Remove(name string) error {
	for _, dir := range []string{s.Dir, s.Icons} {
		err := os.Remove(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *Store) IconPath(name string) string {
	return filepath.Join(s.Icons, name)
}

func (s *Store) InstallIcon(name string, data []byte) (string, error) {
	if err := os.MkdirAll(s.Icons, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(s.Icons, "."+name+".")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	path := s.IconPath(name)
	return path, os.Rename(tmp.Name(), path)
}
