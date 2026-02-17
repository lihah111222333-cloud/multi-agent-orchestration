package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillInfo Skill 目录元数据。
type SkillInfo struct {
	Name        string `json:"name"`
	Dir         string `json:"dir"`
	Description string `json:"description"` // 从 SKILL.md frontmatter 提取
}

// SkillService 扫描 .agent/skills/ 文件系统。
type SkillService struct {
	dir string
}

// NewSkillService 创建 SkillService。
func NewSkillService(dir string) *SkillService {
	return &SkillService{dir: dir}
}

// ListSkills 扫描目录并返回所有 Skill 信息。
func (s *SkillService) ListSkills() ([]SkillInfo, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var skills []SkillInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info := SkillInfo{
			Name: entry.Name(),
			Dir:  filepath.Join(s.dir, entry.Name()),
		}
		// 尝试读取 SKILL.md 的 description
		info.Description = extractDescription(filepath.Join(info.Dir, "SKILL.md"))
		skills = append(skills, info)
	}
	return skills, nil
}

// ReadSkillContent 读取 SKILL.md 完整内容。
//
// 含路径遍历防护: 拒绝包含 "/", "\", ".." 的名称。
func (s *SkillService) ReadSkillContent(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid skill name: %q", name)
	}
	path := filepath.Join(s.dir, name, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// extractDescription 从 SKILL.md frontmatter 提取 description 字段。
func extractDescription(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	// 简单解析 YAML frontmatter: ---\ndescription: "xxx"\n---
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return ""
	}
	frontmatter := content[3 : 3+end]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimPrefix(line, "description:")
			desc = strings.TrimSpace(desc)
			desc = strings.Trim(desc, "\"'")
			// 截断过长的 description
			if len(desc) > 120 {
				desc = desc[:120] + "..."
			}
			return desc
		}
	}
	return ""
}
