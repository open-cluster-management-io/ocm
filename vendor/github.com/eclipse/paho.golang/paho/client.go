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
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/packets"
	"github.com/eclipse/paho.golang/paho/log"
	"github.com/eclipse/paho.golang/paho/session"
	"github.com/eclipse/paho.golang/paho/session/state"
)

const defaultSendAckInterval = 50 * time.Millisecond

var (
	ErrManualAcknowledgmentDisabled = errors.New("manual acknowledgments disabled")
	ErrNetworkErrorAfterStored      = errors.New("error after packet added to state")         // Could not send packet but its stored (and response will be sent on chan at some point in the future)
	ErrConnectionLost               = errors.New("connection lost after request transmitted") // We don't know whether the server received the request or not

	ErrInvalidArguments = errors.New("invalid argument") // If included (errors.Join) in an error, there is a problem with the arguments passed. Retrying on the same connection with the same arguments will not succeed.
)

type (
	PublishReceived struct {
		Packet *Publish
		Client *Client // The Client that received the message (note that the connection may have been lost post-receipt)

		AlreadyHandled bool    // Set to true if a previous callback has returned true (indicating some action has already been taken re the message)
		Errs           []error // Errors returned by previous handlers (if any).
	}

	// ClientConfig are the user-configurable options for the client, an
	// instance of this struct is passed into NewClient(), not all options
	// are required to be set, defaults are provided for Persistence, MIDs,
	// PingHandler, PacketTimeout and Router.
	ClientConfig struct {
		ClientID string
		// Conn is the connection to broker.
		// BEWARE that most wrapped net.Conn implementations like tls.Conn are
		// not thread safe for writing. To fix, use packets.NewThreadSafeConn
		// wrapper or extend the custom net.Conn struct with sync.Locker.
		Conn net.Conn

		Session          session.SessionManager
		autoCloseSession bool

		AuthHandler   Auther
		PingHandler   Pinger
		defaultPinger bool

		// Router - new inbound messages will be passed to the `Route(*packets.Publish)` function.
		//
		// Depreciated: If a router is provided, it will now be added to the end of the OnPublishReceived
		// slice (which provides a more flexible approach to handling incoming messages).
		Router Router

		// OnPublishReceived provides a slice of callbacks; additional handlers may be added after the client has been
		// created via the AddOnPublishReceived function (Client holds a copy of the slice; OnPublishReceived will not change).
		// When a `PUBLISH` is received, the callbacks will be called in order. If a callback processes the message,
		// then it should return true. This boolean, and any errors, will be passed to subsequent handlers.
		OnPublishReceived []func(PublishReceived) (bool, error)

		PacketTimeout time.Duration
		// OnServerDisconnect is called only when a packets.DISCONNECT is received from server
		OnServerDisconnect func(*Disconnect)
		// OnClientError is for example called on net.Error. Note that this may be called multiple times and may be
		// called following a successful `Disconnect`. See autopaho.errorHandler for an example.
		OnClientError func(error)
		// PublishHook allows a user provided function to be called before
		// a Publish packet is sent allowing it to inspect or modify the
		// Publish, an example of the utility of this is provided in the
		// Topic Alias Handler extension which will automatically assign
		// and use topic alias values rather than topic strings.
		PublishHook func(*Publish)
		// EnableManualAcknowledgment is used to control the acknowledgment of packets manually.
		// BEWARE that the MQTT specs require clients to send acknowledgments in the order in which the corresponding
		// PUBLISH packets were received.
		// Consider the following scenario: the client receives packets 1,2,3,4
		// If you acknowledge 3 first, no ack is actually sent to the server but it's buffered until also 1 and 2
		// are acknowledged.
		EnableManualAcknowledgment bool
		// SendAcksInterval is used only when EnableManualAcknowledgment is true
		// it determines how often the client tries to send a batch of acknowledgments in the right order to the server.
		SendAcksInterval time.Duration
	}
	// Client is the struct representing an MQTT client
	Client struct {
		config ClientConfig

		// OnPublishReceived copy of OnPublishReceived from ClientConfig (perhaps with added callback form Router)
		onPublishReceived        []func(PublishReceived) (bool, error)
		onPublishReceivedTracker []int // Used to track positions in above
		onPublishReceivedMu      sync.Mutex

		// authResponse is used for handling the MQTTv5 authentication exchange (MUST be buffered)
		authResponse   chan<- packets.ControlPacket
		authResponseMu sync.Mutex // protects the above

		cancelFunc func()

		connectCalled   bool       // if true `Connect` has been called and a connection is being managed
		connectCalledMu sync.Mutex // protects the above

		done           <-chan struct{} // closed when shutdown complete (only valid after Connect returns nil error)
		publishPackets chan *packets.Publish
		acksTracker    acksTracker
		workers        sync.WaitGroup
		serverProps    CommsProperties
		clientProps    CommsProperties
		debug          log.Logger
		errors         log.Logger
	}

	// CommsProperties is a struct of the communication properties that may
	// be set by the server in the Connack and that the client needs to be
	// aware of for future subscribes/publishes
	CommsProperties struct {
		MaximumPacketSize    uint32
		ReceiveMaximum       uint16
		TopicAliasMaximum    uint16
		MaximumQoS           byte
		RetainAvailable      bool
		WildcardSubAvailable bool
		SubIDAvailable       bool
		SharedSubAvailable   bool
	}
)

