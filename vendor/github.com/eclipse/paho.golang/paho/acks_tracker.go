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

package paho

import (
	"errors"
	"sync"

	"github.com/eclipse/paho.golang/packets"
)

var (
	ErrPacketNotFound = errors.New("packet not found")
)

type acksTracker struct {
	mx    sync.Mutex
	order []packet
}

func (t *acksTracker) add(pb *packets.Publish) {
	t.mx.Lock()
	defer t.mx.Unlock()

	for _, v := range t.order {
		if v.pb.PacketID == pb.PacketID {
			return // already added
		}
	}

	t.order = append(t.order, packet{pb: pb})
}

func (t *acksTracker) markAsAcked(pb *packets.Publish) error {
	t.mx.Lock()
	defer t.mx.Unlock()

	for k, v := range t.order {
		if pb.PacketID == v.pb.PacketID {
			t.order[k].acknowledged = true
			return nil
		}
	}

	return ErrPacketNotFound
}

func (t *acksTracker) flush(do func([]*packets.Publish)) {
	t.mx.Lock()
	defer t.mx.Unlock()

	var (
		buf []*packets.Publish
	)
	for _, v := range t.order {
		if v.acknowledged {
			buf = append(buf, v.pb)
		} else {
			break
		}
	}

	if len(buf) == 0 {
		return
	}

	do(buf)
	t.order = t.order[len(buf):]
}

// reset should be used upon disconnections
func (t *acksTracker) reset() {
	t.mx.Lock()
	defer t.mx.Unlock()
	t.order = nil
}

type packet struct {
	pb           *packets.Publish
	acknowledged bool
}
