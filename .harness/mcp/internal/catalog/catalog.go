package catalog

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Entry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
	Root        string `json:"root"`
	Boundary    string `json:"boundary"`
	Entry       string `json:"entry"`
}
type Content struct {
	Data     string `json:"data"`
	Encoding string `json:"encoding"`
}
type Catalog struct {
	Version   int               `json:"version"`
	Skills    []Entry           `json:"skills"`
	Personas  []Entry           `json:"personas"`
	Revisions map[string]string `json:"revisions"`
}

func Load(path string) (*Catalog, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var c Catalog
	if e = json.Unmarshal(b, &c); e != nil {
		return nil, e
	}
	if c.Version != 1 {
		return nil, fmt.Errorf("unsupported catalog version %d", c.Version)
	}
	return &c, nil
}
func clamp(n int) int {
	if n <= 0 {
		return 20
	}
	if n > 100 {
		return 100
	}
	return n
}
func search(entries []Entry, q string, limit int) []Entry {
	q = strings.ToLower(q)
	out := []Entry{}
	for _, e := range entries {
		if q == "" || strings.Contains(strings.ToLower(e.Name+" "+e.Description+" "+e.Source), q) {
			out = append(out, e)
			if len(out) == clamp(limit) {
				break
			}
		}
	}
	return out
}
func (c *Catalog) SearchSkills(q string, limit int) []Entry   { return search(c.Skills, q, limit) }
func (c *Catalog) SearchPersonas(q string, limit int) []Entry { return search(c.Personas, q, limit) }

func find(entries []Entry, name string) (Entry, error) {
	var found *Entry
	for i := range entries {
		if entries[i].Name == name {
			if found != nil {
				return Entry{}, fmt.Errorf("ambiguous catalog name %q; specify source:name", name)
			}
			v := entries[i]
			found = &v
		}
		if entries[i].Source+":"+entries[i].Name == name {
			v := entries[i]
			return v, nil
		}
	}
	if found == nil {
		return Entry{}, errors.New("catalog entry not found")
	}
	return *found, nil
}
func checkoutRoot(entry Entry) string {
	if entry.Boundary != "" {
		return filepath.Clean(entry.Boundary)
	}
	root := filepath.Clean(entry.Root)
	for {
		if filepath.Base(root) == "skills" || filepath.Base(root) == "for-agents" {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			return filepath.Clean(entry.Root)
		}
		root = parent
	}
}
func read(entry Entry, path string) (Content, error) {
	if path == "" {
		path = entry.Entry
	}
	if filepath.IsAbs(path) {
		return Content{}, errors.New("absolute paths are not allowed")
	}
	base, err := filepath.EvalSymlinks(entry.Root)
	if err != nil {
		return Content{}, err
	}
	boundary, err := filepath.EvalSymlinks(checkoutRoot(entry))
	if err != nil {
		return Content{}, err
	}
	target, err := filepath.EvalSymlinks(filepath.Join(base, path))
	if err != nil {
		return Content{}, err
	}
	rel, err := filepath.Rel(boundary, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Content{}, errors.New("path escapes managed root")
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == ".git" {
			return Content{}, errors.New(".git is not readable")
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		return Content{}, err
	}
	if !info.Mode().IsRegular() {
		return Content{}, errors.New("only regular files are readable")
	}
	if info.Size() > 1024*1024 {
		return Content{}, errors.New("catalog file exceeds 1 MiB")
	}
	b, err := os.ReadFile(target)
	if err != nil {
		return Content{}, err
	}
	if utf8.Valid(b) {
		return Content{Data: string(b), Encoding: "utf-8"}, nil
	}
	return Content{Data: base64.StdEncoding.EncodeToString(b), Encoding: "base64"}, nil
}
func (c *Catalog) ReadSkill(name, path string) (Entry, Content, error) {
	e, err := find(c.Skills, name)
	if err != nil {
		return Entry{}, Content{}, err
	}
	v, err := read(e, path)
	return e, v, err
}
func (c *Catalog) ReadPersona(name string) (Entry, Content, error) {
	e, err := find(c.Personas, name)
	if err != nil {
		return Entry{}, Content{}, err
	}
	v, err := read(e, e.Entry)
	return e, v, err
}
