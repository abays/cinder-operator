/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cinderbackup

import (
	cinderv1beta1 "github.com/openstack-k8s-operators/cinder-operator/api/v1beta1"
	cinder "github.com/openstack-k8s-operators/cinder-operator/pkg/cinder"
	common "github.com/openstack-k8s-operators/lib-common/modules/common"
	"github.com/openstack-k8s-operators/lib-common/modules/common/affinity"
	"github.com/openstack-k8s-operators/lib-common/modules/common/env"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// ServiceCommand -
	ServiceCommand = "/usr/local/bin/kolla_set_configs && /usr/local/bin/kolla_start"
)

// StatefulSet func
func StatefulSet(
	instance *cinderv1beta1.CinderBackup,
	configHash string,
	labels map[string]string,
) *appsv1.StatefulSet {
	trueVar := true
	rootUser := int64(0)
	// Cinder's uid and gid magic numbers come from the 'cinder-user' in
	// https://github.com/openstack/kolla/blob/master/kolla/common/users.py
	cinderUser := int64(42407)
	cinderGroup := int64(42407)

	// TODO until we determine how to properly query for these
	livenessProbe := &corev1.Probe{
		// TODO might need tuning
		TimeoutSeconds:      5,
		PeriodSeconds:       3,
		InitialDelaySeconds: 3,
	}

	startupProbe := &corev1.Probe{
		TimeoutSeconds:      5,
		FailureThreshold:    12,
		PeriodSeconds:       5,
		InitialDelaySeconds: 5,
	}

	args := []string{"-c"}
	var probeCommand []string
	// When debugging the service container will run kolla_set_configs and
	// sleep forever and the probe container will just sleep forever.
	if instance.Spec.Debug.Service {
		args = append(args, common.DebugCommand)
		livenessProbe.Exec = &corev1.ExecAction{
			Command: []string{
				"/bin/true",
			},
		}
		startupProbe.Exec = livenessProbe.Exec
		probeCommand = []string{
			"/bin/sleep", "infinity",
		}
	} else {
		args = append(args, ServiceCommand)
		// Use the HTTP probe now that we have a simple server running
		livenessProbe.HTTPGet = &corev1.HTTPGetAction{
			Port: intstr.FromInt(8080),
		}
		startupProbe.HTTPGet = livenessProbe.HTTPGet
		// Probe doesn't run kolla_set_configs because it uses the 'cinder' uid
		// and gid and doesn't have permissions to make files be owned by root,
		// so cinder.conf is in its original location
		probeCommand = []string{
			"/usr/local/bin/container-scripts/healthcheck.py",
			"backup",
			"/var/lib/config-data/merged/cinder.conf",
		}
	}

	envVars := map[string]env.Setter{}
	envVars["KOLLA_CONFIG_FILE"] = env.SetValue(KollaConfig)
	envVars["KOLLA_CONFIG_STRATEGY"] = env.SetValue("COPY_ALWAYS")
	envVars["CONFIG_HASH"] = env.SetValue(configHash)

	// Tune glibc for reduced memory usage and fragmentation using single malloc arena for all
	// threads and disabling dynamic thresholds to reduce memory usage when using native threads
	// directly or via eventlet.tpool
	// https://www.gnu.org/software/libc/manual/html_node/Memory-Allocation-Tunables.html
	envVars["MALLOC_ARENA_MAX"] = env.SetValue("1")
	envVars["MALLOC_MMAP_THRESHOLD_"] = env.SetValue("131072")
	envVars["MALLOC_TRIM_THRESHOLD_"] = env.SetValue("262144")

	volumeMounts := GetVolumeMounts()

	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Replicas: &instance.Spec.Replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: cinder.ServiceAccount,
					Containers: []corev1.Container{
						{
							Name: cinder.ServiceName + "-backup",
							Command: []string{
								"/bin/bash",
							},
							Args:  args,
							Image: instance.Spec.ContainerImage,
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  &rootUser,
								Privileged: &trueVar,
							},
							Env:           env.MergeEnvs([]corev1.EnvVar{}, envVars),
							VolumeMounts:  volumeMounts,
							Resources:     instance.Spec.Resources,
							LivenessProbe: livenessProbe,
							StartupProbe:  startupProbe,
						},
						{
							Name:    "probe",
							Command: probeCommand,
							Image:   instance.Spec.ContainerImage,
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  &cinderUser,
								RunAsGroup: &cinderGroup,
							},
							VolumeMounts: volumeMounts,
						},
					},
					NodeSelector: instance.Spec.NodeSelector,
				},
			},
		},
	}
	statefulset.Spec.Template.Spec.Volumes = GetVolumes(cinder.GetOwningCinderName(instance), instance.Name)
	// If possible two pods of the same service should not
	// run on the same worker node. If this is not possible
	// the get still created on the same worker node.
	statefulset.Spec.Template.Spec.Affinity = affinity.DistributePods(
		common.AppSelector,
		[]string{
			cinder.ServiceName,
		},
		corev1.LabelHostname,
	)
	if instance.Spec.NodeSelector != nil && len(instance.Spec.NodeSelector) > 0 {
		statefulset.Spec.Template.Spec.NodeSelector = instance.Spec.NodeSelector
	}

	initContainerDetails := cinder.APIDetails{
		ContainerImage:       instance.Spec.ContainerImage,
		DatabaseHost:         instance.Spec.DatabaseHostname,
		DatabaseUser:         instance.Spec.DatabaseUser,
		DatabaseName:         cinder.DatabaseName,
		OSPSecret:            instance.Spec.Secret,
		TransportURLSecret:   instance.Spec.TransportURLSecret,
		DBPasswordSelector:   instance.Spec.PasswordSelectors.Database,
		UserPasswordSelector: instance.Spec.PasswordSelectors.Service,
		VolumeMounts:         GetInitVolumeMounts(),
		Debug:                instance.Spec.Debug.InitContainer,
	}

	statefulset.Spec.Template.Spec.InitContainers = cinder.InitContainer(initContainerDetails)

	// TODO: Clean up this hack
	// Add custom config for the Backup Service
	envVars = map[string]env.Setter{}
	envVars["CustomConf"] = env.SetValue(common.CustomServiceConfigFileName)
	statefulset.Spec.Template.Spec.InitContainers[0].Env = env.MergeEnvs(statefulset.Spec.Template.Spec.InitContainers[0].Env, envVars)

	return statefulset
}
