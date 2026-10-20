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
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	k8sappsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

const (
	maxPortLen = 15
)

func Test_NewTapCommand(t *testing.T) {
	tests := []struct {
		Name       string
		ClientFunc func() *fake.Clientset
		ProxyPort  int32
		Namespace  string
		Err        error
	}{
		{"simple", fakeClientUntappedSimple, 80, "default", nil},
		{"no_namespace", fakeClientUntappedSimple, 80, "", nil},
		{"stray_configmap", fakeClientUntappedWithConfigMap, 80, "default", nil},
		{"named_ports", fakeClientUntappedNamedPorts, 80, "default", nil},
		{"no_port_name", fakeClientUntappedNoPortName, 80, "default", nil},
		{"named_ports_no_match", fakeClientUntappedNamedPortsMissing, 80, "default", ErrDeploymentMissingPorts},
		{"incorrect_namespace", fakeClientUntappedSimple, 80, "notexist", ErrNamespaceNotExist},
		{"incorrect_port", fakeClientUntappedSimple, 9999, "default", ErrServiceMissingPort},
		{"tapped_simple", fakeClientTappedSimple, 80, "default", ErrServiceTapped},
		{"missing_deployment", fakeClientUntappedWithoutDeployment, 80, "default", ErrServiceSelectorNoMatch},
		{"no_namespace_in_cluster", fakeClientUntappedWithoutNamespace, 80, "default", ErrNamespaceNotExist},
		{"deployment_without_labels", fakeClientUntappedNoLabels, 80, "default", ErrServiceSelectorNoMatch},
		{"service_without_selectors", fakeClientUntappedNoSelectors, 80, "default", ErrSelectorsMissing},
		{"multi_deployment_match", fakeClientUntappedMultiDeploymentMatch, 80, "default", ErrServiceSelectorMultiMatch},
		{"deployment_match_outside_namespace", fakeClientUntappedMatchOutsideNamespace, 80, "default", ErrServiceSelectorNoMatch},
	}
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			require := require.New(t)
			fakeClient := tc.ClientFunc()
			testViper := viper.New()
			testViper.Set("proxyPort", tc.ProxyPort)
			testViper.Set("namespace", tc.Namespace)
			cmd := &cobra.Command{}
			cmd.SetContext(t.Context())
			cmd.SetOutput(ioutil.Discard)
			err := NewTapCommand(fakeClient, &rest.Config{}, testViper)(cmd, []string{"sample-service"})
			if tc.Err != nil {
				if !errors.Is(err, tc.Err) {
					t.Fatalf("expected (%q), got (%q)", tc.Err, err)
				}
			} else {
				// sanity checks
				require.Nil(err)
				fakeDeployment, err := fakeClient.AppsV1().Deployments(testViper.GetString("namespace")).Get(t.Context(), "sample-deployment", metav1.GetOptions{})
				require.Nil(err)
				require.Len(fakeDeployment.Spec.Template.Spec.Containers, 2, "sidecar was not successfully added to deployment spec")
				// container checks
				for _, c := range fakeDeployment.Spec.Template.Spec.Containers {
					if c.Name != kubetapContainerName {
						continue
					}
					for _, p := range c.Ports {
						require.LessOrEqual(len(p.Name), maxPortLen, "port name max length exceeded")
					}
					require.GreaterOrEqual(len(c.Ports), 2, "tap port was not added to the deployment")
				}
				// configmap checks
				fakeCM, err := fakeClient.CoreV1().ConfigMaps(testViper.GetString("namespace")).Get(t.Context(), kubetapConfigMapPrefix+fakeDeployment.Name, metav1.GetOptions{})
				require.Nil(err)
				require.NotNil(fakeCM)
				require.True(strings.Contains(fakeCM.Name, kubetapConfigMapPrefix))
				require.Contains(fakeCM.BinaryData, mitmproxyConfigFile)
				require.Greater(len(fakeCM.BinaryData[mitmproxyConfigFile]), 0, "no data in ConfigMap")
			}
		})
	}
}

