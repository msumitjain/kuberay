package util

import (
	"fmt"
	"net"
	"strconv"

	api "github.com/ray-project/kuberay/proto/go_client"
	rayalphaapi "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RayCluster struct {
	*rayalphaapi.RayCluster
}

// NewRayCluster creates a RayCluster.
// func NewRayCluster(apiCluster *api.Cluster, clusterRuntime *api.ClusterRuntime, computeRuntime *api.ComputeRuntime) *RayCluster {
func NewRayCluster(apiCluster *api.Cluster, computeTemplateMap map[string]*api.ComputeTemplate) *RayCluster {
	rayCluster := &rayalphaapi.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        apiCluster.Name,
			Namespace:   apiCluster.Namespace,
			Labels:      buildRayClusterLabels(apiCluster),
			Annotations: buildRayClusterAnnotations(apiCluster),
		},
		Spec: *buildRayClusterSpec(apiCluster.Version, apiCluster.Envs, apiCluster.ClusterSpec, computeTemplateMap),
	}

	return &RayCluster{rayCluster}
}

func buildRayClusterLabels(cluster *api.Cluster) map[string]string {
	labels := map[string]string{}
	labels[RayClusterNameLabelKey] = cluster.Name
	labels[RayClusterUserLabelKey] = cluster.User
	labels[RayClusterVersionLabelKey] = cluster.Version
	labels[RayClusterEnvironmentLabelKey] = cluster.Environment.String()
	labels[KubernetesApplicationNameLabelKey] = ApplicationName
	labels[KubernetesManagedByLabelKey] = ComponentName
	return labels
}

func buildRayClusterAnnotations(cluster *api.Cluster) map[string]string {
	annotations := map[string]string{}
	// TODO: Add optional annotations
	return annotations
}

// TODO(Basasuya & MissionToMars): The job spec depends on ClusterSpec which not all cluster-related configs are included,
// such as `metadata` and `envs`. We just put `imageVersion` and `envs` in the arguments list, and should be refactored later.
func buildRayClusterSpec(imageVersion string, envs map[string]string, clusterSpec *api.ClusterSpec, computeTemplateMap map[string]*api.ComputeTemplate) *rayalphaapi.RayClusterSpec {
	computeTemplate := computeTemplateMap[clusterSpec.HeadGroupSpec.ComputeTemplate]
	headPodTemplate := buildHeadPodTemplate(imageVersion, envs, clusterSpec.HeadGroupSpec, computeTemplate)
	headReplicas := int32(1)
	rayClusterSpec := &rayalphaapi.RayClusterSpec{
		RayVersion: imageVersion,
		HeadGroupSpec: rayalphaapi.HeadGroupSpec{
			ServiceType:    v1.ServiceType(clusterSpec.HeadGroupSpec.ServiceType),
			Template:       headPodTemplate,
			Replicas:       &headReplicas,
			RayStartParams: clusterSpec.HeadGroupSpec.RayStartParams,
		},
		WorkerGroupSpecs: []rayalphaapi.WorkerGroupSpec{},
	}

	for _, spec := range clusterSpec.WorkerGroupSpec {
		computeTemplate = computeTemplateMap[spec.ComputeTemplate]
		workerPodTemplate := buildWorkerPodTemplate(imageVersion, envs, spec, computeTemplate)

		minReplicas := spec.Replicas
		maxReplicas := spec.Replicas
		if spec.MinReplicas != 0 {
			minReplicas = spec.MinReplicas
		}
		if spec.MaxReplicas != 0 {
			maxReplicas = spec.MaxReplicas
		}

		workerNodeSpec := rayalphaapi.WorkerGroupSpec{
			GroupName:      spec.GroupName,
			MinReplicas:    intPointer(minReplicas),
			MaxReplicas:    intPointer(maxReplicas),
			Replicas:       intPointer(spec.Replicas),
			RayStartParams: spec.RayStartParams,
			Template:       workerPodTemplate,
		}

		rayClusterSpec.WorkerGroupSpecs = append(rayClusterSpec.WorkerGroupSpecs, workerNodeSpec)
	}

	return rayClusterSpec
}

func buildNodeGroupAnnotations(computeTemplate *api.ComputeTemplate, image string) map[string]string {
	annotations := map[string]string{}
	annotations[RayClusterComputeTemplateAnnotationKey] = computeTemplate.Name
	annotations[RayClusterImageAnnotationKey] = image
	return annotations
}

