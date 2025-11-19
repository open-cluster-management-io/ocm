package mqtt

import (
	"fmt"
	"github.com/eclipse/paho.golang/paho/log"
	"k8s.io/klog/v2"
)

type PahoErrorLogger struct {
	logger klog.Logger
}

type PahoDebugLogger struct {
	logger klog.Logger
}

var _ log.Logger = &PahoErrorLogger{}
var _ log.Logger = &PahoDebugLogger{}

func (l *PahoErrorLogger) Println(v ...interface{}) {
	l.logger.Error(fmt.Errorf("get err %s", fmt.Sprint(v...)), "MQTT error message")
}

func (l *PahoErrorLogger) Printf(format string, v ...interface{}) {
	l.logger.Error(fmt.Errorf(format, v...), "MQTT error message")
}

func (l *PahoDebugLogger) Println(v ...interface{}) {
	l.logger.V(4).Info(fmt.Sprint(v...))
}

func (l *PahoDebugLogger) Printf(format string, v ...interface{}) {
	l.logger.V(4).Info(fmt.Sprintf(format, v...))
}