func Test_NewUntapCommand(t *testing.T) {
	tests := []struct {
		Name       string
		ClientFunc func() *fake.Clientset
		Namespace  string
		Err        error
	}{
		{"simple", fakeClientTappedSimple, "default", nil},
		{"untapped", fakeClientUntappedSimple, "default", nil},
		{"named_ports", fakeClientTappedNamedPorts, "default", nil},
		{"incorrect_namespace", fakeClientUntappedSimple, "nsnotexist", nil},
		{"no_port_name", fakeClientTappedNamedPorts, "default", nil},
		{"no_namespace_in_cluster", fakeClientUntappedWithoutNamespace, "none", ErrNamespaceNotExist},
		{"missing_deployment", fakeClientUntappedWithoutDeployment, "default", ErrServiceSelectorNoMatch},
		{"deployment_without_labels", fakeClientUntappedNoLabels, "default", ErrServiceSelectorNoMatch},
		{"service_without_selectors", fakeClientUntappedNoSelectors, "default", ErrSelectorsMissing},
		{"multi_deployment_match", fakeClientUntappedMultiDeploymentMatch, "default", ErrServiceSelectorMultiMatch},
		{"deployment_match_outside_namespace", fakeClientUntappedMatchOutsideNamespace, "default", ErrServiceSelectorNoMatch},
	}
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			require := require.New(t)
			fakeClient := tc.ClientFunc()
			testViper := viper.New()
			b := bytes.NewBufferString("")
			cmd := &cobra.Command{}
			cmd.SetContext(t.Context())
			cmd.SetOutput(b)
			err := NewUntapCommand(fakeClient, testViper)(cmd, []string{"sample-service"})
			if tc.Err != nil {
				require.NotNil(err)
				require.True(errors.Is(err, tc.Err))
			} else {
				require.Nil(err)
				out, err := ioutil.ReadAll(b)
				require.Nil(err)
				require.Contains(string(out), "Untapped Service \"", "did not untap successfully")
			}
		})
	}
}

func Test_NewListCommand(t *testing.T) {
	tests := []struct {
		Name        string
		ClientFunc  func() *fake.Clientset
		Namespace   string
		Err         error
		ExpectedOut string
	}{
		{"simple", fakeClientTappedSimple, "default", nil, "Tapped Services in the default namespace:\n\nsample-service\n"},
		{"all_namespaces", fakeClientTappedSimple, "", nil, "default/sample-service\n"},
		{"namespace_not_exist", fakeClientTappedSimple, "notexist", ErrNamespaceNotExist, ""},
		{"untapped", fakeClientUntappedSimple, "default", nil, "No Services in the default namespace are tapped.\n"},
		{"untapped_all_ns", fakeClientUntappedSimple, "", nil, "No Services are tapped.\n"},
	}
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			require := require.New(t)
			fakeClient := tc.ClientFunc()
			testViper := viper.New()
			testViper.Set("namespace", tc.Namespace)
			b := bytes.NewBufferString("")
			cmd := &cobra.Command{}
			cmd.SetContext(t.Context())
			cmd.SetOutput(b)
			err := NewListCommand(fakeClient, testViper)(cmd, []string{})
			if tc.Err != nil {
				require.NotNil(err)
				require.True(errors.Is(err, tc.Err))
			} else {
				require.Nil(err)
				out, err := ioutil.ReadAll(b)
				require.Nil(err)
				require.Contains(string(out), tc.ExpectedOut)
			}
		})
	}
}

var (
	simpleNamespace = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}

	simpleDeployment = k8sappsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-deployment",
			Namespace: "default",
			Annotations: map[string]string{
				"my-annotation": "some-annotation",
			},
			Labels: map[string]string{
				"app": "myapp",
			},
		},
		Spec: k8sappsv1.DeploymentSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "someapp",
							Image: "ghcr.io/xorps/someapp:latest",
						},
					},
				},
			},
		},
	}

	simpleService = v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-service",
			Namespace: "default",
			Annotations: map[string]string{
				"my-annotation": "some-annotation",
			},
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "servicePortOne",
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
			},
			Selector: map[string]string{
				"app": "myapp",
			},
		},
	}

	simpleDeploymentTapped = k8sappsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "sample-deployment",
			Namespace:   "default",
			Annotations: map[string]string{},
			Labels: map[string]string{
				"app": "myapp",
			},
		},
		Spec: k8sappsv1.DeploymentSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "someapp",
							Image: "ghcr.io/xorps/someapp:latest",
						},
						{
							Name:  kubetapContainerName,
							Image: "ghcr.io/xorps/kubetap-mitmproxy:latest",
						},
					},
					Volumes: []v1.Volume{
						{
							Name: kubetapConfigMapPrefix + "sample-deployment",
						},
					},
				},
			},
		},
	}

	simpleServiceTapped = v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-service",
			Namespace: "default",
			Annotations: map[string]string{
				annotationOriginalTargetPort: "8080",
			},
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "servicePortOne",
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
				{
					Name:       kubetapServicePortName,
					Port:       kubetapProxyWebInterfacePort,
					TargetPort: intstr.FromInt(kubetapProxyListenPort),
				},
			},
			Selector: map[string]string{
				"app": "myapp",
			},
		},
	}

	simpleConfigMapTapped = v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubetapConfigMapPrefix + "sample-deployment",
			Namespace: "default",
			Annotations: map[string]string{
				annotationConfigMap: configMapAnnotationPrefix + "sample-deployment",
			},
		},
	}
)

