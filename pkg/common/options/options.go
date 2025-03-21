package options

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/utils/clock"
)

type Options struct {
	CmdConfig *controllercmd.ControllerCommandConfig
	Burst     int
	QPS       float32
}

// NewOptions returns the flags with default value set
func NewOptions() *Options {
	opts := &Options{
		QPS:   50,
		Burst: 100,
	}
	return opts
}

func (o *Options) NewControllerCommandConfig(
	componentName string, version version.Info, startFunc controllercmd.StartFunc, clock clock.Clock) *controllercmd.ControllerCommandConfig {
	o.CmdConfig = controllercmd.NewControllerCommandConfig(componentName, version, o.startWithQPS(startFunc), clock)
	return o.CmdConfig
}

func (o *Options) startWithQPS(startFunc controllercmd.StartFunc) controllercmd.StartFunc {
	return func(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
		controllerContext.KubeConfig.QPS = o.QPS
		controllerContext.KubeConfig.Burst = o.Burst
		return startFunc(ctx, controllerContext)
	}
}

func (o *Options) AddFlags(flags *pflag.FlagSet) {
	flags.Float32Var(&o.QPS, "kube-api-qps", o.QPS, "QPS to use while talking with apiserver on spoke cluster.")
	flags.IntVar(&o.Burst, "kube-api-burst", o.Burst, "Burst to use while talking with apiserver on spoke cluster.")
	if o.CmdConfig != nil {
		flags.BoolVar(&o.CmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election.")
		flags.DurationVar(&o.CmdConfig.LeaseDuration.Duration, "leader-election-lease-duration", 137*time.Second, ""+
			"The duration that non-leader candidates will wait after observing a leadership "+
			"renewal until attempting to acquire leadership of a led but unrenewed leader "+
			"slot. This is effectively the maximum duration that a leader can be stopped "+
			"before it is replaced by another candidate. This is only applicable if leader "+
			"election is enabled.")
		flags.DurationVar(&o.CmdConfig.RenewDeadline.Duration, "leader-election-renew-deadline", 107*time.Second, ""+
			"The interval between attempts by the acting master to renew a leadership slot "+
			"before it stops leading. This must be less than or equal to the lease duration. "+
			"This is only applicable if leader election is enabled.")
		flags.DurationVar(&o.CmdConfig.RetryPeriod.Duration, "leader-election-retry-period", 26*time.Second, ""+
			"The duration the clients should wait between attempting acquisition and renewal "+
			"of a leadership. This is only applicable if leader election is enabled.")
	}
}
