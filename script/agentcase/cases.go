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

package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Case 是一个任务和它的判定条件，从 cases.yaml 读
type Case struct {
	Name   string `yaml:"name"`
	Task   string `yaml:"task"`
	Expect Expect `yaml:"expect"`
}

// Expect 里的条件全部满足才算通过，每个字段的意思见 cases.yaml 开头
type Expect struct {
	Visited   []string `yaml:"visited"`
	Used      []string `yaml:"used"`
	Seen      []string `yaml:"seen"`
	MinPrices int      `yaml:"min_prices"`
}

// loadCases 读 case 文件；only 不为空时只留名字包含它的 case
func loadCases(path, only string) ([]Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var all []Case
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true) // 条件名拼错了要报出来，不能悄悄当成"没有这个条件"
	if err := dec.Decode(&all); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var out []Case
	for _, c := range all {
		if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Task) == "" {
			return nil, fmt.Errorf("%s: every case needs a name and a task", path)
		}
		if only == "" || strings.Contains(c.Name, only) {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no case matches %q", path, only)
	}
	return out, nil
}
