package cinder

import (
	cinderv1beta1 "github.com/openstack-k8s-operators/cinder-operator/api/v1beta1"
	common "github.com/openstack-k8s-operators/lib-common/pkg/common"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// FIXME?: The old CN-OSP use of bootstrap.sh does not work here, but not using it might be
	// a problem as it has a few conditionals that should perhaps be considered (and they're not here)
	// DBSyncCommand -
	DBSyncCommand = "/usr/local/bin/kolla_set_configs && su -s /bin/sh -c \"cinder-manage db sync\""
)

// DbSyncJob func
func DbSyncJob(instance *cinderv1beta1.Cinder, labels map[string]string) *batchv1.Job {

	args := []string{"-c"}
	if instance.Spec.Debug.DBSync {
		args = append(args, common.DebugCommand)
	} else {
		args = append(args, DBSyncCommand)
	}

	runAsUser := int64(0)
	envVars := map[string]common.EnvSetter{}
	envVars["KOLLA_CONFIG_FILE"] = common.EnvValue(KollaConfigDbSync)
	envVars["KOLLA_CONFIG_STRATEGY"] = common.EnvValue("COPY_ALWAYS")
	envVars["KOLLA_BOOTSTRAP"] = common.EnvValue("TRUE")

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-db-sync",
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      "OnFailure",
					ServiceAccountName: ServiceAccount,
					Containers: []corev1.Container{
						{
							Name: instance.Name + "-db-sync",
							Command: []string{
								"/bin/bash",
							},
							Args:  args,
							Image: instance.Spec.CinderAPIContainerImage,
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: &runAsUser,
							},
							Env:          common.MergeEnvs([]corev1.EnvVar{}, envVars),
							VolumeMounts: GetVolumeMounts(),
						},
					},
					Volumes: GetVolumes(instance.Name),
				},
			},
		},
	}

	job.Spec.Template.Spec.Volumes = GetVolumes(ServiceName)

	initContainerDetails := APIDetails{
		ContainerImage:       instance.Spec.CinderAPIContainerImage,
		DatabaseHost:         instance.Status.DatabaseHostname,
		DatabaseUser:         instance.Spec.DatabaseUser,
		DatabaseName:         DatabaseName,
		OSPSecret:            instance.Spec.Secret,
		DBPasswordSelector:   instance.Spec.PasswordSelectors.Database,
		UserPasswordSelector: instance.Spec.PasswordSelectors.Service,
		VolumeMounts:         GetInitVolumeMounts(),
	}
	job.Spec.Template.Spec.InitContainers = InitContainer(initContainerDetails)

	return job
}
