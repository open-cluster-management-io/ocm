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

import (
	"sync"
	"time"
)

// test implements a logger than can be passed a testing.T (which will only output logs for failed tests)

// testLogger contains the logging functions provided by testing.T
type testLogger interface {
	Log(args ...interface{})
	Logf(format string, args ...interface{})
}

// The TestLog type is an adapter to allow the use of testing.T as a paho.Logger.
// With this implementation, log messages will only be output when a test fails (and will be associated with the test).
type TestLog struct {
	sync.Mutex
	l      testLogger
	prefix string
}

// NewTestLogger accepts a testLogger (e.g. Testing.T) and a prefix (added to messages logged) and returns a Logger
func NewTestLogger(l testLogger, prefix string) *TestLog {
	return &TestLog{
		l:      l,
		prefix: prefix,
	}
}

// Println prints a line to the log
// Println its arguments in the test log (only printed if the test files or appropriate arguments passed to go test).
func (t *TestLog) Println(v ...interface{}) {
	t.Lock()
	defer t.Unlock()
	if t.l != nil {
		t.l.Log(append([]interface{}{time.Now().Format(time.RFC3339Nano), t.prefix}, v...)...)
	}
}

// Printf formats its arguments according to the format, analogous to fmt.Printf, and
// records the text in the test log (only printed if the test files or appropriate arguments passed to go test).
func (t *TestLog) Printf(format string, v ...interface{}) {
	t.Lock()
	defer t.Unlock()
	if t.l != nil {
		t.l.Logf(time.Now().Format(time.RFC3339Nano)+" "+t.prefix+format, v...)
	}
}

// Stop prevents future logging
// func (t *TestLog) Stop() {
// 	t.Lock()
// 	defer t.Unlock()
// 	t.l = nil
// }
