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

package websearch

import (
	"context"
	"strings"

	"github.com/apache/answer/internal/service/service_config"
	"github.com/segmentfault/pacman/log"
)

// NewProvider picks the engine named in service_config.search. A missing
// section or an empty key gives a provider that always returns ErrDisabled,
// so the site runs fine without a search key.
func NewProvider(serviceConf *service_config.ServiceConfig) Provider {
	if serviceConf == nil || serviceConf.Search == nil || serviceConf.Search.APIKey == "" {
		log.Info("web search is off: no service_config.search.api_key")
		return disabled{}
	}

	switch name := strings.ToLower(serviceConf.Search.Provider); name {
	case "bocha", "":
		return newBocha(serviceConf.Search.APIKey)
	default:
		log.Warnf("web search is off: unknown provider %q", name)
		return disabled{}
	}
}

// disabled is the provider used when nothing is configured.
type disabled struct{}

func (disabled) Search(context.Context, string, Options) ([]Result, error) {
	return nil, ErrDisabled
}
