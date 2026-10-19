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
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/segmentfault/pacman/log"
)

// The new front end (frontend/web, the chat) is served under /app while the
// old ui/ keeps /. Vite builds it with base /app/ and the Dockerfile copies the
// build to defaultWebDir; WEB_DIR points elsewhere for a local check. Local
// development uses Vite's dev server, so a missing directory only means /app
// is not served.
const (
	webBasePath   = "/app"
	webDirEnv     = "WEB_DIR"
	defaultWebDir = "/usr/share/maibumai/web"
)

// RegisterWeb serves the new front end under /app and makes it the home page:
// / redirects there, while the old ui keeps every other path, the admin pages
// at /admin among them. It returns false when there is no build to serve, and
// then / stays the old ui's.
func RegisterWeb(r *gin.Engine) bool {
	dir := os.Getenv(webDirEnv)
	if dir == "" {
		dir = defaultWebDir
	}
	if info, err := os.Stat(filepath.Join(dir, "index.html")); err != nil || info.IsDir() {
		log.Infof("web front end not found in %s, %s is not served", dir, webBasePath)
		return false
	}
	r.GET(webBasePath, func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, webBasePath+"/") })
	// / already belongs to the template router (the page rendered for search
	// engines), and gin refuses a second handler, so a middleware answers it
	// first. It only reaches the routes registered after it, which is why the
	// server calls RegisterWeb before the template router.
	// 302, not 301: a browser keeps a 301 for good, and the home page may move back
	r.Use(func(c *gin.Context) {
		if c.Request.Method == http.MethodGet && c.Request.URL.Path == "/" {
			c.Redirect(http.StatusFound, webBasePath+"/")
			c.Abort()
		}
	})
	r.GET(webBasePath+"/*filepath", webHandler(dir))
	return true
}

// webHandler sends the file when it exists. Anything else without an
// extension is a route of the single page app (/app/login), so it gets
// index.html and the app's router takes over; a missing file with an
// extension is a real 404, or a stale page would get HTML for its script.
func webHandler(dir string) gin.HandlerFunc {
	root := http.Dir(dir)
	return func(c *gin.Context) {
		// http.Dir refuses ".." and keeps the path inside dir
		name := path.Clean("/" + c.Param("filepath"))
		if f, err := root.Open(name); err == nil {
			info, statErr := f.Stat()
			_ = f.Close()
			if statErr == nil && !info.IsDir() {
				// the build names assets by their hash, so they never change
				if strings.HasPrefix(name, "/assets/") {
					c.Header("Cache-Control", "public, max-age=31536000, immutable")
				}
				c.File(filepath.Join(dir, filepath.FromSlash(name)))
				return
			}
		}
		if path.Ext(name) != "" {
			c.Status(http.StatusNotFound)
			return
		}
		// index.html names the current assets, so it must not be cached
		c.Header("Cache-Control", "no-cache")
		c.File(filepath.Join(dir, "index.html"))
	}
}
