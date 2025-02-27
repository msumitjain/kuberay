package common

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var (
	headServiceAnnotationKey1   = "HeadServiceAnnotationKey1"
	headServiceAnnotationValue1 = "HeadServiceAnnotationValue1"
	headServiceAnnotationKey2   = "HeadServiceAnnotationKey2"
	headServiceAnnotationValue2 = "HeadServiceAnnotationValue2"
	instanceWithWrongSvc        = &rayiov1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-sample",
			Namespace: "default",
		},
		Spec: rayiov1alpha1.RayClusterSpec{
			RayVersion: "1.0",
			HeadServiceAnnotations: map[string]string{
				headServiceAnnotationKey1: headServiceAnnotationValue1,
				headServiceAnnotationKey2: headServiceAnnotationValue2,
			},
			HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
				Replicas: pointer.Int32Ptr(1),
				RayStartParams: map[string]string{
					"port":                "6379",
					"object-manager-port": "12345",
					"node-manager-port":   "12346",
					"object-store-memory": "100000000",
					"num-cpus":            "1",
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Labels: map[string]string{
							"groupName": "headgroup",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Image: "rayproject/autoscaler",
								Ports: []corev1.ContainerPort{
									{
										ContainerPort: 6379,
										Name:          "gcs",
									},
									{
										ContainerPort: 8265,
									},
								},
								Command: []string{"python"},
								Args:    []string{"/opt/code.py"},
								Env: []corev1.EnvVar{
									{
										Name: "MY_POD_IP",
										ValueFrom: &corev1.EnvVarSource{
											FieldRef: &corev1.ObjectFieldSelector{
												FieldPath: "status.podIP",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
)

func TestBuildServiceForHeadPod(t *testing.T) {
	svc, err := BuildServiceForHeadPod(*instanceWithWrongSvc, nil, nil)
	assert.Nil(t, err)

	actualResult := svc.Spec.Selector[RayClusterLabelKey]
	expectedResult := string(instanceWithWrongSvc.Name)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	actualResult = svc.Spec.Selector[RayNodeTypeLabelKey]
	expectedResult = string(rayiov1alpha1.HeadNode)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	actualResult = svc.Spec.Selector[KubernetesApplicationNameLabelKey]
	expectedResult = ApplicationName
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	ports := svc.Spec.Ports
	expectedResult = DefaultServiceAppProtocol
	for _, port := range ports {
		if *port.AppProtocol != DefaultServiceAppProtocol {
			t.Fatalf("Expected `%v` but got `%v`", expectedResult, *port.AppProtocol)
		}
	}
}

func TestBuildServiceForHeadPodWithAppNameLabel(t *testing.T) {
	labels := make(map[string]string)
	labels[KubernetesApplicationNameLabelKey] = "testname"

	svc, err := BuildServiceForHeadPod(*instanceWithWrongSvc, labels, nil)
	assert.Nil(t, err)

	actualResult := svc.Spec.Selector[KubernetesApplicationNameLabelKey]
	expectedResult := "testname"
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	actualLength := len(svc.Spec.Selector)
	// We have 5 default labels in `BuildServiceForHeadPod`, and `KubernetesApplicationNameLabelKey`
	// is one of the default labels. Hence, `expectedLength` should also be 5.
	expectedLength := 5
	if actualLength != expectedLength {
		t.Fatalf("Expected `%v` but got `%v`", expectedLength, actualLength)
	}
}

func TestBuildServiceForHeadPodWithAnnotations(t *testing.T) {
	annotations := make(map[string]string)
	annotations["key1"] = "testvalue1"
	annotations["key2"] = "testvalue2"
	svc, err := BuildServiceForHeadPod(*instanceWithWrongSvc, nil, annotations)
	assert.Nil(t, err)

	if !reflect.DeepEqual(svc.ObjectMeta.Annotations, annotations) {
		t.Fatalf("Expected `%v` but got `%v`", annotations, svc.ObjectMeta.Annotations)
	}
}

func TestGetPortsFromCluster(t *testing.T) {
	svcPorts, err := getPortsFromCluster(*instanceWithWrongSvc)
	assert.Nil(t, err)

	// getPortsFromCluster creates service ports based on the container ports.
	// It will assign a generated service port name if the container port name
	// is not defined. To compare created service ports with container ports,
	// all generated service port names need to be reverted to empty strings.
	svcNames := map[int32]string{}
	for name, port := range svcPorts {
		if name == (fmt.Sprint(port) + "-port") {
			name = ""
		}
		svcNames[port] = name
	}

	index := utils.FindRayContainerIndex(instanceWithWrongSvc.Spec.HeadGroupSpec.Template.Spec)
	cPorts := instanceWithWrongSvc.Spec.HeadGroupSpec.Template.Spec.Containers[index].Ports

	for _, cPort := range cPorts {
		expectedResult := cPort.Name
		actualResult := svcNames[cPort.ContainerPort]
		if !reflect.DeepEqual(expectedResult, actualResult) {
			t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
		}
	}
}

func TestGetServicePortsWithMetricsPort(t *testing.T) {
	cluster := instanceWithWrongSvc.DeepCopy()

	// Test case 1: No ports are specified by the user.
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{}
	ports := getServicePorts(*cluster)
	// Verify that getServicePorts sets the default metrics port when the user doesn't specify any ports.
	if ports[DefaultMetricsName] != int32(DefaultMetricsPort) {
		t.Fatalf("Expected `%v` but got `%v`", int32(DefaultMetricsPort), ports[DefaultMetricsName])
	}

	// Test case 2: Only a random port is specified by the user.
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
		{
			Name:          "random",
			ContainerPort: 1234,
		},
	}
	ports = getServicePorts(*cluster)
	// Verify that getServicePorts sets the default metrics port when the user doesn't specify the metrics port but specifies other ports.
	if ports[DefaultMetricsName] != int32(DefaultMetricsPort) {
		t.Fatalf("Expected `%v` but got `%v`", int32(DefaultMetricsPort), ports[DefaultMetricsName])
	}

	// Test case 3: A custom metrics port is specified by the user.
	customMetricsPort := int32(DefaultMetricsPort) + 1
	metricsPort := corev1.ContainerPort{
		Name:          DefaultMetricsName,
		ContainerPort: customMetricsPort,
	}
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports = append(cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports, metricsPort)
	ports = getServicePorts(*cluster)
	// Verify that getServicePorts uses the user's custom metrics port when the user specifies the metrics port.
	if ports[DefaultMetricsName] != customMetricsPort {
		t.Fatalf("Expected `%v` but got `%v`", customMetricsPort, ports[DefaultMetricsName])
	}
}

func TestUserSpecifiedHeadService(t *testing.T) {
	// Use any RayCluster instance as a base for the test.
	testRayClusterWithHeadService := instanceWithWrongSvc.DeepCopy()

	// Set user-specified head service with user-specified labels, annotations, and ports.
	userName := "user-custom-name"
	userNamespace := "user-custom-namespace"
	userLabels := map[string]string{"userLabelKey": "userLabelValue", RayClusterLabelKey: "userClusterName"} // Override default cluster name
	userAnnotations := map[string]string{"userAnnotationKey": "userAnnotationValue", headServiceAnnotationKey1: "user_override"}
	userPort := corev1.ServicePort{Name: "userPort", Port: 12345}
	userPortOverride := corev1.ServicePort{Name: DefaultClientPortName, Port: 98765} // Override default client port (10001)
	userPorts := []corev1.ServicePort{userPort, userPortOverride}
	userSelector := map[string]string{"userSelectorKey": "userSelectorValue", RayClusterLabelKey: "userSelectorClusterName"}
	// Specify a "LoadBalancer" type, which differs from the default "ClusterIP" type.
	userType := corev1.ServiceTypeLoadBalancer
	testRayClusterWithHeadService.Spec.HeadGroupSpec.HeadService = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        userName,
			Namespace:   userNamespace,
			Labels:      userLabels,
			Annotations: userAnnotations,
		},
		Spec: corev1.ServiceSpec{
			Ports:    userPorts,
			Selector: userSelector,
			Type:     userType,
		},
	}
	// These labels originate from HeadGroupSpec.Template.ObjectMeta.Labels
	userTemplateClusterName := "userTemplateClusterName"
	template_labels := map[string]string{RayClusterLabelKey: userTemplateClusterName}
	headService, err := BuildServiceForHeadPod(*testRayClusterWithHeadService, template_labels, testRayClusterWithHeadService.Spec.HeadServiceAnnotations)
	if err != nil {
		t.Errorf("failed to build head service: %v", err)
	}
	// The user-provided namespace should be ignored, but the name should be respected
	if headService.ObjectMeta.Namespace != testRayClusterWithHeadService.ObjectMeta.Namespace {
		t.Errorf("User-provided namespace should be ignored: expected namespace=%s, actual namespace=%s", testRayClusterWithHeadService.ObjectMeta.Namespace, headService.ObjectMeta.Namespace)
	}
	if headService.ObjectMeta.Name != userName {
		t.Errorf("User-provided name should be respected: expected name=%s, actual name=%s", userName, headService.ObjectMeta.Name)
	}

	// The selector field should only use the keys from the five default labels.  The values should be updated with the values from the template labels.
	// The user-provided HeadService labels should be ignored for the purposes of the selector field. The user-provided Selector field should be ignored.
	default_labels := HeadServiceLabels(*testRayClusterWithHeadService)
	// Make sure this test isn't spuriously passing. Check that RayClusterLabelKey is in the default labels.
	if _, ok := default_labels[RayClusterLabelKey]; !ok {
		t.Errorf("RayClusterLabelKey=%s should be in the default labels", RayClusterLabelKey)
	}
	for k, v := range headService.Spec.Selector {
		// If k is not in the default labels, then the selector field should not contain it.
		if _, ok := default_labels[k]; !ok {
			t.Errorf("Selector field should not contain key=%s", k)
		}
		// If k is in the template labels, then the selector field should contain it with the value from the template labels.
		// Otherwise, it should contain the value from the default labels.
		if _, ok := template_labels[k]; ok {
			if v != template_labels[k] {
				t.Errorf("Selector field should contain key=%s with value=%s, actual value=%s", k, template_labels[k], v)
			}
		} else {
			if v != default_labels[k] {
				t.Errorf("Selector field should contain key=%s with value=%s, actual value=%s", k, default_labels[k], v)
			}
		}
	}
	// The selector field should have every key from the default labels.
	for k := range default_labels {
		if _, ok := headService.Spec.Selector[k]; !ok {
			t.Errorf("Selector field should contain key=%s", k)
		}
	}

	// Print default labels for debugging
	for k, v := range default_labels {
		fmt.Printf("default label: key=%s, value=%s\n", k, v)
	}

	// Test merged labels. The final labels (headService.ObjectMeta.Labels) should consist of:
	// 1. The final selector (headService.Spec.Selector), updated with
	// 2. The user-specified labels from the HeadService (userLabels).
	// In the case of overlap, the selector labels have priority over userLabels.
	for k, v := range headService.ObjectMeta.Labels {
		// If k is in the user-specified labels, then the final labels should contain it with the value from the final selector.
		// Otherwise, it should contain the value from userLabels from the HeadService.
		if _, ok := headService.Spec.Selector[k]; ok {
			if v != headService.Spec.Selector[k] {
				t.Errorf("Final labels should contain key=%s with value=%s, actual value=%s", k, headService.Spec.Selector[k], v)
			}
		} else if _, ok := userLabels[k]; ok {
			if v != userLabels[k] {
				t.Errorf("Final labels should contain key=%s with value=%s, actual value=%s", k, userLabels[k], v)
			}
		} else {
			t.Errorf("Final labels contains key=%s but it should not", k)
		}
	}
	// Check that every key from the final selector (headService.Spec.Selector) and userLabels is in the final labels.
	for k := range headService.Spec.Selector {
		if _, ok := headService.ObjectMeta.Labels[k]; !ok {
			t.Errorf("Final labels should contain key=%s", k)
		}
	}
	for k := range userLabels {
		if _, ok := headService.ObjectMeta.Labels[k]; !ok {
			t.Errorf("Final labels should contain key=%s", k)
		}
	}

	// Test merged annotations. In the case of overlap (HeadServiceAnnotationKey1) the user annotation should be ignored.
	for k, v := range userAnnotations {
		if headService.ObjectMeta.Annotations[k] != v && k != headServiceAnnotationKey1 {
			t.Errorf("User annotation not found or incorrect value: key=%s, expected value=%s, actual value=%s", k, v, headService.ObjectMeta.Annotations[k])
		}
	}
	if headService.ObjectMeta.Annotations[headServiceAnnotationKey1] != headServiceAnnotationValue1 {
		t.Errorf("User annotation not found or incorrect value: key=%s, expected value=%s, actual value=%s", headServiceAnnotationKey1, headServiceAnnotationValue1, headService.ObjectMeta.Annotations[headServiceAnnotationKey1])
	}
	// HeadServiceAnnotationKey2 should be present with value HeadServiceAnnotationValue2 since it was only specified in HeadServiceAnnotations.
	if headService.ObjectMeta.Annotations[headServiceAnnotationKey2] != headServiceAnnotationValue2 {
		t.Errorf("User annotation not found or incorrect value: key=%s, expected value=%s, actual value=%s", headServiceAnnotationKey2, headServiceAnnotationValue2, headService.ObjectMeta.Annotations[headServiceAnnotationKey2])
	}

	// Test merged ports. In the case of overlap (DefaultClientPortName) the user port should be ignored.
	// DEBUG: Print out the entire head service to help with debugging.
	headServiceJSON, err := json.MarshalIndent(headService, "", "  ")
	if err != nil {
		t.Errorf("failed to marshal head service: %v", err)
	}
	t.Logf("head service: %s", string(headServiceJSON))

	// Test merged ports
	for _, p := range userPorts {
		found := false
		for _, hp := range headService.Spec.Ports {
			if p.Name == hp.Name && p.Port == hp.Port {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("User port not found: %v", p)
		}
	}

	// Test name and namespace are generated if not specified
	if headService.ObjectMeta.Name == "" {
		t.Errorf("Generated head service name is empty")
	}
	if headService.ObjectMeta.Namespace == "" {
		t.Errorf("Generated head service namespace is empty")
	}

	// Test that the user service type takes priority over the default service type (ClusterIP)
	if headService.Spec.Type != userType {
		t.Errorf("Generated head service type is not %s", userType)
	}
}

func TestBuildServiceForHeadPodPortsOrder(t *testing.T) {
	svc1, err1 := BuildServiceForHeadPod(*instanceWithWrongSvc, nil, nil)
	svc2, err2 := BuildServiceForHeadPod(*instanceWithWrongSvc, nil, nil)
	assert.Nil(t, err1)
	assert.Nil(t, err2)

	ports1 := svc1.Spec.Ports
	ports2 := svc2.Spec.Ports

	// length should be same
	assert.Equal(t, len(ports1), len(ports2))
	for i := 0; i < len(ports1); i++ {
		// name should be same
		assert.Equal(t, ports1[i].Name, ports2[i].Name)
	}
}
