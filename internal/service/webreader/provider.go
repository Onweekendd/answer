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

package webreader

import (
	"context"
	"strings"

	"github.com/apache/answer/internal/service/service_config"
	"github.com/segmentfault/pacman/log"
)

// NewReader picks the reader named in service_config.reader. Jina works without
// a key, so a missing section means Jina at its free rate limit; provider "off"
// turns reading off and gives a Reader that always returns ErrDisabled.
func NewReader(serviceConf *service_config.ServiceConfig) Reader {
	var conf service_config.ReaderConfig
	if serviceConf != nil && serviceConf.Reader != nil {
		conf = *serviceConf.Reader
	}

	switch name := strings.ToLower(conf.Provider); name {
	case "jina", "":
		if conf.APIKey == "" {
			log.Info("web page reading uses jina without a key, at its free rate limit")
		}
		return newJina(conf.APIKey)
	case "off":
		log.Info("web page reading is off")
		return disabled{}
	default:
		log.Warnf("web page reading is off: unknown provider %q", name)
		return disabled{}
	}
}

// disabled is the reader used when reading is turned off.
type disabled struct{}

func (disabled) Read(context.Context, string) (*Page, error) {
	return nil, ErrDisabled
}
