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
	// Unsuback is a representation of an MQTT Unsuback packet
	Unsuback struct {
		Reasons    []byte
		Properties *UnsubackProperties
	}

	// UnsubackProperties is a struct of the properties that can be set
	// for a Unsuback packet
	UnsubackProperties struct {
		ReasonString string
		User         UserProperties
	}
)

// Packet returns a packets library Unsuback from the paho Unsuback
// on which it is called
func (u *Unsuback) Packet() *packets.Unsuback {
	return &packets.Unsuback{
		Reasons: u.Reasons,
		Properties: &packets.Properties{
			User: u.Properties.User.ToPacketProperties(),
		},
	}
}

// UnsubackFromPacketUnsuback takes a packets library Unsuback and
// returns a paho library Unsuback
func UnsubackFromPacketUnsuback(u *packets.Unsuback) *Unsuback {
	return &Unsuback{
		Reasons: u.Reasons,
		Properties: &UnsubackProperties{
			ReasonString: u.Properties.ReasonString,
			User:         UserPropertiesFromPacketUser(u.Properties.User),
		},
	}
}
