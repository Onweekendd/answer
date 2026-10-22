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

package uploader

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/pkg/uid"
)

// AgentFileMaxBytes caps one file the agent delivers. A screenshot of a long
// page is a few MB; a PDF or a spreadsheet of a few dozen products is less.
const AgentFileMaxBytes = 20 << 20

// AgentFileTypes are the files the agent may deliver, by extension, with the
// Content-Type they are served as. Anything else is refused: the files are
// served from this site, so the type decides what a browser does with them.
// SVG is left out on purpose, it is a document that can run scripts.
var AgentFileTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".pdf":  "application/pdf",
	".html": "text/html; charset=utf-8",
	".csv":  "text/csv; charset=utf-8",
	".md":   "text/markdown; charset=utf-8",
	".txt":  "text/plain; charset=utf-8",
	".json": "application/json",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".zip":  "application/zip",
}

// agentFileSniff is what http.DetectContentType must say for the types whose
// first bytes are known, so a page cannot be delivered under a picture's name.
// The office files are zips. Text types are only checked to be text.
var agentFileSniff = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".pdf":  "application/pdf",
	".xlsx": "application/zip",
	".pptx": "application/zip",
	".docx": "application/zip",
	".zip":  "application/zip",
}

// AgentFileName matches the names SaveAgentFile gives: letters and digits and
// one of the extensions above. The route serves nothing else.
var AgentFileName = regexp.MustCompile(`^[0-9A-Za-z]{1,32}\.(png|jpg|jpeg|webp|gif|pdf|html|csv|md|txt|json|xlsx|pptx|docx|zip)$`)

// AgentFileKey is where an agent file named name lives in the object store.
func AgentFileKey(name string) string {
	return path.Join(constant.AgentFilesSubPath, name)
}

// CheckAgentFile says whether data may be stored as a file with extension ext.
func CheckAgentFile(ext string, data []byte) error {
	ext = strings.ToLower(ext)
	if _, ok := AgentFileTypes[ext]; !ok {
		return fmt.Errorf("agent file type %q is not allowed", ext)
	}
	if len(data) == 0 || len(data) > AgentFileMaxBytes {
		return fmt.Errorf("agent file is %d bytes", len(data))
	}
	sniffed := http.DetectContentType(data)
	if want, ok := agentFileSniff[ext]; ok {
		if !strings.HasPrefix(sniffed, want) {
			return fmt.Errorf("agent file %s looks like %s", ext, sniffed)
		}
		return nil
	}
	if !strings.HasPrefix(sniffed, "text/") && !strings.HasPrefix(sniffed, "application/json") {
		return fmt.Errorf("agent file %s looks like %s", ext, sniffed)
	}
	return nil
}

// SaveAgentFile stores a file the purchase agent delivered from its sandbox
// and returns its URL. Like SaveAgentImage it has no file record, so the
// orphan cleanup leaves it alone.
func (us *uploaderService) SaveAgentFile(ctx context.Context, ext string, data []byte) (string, error) {
	ext = strings.ToLower(ext)
	if err := CheckAgentFile(ext, data); err != nil {
		return "", err
	}
	siteGeneral, err := us.siteInfoService.GetSiteGeneral(ctx)
	if err != nil {
		return "", err
	}
	name := uid.IDStr12() + ext
	if us.objectStore != nil {
		err = us.objectStore.Put(ctx, AgentFileKey(name), data, AgentFileTypes[ext])
	} else {
		err = os.WriteFile(filepath.Join(us.serviceConfig.UploadPath, constant.AgentFilesSubPath, name), data, 0o644)
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/uploads/%s", siteGeneral.SiteUrl, AgentFileKey(name)), nil
}
