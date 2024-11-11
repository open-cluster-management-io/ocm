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
	"io"
	"net"
)

// Pingresp is the Variable Header definition for a Pingresp control packet
type Pingresp struct {
}

func (p *Pingresp) String() string {
	return "PINGRESP"
}

// Unpack is the implementation of the interface required function for a packet
func (p *Pingresp) Unpack(r *bytes.Buffer) error {
	return nil
}

// Buffers is the implementation of the interface required function for a packet
func (p *Pingresp) Buffers() net.Buffers {
	return nil
}

// WriteTo is the implementation of the interface required function for a packet
func (p *Pingresp) WriteTo(w io.Writer) (int64, error) {
	cp := &ControlPacket{FixedHeader: FixedHeader{Type: PINGRESP}}
	cp.Content = p

	return cp.WriteTo(w)
}
