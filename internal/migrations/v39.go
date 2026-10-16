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
)

// dropBadges removes what the badge feature left in the database: its three tables, and the
// notifications that announced an earned badge. Those notifications are cut out of the
// notification table because their badge no longer exists, so the inbox could not render them.
//
// This destroys data and cannot be undone, so a backup comes first. It is safe to run any
// number of times, and on a database that never had the tables.
func dropBadges(ctx context.Context, x *xorm.Engine) error {
	exists, err := x.Context(ctx).IsTableExist("notification")
	if err != nil {
		return fmt.Errorf("check notification table failed: %w", err)
	}
	if exists {
		// The content is JSON written by json.Marshal, so the key and value are adjacent
		// with no spaces; "badge_award" is the object type the badge feature used.
		_, err = x.Context(ctx).Exec(`DELETE FROM notification WHERE content LIKE ?`,
			`%"object_type":"badge_award"%`)
		if err != nil {
			return fmt.Errorf("delete badge notifications failed: %w", err)
		}
	}

	// the award table points at the other two, so it goes first
	for _, table := range []string{"badge_award", "badge", "badge_group"} {
		if err = x.Context(ctx).DropTable(table); err != nil {
			return fmt.Errorf("drop table %s failed: %w", table, err)
		}
	}
	return nil
}
