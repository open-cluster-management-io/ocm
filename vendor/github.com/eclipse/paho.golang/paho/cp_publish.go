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
	"bytes"
	"fmt"

	"github.com/eclipse/paho.golang/packets"
)

type (
	// Publish is a representation of the MQTT Publish packet
	Publish struct {
		PacketID   uint16
		QoS        byte
		duplicate  bool // private because this should only ever be set in paho/session
		Retain     bool
		Topic      string
		Properties *PublishProperties
		Payload    []byte
	}

	// PublishProperties is a struct of the properties that can be set
	// for a Publish packet
	PublishProperties struct {
		CorrelationData        []byte
		ContentType            string
		ResponseTopic          string
		PayloadFormat          *byte
		MessageExpiry          *uint32
		SubscriptionIdentifier *int
		TopicAlias             *uint16
		User                   UserProperties
	}
)

// InitProperties is a function that takes a lower level
// Properties struct and completes the properties of the Publish on
// which it is called
func (p *Publish) InitProperties(prop *packets.Properties) {
	p.Properties = &PublishProperties{
		PayloadFormat:          prop.PayloadFormat,
		MessageExpiry:          prop.MessageExpiry,
		ContentType:            prop.ContentType,
		ResponseTopic:          prop.ResponseTopic,
		CorrelationData:        prop.CorrelationData,
		TopicAlias:             prop.TopicAlias,
		SubscriptionIdentifier: prop.SubscriptionIdentifier,
		User:                   UserPropertiesFromPacketUser(prop.User),
	}
}

// PublishFromPacketPublish takes a packets library Publish and
// returns a paho library Publish
func PublishFromPacketPublish(p *packets.Publish) *Publish {
	v := &Publish{
		PacketID:  p.PacketID,
		QoS:       p.QoS,
		duplicate: p.Duplicate,
		Retain:    p.Retain,
		Topic:     p.Topic,
		Payload:   p.Payload,
	}
	v.InitProperties(p.Properties)

	return v
}

// Duplicate returns true if the duplicate flag is set (the server sets this if the message has
// been sent previously; this does not necessarily mean the client has previously processed the message).
func (p *Publish) Duplicate() bool {
	return p.duplicate
}

// Packet returns a packets library Publish from the paho Publish
// on which it is called
func (p *Publish) Packet() *packets.Publish {
	v := &packets.Publish{
		PacketID:  p.PacketID,
		QoS:       p.QoS,
		Duplicate: p.duplicate,
		Retain:    p.Retain,
		Topic:     p.Topic,
		Payload:   p.Payload,
	}
	if p.Properties != nil {
		v.Properties = &packets.Properties{
			PayloadFormat:          p.Properties.PayloadFormat,
			MessageExpiry:          p.Properties.MessageExpiry,
			ContentType:            p.Properties.ContentType,
			ResponseTopic:          p.Properties.ResponseTopic,
			CorrelationData:        p.Properties.CorrelationData,
			TopicAlias:             p.Properties.TopicAlias,
			SubscriptionIdentifier: p.Properties.SubscriptionIdentifier,
			User:                   p.Properties.User.ToPacketProperties(),
		}
	}

	return v
}

func (p *Publish) String() string {
	if p == nil {
		return "Publish==nil"
	}
	var b bytes.Buffer

	fmt.Fprintf(&b, "topic: %s  qos: %d  retain: %t\n", p.Topic, p.QoS, p.Retain)
	if p.Properties.PayloadFormat != nil {
		fmt.Fprintf(&b, "PayloadFormat: %v\n", p.Properties.PayloadFormat)
	}
	if p.Properties.MessageExpiry != nil {
		fmt.Fprintf(&b, "MessageExpiry: %v\n", p.Properties.MessageExpiry)
	}
	if p.Properties.ContentType != "" {
		fmt.Fprintf(&b, "ContentType: %v\n", p.Properties.ContentType)
	}
	if p.Properties.ResponseTopic != "" {
		fmt.Fprintf(&b, "ResponseTopic: %v\n", p.Properties.ResponseTopic)
	}
	if p.Properties.CorrelationData != nil {
		fmt.Fprintf(&b, "CorrelationData: %v\n", p.Properties.CorrelationData)
	}
	if p.Properties.TopicAlias != nil {
		fmt.Fprintf(&b, "TopicAlias: %d\n", p.Properties.TopicAlias)
	}
	if p.Properties.SubscriptionIdentifier != nil {
		fmt.Fprintf(&b, "SubscriptionIdentifier: %v\n", p.Properties.SubscriptionIdentifier)
	}
	for _, v := range p.Properties.User {
		fmt.Fprintf(&b, "User: %s : %s\n", v.Key, v.Value)
	}
	b.WriteString(string(p.Payload))

	return b.String()
}
