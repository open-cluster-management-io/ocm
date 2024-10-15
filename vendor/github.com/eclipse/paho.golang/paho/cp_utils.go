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
	"github.com/eclipse/paho.golang/packets"
)

// UserProperty is a struct for the user provided values
// permitted in the properties section
type UserProperty struct {
	Key, Value string
}

// UserProperties is a slice of UserProperty
type UserProperties []UserProperty

// Add is a helper function for easily adding a new user property
func (u *UserProperties) Add(key, value string) *UserProperties {
	*u = append(*u, UserProperty{key, value})

	return u
}

// Get returns the first entry in the UserProperties that matches
// key, or an empty string if the key is not found. Note that it is
// permitted to have multiple entries with the same key, use GetAll
// if it is expected to have multiple matches
func (u UserProperties) Get(key string) string {
	for _, v := range u {
		if v.Key == key {
			return v.Value
		}
	}

	return ""
}

// GetAll returns a slice of all entries in the UserProperties
// that match key, or a nil slice if none were found.
func (u UserProperties) GetAll(key string) []string {
	var ret []string
	for _, v := range u {
		if v.Key == key {
			ret = append(ret, v.Value)
		}
	}

	return ret
}

// ToPacketProperties converts a UserProperties to a slice
// of packets.User which is used internally in the packets
// library for user properties
func (u UserProperties) ToPacketProperties() []packets.User {
	ret := make([]packets.User, len(u))
	for i, v := range u {
		ret[i] = packets.User{Key: v.Key, Value: v.Value}
	}

	return ret
}

// UserPropertiesFromPacketUser converts a slice of packets.User
// to an instance of UserProperties for easier consumption within
// the client library
func UserPropertiesFromPacketUser(up []packets.User) UserProperties {
	ret := make(UserProperties, len(up))
	for i, v := range up {
		ret[i] = UserProperty{v.Key, v.Value}
	}

	return ret
}

// Byte is a helper function that take a byte and returns
// a pointer to a byte of that value
func Byte(b byte) *byte {
	return &b
}

// Uint32 is a helper function that take a uint32 and returns
// a pointer to a uint32 of that value
func Uint32(u uint32) *uint32 {
	return &u
}

// Uint16 is a helper function that take a uint16 and returns
// a pointer to a uint16 of that value
func Uint16(u uint16) *uint16 {
	return &u
}

// BoolToByte is a helper function that take a bool and returns
// a pointer to a byte of value 1 if true or 0 if false
func BoolToByte(b bool) *byte {
	var v byte
	if b {
		v = 1
	}
	return &v
}
