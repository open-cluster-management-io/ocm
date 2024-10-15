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
)

// Unsubscribe is the Variable Header definition for a Unsubscribe control packet
type Unsubscribe struct {
	Topics     []string
	Properties *Properties
	PacketID   uint16
}

func (u *Unsubscribe) String() string {
	return fmt.Sprintf("UNSUBSCRIBE: PacketID:%d Topics:%v Properties:\n%s", u.PacketID, u.Topics, u.Properties)
}

// SetIdentifier sets the packet identifier
func (u *Unsubscribe) SetIdentifier(packetID uint16) {
	u.PacketID = packetID
}

// Type returns the current packet type
func (s *Unsubscribe) Type() byte {
	return UNSUBSCRIBE
}

// Unpack is the implementation of the interface required function for a packet
func (u *Unsubscribe) Unpack(r *bytes.Buffer) error {
	var err error
	u.PacketID, err = readUint16(r)
	if err != nil {
		return err
	}

	err = u.Properties.Unpack(r, UNSUBSCRIBE)
	if err != nil {
		return err
	}

	for {
		t, err := readString(r)
		if err != nil && err != io.EOF {
			return err
		}
		if err == io.EOF {
			break
		}
		u.Topics = append(u.Topics, t)
	}

	return nil
}

// Buffers is the implementation of the interface required function for a packet
func (u *Unsubscribe) Buffers() net.Buffers {
	var b bytes.Buffer
	writeUint16(u.PacketID, &b)
	var topics bytes.Buffer
	for _, t := range u.Topics {
		writeString(t, &topics)
	}
	idvp := u.Properties.Pack(UNSUBSCRIBE)
	propLen := encodeVBI(len(idvp))
	return net.Buffers{b.Bytes(), propLen, idvp, topics.Bytes()}
}

// WriteTo is the implementation of the interface required function for a packet
func (u *Unsubscribe) WriteTo(w io.Writer) (int64, error) {
	cp := &ControlPacket{FixedHeader: FixedHeader{Type: UNSUBSCRIBE, Flags: 2}}
	cp.Content = u

	return cp.WriteTo(w)
}
