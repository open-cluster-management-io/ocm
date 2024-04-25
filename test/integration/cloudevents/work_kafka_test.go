package cloudevents

import (
	"github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("ManifestWork (Kafka)", runWorkTest(kafkaSourceInfo, kClusterNameGenerator.generate))
