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

package llm

import (
	"encoding/json"
	"fmt"

	"github.com/sashabaranov/go-openai/jsonschema"
)

// SchemaFor describes a tool's arguments struct as a JSON Schema, so the struct
// the tool decodes into is also the one the model is told about.
//
// Property names come from the json tag and descriptions from a description
// tag. Every field is required unless its json tag has omitempty.
//
// It only describes: json.Unmarshal does not enforce the schema, a missing
// required field simply stays zero, so the tool still checks what it gets.
func SchemaFor(v any) (json.RawMessage, error) {
	schema, err := jsonschema.GenerateSchemaForType(v)
	if err != nil {
		return nil, fmt.Errorf("llm: schema for %T: %w", v, err)
	}
	return json.Marshal(schema)
}