// NewClient is used to create a new default instance of an MQTT client.
// It returns a pointer to the new client instance.
// The default client uses the provided PingHandler, MessageID and
// StandardRouter implementations, and a noop Persistence.
// These should be replaced if desired before the client is connected.
// client.Conn *MUST* be set to an already connected net.Conn before
// Connect() is called.
func NewClient(conf ClientConfig) *Client {
	c := &Client{
		serverProps: CommsProperties{
			ReceiveMaximum:       65535,
			MaximumQoS:           2,
			MaximumPacketSize:    0,
			TopicAliasMaximum:    0,
			RetainAvailable:      true,
			WildcardSubAvailable: true,
			SubIDAvailable:       true,
			SharedSubAvailable:   true,
		},
		clientProps: CommsProperties{
			ReceiveMaximum:    65535,
			MaximumQoS:        2,
			MaximumPacketSize: 0,
			TopicAliasMaximum: 0,
		},
		config:            conf,
		onPublishReceived: conf.OnPublishReceived,
		done:              make(chan struct{}),
		errors:            log.NOOPLogger{},
		debug:             log.NOOPLogger{},
	}

	if c.config.Session == nil {
		c.config.Session = state.NewInMemory()
		c.config.autoCloseSession = true // We created `Session`, so need to close it when done (so handlers all return)
	}
	if c.config.PacketTimeout == 0 {
		c.config.PacketTimeout = 10 * time.Second
	}

	if c.config.Router == nil && len(c.onPublishReceived) == 0 {
		c.config.Router = NewStandardRouter() // Maintain backwards compatibility (for now!)
	}
	if c.config.Router != nil {
		r := c.config.Router
		c.onPublishReceived = append(c.onPublishReceived,
			func(p PublishReceived) (bool, error) {
				r.Route(p.Packet.Packet())
				return false, nil
			})
	}
	c.onPublishReceivedTracker = make([]int, len(c.onPublishReceived)) // Must have the same number of elements as onPublishReceived

	if c.config.PingHandler == nil {
		c.config.PingHandler = NewDefaultPinger()
		c.config.defaultPinger = true
	}
	if c.config.OnClientError == nil {
		c.config.OnClientError = func(e error) {}
	}

	return c
}

