//go:generate go generate ./core

package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/dell/csi-vxflexos/k8sutils"
	"github.com/dell/csi-vxflexos/provider"
	"github.com/dell/csi-vxflexos/service"
	"github.com/dell/gocsi"
	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"os"
	"strings"
)

// main is ignored when this package is built as a go plug-in
func main() {

	logger := logrus.New()

	// enable viper to get properties from environment variables or default configuration file
	viper.AutomaticEnv()

	arrayConfig := flag.String("array-config", "", "yaml file with array(s) configuration")
	logConfigfile := flag.String("log-config", "", "yaml file with logrus configuration")
	enableLeaderElection := flag.Bool("leader-election", false, "boolean to enable leader election")
	leaderElectionNamespace := flag.String("leader-election-namespace", "", "namespace where leader election lease will be created")
	kubeconfig := flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	flag.Parse()

	if *arrayConfig == "" {
		fmt.Fprintf(os.Stderr, "array-config argument is mandatory")
		os.Exit(1)
	}
	service.ArrayConfig = *arrayConfig

	viper.SetConfigFile(*logConfigfile)

	err := viper.ReadInConfig()
	// if unable to read configuration file, default values will be used in updateLoggingSettings
	if err != nil {
		logger.WithError(err).Error("unable to read config file, using default values")
	}

	updateLoggingSettings(logger)
	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		logger.WithField("file", logConfigfile).Info("log configuration file changed")
		updateLoggingSettings(logger)
	})

	service.Log = logger

	run := func(ctx context.Context) {
		gocsi.Run(ctx, service.Name, "A PowerFlex Container Storage Interface (CSI) Plugin",
			usage, provider.New())
	}
	if !*enableLeaderElection {
		run(context.Background())
	} else {
		driverName := strings.Replace(service.Name, ".", "-", -1)
		lockName := fmt.Sprintf("driver-%s", driverName)
		k8sclientset, err := k8sutils.CreateKubeClientSet(*kubeconfig)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "failed to initialize leader election: %v", err)
			os.Exit(1)
		}
		// Attempt to become leader and start the driver
		k8sutils.LeaderElection(k8sclientset, lockName, *leaderElectionNamespace, run)
	}

}

func updateLoggingSettings(logger *logrus.Logger) {
	logFormat := viper.GetString("LOG_FORMAT")
	logFormat = strings.ToLower(logFormat)
	logger.WithField("format", logFormat).Info("Read LOG_FORMAT from log configuration file")
	if strings.EqualFold(logFormat, "json") {
		logger.SetFormatter(&logrus.JSONFormatter{})
	} else {
		// use text formatter by default
		if logFormat != "text" {
			logger.WithField("format", logFormat).Info("LOG_FORMAT value not recognized, setting to text")
		}
		logger.SetFormatter(&logrus.TextFormatter{})
	}
	logLevel := viper.GetString("LOG_LEVEL")
	logLevel = strings.ToLower(logLevel)
	logger.WithField("level", logLevel).Info("Read LOG_LEVEL from log configuration file")
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		logger.WithField("level", logLevel).Info("LOG_LEVEL value not recognized, setting to info")
		// use INFO level by default
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)
}

const usage = `    X_CSI_VXFLEXOS_SDCGUID
        Specifies the GUID of the SDC. This is only used by the Node Service,
        and removes a need for calling an external binary to retrieve the GUID.
        If not set, the external binary will be invoked.

        The default value is empty.

    X_CSI_VXFLEXOS_THICKPROVISIONING
        Specifies whether thick provisiong should be used when creating volumes.

        The default value is false.

    X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE
        When a snapshot is deleted, if it is a member of a Consistency Group, enable automatic deletion
        of all snapshots in the consistency group.

        The default value is false.

    X_CSI_VXFLEXOS_ENABLELISTVOLUMESNAPSHOTS
        When listing volumes, if this option is is enabled, then volumes and snapshots will be returned.
        Otherwise only volumes are returned.

        The default value is false.
`
