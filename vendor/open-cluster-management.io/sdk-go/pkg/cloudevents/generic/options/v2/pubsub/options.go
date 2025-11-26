package pubsub

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"k8s.io/apimachinery/pkg/util/errors"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// PubSubOptions holds the options that are used to build Pub/Sub client.
type PubSubOptions struct {
	Endpoint          string
	ProjectID         string
	CredentialsFile   string
	Topics            types.Topics
	Subscriptions     types.Subscriptions
	KeepaliveSettings *KeepaliveSettings
	ReceiveSettings   *ReceiveSettings
}

// PubSubConfig holds the information needed to connect to Google Cloud Pub/Sub.
type PubSubConfig struct {
	// Endpoint specifies the Pub/Sub service endpoint.
	// Optional: use this field to connect to a regional endpoint (e.g., https://us-west1-pubsub.googleapis.com)
	// instead of the global endpoint, or to a local emulator or test server.
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`

	// ProjectID is the Google Cloud project ID
	// Required: the ID of the Google Cloud project to use.
	ProjectID string `json:"projectID" yaml:"projectID"`

	// CredentialsFile is the path to the service account credentials JSON file
	// Optional: if not provided, the client will connect without credentials, useful for local emulator or test server.
	CredentialsFile string `json:"credentialsFile,omitempty" yaml:"credentialsFile,omitempty"`

	// Topics are PubSub topics for resource spec, status and resync.
	// Required: must be provided to specify the topics to publish events to.
	Topics *types.Topics `json:"topics,omitempty" yaml:"topics,omitempty"`

	// Subscriptions are PubSub subscriptions for resource spec, status and resync.
	// Required: must be provided to specify the subscriptions to receive events from.
	Subscriptions *types.Subscriptions `json:"subscriptions,omitempty" yaml:"subscriptions,omitempty"`

	// (Optional) KeepaliveSettings configures the keepalive parameters for Pub/Sub client.
	KeepaliveSettings *KeepaliveSettings `json:"keepaliveSettings,omitempty" yaml:"keepaliveSettings,omitempty"`

	// (Optional) ReceiveSettings configures the pubsub subscriber's receive settings.
	ReceiveSettings *ReceiveSettings `json:"receiveSettings,omitempty" yaml:"receiveSettings,omitempty"`
}

// KeepaliveSettings defines gRPC keepalive options for the Pub/Sub client.
type KeepaliveSettings struct {
	// Time between pings when there’s no activity, minimum is 10s, default: 5m.
	Time time.Duration `json:"time,omitempty" yaml:"time,omitempty"`

	// Wait time for a ping response before closing the connection, default: 20s.
	Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`

	// If true, send pings even when no RPCs are active, default: false.
	PermitWithoutStream bool `json:"permitWithoutStream,omitempty" yaml:"permitWithoutStream,omitempty"`
}

// ReceiveSettings defines how the Pub/Sub subscriber receives and processes messages.
type ReceiveSettings struct {
	// MaxExtension is the maximum period for which the Subscriber should
	// automatically extend the ack deadline for each message.
	MaxExtension time.Duration `json:"maxExtension,omitempty" yaml:"maxExtension,omitempty"`

	// MaxDurationPerAckExtension is the maximum duration per lease extension.
	MaxDurationPerAckExtension time.Duration `json:"maxDurationPerAckExtension,omitempty" yaml:"maxDurationPerAckExtension,omitempty"`

	// MinDurationPerAckExtension is the minimum duration per lease extension.
	MinDurationPerAckExtension time.Duration `json:"minDurationPerAckExtension,omitempty" yaml:"minDurationPerAckExtension,omitempty"`

	// MaxOutstandingMessages is the maximum number of unprocessed messages.
	MaxOutstandingMessages int `json:"maxOutstandingMessages,omitempty" yaml:"maxOutstandingMessages,omitempty"`

	// MaxOutstandingBytes is the maximum size of unprocessed messages.
	MaxOutstandingBytes int `json:"maxOutstandingBytes,omitempty" yaml:"maxOutstandingBytes,omitempty"`

	// NumGoroutines is the number of StreamingPull streams to pull messages from the subscription.
	NumGoroutines int `json:"numGoroutines,omitempty" yaml:"numGoroutines,omitempty"`
}

// LoadConfig loads the Pub/Sub configuration from a file.
func LoadConfig(configPath string) (*PubSubConfig, error) {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	config := &PubSubConfig{}
	if err := yaml.Unmarshal(configData, config); err != nil {
		return nil, err
	}

	return config, nil
}

// BuildPubSubOptionsFromFlags builds Pub/Sub options from a config file path.
func BuildPubSubOptionsFromFlags(configPath string) (*PubSubOptions, error) {
	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	// validate projectID
	if err := validateProjectID(config.ProjectID); err != nil {
		return nil, err
	}

	// validate topics and subscriptions
	if err := validateTopicsAndSubscriptions(config.Topics, config.Subscriptions, config.ProjectID); err != nil {
		return nil, err
	}

	options := &PubSubOptions{
		Endpoint:        config.Endpoint,
		ProjectID:       config.ProjectID,
		CredentialsFile: config.CredentialsFile,
		Topics:          *config.Topics,
		Subscriptions:   *config.Subscriptions,
		// enable keepalive by default
		KeepaliveSettings: &KeepaliveSettings{
			Time:                5 * time.Minute,
			Timeout:             20 * time.Second,
			PermitWithoutStream: false,
		},
	}

	if config.KeepaliveSettings != nil {
		options.KeepaliveSettings = config.KeepaliveSettings
	}

	if config.ReceiveSettings != nil {
		options.ReceiveSettings = config.ReceiveSettings
	}

	return options, nil
}