// Connect is used to connect the client to a server. It presumes that
// the Client instance already has a working network connection.
// The function takes a pre-prepared Connect packet, and uses that to
// establish an MQTT connection. Assuming the connection completes
// successfully, the rest of the client is initiated and the Connack
// returned. Otherwise, the failure Connack (if there is one) is returned
// along with an error indicating the reason for the failure to connect.
func (c *Client) Connect(ctx context.Context, cp *Connect) (*Connack, error) {
	if c.config.Conn == nil {
		return nil, fmt.Errorf("client connection is nil")
	}

	// The connection is in c.config.Conn which is inaccessible to the user.
	// The end result of `Connect` (possibly some time after it returns) will be to close the connection so calling
	// Connect twice is invalid.
	c.connectCalledMu.Lock()
	if c.connectCalled {
		c.connectCalledMu.Unlock()
		return nil, fmt.Errorf("connect must only be called once")
	}
	c.connectCalled = true
	c.connectCalledMu.Unlock()

	// The passed in ctx applies to the connection process only. clientCtx applies to Client (signals that the
	// client should shut down).
	clientCtx, cancelFunc := context.WithCancel(context.Background())
	done := make(chan struct{})
	cleanup := func() {
		cancelFunc()
		close(c.publishPackets)
		_ = c.config.Conn.Close()
		close(done)
	}

	c.cancelFunc = cancelFunc
	c.done = done

	var publishPacketsSize uint16 = math.MaxUint16
	if cp.Properties != nil && cp.Properties.ReceiveMaximum != nil {
		publishPacketsSize = *cp.Properties.ReceiveMaximum
	}
	c.publishPackets = make(chan *packets.Publish, publishPacketsSize)

	keepalive := cp.KeepAlive
	c.config.ClientID = cp.ClientID
	if cp.Properties != nil {
		if cp.Properties.MaximumPacketSize != nil {
			c.clientProps.MaximumPacketSize = *cp.Properties.MaximumPacketSize
		}
		if cp.Properties.ReceiveMaximum != nil {
			c.clientProps.ReceiveMaximum = *cp.Properties.ReceiveMaximum
		}
		if cp.Properties.TopicAliasMaximum != nil {
			c.clientProps.TopicAliasMaximum = *cp.Properties.TopicAliasMaximum
		}
	}

	c.debug.Println("connecting")
	connCtx, cf := context.WithTimeout(ctx, c.config.PacketTimeout)
	defer cf()

	ccp := cp.Packet()
	ccp.ProtocolName = "MQTT"
	ccp.ProtocolVersion = 5

	c.debug.Println("sending CONNECT")
	if _, err := ccp.WriteTo(c.config.Conn); err != nil {
		cleanup()
		return nil, err
	}

	c.debug.Println("waiting for CONNACK/AUTH")
	var (
		caPacket *packets.Connack
		// We use buffered channels to prevent goroutine leak. The Details are below.
		// - c.expectConnack waits to send data to caPacketCh or caPacketErr.
		// - If connCtx is cancelled (done) before c.expectConnack finishes to send data to either "unbuffered" channel,
		//   c.expectConnack cannot exit (goroutine leak).
		caPacketCh  = make(chan *packets.Connack, 1)
		caPacketErr = make(chan error, 1)
	)
	go c.expectConnack(caPacketCh, caPacketErr)
	select {
	case <-connCtx.Done():
		ctxErr := connCtx.Err()
		c.debug.Println(fmt.Sprintf("terminated due to context waiting for CONNACK: %v", ctxErr))
		cleanup()
		return nil, ctxErr
	case err := <-caPacketErr:
		c.debug.Println(err)
		cleanup()
		return nil, err
	case caPacket = <-caPacketCh:
	}

	ca := ConnackFromPacketConnack(caPacket)

	if ca.ReasonCode >= 0x80 {
		var reason string
		c.debug.Println("received an error code in Connack:", ca.ReasonCode)
		if ca.Properties != nil {
			reason = ca.Properties.ReasonString
		}
		cleanup()
		return ca, fmt.Errorf("failed to connect to server: %s", reason)
	}

	if err := c.config.Session.ConAckReceived(c.config.Conn, ccp, caPacket); err != nil {
		cleanup()
		return ca, fmt.Errorf("session error: %w", err)
	}

	// the connection is now fully up and a nil error will be returned.
	// cleanup() must not be called past this point and will be handled by `shutdown`
	context.AfterFunc(clientCtx, func() { c.shutdown(done) })

	if ca.Properties != nil {
		if ca.Properties.ServerKeepAlive != nil {
			keepalive = *ca.Properties.ServerKeepAlive
		}
		if ca.Properties.AssignedClientID != "" {
			c.config.ClientID = ca.Properties.AssignedClientID
		}
		if ca.Properties.ReceiveMaximum != nil {
			c.serverProps.ReceiveMaximum = *ca.Properties.ReceiveMaximum
		}
		if ca.Properties.MaximumQoS != nil {
			c.serverProps.MaximumQoS = *ca.Properties.MaximumQoS
		}
		if ca.Properties.MaximumPacketSize != nil {
			c.serverProps.MaximumPacketSize = *ca.Properties.MaximumPacketSize
		}
		if ca.Properties.TopicAliasMaximum != nil {
			c.serverProps.TopicAliasMaximum = *ca.Properties.TopicAliasMaximum
		}
		c.serverProps.RetainAvailable = ca.Properties.RetainAvailable
		c.serverProps.WildcardSubAvailable = ca.Properties.WildcardSubAvailable
		c.serverProps.SubIDAvailable = ca.Properties.SubIDAvailable
		c.serverProps.SharedSubAvailable = ca.Properties.SharedSubAvailable
	}

	c.debug.Println("received CONNACK, starting PingHandler")
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		defer c.debug.Println("returning from ping handler worker")
		if err := c.config.PingHandler.Run(clientCtx, c.config.Conn, keepalive); err != nil {
			go c.error(fmt.Errorf("ping handler error: %w", err))
		}
	}()

	c.debug.Println("starting publish packets loop")
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		defer c.debug.Println("returning from publish packets loop worker")
		// exits when `c.publishPackets` is closed (`c.incoming()` closes this). This is important because
		// messages may be passed for processing after `c.stop` has been closed.
		c.routePublishPackets()
	}()

	c.debug.Println("starting incoming")
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		defer c.debug.Println("returning from incoming worker")
		c.incoming(clientCtx)
	}()

	if c.config.EnableManualAcknowledgment {
		c.debug.Println("starting acking routine")

		c.acksTracker.reset()
		sendAcksInterval := defaultSendAckInterval
		if c.config.SendAcksInterval > 0 {
			sendAcksInterval = c.config.SendAcksInterval
		}

		c.workers.Add(1)
		go func() {
			defer c.workers.Done()
			defer c.debug.Println("returning from ack tracker routine")
			t := time.NewTicker(sendAcksInterval)
			for {
				select {
				case <-clientCtx.Done():
					return
				case <-t.C:
					c.acksTracker.flush(func(pbs []*packets.Publish) {
						for _, pb := range pbs {
							c.ack(pb)
						}
					})
				}
			}
		}()
	}

	return ca, nil
}

