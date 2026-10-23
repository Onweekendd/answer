/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Package skills 是编进程序里的 skill：每个目录一个，SKILL.md 开头的 frontmatter 写名字和描述，细节放在 references 里
package skills

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// skill 和 Claude 的 skill 一样：一个目录，SKILL.md 开头的 frontmatter 写名字和描述，正文是做法，
// 细节放在 references 里。系统提示词里只列名字和描述，正文和 reference 由模型用 skill_read 按需读取：
// 提示词保持短而且固定（前缀缓存），没用到的 skill 不占上下文。
// 读到的内容一直留在对话里，不像快照那样压缩：它不会过期。

// 新加一个 skill 目录会自动编进来；文件只能是 SKILL.md 和 references/ 下的 .md
//
//go:embed */SKILL.md */references/*.md
var files embed.FS

// skillsRoot 是 files 里放 skill 的目录：就是这个包的目录
const skillsRoot = "."

type skillInfo struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// all 是编进程序里的全部 skill，按目录名排序。启动时读一次：文件是编进去的，读不出来是代码的问题
var all = mustListSkills(files)

// Prompt 是系统提示词里列 skill 的那一段，每个 skill 一行；没有 skill 是空的
func Prompt() string { return skillsPrompt(all) }

// Read 读 skill 里的一个文件，见 readSkillFile
func Read(p string) (string, error) { return readSkillFile(files, p) }

// FilesOf 列出一个 skill 里能读的文件，见 skillFilesOf
func FilesOf(skill string) []string { return skillFilesOf(files, skill) }

func mustListSkills(fsys fs.FS) []skillInfo {
	list, err := listSkills(fsys)
	if err != nil {
		panic(err)
	}
	return list
}

// listSkills 读每个 skill 目录里 SKILL.md 的 frontmatter。名字必须和目录名一样，模型才能拼出路径
func listSkills(fsys fs.FS) ([]skillInfo, error) {
	entries, err := fs.ReadDir(fsys, skillsRoot)
	if err != nil {
		return nil, fmt.Errorf("purchase ai: list skills: %w", err)
	}
	var list []skillInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := fs.ReadFile(fsys, path.Join(skillsRoot, entry.Name(), "SKILL.md"))
		if err != nil {
			return nil, fmt.Errorf("purchase ai: skill %s: %w", entry.Name(), err)
		}
		info, err := parseFrontmatter(string(raw))
		if err != nil {
			return nil, fmt.Errorf("purchase ai: skill %s: %w", entry.Name(), err)
		}
		if info.Name != entry.Name() || strings.TrimSpace(info.Description) == "" {
			return nil, fmt.Errorf("purchase ai: skill %s: name must match the directory and description must not be empty", entry.Name())
		}
		list = append(list, info)
	}
	slices.SortFunc(list, func(a, b skillInfo) int { return strings.Compare(a.Name, b.Name) })
	return list, nil
}

// parseFrontmatter 解析开头两行 --- 之间的 YAML
func parseFrontmatter(doc string) (skillInfo, error) {
	doc = strings.ReplaceAll(doc, "\r\n", "\n")
	rest, ok := strings.CutPrefix(doc, "---\n")
	if !ok {
		return skillInfo{}, errors.New("SKILL.md must start with a --- frontmatter block")
	}
	front, _, ok := strings.Cut(rest, "\n---")
	if !ok {
		return skillInfo{}, errors.New("frontmatter is not closed with ---")
	}
	var info skillInfo
	if err := yaml.Unmarshal([]byte(front), &info); err != nil {
		return skillInfo{}, fmt.Errorf("frontmatter: %w", err)
	}
	return info, nil
}

// skillsPrompt 是系统提示词里列 skill 的那一段，每个 skill 一行
func skillsPrompt(list []skillInfo) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("可用的 skill（用 skill_read 读取，先读 SKILL.md，它会告诉你什么时候读哪个 reference）：\n")
	for _, s := range list {
		fmt.Fprintf(&b, "- %s/SKILL.md：%s\n", s.Name, strings.TrimSpace(s.Description))
	}
	return strings.TrimRight(b.String(), "\n")
}

// readSkillFile 读 skill 里的一个文件。path 是相对 skills 目录的路径，比如 site-guide/references/taobao.md。
// 只允许读 .md，不能用 .. 跳出 skills 目录：路径是模型写的
func readSkillFile(fsys fs.FS, p string) (string, error) {
	p = strings.TrimPrefix(strings.TrimSpace(p), "/")
	clean := path.Clean(p)
	if p == "" || clean != p || strings.HasPrefix(clean, "..") || path.Ext(clean) != ".md" {
		return "", fmt.Errorf("invalid skill path %q", p)
	}
	raw, err := fs.ReadFile(fsys, path.Join(skillsRoot, clean))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// skillFilesOf 列出一个 skill 里能读的文件，读错路径时告诉模型有哪些
func skillFilesOf(fsys fs.FS, skill string) []string {
	var out []string
	_ = fs.WalkDir(fsys, path.Join(skillsRoot, skill), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && path.Ext(p) == ".md" {
			out = append(out, p)
		}
		return nil
	})
	return out
}
