package cloudevents

import (
	"github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("ManifestWork Delete Option (Kafka)", runDeleteOptionTest(kafkaSourceInfo, kClusterNameGenerator.generate))