// validateProjectID validates that the project ID meets Google Cloud project ID requirements:
// 1. Must be 6–30 characters long
// 2. Only lowercase letters, numbers, and hyphens are allowed
// 3. Must start with a letter
// 4. Cannot end with a hyphen
func validateProjectID(projectID string) error {
	if projectID == "" {
		return fmt.Errorf("projectID is required")
	}

	if len(projectID) < 6 || len(projectID) > 30 {
		return fmt.Errorf("projectID must be 6-30 characters long, got %d characters", len(projectID))
	}

	// Pattern: starts with lowercase letter, followed by lowercase letters/numbers/hyphens, doesn't end with hyphen
	pattern := `^[a-z][a-z0-9-]*[a-z0-9]$`
	matched, err := regexp.MatchString(pattern, projectID)
	if err != nil {
		return fmt.Errorf("failed to validate projectID: %v", err)
	}

	if !matched {
		return fmt.Errorf("projectID %q is invalid: must start with a lowercase letter, "+
			"contain only lowercase letters, numbers, and hyphens, and cannot end with a hyphen", projectID)
	}

	return nil
}

func validateTopicsAndSubscriptions(topics *types.Topics, subscriptions *types.Subscriptions, projectID string) error {
	if topics == nil {
		return fmt.Errorf("the topics must be set")
	}

	if subscriptions == nil {
		return fmt.Errorf("the subscriptions must be set")
	}

	// validate topics and subscription for source
	isValidForSource := len(topics.SourceEvents) != 0 &&
		len(topics.SourceBroadcast) != 0 &&
		len(subscriptions.AgentEvents) != 0 &&
		len(subscriptions.AgentBroadcast) != 0
	// validate topics and subscription for agent
	isValidForAgent := len(topics.AgentEvents) != 0 &&
		len(topics.AgentBroadcast) != 0 &&
		len(subscriptions.SourceEvents) != 0 &&
		len(subscriptions.SourceBroadcast) != 0

	var errs []error
	if !isValidForSource && !isValidForAgent {
		errs = append(errs, fmt.Errorf("invalid topic/subscription combination: "+
			"for source, required topics: 'sourceEvents', 'sourceBroadcast'; required subscriptions: 'agentEvents', 'agentBroadcast'; "+
			"for agent, required topics: 'agentEvents', 'agentBroadcast'; required subscriptions: 'sourceEvents', 'sourceBroadcast'"))
	}

	topicPattern := strings.ReplaceAll(types.PubSubTopicPattern, "PROJECT_ID", regexp.QuoteMeta(projectID))
	if len(topics.SourceEvents) != 0 {
		if !regexp.MustCompile(topicPattern).MatchString(topics.SourceEvents) {
			errs = append(errs, fmt.Errorf("invalid source events topic %q, it should match `%s`",
				topics.SourceEvents, topicPattern))
		}
	}
	if len(topics.SourceBroadcast) != 0 {
		if !regexp.MustCompile(topicPattern).MatchString(topics.SourceBroadcast) {
			errs = append(errs, fmt.Errorf("invalid source broadcast topic %q, it should match `%s`",
				topics.SourceBroadcast, topicPattern))
		}
	}
	if len(topics.AgentEvents) != 0 {
		if !regexp.MustCompile(topicPattern).MatchString(topics.AgentEvents) {
			errs = append(errs, fmt.Errorf("invalid agent events topic %q, it should match `%s`",
				topics.AgentEvents, topicPattern))
		}
	}
	if len(topics.AgentBroadcast) != 0 {
		if !regexp.MustCompile(topicPattern).MatchString(topics.AgentBroadcast) {
			errs = append(errs, fmt.Errorf("invalid agent broadcast topic %q, it should match `%s`",
				topics.AgentBroadcast, topicPattern))
		}
	}

	subscriptionPattern := strings.ReplaceAll(types.PubSubSubscriptionPattern, "PROJECT_ID", regexp.QuoteMeta(projectID))
	if len(subscriptions.SourceEvents) != 0 {
		if !regexp.MustCompile(subscriptionPattern).MatchString(subscriptions.SourceEvents) {
			errs = append(errs, fmt.Errorf("invalid source events subscription %q, it should match `%s`",
				subscriptions.SourceEvents, subscriptionPattern))
		}
	}
	if len(subscriptions.SourceBroadcast) != 0 {
		if !regexp.MustCompile(subscriptionPattern).MatchString(subscriptions.SourceBroadcast) {
			errs = append(errs, fmt.Errorf("invalid source broadcast subscription %q, it should match `%s`",
				subscriptions.SourceBroadcast, subscriptionPattern))
		}
	}
	if len(subscriptions.AgentEvents) != 0 {
		if !regexp.MustCompile(subscriptionPattern).MatchString(subscriptions.AgentEvents) {
			errs = append(errs, fmt.Errorf("invalid agent events subscription %q, it should match `%s`",
				subscriptions.AgentEvents, subscriptionPattern))
		}
	}
	if len(subscriptions.AgentBroadcast) != 0 {
		if !regexp.MustCompile(subscriptionPattern).MatchString(subscriptions.AgentBroadcast) {
			errs = append(errs, fmt.Errorf("invalid agent broadcast subscription %q, it should match `%s`",
				subscriptions.AgentBroadcast, subscriptionPattern))
		}
	}
	return errors.NewAggregate(errs)
}
