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

package purchaseai

import (
	"github.com/apache/answer/internal/service/purchase_ai/tool/sandboxtool"
	"github.com/apache/answer/internal/service/sandbox"
)

// 工作台的接口和工具在 tool/sandboxtool 里，根包里用这几个名字

type (
	Workspace  = sandboxtool.Workspace
	Workspaces = sandboxtool.Workspaces
)

const (
	workspaceDownText  = sandboxtool.DownText
	workspaceResetText = sandboxtool.ResetText
)

// NewWorkspaces 给依赖注入（wire）用，见 sandboxtool.NewWorkspaces
func NewWorkspaces(store sandbox.Store) Workspaces {
	return sandboxtool.NewWorkspaces(store)
}