func fakeClientUntappedSimple() *fake.Clientset {
	namespace := simpleNamespace
	deployment := simpleDeployment
	service := simpleService
	return fake.NewSimpleClientset(
		&namespace,
		&deployment,
		&service,
	)
}

func fakeClientTappedSimple() *fake.Clientset {
	namespace := simpleNamespace
	deployment := simpleDeploymentTapped
	service := simpleServiceTapped
	configMap := simpleConfigMapTapped
	return fake.NewSimpleClientset(
		&namespace,
		&deployment,
		&service,
		&configMap,
	)
}

func fakeClientUntappedWithConfigMap() *fake.Clientset {
	namespace := simpleNamespace
	deployment := simpleDeployment
	service := simpleService
	configMap := simpleConfigMapTapped
	return fake.NewSimpleClientset(
		&namespace,
		&deployment,
		&service,
		&configMap,
	)
}

func fakeClientUntappedWithoutDeployment() *fake.Clientset {
	namespace := simpleNamespace
	service := simpleService
	return fake.NewSimpleClientset(
		&namespace,
		&service,
	)
}

func fakeClientUntappedWithoutNamespace() *fake.Clientset {
	deployment := simpleDeployment
	service := simpleService
	configMap := simpleConfigMapTapped
	return fake.NewSimpleClientset(
		&deployment,
		&service,
		&configMap,
	)
}

func fakeClientUntappedNoLabels() *fake.Clientset {
	namespace := simpleNamespace
	deployment := simpleDeployment
	service := simpleService
	deployment.ObjectMeta.Labels = map[string]string{}
	return fake.NewSimpleClientset(
		&namespace,
		&deployment,
		&service,
	)
}

func fakeClientUntappedNoSelectors() *fake.Clientset {
	namespace := simpleNamespace
	deployment := simpleDeployment
	service := simpleService
	service.Spec.Selector = map[string]string{}
	return fake.NewSimpleClientset(
		&namespace,
		&deployment,
		&service,
	)
}

func fakeClientUntappedMultiDeploymentMatch() *fake.Clientset {
	namespace := simpleNamespace
	deployment := simpleDeployment
	deploymentTwo := simpleDeployment
	deploymentTwo.Name = "two"
	service := simpleService
	return fake.NewSimpleClientset(
		&namespace,
		&deployment,
		&deploymentTwo,
		&service,
	)
}

func fakeClientUntappedMatchOutsideNamespace() *fake.Clientset {
	namespace := simpleNamespace
	deployment := simpleDeployment
	deployment.Namespace = "foo"
	service := simpleService
	return fake.NewSimpleClientset(
		&namespace,
		&deployment,
		&service,
	)
}

func fakeClientTappedWithoutAnnotations() *fake.Clientset {
	namespace := simpleNamespace
	deployment := simpleDeploymentTapped
	service := simpleServiceTapped
	deployment.Annotations = map[string]string{}
	service.Annotations = map[string]string{}
	return fake.NewSimpleClientset(
		&namespace,
		&deployment,
		&service,
	)
}

func fakeClientUntappedNamedPorts() *fake.Clientset {
	namespace := simpleNamespace
	deployment := simpleDeployment
	service := simpleService
	var ports []v1.ServicePort
	for _, p := range service.Spec.Ports {
		p.TargetPort = intstr.FromString("myport")
		ports = append(ports, p)
	}
	service.Spec.Ports = ports
	for i, c := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[i].Ports = append(c.Ports, v1.ContainerPort{
			Name:          "myport",
			ContainerPort: 8080,
			Protocol:      v1.ProtocolTCP,
		})
	}
	return fake.NewSimpleClientset(
		&namespace,
		&deployment,
		&service,
	)
}

// fakeClientUntappedNamedPortsMissing is missing the port name match within the deployment
func fakeClientUntappedNamedPortsMissing() *fake.Clientset {
	namespace := simpleNamespace
	deployment := simpleDeployment
	service := simpleService
	var ports []v1.ServicePort
	for _, p := range service.Spec.Ports {
		p.TargetPort = intstr.FromString(kubetapPortName)
		ports = append(ports, p)
	}
	service.Spec.Ports = ports
	return fake.NewSimpleClientset(
		&namespace,
		&deployment,
		&service,
	)
}

