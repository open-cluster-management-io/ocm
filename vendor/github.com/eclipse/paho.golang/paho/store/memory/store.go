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

package memory

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/eclipse/paho.golang/packets"
)

var (
	ErrNotInStore = errors.New("the requested ID was not found in the store") // Returned when requested ID not found
)

// memoryPacket is an element in the memory store
type memoryPacket struct {
	c int    // message count (used for ordering; as this is 32 bit min chance of rolling over seems remote)
	p []byte // the packet we are storing
}

// New creates a Store
func New() *Store {
	return &Store{
		data: make(map[uint16]memoryPacket),
	}

}

// Store is an implementation of a Store that stores the data in memory
type Store struct {
	// server store - holds packets where the message ID was generated on the server
	sync.Mutex
	data map[uint16]memoryPacket // Holds messages initiated by the server (i.e. we will receive the PUBLISH)
	c    int                     // sequence counter used to maintain message order
}

// Put stores the packet
func (m *Store) Put(packetID uint16, packetType byte, w io.WriterTo) error {
	m.Lock()
	defer m.Unlock()
	var buff bytes.Buffer

	_, err := w.WriteTo(&buff)
	if err != nil {
		panic(err)
	}

	m.data[packetID] = memoryPacket{
		c: m.c,
		p: buff.Bytes(),
	}
	m.c++
	return nil
}

func (m *Store) Get(packetID uint16) (io.ReadCloser, error) {
	m.Lock()
	defer m.Unlock()
	d, ok := m.data[packetID]
	if !ok {
		return nil, ErrNotInStore
	}
	return io.NopCloser(bytes.NewReader(d.p)), nil
}

// Delete removes the message with the specified store ID
func (m *Store) Delete(id uint16) error {
	m.Lock()
	defer m.Unlock()
	if _, ok := m.data[id]; !ok {
		// This could be ignored, but reporting it may help reveal other issues
		return fmt.Errorf("request to delete packet %d; packet not found", id)
	}
	delete(m.data, id)
	return nil
}

// Quarantine is called if a corrupt packet is detected.
// There is little we can do other than deleting the packet.
func (m *Store) Quarantine(id uint16) error {
	return m.Delete(id)
}

// List returns packet IDs in the order they were Put
func (m *Store) List() ([]uint16, error) {
	m.Lock()
	defer m.Unlock()

	ids := make([]uint16, 0, len(m.data))
	seq := make([]int, 0, len(m.data))

	// Basic insert sort from map ordered by time
	// As the map is relatively small, this should be quick enough (data is retrieved infrequently)
	itemNo := 0
	var pos int
	for i, v := range m.data {
		for pos = 0; pos < itemNo; pos++ {
			if seq[pos] > v.c {
				break
			}
		}
		ids = append(ids[:pos], append([]uint16{i}, ids[pos:]...)...)
		seq = append(seq[:pos], append([]int{v.c}, seq[pos:]...)...)
		itemNo++
	}
	return ids, nil
}

// Reset clears the store (deleting all messages)
func (m *Store) Reset() error {
	m.Lock()
	defer m.Unlock()
	m.data = make(map[uint16]memoryPacket)
	return nil
}

// String is for debugging purposes; it dumps the content of the store in a readable format
func (m *Store) String() string {
	var b bytes.Buffer
	for i, c := range m.data {
		p, err := packets.ReadPacket(bytes.NewReader(c.p))
		if err != nil {
			b.WriteString(fmt.Sprintf("packet %d could not be read: %s\n", i, err))
			continue
		}

		b.WriteString(fmt.Sprintf("packet %d is %s\n", i, p))
	}
	return b.String()
}
