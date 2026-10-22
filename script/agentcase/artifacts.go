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
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/apache/answer/internal/service/uploader"
)

// dirStore 把 deliver 交付的网页和截图存进这次运行的记录目录，网址是本机的文件路径：
// case 运行器没有网站。检查和网站上的一样（uploader.CheckAgentFile），网站上存不下的这里也存不下
type dirStore struct {
	dir string
	n   int
}

func (s *dirStore) SaveAgentFile(_ context.Context, ext string, data []byte) (string, error) {
	if err := uploader.CheckAgentFile(ext, data); err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", err
	}
	s.n++
	p := filepath.Join(s.dir, fmt.Sprintf("%d%s", s.n, ext))
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return "file:///" + filepath.ToSlash(abs), nil
}
