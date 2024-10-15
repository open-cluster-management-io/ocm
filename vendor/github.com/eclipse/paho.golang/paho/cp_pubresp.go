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
	// PublishResponse is a generic representation of a response
	// to a QoS1 or QoS2 Publish
	PublishResponse struct {
		Properties *PublishResponseProperties
		ReasonCode byte
	}

	// PublishResponseProperties is the properties associated with
	// a response to a QoS1 or QoS2 Publish
	PublishResponseProperties struct {
		ReasonString string
		User         UserProperties
	}
)

// PublishResponseFromPuback takes a packets library Puback and
// returns a paho library PublishResponse
func PublishResponseFromPuback(pa *packets.Puback) *PublishResponse {
	return &PublishResponse{
		ReasonCode: pa.ReasonCode,
		Properties: &PublishResponseProperties{
			ReasonString: pa.Properties.ReasonString,
			User:         UserPropertiesFromPacketUser(pa.Properties.User),
		},
	}
}

// PublishResponseFromPubcomp takes a packets library Pubcomp and
// returns a paho library PublishResponse
func PublishResponseFromPubcomp(pc *packets.Pubcomp) *PublishResponse {
	return &PublishResponse{
		ReasonCode: pc.ReasonCode,
		Properties: &PublishResponseProperties{
			ReasonString: pc.Properties.ReasonString,
			User:         UserPropertiesFromPacketUser(pc.Properties.User),
		},
	}
}

// PublishResponseFromPubrec takes a packets library Pubrec and
// returns a paho library PublishResponse
func PublishResponseFromPubrec(pr *packets.Pubrec) *PublishResponse {
	return &PublishResponse{
		ReasonCode: pr.ReasonCode,
		Properties: &PublishResponseProperties{
			ReasonString: pr.Properties.ReasonString,
			User:         UserPropertiesFromPacketUser(pr.Properties.User),
		},
	}
}
