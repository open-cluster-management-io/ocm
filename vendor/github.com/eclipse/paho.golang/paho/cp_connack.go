package paho

import "github.com/eclipse/paho.golang/packets"

type (
	// Connack is a representation of the MQTT Connack packet
	Connack struct {
		Properties     *ConnackProperties
		ReasonCode     byte
		SessionPresent bool
	}

	// ConnackProperties is a struct of the properties that can be set
	// for a Connack packet
	ConnackProperties struct {
		SessionExpiryInterval *uint32
		AuthData              []byte
		AuthMethod            string
		ResponseInfo          string
		ServerReference       string
		ReasonString          string
		AssignedClientID      string
		MaximumPacketSize     *uint32
		ReceiveMaximum        *uint16
		TopicAliasMaximum     *uint16
		ServerKeepAlive       *uint16
		MaximumQoS            *byte
		User                  UserProperties
		WildcardSubAvailable  bool
		SubIDAvailable        bool
		SharedSubAvailable    bool
		RetainAvailable       bool
	}
)

// InitProperties is a function that takes a lower level
// Properties struct and completes the properties of the Connack on
// which it is called
func (c *Connack) InitProperties(p *packets.Properties) {
	c.Properties = &ConnackProperties{
		AssignedClientID:      p.AssignedClientID,
		ServerKeepAlive:       p.ServerKeepAlive,
		WildcardSubAvailable:  true,
		SubIDAvailable:        true,
		SharedSubAvailable:    true,
		RetainAvailable:       true,
		ResponseInfo:          p.ResponseInfo,
		SessionExpiryInterval: p.SessionExpiryInterval,
		AuthMethod:            p.AuthMethod,
		AuthData:              p.AuthData,
		ServerReference:       p.ServerReference,
		ReasonString:          p.ReasonString,
		ReceiveMaximum:        p.ReceiveMaximum,
		TopicAliasMaximum:     p.TopicAliasMaximum,
		MaximumQoS:            p.MaximumQOS,
		MaximumPacketSize:     p.MaximumPacketSize,
		User:                  UserPropertiesFromPacketUser(p.User),
	}

	if p.WildcardSubAvailable != nil {
		c.Properties.WildcardSubAvailable = *p.WildcardSubAvailable == 1
	}
	if p.SubIDAvailable != nil {
		c.Properties.SubIDAvailable = *p.SubIDAvailable == 1
	}
	if p.SharedSubAvailable != nil {
		c.Properties.SharedSubAvailable = *p.SharedSubAvailable == 1
	}
	if p.RetainAvailable != nil {
		c.Properties.RetainAvailable = *p.RetainAvailable == 1
	}
}

// ConnackFromPacketConnack takes a packets library Connack and
// returns a paho library Connack
func ConnackFromPacketConnack(c *packets.Connack) *Connack {
	v := &Connack{
		SessionPresent: c.SessionPresent,
		ReasonCode:     c.ReasonCode,
	}
	v.InitProperties(c.Properties)

	return v
}