func fakeClientTappedNamedPorts() *fake.Clientset {
	namespace := simpleNamespace
	deployment := simpleDeploymentTapped
	service := simpleServiceTapped
	var ports []v1.ServicePort
	for _, p := range service.Spec.Ports {
		if p.Name != simpleService.Spec.Ports[0].Name {
			continue
		}
		p.TargetPort = intstr.FromString(kubetapPortName)
		ports = append(ports, p)
	}
	service.Spec.Ports = ports
	return fake.NewSimpleClientset(
		&namespace,
		&deployment,
		&service,
	)
}

func fakeClientUntappedNoPortName() *fake.Clientset {
	namespace := simpleNamespace
	deployment := simpleDeployment
	service := simpleService
	var ports []v1.ServicePort
	for _, p := range service.Spec.Ports {
		p.Name = ""
		ports = append(ports, p)
	}
	service.Spec.Ports = ports
	return fake.NewSimpleClientset(
		&namespace,
		&deployment,
		&service,
	)
}

func Test_NewTapCommand_readyTimeout(t *testing.T) {
	// The fake cluster never creates the tapped Pod, so waiting must time out
	// with an error (previously this path panicked on a closed channel).
	fakeClient := fakeClientUntappedSimple()
	testViper := viper.New()
	testViper.Set("proxyPort", 80)
	testViper.Set("namespace", "default")
	testViper.Set("portForward", true)
	testViper.Set("readyTimeout", 2*time.Second)
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.SetOutput(io.Discard)
	err := NewTapCommand(fakeClient, &rest.Config{}, testViper)(cmd, []string{"sample-service"})
	require.ErrorContains(t, err, "pod not running after 2s")
}

func Test_NewTapCommand_invalidTimeout(t *testing.T) {
	testViper := viper.New()
	testViper.Set("proxyPort", 80)
	testViper.Set("portForward", true)
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.SetOutput(io.Discard)
	err := NewTapCommand(fakeClientUntappedSimple(), &rest.Config{}, testViper)(cmd, []string{"sample-service"})
	require.ErrorContains(t, err, "--timeout must be greater than zero")
}

func Test_podContainersReady(t *testing.T) {
	withCondition := func(status v1.ConditionStatus) v1.Pod {
		return v1.Pod{Status: v1.PodStatus{Conditions: []v1.PodCondition{
			{Type: v1.PodScheduled, Status: v1.ConditionTrue},
			{Type: v1.ContainersReady, Status: status},
		}}}
	}
	tests := []struct {
		Name string
		Pod  v1.Pod
		Want bool
	}{
		{"ready", withCondition(v1.ConditionTrue), true},
		{"not_ready", withCondition(v1.ConditionFalse), false},
		{"unknown", withCondition(v1.ConditionUnknown), false},
		{"no_conditions", v1.Pod{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			require.Equal(t, tc.Want, podContainersReady(tc.Pod))
		})
	}
}

func Test_NewTapCommand_retriesConflictWithLatestDeployment(t *testing.T) {
	fakeClient := fakeClientUntappedSimple()
	// Fail the first Deployment update with a conflict, as if another writer
	// updated it between our read and write.
	var conflicted bool
	fakeClient.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if conflicted {
			return false, nil, nil
		}
		conflicted = true
		return true, nil, apierrors.NewConflict(k8sappsv1.Resource("deployments"), "sample-deployment", errors.New("modified"))
	})
	testViper := viper.New()
	testViper.Set("proxyPort", 80)
	testViper.Set("namespace", "default")
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.SetOutput(io.Discard)
	err := NewTapCommand(fakeClient, &rest.Config{}, testViper)(cmd, []string{"sample-service"})
	require.NoError(t, err)
	require.True(t, conflicted)

	dpl, err := fakeClient.AppsV1().Deployments("default").Get(t.Context(), "sample-deployment", metav1.GetOptions{})
	require.NoError(t, err)
	var sidecars int
	for _, c := range dpl.Spec.Template.Spec.Containers {
		if c.Name == kubetapContainerName {
			sidecars++
		}
	}
	require.Equal(t, 1, sidecars, "a retry must not add the sidecar twice")
}

func Test_NewTapCommand_failedRevertSuggestsTapOff(t *testing.T) {
	fakeClient := fakeClientUntappedSimple()
	// Every Deployment update fails, so both tapping and the revert fail.
	fakeClient.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api server unavailable")
	})
	testViper := viper.New()
	testViper.Set("proxyPort", 80)
	testViper.Set("namespace", "default")
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.SetOutput(io.Discard)
	err := NewTapCommand(fakeClient, &rest.Config{}, testViper)(cmd, []string{"sample-service"})
	require.ErrorContains(t, err, "failed to add sidecars to Deployment")
	require.ErrorContains(t, err, "run 'kubectl tap off -n default sample-service' to clean up")
}