// Done returns a channel that will be closed when Client has shutdown. Only valid after Connect has returned a
// nil error.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// Ack transmits an acknowledgement of the `Publish` packet.
// WARNING: Calling Ack after the connection is closed may have unpredictable results (particularly if the sessionState
// is being accessed by a new connection). See issue #160.
func (c *Client) Ack(pb *Publish) error {
	if !c.config.EnableManualAcknowledgment {
		return ErrManualAcknowledgmentDisabled
	}
	if pb.QoS == 0 {
		return nil
	}
	return c.acksTracker.markAsAcked(pb.Packet())
}

// ack acknowledges a message (note: called by acksTracker to ensure these are sent in order)
func (c *Client) ack(pb *packets.Publish) {
	c.config.Session.Ack(pb)
}

// routePublishPackets listens on c.publishPackets and passes received messages to the handlers
// terminates when publishPackets closed
func (c *Client) routePublishPackets() {
	for pb := range c.publishPackets {
		// Copy onPublishReceived so lock is only held briefly
		c.onPublishReceivedMu.Lock()
		handlers := make([]func(PublishReceived) (bool, error), len(c.onPublishReceived))
		for i := range c.onPublishReceived {
			handlers[i] = c.onPublishReceived[i]
		}
		c.onPublishReceivedMu.Unlock()

		if c.config.EnableManualAcknowledgment && pb.QoS != 0 {
			c.acksTracker.add(pb)
		}

		var handled bool
		var errs []error
		pkt := PublishFromPacketPublish(pb)
		for _, h := range handlers {
			ha, err := h(PublishReceived{
				Packet:         pkt,
				Client:         c,
				AlreadyHandled: handled,
				Errs:           errs,
			})
			if ha {
				handled = true
			}
			errs = append(errs, err)
		}

		if !c.config.EnableManualAcknowledgment {
			c.ack(pb)
		}
	}
}

// incoming is the Client function that reads and handles incoming
// packets from the server. The function is started as a goroutine
// from Connect(), it exits when it receives a server initiated
// Disconnect, the Stop channel is closed or there is an error reading
// a packet from the network connection
// Closes `c.publishPackets` when done (should be the only thing sending on this channel)
func (c *Client) incoming(ctx context.Context) {
	defer c.debug.Println("client stopping, incoming stopping")
	defer close(c.publishPackets)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			recv, err := packets.ReadPacket(c.config.Conn)
			if err != nil {
				go c.error(err)
				return
			}
			c.config.PingHandler.PacketReceived()
			switch recv.Type {
			case packets.CONNACK:
				c.debug.Println("received CONNACK (unexpected)")
				go c.error(fmt.Errorf("received unexpected CONNACK"))
				return
			case packets.AUTH:
				c.debug.Println("received AUTH")
				ap := recv.Content.(*packets.Auth)
				switch ap.ReasonCode {
				case packets.AuthSuccess:
					if c.config.AuthHandler != nil {
						go c.config.AuthHandler.Authenticated()
					}
					c.authResponseMu.Lock()
					if c.authResponse != nil {
						select { // authResponse must be buffered, and we should only receive 1 AUTH packet a time
						case c.authResponse <- *recv:
						default:
						}
					}
					c.authResponseMu.Unlock()
				case packets.AuthContinueAuthentication:
					if c.config.AuthHandler != nil {
						if _, err := c.config.AuthHandler.Authenticate(AuthFromPacketAuth(ap)).Packet().WriteTo(c.config.Conn); err != nil {
							go c.error(err)
							return
						}
						c.config.PingHandler.PacketSent()
					}
				}
			case packets.PUBLISH:
				pb := recv.Content.(*packets.Publish)
				if pb.QoS > 0 { // QOS1 or 2 need to be recorded in session state
					c.config.Session.PacketReceived(recv, c.publishPackets)
				} else {
					c.debug.Printf("received QoS%d PUBLISH", pb.QoS)
					select {
					case <-ctx.Done():
						return
					case c.publishPackets <- pb:
					}
				}
			case packets.PUBACK, packets.PUBCOMP, packets.SUBACK, packets.UNSUBACK, packets.PUBREC, packets.PUBREL:
				c.config.Session.PacketReceived(recv, c.publishPackets)
			case packets.DISCONNECT:
				pd := recv.Content.(*packets.Disconnect)
				c.debug.Println("received DISCONNECT")
				c.authResponseMu.Lock()
				if c.authResponse != nil {
					select { // authResponse must be buffered, and we should only receive 1 AUTH packet a time
					case c.authResponse <- *recv:
					default:
					}
				}
				c.authResponseMu.Unlock()
				c.config.Session.ConnectionLost(pd) // this may impact the session state
				go func() {
					if c.config.OnServerDisconnect != nil {
						go c.serverDisconnect(DisconnectFromPacketDisconnect(pd))
					} else {
						go c.error(fmt.Errorf("server initiated disconnect"))
					}
				}()
				return
			case packets.PINGRESP:
				c.debug.Println("received PINGRESP")
				c.config.PingHandler.PingResp()
			}
		}
	}
}

