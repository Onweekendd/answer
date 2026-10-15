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

package migrations

import (
	"context"
	"fmt"

	"xorm.io/xorm"
	"xorm.io/xorm/schemas"
)

// syncUserIDSequence moves PostgreSQL's user id sequence past the largest id
// in the table. The admin (id 1) and the AI bot are inserted with explicit
// ids, which PostgreSQL does not count against the sequence, so without this
// the next user to sign up is handed id 1 and fails on the primary key.
// It is safe to run any number of times, and does nothing on other databases.
func syncUserIDSequence(ctx context.Context, x *xorm.Engine) error {
	// MySQL's AUTO_INCREMENT and SQLite's rowid already move past an explicit id
	if x.Dialect().URI().DBType != schemas.POSTGRES {
		return nil
	}

	// With is_called = false the next nextval returns the value itself, so an
	// empty table (MAX is NULL) starts at 1. The true form would need 0 there,
	// which is below the sequence's minimum and fails.
	_, err := x.Context(ctx).Exec(`SELECT setval(pg_get_serial_sequence('"user"', 'id'), COALESCE(MAX(id), 0) + 1, false) FROM "user"`)
	if err != nil {
		return fmt.Errorf("sync user id sequence failed: %w", err)
	}
	return nil
}
