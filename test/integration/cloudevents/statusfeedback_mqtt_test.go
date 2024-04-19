package cloudevents

import "github.com/onsi/ginkgo/v2"

var _ = ginkgo.Describe("ManifestWork Status Feedback (MQTT)", runStatusFeedbackTest(mqttSourceInfo, clusterName))
