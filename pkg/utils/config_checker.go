package utils

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"

	"k8s.io/apiserver/pkg/server/healthz"
)

var _ healthz.HealthChecker = &configChecker{}

// configChecker is used to notify container to restart when config files updated
type configChecker struct {
	name        string
	configfiles []string
	checksum    [16]byte
	reload      bool
	sync.Mutex
}

// NewConfigChecker
// name could be any string.
// configfiles should be the same as your target container are using now.
//
// Example:
// type configCheckerServer struct {
// 	checkers []heathz.HealthChecker
// }
//
// func NewConfigCheckerServer(checkers []healthz.HealthChecker) *configCheckerServer {
// 	return &configCheckerServer{checkers: checkers}
// }
//
// func (s *configCheckerServer) ServerHttp(rw http.ResponseWriter, r *http.Request) {
// 	for _, c := range s.chekers {
// 		if c.Name() == r.URL {
// 			if err := c.Check(); err != nil {
// 				rw.WriteHeader(500)
// 			} else {
// 				rw.WriteHeader(200)
// 			}
// 		}
// 	}
// }
//
// Containers in the same pod of configCheckServer can add liveness probe and be triggered restart as config files be modified.
func NewConfigChecker(name string, configfiles ...string) (*configChecker, error) {
	checksum, err := load(configfiles)
	if err != nil {
		return nil, err
	}
	return &configChecker{
		name:        name,
		configfiles: configfiles,
		checksum:    checksum,
		reload:      false,
	}, nil
}

// SetReload can update the ‘reload’ fields of config checker
// If reload equals to false, config checker won't update the checksum value in the cache, and function Check would return error forever if config files are modified.
// but if reload equals to true, config checker only returns err once, and it updates the cache with the latest checksum of config files.
func (c *configChecker) SetReload(reload bool) {
	c.reload = reload
}

// Name return the name fo the configChecker
func (c *configChecker) Name() string {
	return c.name
}

// Check would return nil if current configfiles's checksum is equal to cached checksum
// If checksum not equal, it will return err and update cached checksum with current checksum
// Note that: configChecker performs a instant update after it returns err, so DO NOT use one configChecker for multible containers!!!
func (cc *configChecker) Check(_ *http.Request) error {
	newChecksum, err := load(cc.configfiles)
	if err != nil {
		return err
	}
	if newChecksum != cc.checksum {
		cc.Lock()
		if cc.reload {
			cc.checksum = newChecksum // update checksum
		}
		cc.Unlock()
		return fmt.Errorf("checksum not equal")
	}
	return nil
}

// load generates a checksum of all config files' content
func load(configfiles []string) ([16]byte, error) {
	var allContent []byte
	for _, c := range configfiles {
		content, err := ioutil.ReadFile(c)
		if err != nil {
			return [16]byte{}, fmt.Errorf("read %s failed, %v", c, err)
		}
		allContent = append(allContent, content...)
	}
	return md5.Sum(allContent), nil
}
