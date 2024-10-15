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
	// Suback is a representation of an MQTT suback packet
	Suback struct {
		Properties *SubackProperties
		Reasons    []byte
	}

	// SubackProperties is a struct of the properties that can be set
	// for a Suback packet
	SubackProperties struct {
		ReasonString string
		User         UserProperties
	}
)

// Packet returns a packets library Suback from the paho Suback
// on which it is called
func (s *Suback) Packet() *packets.Suback {
	return &packets.Suback{
		Reasons: s.Reasons,
		Properties: &packets.Properties{
			User: s.Properties.User.ToPacketProperties(),
		},
	}
}

// SubackFromPacketSuback takes a packets library Suback and
// returns a paho library Suback
func SubackFromPacketSuback(s *packets.Suback) *Suback {
	return &Suback{
		Reasons: s.Reasons,
		Properties: &SubackProperties{
			ReasonString: s.Properties.ReasonString,
			User:         UserPropertiesFromPacketUser(s.Properties.User),
		},
	}
}
