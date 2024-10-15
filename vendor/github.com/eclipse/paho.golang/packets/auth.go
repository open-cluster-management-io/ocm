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

package packets

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
)

// Auth is the Variable Header definition for a Auth control packet
type Auth struct {
	Properties *Properties
	ReasonCode byte
}

// AuthSuccess is the return code for successful authentication
const (
	AuthSuccess                = 0x00
	AuthContinueAuthentication = 0x18
	AuthReauthenticate         = 0x19
)

func (a *Auth) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "AUTH: ReasonCode:%X", a.ReasonCode)
	if a.Properties != nil {
		fmt.Fprintf(&b, " Properties:\n%s", a.Properties)
	} else {
		fmt.Fprint(&b, "\n")
	}

	return b.String()
}

// Unpack is the implementation of the interface required function for a packet
func (a *Auth) Unpack(r *bytes.Buffer) error {
	var err error

	success := r.Len() == 0
	noProps := r.Len() == 1
	if !success {
		a.ReasonCode, err = r.ReadByte()
		if err != nil {
			return err
		}

		if !noProps {
			err = a.Properties.Unpack(r, AUTH)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Buffers is the implementation of the interface required function for a packet
func (a *Auth) Buffers() net.Buffers {
	idvp := a.Properties.Pack(AUTH)
	propLen := encodeVBI(len(idvp))
	n := net.Buffers{[]byte{a.ReasonCode}, propLen}
	if len(idvp) > 0 {
		n = append(n, idvp)
	}
	return n
}

// WriteTo is the implementation of the interface required function for a packet
func (a *Auth) WriteTo(w io.Writer) (int64, error) {
	cp := &ControlPacket{FixedHeader: FixedHeader{Type: AUTH}}
	cp.Content = a

	return cp.WriteTo(w)
}