// close terminates the connection and waits for a clean shutdown
// may be called multiple times (subsequent calls will wait on previously requested shutdown)
func (c *Client) close() {
	c.cancelFunc() // cleanup handled by AfterFunc defined in Connect
	<-c.done
}

// shutdown cleanly shutdown the client
// This should only be called via the AfterFunc in `Connect` (shutdown must not be called more than once)
func (c *Client) shutdown(done chan<- struct{}) {
	c.debug.Println("client stop requested")
	_ = c.config.Conn.Close()
	c.debug.Println("conn closed")
	c.acksTracker.reset()
	c.debug.Println("acks tracker reset")
	c.config.Session.ConnectionLost(nil)
	if c.config.autoCloseSession {
		if err := c.config.Session.Close(); err != nil {
			c.errors.Println("error closing session", err)
		}
	}
	c.debug.Println("session updated, waiting on workers")
	c.workers.Wait()
	c.debug.Println("workers done")
	close(done)
}

// error is called to signify that an error situation has occurred, this
// causes the client's Stop channel to be closed (if it hasn't already been)
// which results in the other client goroutines terminating.
// It also closes the client network connection.
func (c *Client) error(e error) {
	c.debug.Println("error called:", e)
	c.close()
	go c.config.OnClientError(e)
}

func (c *Client) serverDisconnect(d *Disconnect) {
	c.close()
	c.debug.Println("calling OnServerDisconnect")
	go c.config.OnServerDisconnect(d)
}

// Authenticate is used to initiate a reauthentication of credentials with the
// server. This function sends the initial Auth packet to start the reauthentication
// then relies on the client AuthHandler managing any further requests from the
// server until either a successful Auth packet is passed back, or a Disconnect
// is received.
func (c *Client) Authenticate(ctx context.Context, a *Auth) (*AuthResponse, error) {
	c.debug.Println("client initiated reauthentication")
	authResp := make(chan packets.ControlPacket, 1)
	c.authResponseMu.Lock()
	if c.authResponse != nil {
		c.authResponseMu.Unlock()
		return nil, fmt.Errorf("previous authentication is still in progress")
	}
	c.authResponse = authResp
	c.authResponseMu.Unlock()
	defer func() {
		c.authResponseMu.Lock()
		c.authResponse = nil
		c.authResponseMu.Unlock()
	}()

	c.debug.Println("sending AUTH")
	if _, err := a.Packet().WriteTo(c.config.Conn); err != nil {
		return nil, err
	}
	c.config.PingHandler.PacketSent()

	var rp packets.ControlPacket
	select {
	case <-ctx.Done():
		ctxErr := ctx.Err()
		c.debug.Println(fmt.Sprintf("terminated due to context waiting for AUTH: %v", ctxErr))
		return nil, ctxErr
	case rp = <-authResp:
	}

	switch rp.Type {
	case packets.AUTH:
		// If we've received one here it must be successful, the only way
		// to abort a reauth is a server initiated disconnect
		return AuthResponseFromPacketAuth(rp.Content.(*packets.Auth)), nil
	case packets.DISCONNECT:
		return AuthResponseFromPacketDisconnect(rp.Content.(*packets.Disconnect)), nil
	}

	return nil, fmt.Errorf("error with Auth, didn't receive Auth or Disconnect")
}

