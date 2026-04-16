/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

// LoadOpt configures a LoadData call.
type LoadOpt func(*loadOptions)

type loadOptions struct {
	schemaPath string
}

// WithSchema applies the given Dgraph schema file before loading data.
// The schema file should contain Dgraph Schema Definition Language.
// If not provided, the schema must already exist in the database.
func WithSchema(path string) LoadOpt {
	return func(o *loadOptions) {
		o.schemaPath = path
	}
}
