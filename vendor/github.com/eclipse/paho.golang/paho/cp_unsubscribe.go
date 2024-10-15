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

import "github.com/eclipse/paho.golang/packets"

type (
	// Unsubscribe is a representation of an MQTT unsubscribe packet
	Unsubscribe struct {
		Topics     []string
		Properties *UnsubscribeProperties
	}

	// UnsubscribeProperties is a struct of the properties that can be set
	// for a Unsubscribe packet
	UnsubscribeProperties struct {
		User UserProperties
	}
)

// Packet returns a packets library Unsubscribe from the paho Unsubscribe
// on which it is called
func (u *Unsubscribe) Packet() *packets.Unsubscribe {
	v := &packets.Unsubscribe{Topics: u.Topics}

	if u.Properties != nil {
		v.Properties = &packets.Properties{
			User: u.Properties.User.ToPacketProperties(),
		}
	}

	return v
}