func buildHeadPodTemplate(imageVersion string, envs map[string]string, spec *api.HeadGroupSpec, computeRuntime *api.ComputeTemplate) v1.PodTemplateSpec {
	image := constructRayImage(RayClusterDefaultImageRepository, imageVersion)
	if len(spec.Image) != 0 {
		image = spec.Image
	}

	// calculate resources
	cpu := fmt.Sprint(computeRuntime.GetCpu())
	memory := fmt.Sprintf("%d%s", computeRuntime.GetMemory(), "Gi")

	// build volume and volumeMounts
	volMounts := buildVolumeMounts(spec.Volumes)
	vols := buildVols(spec.Volumes)

	podTemplateSpec := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: buildNodeGroupAnnotations(computeRuntime, spec.Image),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "ray-head",
					Image: image,
					Env: []v1.EnvVar{
						{
							Name: "MY_POD_IP",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "status.podIP",
								},
							},
						},
					},
					// Customization is not allowed here. We should consider whether to make this part smart.
					// For now we use serve 8000 port for rayservice and added at util/service.go, do not use the 8000 port here for other purpose.
					Ports: []v1.ContainerPort{
						{
							Name:          "redis",
							ContainerPort: 6379,
						},
						{
							Name:          "head",
							ContainerPort: 10001,
						},
						{
							Name:          "dashboard",
							ContainerPort: 8265,
						},
						{
							Name:          "metrics",
							ContainerPort: 8080,
						},
					},
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse(cpu),
							v1.ResourceMemory: resource.MustParse(memory),
						},
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse(cpu),
							v1.ResourceMemory: resource.MustParse(memory),
						},
					},
					VolumeMounts: volMounts,
				},
			},
			Volumes: vols,
		},
	}

	if computeRuntime.GetGpu() != 0 {
		gpu := computeRuntime.GetGpu()
		accelerator := "nvidia.com/gpu"
		if len(computeRuntime.GetGpuAccelerator()) != 0 {
			accelerator = computeRuntime.GetGpuAccelerator()
		}

		// need smarter algorithm to filter main container. for example filter by name `ray-worker`
		podTemplateSpec.Spec.Containers[0].Resources.Requests[v1.ResourceName(accelerator)] = resource.MustParse(fmt.Sprint(gpu))
		podTemplateSpec.Spec.Containers[0].Resources.Limits[v1.ResourceName(accelerator)] = resource.MustParse(fmt.Sprint(gpu))
	}

	for k, v := range envs {
		podTemplateSpec.Spec.Containers[0].Env = append(podTemplateSpec.Spec.Containers[0].Env, v1.EnvVar{
			Name: k, Value: v,
		})
	}
	return podTemplateSpec
}

func constructRayImage(containerImage string, version string) string {
	return fmt.Sprintf("%s:%s", containerImage, version)
}

func buildWorkerPodTemplate(imageVersion string, envs map[string]string, spec *api.WorkerGroupSpec, computeRuntime *api.ComputeTemplate) v1.PodTemplateSpec {
	// If user doesn't provide the image, let's use the default image instead.
	// TODO: verify the versions in the range
	image := constructRayImage(RayClusterDefaultImageRepository, imageVersion)
	if len(spec.Image) != 0 {
		image = spec.Image
	}

	// calculate resources
	cpu := fmt.Sprint(computeRuntime.GetCpu())
	memory := fmt.Sprintf("%d%s", computeRuntime.GetMemory(), "Gi")

	// build volume and volumeMounts
	volMounts := buildVolumeMounts(spec.Volumes)
	vols := buildVols(spec.Volumes)

	podTemplateSpec := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: buildNodeGroupAnnotations(computeRuntime, spec.Image),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "ray-worker",
					Image: image,
					Env: []v1.EnvVar{
						{
							Name:  "RAY_DISABLE_DOCKER_CPU_WARNING",
							Value: "1",
						},
						{
							Name:  "TYPE",
							Value: "worker",
						},
						{
							Name: "CPU_REQUEST",
							ValueFrom: &v1.EnvVarSource{
								ResourceFieldRef: &v1.ResourceFieldSelector{
									ContainerName: "ray-worker",
									Resource:      "requests.cpu",
								},
							},
						},
						{
							Name: "CPU_LIMITS",
							ValueFrom: &v1.EnvVarSource{
								ResourceFieldRef: &v1.ResourceFieldSelector{
									ContainerName: "ray-worker",
									Resource:      "limits.cpu",
								},
							},
						},
						{
							Name: "MEMORY_REQUESTS",
							ValueFrom: &v1.EnvVarSource{
								ResourceFieldRef: &v1.ResourceFieldSelector{
									ContainerName: "ray-worker",
									Resource:      "requests.cpu",
								},
							},
						},
						{
							Name: "MEMORY_LIMITS",
							ValueFrom: &v1.EnvVarSource{
								ResourceFieldRef: &v1.ResourceFieldSelector{
									ContainerName: "ray-worker",
									Resource:      "limits.cpu",
								},
							},
						},
						{
							Name: "MY_POD_NAME",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "metadata.name",
								},
							},
						},
						{
							Name: "MY_POD_IP",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "status.podIP",
								},
							},
						},
					},
					Ports: []v1.ContainerPort{
						{
							ContainerPort: 80,
						},
					},
					Lifecycle: &v1.Lifecycle{
						PreStop: &v1.LifecycleHandler{
							Exec: &v1.ExecAction{
								Command: []string{
									"/bin/sh", "-c", "ray stop",
								},
							},
						},
					},
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse(cpu),
							v1.ResourceMemory: resource.MustParse(memory),
						},
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse(cpu),
							v1.ResourceMemory: resource.MustParse(memory),
						},
					},
					VolumeMounts: volMounts,
				},
			},
			Volumes: vols,
		},
	}

	if computeRuntime.GetGpu() != 0 {
		gpu := computeRuntime.GetGpu()
		accelerator := "nvidia.com/gpu"
		if len(computeRuntime.GetGpuAccelerator()) != 0 {
			accelerator = computeRuntime.GetGpuAccelerator()
		}

		// need smarter algorithm to filter main container. for example filter by name `ray-worker`
		podTemplateSpec.Spec.Containers[0].Resources.Requests[v1.ResourceName(accelerator)] = resource.MustParse(fmt.Sprint(gpu))
		podTemplateSpec.Spec.Containers[0].Resources.Limits[v1.ResourceName(accelerator)] = resource.MustParse(fmt.Sprint(gpu))
	}

	for k, v := range envs {
		podTemplateSpec.Spec.Containers[0].Env = append(podTemplateSpec.Spec.Containers[0].Env, v1.EnvVar{
			Name: k, Value: v,
		})
	}
	return podTemplateSpec
}

