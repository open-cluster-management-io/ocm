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
	// Disconnect is a representation of the MQTT Disconnect packet
	Disconnect struct {
		Properties *DisconnectProperties
		ReasonCode byte
	}

	// DisconnectProperties is a struct of the properties that can be set
	// for a Disconnect packet
	DisconnectProperties struct {
		ServerReference       string
		ReasonString          string
		SessionExpiryInterval *uint32
		User                  UserProperties
	}
)

// InitProperties is a function that takes a lower level
// Properties struct and completes the properties of the Disconnect on
// which it is called
func (d *Disconnect) InitProperties(p *packets.Properties) {
	d.Properties = &DisconnectProperties{
		SessionExpiryInterval: p.SessionExpiryInterval,
		ServerReference:       p.ServerReference,
		ReasonString:          p.ReasonString,
		User:                  UserPropertiesFromPacketUser(p.User),
	}
}

// DisconnectFromPacketDisconnect takes a packets library Disconnect and
// returns a paho library Disconnect
func DisconnectFromPacketDisconnect(p *packets.Disconnect) *Disconnect {
	v := &Disconnect{ReasonCode: p.ReasonCode}
	v.InitProperties(p.Properties)

	return v
}

// Packet returns a packets library Disconnect from the paho Disconnect
// on which it is called
func (d *Disconnect) Packet() *packets.Disconnect {
	v := &packets.Disconnect{ReasonCode: d.ReasonCode}

	if d.Properties != nil {
		v.Properties = &packets.Properties{
			SessionExpiryInterval: d.Properties.SessionExpiryInterval,
			ServerReference:       d.Properties.ServerReference,
			ReasonString:          d.Properties.ReasonString,
			User:                  d.Properties.User.ToPacketProperties(),
		}
	}

	return v
}
