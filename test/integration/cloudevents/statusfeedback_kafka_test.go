package cloudevents

import "github.com/onsi/ginkgo/v2"

var _ = ginkgo.Describe("ManifestWork Status Feedback (Kafka)", runStatusFeedbackTest(kafkaSourceInfo, kClusterNameGenerator.generate))
