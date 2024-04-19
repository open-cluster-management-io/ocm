package cloudevents

import "github.com/onsi/ginkgo/v2"

var _ = ginkgo.Describe("ManifestWork Update Strategy (MQTT)", runUpdateStrategyTest(mqttSourceInfo, clusterName))
