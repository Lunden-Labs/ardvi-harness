package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSearchReadAndTraversal(t *testing.T) {
	root := t.TempDir()
	skill := filepath.Join(root, "for-agents", "writing")
	sibling := filepath.Join(root, "for-agents", "general-writing")
	os.MkdirAll(skill, 0755)
	os.MkdirAll(sibling, 0755)
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("router"), 0644)
	os.WriteFile(filepath.Join(sibling, "SKILL.md"), []byte("dependency"), 0644)
	c := &Catalog{Version: 1, Skills: []Entry{{Name: "writing", Description: "router", Source: "upstream", Root: skill, Entry: "SKILL.md"}}}
	if len(c.SearchSkills("router", 3)) != 1 {
		t.Fatal("search failed")
	}
	_, body, err := c.ReadSkill("writing", "")
	if err != nil || body.Data != "router" || body.Encoding != "utf-8" {
		t.Fatalf("entry read: %q %v", body, err)
	}
	_, body, err = c.ReadSkill("writing", "../general-writing/SKILL.md")
	if err != nil || body.Data != "dependency" {
		t.Fatalf("sibling read: %q %v", body, err)
	}
	if _, _, err = c.ReadSkill("writing", "../../../outside"); err == nil {
		t.Fatal("checkout escape accepted")
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	b, _ := json.Marshal(Catalog{Version: 1})
	os.WriteFile(path, b, 0644)
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
}

func TestCheckoutDependencyAndBinary(t *testing.T) {
	root := t.TempDir()
	skill := filepath.Join(root, "for-agents", "paper-writing")
	asset := filepath.Join(root, "for-humans", "guide.pdf")
	os.MkdirAll(skill, 0755)
	os.MkdirAll(filepath.Dir(asset), 0755)
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("paper"), 0644)
	os.WriteFile(asset, []byte{0xff, 0x00, 0x01}, 0644)
	c := &Catalog{Version: 1, Skills: []Entry{{Name: "paper-writing", Root: skill, Boundary: root, Entry: "SKILL.md"}}}
	_, content, err := c.ReadSkill("paper-writing", "../../for-humans/guide.pdf")
	if err != nil || content.Encoding != "base64" || content.Data != "/wAB" {
		t.Fatalf("binary dependency: %#v %v", content, err)
	}
}
