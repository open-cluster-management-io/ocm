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

package session

import (
	"context"
	"errors"
	"io"

	"github.com/eclipse/paho.golang/packets"
	paholog "github.com/eclipse/paho.golang/paho/log"
)

// The session state includes:
//   * Inflight QOS1/2 Publish transactions (both sent and received)
//   * All requests

var (
	ErrNoConnection               = errors.New("no connection available")       // We are not in-between a call to ConAckReceived and ConnectionLost
	ErrPacketIdentifiersExhausted = errors.New("all packet identifiers in use") // There are no available Packet IDs
)

// Packet provides sufficient functionality to enable a packet to be transmitted with a packet identifier
type Packet interface {
	SetIdentifier(uint16) // Sets the packet identifier
	Type() byte           // Gets the packet type
	WriteTo(io.Writer) (int64, error)
}

// SessionManager will manage the mqtt session state; note that the state may outlast a single `Client` instance
type SessionManager interface {
	// ConAckReceived must be called when a CONNACK has been received (with no error). If an error is returned
	// then the connection will be dropped.
	ConAckReceived(io.Writer, *packets.Connect, *packets.Connack) error

	// ConnectionLost must be called whenever the connection is lost or a DISCONNECT packet is received. It can be
	// called multiple times for the same event as long as ConAckReceived is not called in the interim.
	ConnectionLost(dp *packets.Disconnect) error

	// AddToSession adds a packet to the session state (including allocation of a message identifier).
	// This should only be used for packets that impact the session state (which does not include QOS0 publish).
	// If this function returns a nil then:
	//   - A message Identifier has been added to the passed in packet
	//   - If a `PUBLISH` then a slot has been allocated (function will block if RECEIVE MAXIMUM messages are inflight)
	//   - Publish messages will have been written to the store (and will be automatically transmitted if a new connection
	//     is established before the message is fully acknowledged - subject to state rules in the MQTTv5 spec)
	//   - Something will be sent to `resp` when either the message is fully acknowledged or the packet is removed from
	//     the session (in which case nil will be sent).
	//
	// If the function returns an error, then any actions taken will be rewound prior to return.
	AddToSession(ctx context.Context, packet Packet, resp chan<- packets.ControlPacket) error

	// PacketReceived must be called when any packet with a packet identifier is received. It will make any required
	// response and pass any `PUBLISH` messages that need to be passed to the user via the channel.
	PacketReceived(*packets.ControlPacket, chan<- *packets.Publish) error

	// Ack must be called when the client message handlers have completed (or, if manual acknowledgements are enabled,
	// when`client.ACK()` has been called - note the potential issues discussed in issue #160.
	Ack(pb *packets.Publish) error

	// Close shuts down the session store, this will release any blocked calls
	// Note: `paho` will only call this if it created the session (i.e. it was not passed in the config)
	Close() error

	// SetErrorLogger enables error logging via the passed logger (not thread safe)
	SetErrorLogger(l paholog.Logger)

	// SetDebugLogger enables debug logging via the passed logger (not thread safe)
	SetDebugLogger(l paholog.Logger)
}
