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

package router

import (
	"errors"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/base/objectstore"
	"github.com/apache/answer/internal/service/service_config"
	"github.com/apache/answer/internal/service/uploader"
	"github.com/apache/answer/pkg/dir"
	"github.com/gin-gonic/gin"
	"github.com/segmentfault/pacman/log"
)

// StaticRouter static api router
type StaticRouter struct {
	serviceConfig *service_config.ServiceConfig
	objectStore   *objectstore.Store
}

// NewStaticRouter new static api router
func NewStaticRouter(serviceConfig *service_config.ServiceConfig, objectStore *objectstore.Store) *StaticRouter {
	return &StaticRouter{
		serviceConfig: serviceConfig,
		objectStore:   objectStore,
	}
}

// RegisterStaticRouter register static api router
func (a *StaticRouter) RegisterStaticRouter(r *gin.RouterGroup) {
	r.Static("/uploads/"+constant.AvatarSubPath, filepath.Join(a.serviceConfig.UploadPath, constant.AvatarSubPath))
	r.Static("/uploads/"+constant.AvatarThumbSubPath, filepath.Join(a.serviceConfig.UploadPath, constant.AvatarThumbSubPath))
	r.Static("/uploads/"+constant.PostSubPath, filepath.Join(a.serviceConfig.UploadPath, constant.PostSubPath))
	r.Static("/uploads/"+constant.BrandingSubPath, filepath.Join(a.serviceConfig.UploadPath, constant.BrandingSubPath))
	r.GET("/uploads/"+constant.AgentSubPath+"/:name", a.agentImage)
	r.GET("/uploads/"+constant.AgentFilesSubPath+"/:name", a.agentFile)
	r.GET("/uploads/"+constant.FilesPostSubPath+"/*filepath", func(c *gin.Context) {
		// The filepath such as hash/123.pdf
		filePath := c.Param("filepath")
		// The original filename is 123.pdf
		originalFilename := filepath.Base(filePath)
		// The real filename is hash.pdf
		realFilename := strings.TrimSuffix(filePath, "/"+originalFilename) + filepath.Ext(originalFilename)
		// The file local path is /uploads/files/post/hash.pdf
		fileLocalPath := filepath.Join(a.serviceConfig.UploadPath, constant.FilesPostSubPath, realFilename)
		// If the file is not exist, return 404
		if !dir.CheckFileExist(fileLocalPath) {
			c.Redirect(http.StatusFound, "/404")
			return
		}
		c.FileAttachment(fileLocalPath, originalFilename)
	})
}

// agentImageName is the name SaveAgentImage gives an image; anything else is not one
var agentImageName = regexp.MustCompile(`^[0-9A-Za-z]{1,32}\.jpg$`)

// agentImage serves an image the purchase agent saved. With an object store it
// comes from there; images saved before the store existed are still on disk.
func (a *StaticRouter) agentImage(c *gin.Context) {
	name := c.Param("name")
	if !agentImageName.MatchString(name) {
		c.Status(http.StatusNotFound)
		return
	}
	// the name is new for every image, so a copy never goes stale
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	if a.objectStore != nil {
		body, size, err := a.objectStore.Get(c.Request.Context(), uploader.AgentImageKey(name))
		if err == nil {
			defer func() { _ = body.Close() }()
			c.DataFromReader(http.StatusOK, size, "image/jpeg", body, nil)
			return
		}
		if !errors.Is(err, objectstore.ErrNotFound) {
			log.Warnf("agent image %s from the object store: %v", name, err)
		}
	}
	local := filepath.Join(a.serviceConfig.UploadPath, constant.AgentSubPath, name)
	if !dir.CheckFileExist(local) {
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusNotFound)
		return
	}
	c.File(local)
}

// htmlSandboxCSP 是 Agent 交付的网页的 CSP。网页是模型写的，模型的输入来自外部网页，可能被提示词注入：
//   - sandbox allow-scripts：浏览器把它当成一个不透明的独立来源，即使挂在我们的域名下，
//     也读不到我们的 cookie、localStorage，调不了带登录态的接口。脚本可以跑（图表、切换分组要用）
//   - allow-popups allow-popups-to-escape-sandbox：网页上的出处链接（target=_blank）能在新标签页打开，
//     打开的原网站（小红书、官网）照常运行，不继承这里的限制。网页本身照样读不到我们的登录态
//   - default-src 'none'：不许发任何请求、不许加载外面的东西，被注入了也没法把数据送出去
//   - 网页要自包含：样式、脚本写在页面里（'unsafe-inline'），图片、字体用 data: 内嵌
//   - 绝对不能加 allow-same-origin：和 allow-scripts 一起，脚本能把 sandbox 自己拿掉
const htmlSandboxCSP = "sandbox allow-scripts allow-popups allow-popups-to-escape-sandbox; default-src 'none'; " +
	"script-src 'unsafe-inline'; style-src 'unsafe-inline'; " +
	"img-src data: blob:; font-src data:; media-src data: blob:"

// agentFile 提供 Agent 交付的文件（deliver 工具存的，见 uploader.SaveAgentFile）：产物的网页和它的截图。
// 页面上的产物卡片和预览面板（<img>、<iframe>、下载链接）都指向这里。
// 和 agentImage 一样先找对象存储再找磁盘；不同的是类型按扩展名给，网页要隔离
func (a *StaticRouter) agentFile(c *gin.Context) {
	name := c.Param("name")
	if !uploader.AgentFileName.MatchString(name) {
		agentFileNotFound(c)
		return
	}
	ext := path.Ext(name)
	contentType := uploader.AgentFileTypes[ext]
	// Set this explicitly before streaming as Gin preserves an existing type.
	c.Header("Content-Type", contentType)
	// 名字每次都是新的，一份拷贝永远不会过期
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	switch ext {
	case ".html":
		c.Header("Content-Security-Policy", htmlSandboxCSP)
	case ".xlsx", ".pptx", ".docx", ".zip":
		c.Header("Content-Disposition", "attachment")
	}

	if a.objectStore != nil {
		body, size, err := a.objectStore.Get(c.Request.Context(), uploader.AgentFileKey(name))
		if err == nil {
			defer func() { _ = body.Close() }()
			c.DataFromReader(http.StatusOK, size, contentType, body, nil)
			return
		}
		if !errors.Is(err, objectstore.ErrNotFound) {
			log.Warnf("agent file %s from the object store: %v", name, err)
		}
	}
	local := filepath.Join(a.serviceConfig.UploadPath, constant.AgentFilesSubPath, name)
	if !dir.CheckFileExist(local) {
		agentFileNotFound(c)
		return
	}
	// c.File 会按扩展名自己猜类型（.md、.csv 猜得五花八门），先定死
	c.File(local)
}

// agentFileNotFound 回 404，不许缓存：还没存好时的 404 不能被记住。前面设过的头都清掉
func agentFileNotFound(c *gin.Context) {
	for _, h := range []string{"Content-Security-Policy", "Content-Disposition"} {
		c.Writer.Header().Del(h)
	}
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNotFound)
}
