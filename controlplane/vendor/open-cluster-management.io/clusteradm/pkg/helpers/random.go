// Copyright Contributors to the Open Cluster Management project

package helpers

import (
	"math/rand"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letterRunes_az09 = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

func RandStringRunes_az09(n int) string {
	return randStringRunes(n, letterRunes_az09)
}

func randStringRunes(n int, runes []rune) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = runes[rand.Intn(len(runes))]
	}
	return string(b)
}
