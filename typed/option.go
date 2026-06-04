/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed

// Option configures a *T. Generated With<Field> constructors return an Option;
// generated New<E>/Wrap<E> constructors apply them via Apply.
type Option[T any] func(*T)

// Apply applies opts to target in declaration order.
func Apply[T any](target *T, opts ...Option[T]) {
	for _, opt := range opts {
		opt(target)
	}
}
