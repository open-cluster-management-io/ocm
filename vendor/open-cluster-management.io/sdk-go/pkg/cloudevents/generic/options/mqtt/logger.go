package mqtt

import (
	"github.com/eclipse/paho.golang/paho/log"
	"k8s.io/klog/v2"
)

type PahoErrorLogger struct {
}

type PahoDebugLogger struct {
}

var _ log.Logger = &PahoErrorLogger{}
var _ log.Logger = &PahoDebugLogger{}

func (l *PahoErrorLogger) Println(v ...interface{}) {
	klog.Error(v...)
}

func (l *PahoErrorLogger) Printf(format string, v ...interface{}) {
	klog.Errorf(format, v...)
}

func (l *PahoDebugLogger) Println(v ...interface{}) {
	klog.V(4).Info(v...)
}

func (l *PahoDebugLogger) Printf(format string, v ...interface{}) {
	klog.V(4).Infof(format, v...)
}
