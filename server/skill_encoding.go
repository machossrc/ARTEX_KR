package server

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Autumn-27/norma/skill"
	"gopkg.in/yaml.v3"
)

// loadSkills preserves norma v0.4.3 directory/frontmatter semantics, adding
// only support for the UTF-8 BOM used by the Korean source distribution.
// It never rewrites a user's skill file or changes its working directory.
func loadSkills(dir string) (*skill.Registry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	reg := skill.NewRegistry()
	for _, entry := range entries {
		var path string
		if entry.IsDir() {
			path = filepath.Join(dir, entry.Name(), "SKILL.md")
			if _, err := os.Stat(path); err != nil {
				continue
			}
		} else if strings.HasSuffix(entry.Name(), ".md") {
			path = filepath.Join(dir, entry.Name())
		} else {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		s := parseEncodedSkill(strings.TrimPrefix(string(data), "\ufeff"))
		if s.Name == "" {
			s.Name = strings.TrimSuffix(entry.Name(), ".md")
		}
		if entry.IsDir() {
			s.Dir = filepath.Join(dir, entry.Name())
		} else {
			s.Dir = dir
		}
		reg.Add(s)
	}
	return reg, nil
}

func parseEncodedSkill(text string) skill.Skill {
	var result skill.Skill
	body := text
	if strings.HasPrefix(text, "---") {
		rest := strings.TrimLeft(text[3:], "\r\n")
		if end := strings.Index(rest, "\n---"); end >= 0 {
			body = strings.TrimLeft(rest[end+4:], "-\r\n")
			var head struct {
				Name          string          `yaml:"name"`
				Description   string          `yaml:"description"`
				License       string          `yaml:"license"`
				Compatibility string          `yaml:"compatibility"`
				WhenToUse     string          `yaml:"whenToUse"`
				WhenToUseUS   string          `yaml:"when_to_use"`
				MCPs          skillStringList `yaml:"mcps"`
				MCP           skillStringList `yaml:"mcp"`
			}
			if yaml.Unmarshal([]byte(rest[:end]), &head) == nil {
				result.Name, result.Description = head.Name, head.Description
				result.License, result.Compatibility = head.License, head.Compatibility
				result.WhenToUse = head.WhenToUse
				if result.WhenToUse == "" {
					result.WhenToUse = head.WhenToUseUS
				}
				result.MCPs = head.MCPs
				if result.MCPs == nil {
					result.MCPs = head.MCP
				}
			}
		}
	}
	result.Instructions = strings.TrimSpace(body)
	return result
}

type skillStringList []string

func (list *skillStringList) UnmarshalYAML(node *yaml.Node) error {
	var value string
	var values []string
	if node.Decode(&value) == nil {
		value = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(value), "["), "]")
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(strings.Trim(item, "\"' "))
			if item != "" {
				*list = append(*list, item)
			}
		}
	} else if node.Decode(&values) == nil {
		for _, item := range values {
			if item = strings.TrimSpace(item); item != "" {
				*list = append(*list, item)
			}
		}
	}
	return nil
}
