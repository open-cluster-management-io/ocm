package helpers

import (
	"crypto/md5" // #nosec G501
	"encoding/hex"
	"strings"
)

func Md5HashSuffix(hubClusterAccountId string, hubClusterName string, managedClusterAccountId string, managedClusterName string) string {
	hash := md5.Sum([]byte(strings.Join([]string{hubClusterAccountId, hubClusterName, managedClusterAccountId, managedClusterName}, "#"))) // #nosec G401
	return hex.EncodeToString(hash[:])
}