// Subscribe is used to send a Subscription request to the MQTT server.
// It is passed a pre-prepared Subscribe packet and blocks waiting for
// a response Suback, or for the timeout to fire. Any response Suback
// is returned from the function, along with any errors.
func (c *Client) Subscribe(ctx context.Context, s *Subscribe) (*Suback, error) {
	if !c.serverProps.WildcardSubAvailable {
		for _, sub := range s.Subscriptions {
			if strings.ContainsAny(sub.Topic, "#+") {
				// Using a wildcard in a subscription when not supported
				return nil, fmt.Errorf("%w: cannot subscribe to %s, server does not support wildcards", ErrInvalidArguments, sub.Topic)
			}
		}
	}
	if !c.serverProps.SubIDAvailable && s.Properties != nil && s.Properties.SubscriptionIdentifier != nil {
		return nil, fmt.Errorf("%w: cannot send subscribe with subID set, server does not support subID", ErrInvalidArguments)
	}
	if !c.serverProps.SharedSubAvailable {
		for _, sub := range s.Subscriptions {
			if strings.HasPrefix(sub.Topic, "$share") {
				return nil, fmt.Errorf("%w: cannont subscribe to %s, server does not support shared subscriptions", ErrInvalidArguments, sub.Topic)
			}
		}
	}

	c.debug.Printf("subscribing to %+v", s.Subscriptions)

	ret := make(chan packets.ControlPacket, 1)
	sp := s.Packet()
	if err := c.config.Session.AddToSession(ctx, sp, ret); err != nil {
		return nil, err
	}

	// From this point on the message is in store, and ret will receive something regardless of whether we succeed in
	// writing the packet to the connection or not.
	if _, err := sp.WriteTo(c.config.Conn); err != nil {
		// The packet will remain in the session state until `Session` is notified of the disconnection.
		return nil, err
	}
	c.config.PingHandler.PacketSent()

	c.debug.Println("waiting for SUBACK")
	subCtx, cf := context.WithTimeout(ctx, c.config.PacketTimeout)
	defer cf()
	var sap packets.ControlPacket

	select {
	case <-subCtx.Done():
		ctxErr := subCtx.Err()
		c.debug.Println(fmt.Sprintf("terminated due to context waiting for SUBACK: %v", ctxErr))
		return nil, ctxErr
	case sap = <-ret:
	}

	if sap.Type == 0 { // default ControlPacket indicates we are shutting down
		return nil, ErrConnectionLost
	}

	if sap.Type != packets.SUBACK {
		return nil, fmt.Errorf("received %d instead of Suback", sap.Type)
	}
	c.debug.Println("received SUBACK")

	sa := SubackFromPacketSuback(sap.Content.(*packets.Suback))
	switch {
	case len(sa.Reasons) == 1:
		if sa.Reasons[0] >= 0x80 {
			var reason string
			c.debug.Println("received an error code in Suback:", sa.Reasons[0])
			if sa.Properties != nil {
				reason = sa.Properties.ReasonString
			}
			return sa, fmt.Errorf("failed to subscribe to topic: %s", reason)
		}
	default:
		for _, code := range sa.Reasons {
			if code >= 0x80 {
				c.debug.Println("received an error code in Suback:", code)
				return sa, fmt.Errorf("at least one requested subscription failed")
			}
		}
	}

	return sa, nil
}

// Unsubscribe is used to send an Unsubscribe request to the MQTT server.
// It is passed a pre-prepared Unsubscribe packet and blocks waiting for
// a response Unsuback, or for the timeout to fire. Any response Unsuback
// is returned from the function, along with any errors.
func (c *Client) Unsubscribe(ctx context.Context, u *Unsubscribe) (*Unsuback, error) {
	c.debug.Printf("unsubscribing from %+v", u.Topics)
	ret := make(chan packets.ControlPacket, 1)
	up := u.Packet()
	if err := c.config.Session.AddToSession(ctx, up, ret); err != nil {
		return nil, err
	}

	// From this point on the message is in store, and ret will receive something regardless of whether we succeed in
	// writing the packet to the connection or not
	if _, err := up.WriteTo(c.config.Conn); err != nil {
		// The packet will remain in the session state until `Session` is notified of the disconnection.
		return nil, err
	}
	c.config.PingHandler.PacketSent()

	unsubCtx, cf := context.WithTimeout(ctx, c.config.PacketTimeout)
	defer cf()
	var uap packets.ControlPacket

	c.debug.Println("waiting for UNSUBACK")
	select {
	case <-unsubCtx.Done():
		ctxErr := unsubCtx.Err()
		c.debug.Println(fmt.Sprintf("terminated due to context waiting for UNSUBACK: %v", ctxErr))
		return nil, ctxErr
	case uap = <-ret:
	}

	if uap.Type == 0 { // default ControlPacket indicates we are shutting down
		return nil, ErrConnectionLost
	}

	if uap.Type != packets.UNSUBACK {
		return nil, fmt.Errorf("received %d instead of Unsuback", uap.Type)
	}
	c.debug.Println("received SUBACK")

	ua := UnsubackFromPacketUnsuback(uap.Content.(*packets.Unsuback))
	switch {
	case len(ua.Reasons) == 1:
		if ua.Reasons[0] >= 0x80 {
			var reason string
			c.debug.Println("received an error code in Unsuback:", ua.Reasons[0])
			if ua.Properties != nil {
				reason = ua.Properties.ReasonString
			}
			return ua, fmt.Errorf("failed to unsubscribe from topic: %s", reason)
		}
	default:
		for _, code := range ua.Reasons {
			if code >= 0x80 {
				c.debug.Println("received an error code in Suback:", code)
				return ua, fmt.Errorf("at least one requested unsubscribe failed")
			}
		}
	}

	return ua, nil
}

// Publish is used to send a publication to the MQTT server.
// It is passed a pre-prepared Publish packet and blocks waiting for the appropriate response, or for the timeout to fire.
// A PublishResponse is returned, which is relevant for QOS1+. For QOS0, a default success response is returned.
// Note that a message may still be delivered even if Publish times out (once the message is part of the session state,
// it may even be delivered following an application restart).
// Warning: Publish may outlive the connection when QOS1+ (managed in `session_state`)
func (c *Client) Publish(ctx context.Context, p *Publish) (*PublishResponse, error) {
	return c.PublishWithOptions(ctx, p, PublishOptions{})
}

type PublishMethod int

