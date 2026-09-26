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

package service_config

type ServiceConfig struct {
	UploadPath                    string        `json:"upload_path" mapstructure:"upload_path" yaml:"upload_path"`
	CleanUpUploads                bool          `json:"clean_up_uploads" mapstructure:"clean_up_uploads" yaml:"clean_up_uploads"`
	CleanOrphanUploadsPeriodHours int           `json:"clean_orphan_uploads_period_hours" mapstructure:"clean_orphan_uploads_period_hours" yaml:"clean_orphan_uploads_period_hours"`
	PurgeDeletedFilesPeriodDays   int           `json:"purge_deleted_files_period_days" mapstructure:"purge_deleted_files_period_days" yaml:"purge_deleted_files_period_days"`
	Search                        *SearchConfig `json:"search" mapstructure:"search" yaml:"search"`
}

// SearchConfig picks the web search provider the AI analysis uses. Leaving
// APIKey empty, or the whole section out, turns search off: the analysis still
// runs, it just has no sources to cite.
type SearchConfig struct {
	Provider string `json:"provider" mapstructure:"provider" yaml:"provider"`
	APIKey   string `json:"api_key" mapstructure:"api_key" yaml:"api_key"`
}
