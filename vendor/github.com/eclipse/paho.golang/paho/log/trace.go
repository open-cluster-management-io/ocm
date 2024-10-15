/*
 * Copyright (c) 2024 Contributors to the Eclipse Foundation
 *
 *  All rights reserved. This program and the accompanying materials
 *  are made available under the terms of the Eclipse Public License v2.0
 *  and Eclipse Distribution License v1.0 which accompany this distribution.
 *
 * The Eclipse Public License is available at
 *    https://www.eclipse.org/legal/epl-2.0/
 *  and the Eclipse Distribution License is available at
 *    http://www.eclipse.org/org/documents/edl-v10.php.
 *
 *  SPDX-License-Identifier: EPL-2.0 OR BSD-3-Clause
 */

package log

type (
	// Logger interface allows implementations to provide to this package any
	// object that implements the methods defined in it.
	Logger interface {
		Println(v ...interface{})
		Printf(format string, v ...interface{})
	}

	// NOOPLogger implements the logger that does not perform any operation
	// by default. This allows us to efficiently discard the unwanted messages.
	NOOPLogger struct{}
)

// Println is the library provided NOOPLogger's
// implementation of the required interface function()
func (NOOPLogger) Println(v ...interface{}) {}

// Printf is the library provided NOOPLogger's
// implementation of the required interface function(){}
func (NOOPLogger) Printf(format string, v ...interface{}) {}