const (
	PublishMethod_Blocking  PublishMethod = iota // by default PublishWithOptions will block until the publish transaction is complete
	PublishMethod_AsyncSend                      // PublishWithOptions will add the message to the session and then return (no method to check status is provided)
)

// PublishOptions enables the behaviour of Publish to be modified
type PublishOptions struct {
	// Method enables a degree of control over how  PublishWithOptions operates
	Method PublishMethod
}

// PublishWithOptions is used to send a publication to the MQTT server (with options to customise its behaviour)
// It is passed a pre-prepared Publish packet and, by default, blocks waiting for the appropriate response, or for the
// timeout to fire. A PublishResponse is returned, which is relevant for QOS1+. For QOS0, a default success response is returned.
// Note that a message may still be delivered even if Publish times out (once the message is part of the session state,
// it may even be delivered following an application restart).
// Warning: Publish may outlive the connection when QOS1+ (managed in `session_state`)
func (c *Client) PublishWithOptions(ctx context.Context, p *Publish, o PublishOptions) (*PublishResponse, error) {
	if p.QoS > c.serverProps.MaximumQoS {
		return nil, fmt.Errorf("%w: cannot send Publish with QoS %d, server maximum QoS is %d", ErrInvalidArguments, p.QoS, c.serverProps.MaximumQoS)
	}
	if p.Properties != nil && p.Properties.TopicAlias != nil {
		if c.serverProps.TopicAliasMaximum > 0 && *p.Properties.TopicAlias > c.serverProps.TopicAliasMaximum {
			return nil, fmt.Errorf("%w: cannot send publish with TopicAlias %d, server topic alias maximum is %d", ErrInvalidArguments, *p.Properties.TopicAlias, c.serverProps.TopicAliasMaximum)
		}
	}
	if !c.serverProps.RetainAvailable && p.Retain {
		return nil, fmt.Errorf("%w: cannot send Publish with retain flag set, server does not support retained messages", ErrInvalidArguments)
	}
	if (p.Properties == nil || p.Properties.TopicAlias == nil) && p.Topic == "" {
		return nil, fmt.Errorf("%w: cannot send a publish with no TopicAlias and no Topic set", ErrInvalidArguments)
	}

	if c.config.PublishHook != nil {
		c.config.PublishHook(p)
	}

	c.debug.Printf("sending message to %s", p.Topic)

	pb := p.Packet()

	switch p.QoS {
	case 0:
		c.debug.Println("sending QoS0 message")
		if _, err := pb.WriteTo(c.config.Conn); err != nil {
			go c.error(err)
			return nil, err
		}
		c.config.PingHandler.PacketSent()
		return &PublishResponse{}, nil
	case 1, 2:
		return c.publishQoS12(ctx, pb, o)
	}

	return nil, fmt.Errorf("%w: QoS isn't 0, 1 or 2", ErrInvalidArguments)
}

func (c *Client) publishQoS12(ctx context.Context, pb *packets.Publish, o PublishOptions) (*PublishResponse, error) {
	c.debug.Println("sending QoS12 message")
	pubCtx, cf := context.WithTimeout(ctx, c.config.PacketTimeout)
	defer cf()

	ret := make(chan packets.ControlPacket, 1)
	if err := c.config.Session.AddToSession(pubCtx, pb, ret); err != nil {
		return nil, err
	}

	// From this point on the message is in store, and ret will receive something regardless of whether we succeed in
	// writing the packet to the connection
	if _, err := pb.WriteTo(c.config.Conn); err != nil {
		c.debug.Printf("failed to write packet %d to connection: %s", pb.PacketID, err)
		if o.Method == PublishMethod_AsyncSend {
			return nil, ErrNetworkErrorAfterStored // Async send, so we don't wait for the response (may add callbacks in the future to enable user to obtain status)
		}
	}
	c.config.PingHandler.PacketSent()

	if o.Method == PublishMethod_AsyncSend {
		return nil, nil // Async send, so we don't wait for the response (may add callbacks in the future to enable user to obtain status)
	}

	var resp packets.ControlPacket
	select {
	case <-pubCtx.Done():
		ctxErr := pubCtx.Err()
		c.debug.Println(fmt.Sprintf("terminated due to context waiting for Publish ack: %v", ctxErr))
		return nil, ctxErr
	case resp = <-ret:
	}

	if resp.Type == 0 { // default ControlPacket indicates we are shutting down
		return nil, errors.New("PUBLISH transmitted but not fully acknowledged at time of shutdown")
	}

	switch pb.QoS {
	case 1:
		if resp.Type != packets.PUBACK {
			return nil, fmt.Errorf("received %d instead of PUBACK", resp.Type)
		}

		pr := PublishResponseFromPuback(resp.Content.(*packets.Puback))
		if pr.ReasonCode >= 0x80 {
			c.debug.Println("received an error code in Puback:", pr.ReasonCode)
			return pr, fmt.Errorf("error publishing: %s", resp.Content.(*packets.Puback).Reason())
		}
		return pr, nil
	case 2:
		switch resp.Type {
		case packets.PUBCOMP:
			pr := PublishResponseFromPubcomp(resp.Content.(*packets.Pubcomp))
			return pr, nil
		case packets.PUBREC:
			c.debug.Printf("received PUBREC for %s (must have errored)", pb.PacketID)
			pr := PublishResponseFromPubrec(resp.Content.(*packets.Pubrec))
			return pr, nil
		default:
			return nil, fmt.Errorf("received %d instead of PUBCOMP", resp.Type)
		}
	}

	c.debug.Println("ended up with a non QoS1/2 message:", pb.QoS)
	return nil, fmt.Errorf("ended up with a non QoS1/2 message: %d", pb.QoS)
}

