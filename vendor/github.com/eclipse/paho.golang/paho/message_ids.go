package paho

import (
	"context"
	"errors"
	"sync"

	"github.com/eclipse/paho.golang/packets"
)

const (
	midMin uint16 = 1
	midMax uint16 = 65535
)

// ErrorMidsExhausted is returned from Request() when there are no
// free message ids to be used.
var ErrorMidsExhausted = errors.New("all message ids in use")

// MIDService defines the interface for a struct that handles the
// relationship between message ids and CPContexts
// Request() takes a *CPContext and returns a uint16 that is the
// messageid that should be used by the code that called Request()
// Get() takes a uint16 that is a messageid and returns the matching
// *CPContext that the MIDService has associated with that messageid
// Free() takes a uint16 that is a messageid and instructs the MIDService
// to mark that messageid as available for reuse
// Clear() resets the internal state of the MIDService
type MIDService interface {
	Request(*CPContext) (uint16, error)
	Get(uint16) *CPContext
	Free(uint16)
	Clear()
}

// CPContext is the struct that is used to return responses to
// ControlPackets that have them, eg: the suback to a subscribe.
// The response packet is send down the Return channel and the
// Context is used to track timeouts.
type CPContext struct {
	Context context.Context
	Return  chan packets.ControlPacket
}

// MIDs is the default MIDService provided by this library.
// It uses a map of uint16 to *CPContext to track responses
// to messages with a messageid
type MIDs struct {
	sync.Mutex
	lastMid uint16
	index   []*CPContext
}

// Request is the library provided MIDService's implementation of
// the required interface function()
func (m *MIDs) Request(c *CPContext) (uint16, error) {
	m.Lock()
	defer m.Unlock()
	for i := uint16(1); i < midMax; i++ {
		v := (m.lastMid + i) % midMax
		if v == 0 {
			continue
		}
		if inuse := m.index[v]; inuse == nil {
			m.index[v] = c
			m.lastMid = v
			return v, nil
		}
	}
	return 0, ErrorMidsExhausted
}

// Get is the library provided MIDService's implementation of
// the required interface function()
func (m *MIDs) Get(i uint16) *CPContext {
	m.Lock()
	defer m.Unlock()
	return m.index[i]
}

// Free is the library provided MIDService's implementation of
// the required interface function()
func (m *MIDs) Free(i uint16) {
	m.Lock()
	m.index[i] = nil
	m.Unlock()
}

// Clear is the library provided MIDService's implementation of
// the required interface function()
func (m *MIDs) Clear() {
	m.index = make([]*CPContext, int(midMax))
}
