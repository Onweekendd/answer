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
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"image"
	"os"
	"path"
	"path/filepath"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/pkg/uid"
)

// agentImageMaxBytes caps one image. A product thumbnail is a 480px jpeg of a
// few dozen KB; anything near this is not what the agent meant to save.
const agentImageMaxBytes = 2 << 20

// ErrNotJPEG means the bytes the agent asked to save are not a jpeg image.
var ErrNotJPEG = stderrors.New("not a jpeg image")

// SaveAgentImage stores an image the purchase agent took itself, a product
// card thumbnail, and returns its URL. Unlike a post image it has no file
// record: no revision ever references it, so the orphan cleanup would delete
// it two days later and old chats would show a broken image.
func (us *uploaderService) SaveAgentImage(ctx context.Context, data []byte) (string, error) {
	siteGeneral, err := us.siteInfoService.GetSiteGeneral(ctx)
	if err != nil {
		return "", err
	}
	name, err := saveAgentImage(us.serviceConfig.UploadPath, data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/uploads/%s", siteGeneral.SiteUrl, path.Join(constant.AgentSubPath, name)), nil
}

// saveAgentImage checks the bytes are a jpeg and writes them under
// UploadPath/agent with a fresh name, returning that name. The bytes come from
// a browser extension, so they are checked rather than trusted: the file is
// served from this site, and a file of the wrong type there could be HTML.
func saveAgentImage(uploadPath string, data []byte) (string, error) {
	if len(data) == 0 || len(data) > agentImageMaxBytes {
		return "", fmt.Errorf("agent image is %d bytes", len(data))
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || format != "jpeg" {
		return "", ErrNotJPEG
	}
	name := uid.IDStr12() + ".jpg"
	if err := os.WriteFile(filepath.Join(uploadPath, constant.AgentSubPath, name), data, 0o644); err != nil {
		return "", err
	}
	return name, nil
}