func (c *Client) expectConnack(packet chan<- *packets.Connack, errs chan<- error) {
	recv, err := packets.ReadPacket(c.config.Conn)
	if err != nil {
		errs <- err
		return
	}
	switch r := recv.Content.(type) {
	case *packets.Connack:
		c.debug.Println("received CONNACK")
		if r.ReasonCode == packets.ConnackSuccess && r.Properties != nil && r.Properties.AuthMethod != "" {
			// Successful connack and AuthMethod is defined, must have successfully authed during connect
			go c.config.AuthHandler.Authenticated()
		}
		packet <- r
	case *packets.Auth:
		c.debug.Println("received AUTH")
		if c.config.AuthHandler == nil {
			errs <- fmt.Errorf("enhanced authentication flow started but no AuthHandler configured")
			return
		}
		c.debug.Println("sending AUTH")
		_, err := c.config.AuthHandler.Authenticate(AuthFromPacketAuth(r)).Packet().WriteTo(c.config.Conn)
		if err != nil {
			errs <- fmt.Errorf("error sending authentication packet: %w", err)
			return
		}
		// go round again, either another AUTH or CONNACK
		go c.expectConnack(packet, errs)
	default:
		errs <- fmt.Errorf("received unexpected packet %v", recv.Type)
	}

}

// Disconnect is used to send a Disconnect packet to the MQTT server
// Whether or not the attempt to send the Disconnect packet fails
// (and if it does this function returns any error) the network connection
// is closed.
func (c *Client) Disconnect(d *Disconnect) error {
	c.debug.Println("disconnecting", d)
	_, err := d.Packet().WriteTo(c.config.Conn)

	c.close()

	return err
}

// AddOnPublishReceived adds a function that will be called when a PUBLISH is received
// The new function will be called after any functions already in the list
// Returns a function that can be called to remove the callback
func (c *Client) AddOnPublishReceived(f func(PublishReceived) (bool, error)) func() {
	c.onPublishReceivedMu.Lock()
	defer c.onPublishReceivedMu.Unlock()

	c.onPublishReceived = append(c.onPublishReceived, f)

	// We insert a unique ID into the same position in onPublishReceivedTracker; this enables us to
	// remove the handler later (without complicating onPublishReceived which will be called frequently)
	var id int
idLoop:
	for id = 0; ; id++ {
		for _, used := range c.onPublishReceivedTracker {
			if used == id {
				continue idLoop
			}
		}
		break
	}
	c.onPublishReceivedTracker = append(c.onPublishReceivedTracker, id)

	return func() {
		c.onPublishReceivedMu.Lock()
		defer c.onPublishReceivedMu.Unlock()
		for pos, storedID := range c.onPublishReceivedTracker {
			if id == storedID {
				c.onPublishReceivedTracker = append(c.onPublishReceivedTracker[:pos], c.onPublishReceivedTracker[pos+1:]...)
				c.onPublishReceived = append(c.onPublishReceived[:pos], c.onPublishReceived[pos+1:]...)
			}
		}
	}
}

// ClientID retrieves the client ID from the config (sometimes used in handlers that require the ID)
func (c *Client) ClientID() string {
	return c.config.ClientID
}

// SetDebugLogger takes an instance of the paho Logger interface
// and sets it to be used by the debug log endpoint
func (c *Client) SetDebugLogger(l log.Logger) {
	c.debug = l
	if c.config.autoCloseSession { // If we created the session store then it should use the same logger
		c.config.Session.SetDebugLogger(l)
	}
	if c.config.defaultPinger { // Debug logger is set after the client is created so need to copy it to pinger
		c.config.PingHandler.SetDebug(c.debug)
	}
}

// SetErrorLogger takes an instance of the paho Logger interface
// and sets it to be used by the error log endpoint
func (c *Client) SetErrorLogger(l log.Logger) {
	c.errors = l
	if c.config.autoCloseSession { // If we created the session store then it should use the same logger
		c.config.Session.SetErrorLogger(l)
	}
}

// TerminateConnectionForTest closes the active connection (if any). This function is intended for testing only, it
// simulates connection loss which supports testing QOS1 and 2 message delivery.
func (c *Client) TerminateConnectionForTest() {
	_ = c.config.Conn.Close()
}