func buildVolumeMounts(apiVolumes []*api.Volume) []v1.VolumeMount {
	var (
		volMounts       []v1.VolumeMount
		hostToContainer = v1.MountPropagationHostToContainer
		bidirectonal    = v1.MountPropagationBidirectional
	)
	for _, vol := range apiVolumes {
		volMount := v1.VolumeMount{
			Name:      vol.Name,
			ReadOnly:  vol.ReadOnly,
			MountPath: vol.MountPath,
		}
		switch vol.MountPropagationMode {
		case api.Volume_HOSTTOCONTAINER:
			volMount.MountPropagation = &hostToContainer
		case api.Volume_BIDIRECTIONAL:
			volMount.MountPropagation = &bidirectonal
		}
		volMounts = append(volMounts, volMount)
	}
	return volMounts
}

func newHostPathType(pathType string) *v1.HostPathType {
	hostPathType := new(v1.HostPathType)
	*hostPathType = v1.HostPathType(pathType)
	return hostPathType
}

func buildVols(apiVolumes []*api.Volume) []v1.Volume {
	var vols []v1.Volume
	for _, rayVol := range apiVolumes {
		if rayVol.VolumeType == api.Volume_HOST_PATH {
			vol := v1.Volume{
				Name: rayVol.Name,
				VolumeSource: v1.VolumeSource{
					HostPath: &v1.HostPathVolumeSource{
						Path: rayVol.Source,
					},
				},
			}
			switch rayVol.HostPathType {
			case api.Volume_DIRECTORY:
				vol.VolumeSource.HostPath.Type = newHostPathType(string(v1.HostPathDirectory))
			case api.Volume_FILE:
				vol.VolumeSource.HostPath.Type = newHostPathType(string(v1.HostPathFile))
			default:
				vol.VolumeSource.HostPath.Type = newHostPathType(string(v1.HostPathDirectory))
			}
			vols = append(vols, vol)
		}
		// TODO(Jeffwan@): handle PVC in the future
	}

	return vols
}

func intPointer(value int32) *int32 {
	return &value
}

// Get converts this object to a rayalphaapi.Workflow.
func (c *RayCluster) Get() *rayalphaapi.RayCluster {
	return c.RayCluster
}

// SetAnnotations sets annotations on all templates in a RayCluster
func (c *RayCluster) SetAnnotationsToAllTemplates(key string, value string) {
	// TODO: reserved for common parameters.
}

func NewComputeTemplate(runtime *api.ComputeTemplate) (*v1.ConfigMap, error) {
	config := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runtime.Name,
			Namespace: runtime.Namespace,
			Labels: map[string]string{
				"ray.io/config-type":      "compute-template",
				"ray.io/compute-template": runtime.Name,
			},
		},
		Data: map[string]string{
			"name":            runtime.Name,
			"namespace":       runtime.Namespace,
			"cpu":             strconv.FormatUint(uint64(runtime.Cpu), 10),
			"memory":          strconv.FormatUint(uint64(runtime.Memory), 10),
			"gpu":             strconv.FormatUint(uint64(runtime.Gpu), 10),
			"gpu_accelerator": runtime.GpuAccelerator,
		},
	}

	return config, nil
}

// GetNodeHostIP returns the provided node's IP, based on the priority:
// 1. NodeInternalIP
// 2. NodeExternalIP
func GetNodeHostIP(node *v1.Node) (net.IP, error) {
	addresses := node.Status.Addresses
	addressMap := make(map[v1.NodeAddressType][]v1.NodeAddress)
	for i := range addresses {
		addressMap[addresses[i].Type] = append(addressMap[addresses[i].Type], addresses[i])
	}
	if addresses, ok := addressMap[v1.NodeInternalIP]; ok {
		return net.ParseIP(addresses[0].Address), nil
	}
	if addresses, ok := addressMap[v1.NodeExternalIP]; ok {
		return net.ParseIP(addresses[0].Address), nil
	}
	return nil, fmt.Errorf("host IP unknown; known addresses: %v", addresses)
}
