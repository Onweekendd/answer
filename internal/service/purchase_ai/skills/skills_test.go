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

package skills

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func Test_skills(t *testing.T) {
	t.Run("编进程序的 skill 都能读出名字和描述：现在有 homework、site-guide", func(t *testing.T) {
		// Act
		list, err := listSkills(files)

		// Assert
		require.NoError(t, err)
		require.Len(t, list, 2)
		require.Equal(t, "homework", list[0].Name)
		require.Equal(t, "site-guide", list[1].Name)
		for _, s := range list {
			require.NotEmpty(t, s.Description, s.Name)
		}
	})

	t.Run("SKILL.md 里提到的 reference 都存在，references 里的每个文件也都在 SKILL.md 里提到", func(t *testing.T) {
		// Arrange
		for _, s := range all {
			doc, err := readSkillFile(files, s.Name+"/SKILL.md")
			require.NoError(t, err)
			mentioned := map[string]bool{}
			for _, m := range regexp.MustCompile(`references/[\w.-]+\.md`).FindAllString(doc, -1) {
				mentioned[m] = true
			}

			// Act
			var refs []string
			for _, f := range skillFilesOf(files, s.Name) {
				if rel := strings.TrimPrefix(f, s.Name+"/"); strings.HasPrefix(rel, "references/") {
					refs = append(refs, rel)
				}
			}

			// Assert
			for m := range mentioned {
				require.Contains(t, refs, m, "%s/SKILL.md 提到了不存在的 %s", s.Name, m)
			}
			for _, f := range refs {
				require.True(t, mentioned[f], "%s 没有在 %s/SKILL.md 里提到，模型不会知道要读它", f, s.Name)
			}
		}
	})

	t.Run("名字和目录名不一样、或者没有描述的 skill 不能通过", func(t *testing.T) {
		// Arrange
		bad := fstest.MapFS{
			"a/SKILL.md": {Data: []byte("---\nname: b\ndescription: x\n---\n正文")},
		}
		noDesc := fstest.MapFS{
			"a/SKILL.md": {Data: []byte("---\nname: a\n---\n正文")},
		}
		noFront := fstest.MapFS{
			"a/SKILL.md": {Data: []byte("# 没有 frontmatter")},
		}

		// Act + Assert
		for _, fsys := range []fs.FS{bad, noDesc, noFront} {
			_, err := listSkills(fsys)
			require.Error(t, err)
		}
	})
}

func Test_readSkillFile(t *testing.T) {
	cases := []struct {
		name string
		path string
		ok   bool
	}{
		{"SKILL.md", "site-guide/SKILL.md", true},
		{"reference", "site-guide/references/taobao.md", true},
		{"开头多一个斜杠也行", "/site-guide/SKILL.md", true},
		{"不能用 .. 跳出 skills 目录", "../skills.go", false},
		{"中间带 .. 的路径不接受", "site-guide/../site-guide/SKILL.md", false},
		{"只能读 .md", "site-guide/references", false},
		{"不存在的文件", "site-guide/references/pdd.md", false},
		{"空路径", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			content, err := readSkillFile(files, tc.path)

			// Assert
			if tc.ok {
				require.NoError(t, err)
				require.NotEmpty(t, content)
			} else {
				require.Error(t, err)
			}
		})
	}
}
