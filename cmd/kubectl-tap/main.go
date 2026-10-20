// Copyright 2020 Soluble Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	// Set by CI.
	version = "dev"
	date    = "not_set" //nolint: gochecknoglobals
	commit  = "not_set" //nolint: gochecknoglobals
)

const (
	annotationOriginalTargetPort = "kubetap.io/original-port"
	annotationConfigMap          = "kubetap.io/proxy-config"
	annotationIsTapped           = "kubetap.io/tapped"

	defaultImageHTTP = "ghcr.io/xorps/kubetap-mitmproxy:latest"
	defaultImageRaw  = "ghcr.io/xorps/kubetap-raw:latest"
	defaultImageGRPC = "ghcr.io/xorps/kubetap-grpc:latest"
)

// errUsage is returned when kubectl tap is run without a subcommand. Usage has
// already been printed, so main exits with EX_USAGE without printing it.
var errUsage = errors.New("no subcommand given")

func main() {
	err := func() error {
		// Cancelled on Ctrl-C/SIGTERM so in-flight work stops and taps can be reverted.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		return run(ctx)
	}()
	if errors.Is(err, errUsage) {
		os.Exit(64) // EX_USAGE
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	rootCmd, err := newCLI() //nolint:contextcheck // commands get ctx via ExecuteContext and cmd.Context()
	if err != nil {
		return err
	}
	return rootCmd.ExecuteContext(ctx)
}

// newCLI assembles the root command and its subcommands.
func newCLI() (*cobra.Command, error) {
	rootCmd := NewRootCmd()

	kubernetesConfigFlags := genericclioptions.NewConfigFlags(false)
	kubernetesConfigFlags.AddFlags(rootCmd.PersistentFlags())
	if err := viper.BindPFlags(rootCmd.PersistentFlags()); err != nil {
		return nil, err
	}

	// Clients are built when a command runs, after flags such as --context and
	// --kubeconfig have been parsed.
	clients := func() (*rest.Config, kubernetes.Interface, error) {
		config, err := kubernetesConfigFlags.ToRESTConfig()
		if err != nil {
			return nil, nil, err
		}
		client, err := kubernetes.NewForConfig(config)
		if err != nil {
			return nil, nil, err
		}
		return config, client, nil
	}

	versionCmd := NewVersionCmd()
	onCmd := NewOnCmd(clients)
	offCmd := NewOffCmd(clients)
	listCmd := NewListCmd(clients)

	onCmd.Flags().StringP("port", "p", "", "target Service port")
	onCmd.Flags().StringP("image", "i", defaultImageHTTP, "image to run in proxy container")
	onCmd.Flags().Bool("https", false, "enable if target listener uses HTTPS")
	onCmd.Flags().String("command-args", "mitmweb", "specify command arguments for the proxy sidecar container")
	onCmd.Flags().Bool("port-forward", false, "enable to automatically kubctl port-forward to services")
	onCmd.Flags().Bool("browser", false, "enable to open browser windows to service and proxy. Also enables --port-forward")
	onCmd.Flags().String("protocol", "http", "specify a protocol. Supported protocols: [ http ]")

	rootCmd.AddCommand(versionCmd, onCmd, offCmd, listCmd)

	return rootCmd, nil
}

// bindTapFlags is a workaround for https://github.com/spf13/viper/issues/233
func bindTapFlags(cmd *cobra.Command, _ []string) error {
	if err := viper.BindPFlag("proxyPort", cmd.Flags().Lookup("port")); err != nil {
		return err
	}
	if err := viper.BindPFlag("proxyImage", cmd.Flags().Lookup("image")); err != nil {
		return err
	}
	if err := viper.BindPFlag("https", cmd.Flags().Lookup("https")); err != nil {
		return err
	}
	if err := viper.BindPFlag("commandArgs", cmd.Flags().Lookup("command-args")); err != nil {
		return err
	}
	if err := viper.BindPFlag("portForward", cmd.Flags().Lookup("port-forward")); err != nil {
		return err
	}
	if err := viper.BindPFlag("browser", cmd.Flags().Lookup("browser")); err != nil {
		return err
	}
	if err := viper.BindPFlag("protocol", cmd.Flags().Lookup("protocol")); err != nil {
		return err
	}
	return nil
}

func NewRootCmd() *cobra.Command {
	return &cobra.Command{
		// HACK: there is a "bug" in cobra's handling of Use strings with spaces, so the space
		// below in the Use field isn't a true space -- it's a unicode Em space.
		// Also, if you can't see the space below prominently, you need to change your editor settings
		// to reveal Unicode characters. Otherwise, you're likely to PR some malicious code with a unicode
		// domain at some point.
		Use:   "kubectl tap",
		Short: "kubetap",
		Example: ` Create a tap for a new Service:
   kubectl tap on -n demo -p443 --https sample-service",

 List active taps in all namespaces:
   kubectl tap list

 Disable the tap with the off command:
   kubectl tap off -n demo sample-service`,
		Long: `kubetap - proxy Services in Kubernetes with ease.

 More information is available at the project website:
   https://xorps.github.io/kubetap/

 Created and maintained by Soluble:
   https://www.soluble.ai/
`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// NOTE: explicitly print out usage here, but overriding it for subcommands by way
			// of SilenceUsage: true
			_ = cmd.Usage()
			return errUsage
		},
		SilenceUsage: true,
		// main prints errors, so cobra shouldn't as well.
		SilenceErrors: true,
	}
}

// clientsFunc returns Kubernetes clients built from the parsed command-line flags.
type clientsFunc func() (*rest.Config, kubernetes.Interface, error)

func NewOnCmd(clients clientsFunc) *cobra.Command {
	return &cobra.Command{
		Use:     "on",
		Short:   "Tap a Service",
		Example: "kubectl tap on -n my-namespace -p443 --https my-sample-service",
		PreRunE: bindTapFlags,
		RunE: func(cmd *cobra.Command, args []string) error {
			config, client, err := clients()
			if err != nil {
				return err
			}
			return NewTapCommand(client, config, viper.GetViper())(cmd, args)
		},
		Args: cobra.ExactArgs(1),
	}
}

func NewOffCmd(clients clientsFunc) *cobra.Command {
	return &cobra.Command{
		Use:     "off",
		Short:   "Untap a Service",
		Example: "kubectl tap off -n my-namespace my-sample-service",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := clients()
			if err != nil {
				return err
			}
			return NewUntapCommand(client, viper.GetViper())(cmd, args)
		},
		Args: cobra.ExactArgs(1),
	}
}

func NewListCmd(clients clientsFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List tapped Services",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := clients()
			if err != nil {
				return err
			}
			return NewListCommand(client, viper.GetViper())(cmd, args)
		},
	}
}

func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version of kubectl-tap",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "version: %s, commit: %s, built at: %s\n", version, commit, date)
		},
	}
}
