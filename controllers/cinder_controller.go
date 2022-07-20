/*
Copyright 2022.

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

package controllers

import (
	"context"
	"fmt"
	"time"

	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	cinderv1beta1 "github.com/openstack-k8s-operators/cinder-operator/api/v1beta1"
	"github.com/openstack-k8s-operators/cinder-operator/pkg/cinder"
	keystonev1 "github.com/openstack-k8s-operators/keystone-operator/api/v1beta1"
	keystone "github.com/openstack-k8s-operators/keystone-operator/pkg/external"
	"github.com/openstack-k8s-operators/lib-common/pkg/common"
	"github.com/openstack-k8s-operators/lib-common/pkg/condition"
	"github.com/openstack-k8s-operators/lib-common/pkg/database"
	"github.com/openstack-k8s-operators/lib-common/pkg/helper"
	mariadbv1 "github.com/openstack-k8s-operators/mariadb-operator/api/v1beta1"

	routev1 "github.com/openshift/api/route/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetClient -
func (r *CinderReconciler) GetClient() client.Client {
	return r.Client
}

// GetKClient -
func (r *CinderReconciler) GetKClient() kubernetes.Interface {
	return r.Kclient
}

// GetLogger -
func (r *CinderReconciler) GetLogger() logr.Logger {
	return r.Log
}

// GetScheme -
func (r *CinderReconciler) GetScheme() *runtime.Scheme {
	return r.Scheme
}

// CinderReconciler reconciles a Cinder object
type CinderReconciler struct {
	client.Client
	Kclient kubernetes.Interface
	Log     logr.Logger
	Scheme  *runtime.Scheme
}

// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cinders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cinders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cinders/finalizers,verbs=update
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cinderapis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cinderapis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cinderapis/finalizers,verbs=update
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cinderschedulers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cinderschedulers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cinderschedulers/finalizers,verbs=update
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cinderbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cinderbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cinderbackups/finalizers,verbs=update
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cindervolumes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cindervolumes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cinder.openstack.org,resources=cindervolumes/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;create;update;delete;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;create;update;patch;delete;watch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;create;update;delete;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;create;update;delete;watch
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mariadb.openstack.org,resources=mariadbdatabases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keystone.openstack.org,resources=keystoneapis,verbs=get;list;watch
// +kubebuilder:rbac:groups=keystone.openstack.org,resources=keystoneservices,verbs=get;list;watch;create;update;patch;delete

// Reconcile -
func (r *CinderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// Fetch the Cinder instance
	instance := &cinderv1beta1.Cinder{}
	err := r.Client.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected.
			// For additional cleanup logic use finalizers. Return and don't requeue.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	//
	// initialize status
	//
	if instance.Status.Conditions == nil {
		instance.Status.Conditions = condition.List{}
	}
	if instance.Status.Hash == nil {
		instance.Status.Hash = map[string]string{}
	}

	helper, err := helper.NewHelper(
		instance,
		r.Client,
		r.Kclient,
		r.Scheme,
		r.Log,
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always patch the instance status when exiting this function so we can persist any changes.
	defer func() {
		if err := helper.SetAfter(instance); err != nil {
			common.LogErrorForObject(r, err, "Set after and calc patch/diff", instance)
		}

		if changed := helper.GetChanges()["status"]; changed {
			patch := client.MergeFrom(helper.GetBeforeObject())

			if err := r.Status().Patch(ctx, instance, patch); err != nil && !k8s_errors.IsNotFound(err) {
				common.LogErrorForObject(r, err, "Update status", instance)
			}
		}
	}()

	// Handle service delete
	if !instance.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, instance, helper)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(ctx, instance, helper)
}

// SetupWithManager sets up the controller with the Manager.
func (r *CinderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cinderv1beta1.Cinder{}).
		Owns(&mariadbv1.MariaDBDatabase{}).
		Owns(&keystonev1.KeystoneService{}).
		Owns(&cinderv1beta1.CinderAPI{}).
		Owns(&cinderv1beta1.CinderScheduler{}).
		Owns(&cinderv1beta1.CinderBackup{}).
		Owns(&cinderv1beta1.CinderVolume{}).
		Owns(&batchv1.Job{}).
		Owns(&routev1.Route{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

func (r *CinderReconciler) reconcileDelete(ctx context.Context, instance *cinderv1beta1.Cinder, helper *helper.Helper) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service delete")

	// Service is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(instance, helper.GetFinalizer())
	r.Log.Info("Reconciled Service delete successfully")
	if err := r.Update(ctx, instance); err != nil && !k8s_errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *CinderReconciler) reconcileInit(
	ctx context.Context,
	instance *cinderv1beta1.Cinder,
	helper *helper.Helper,
	serviceLabels map[string]string,
) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service init")

	//
	// create service DB instance
	//
	db := database.NewDatabase(
		instance.Name,
		instance.Spec.DatabaseUser,
		instance.Spec.Secret,
		map[string]string{
			"dbName": instance.Spec.DatabaseInstance,
		},
	)
	// create or patch the DB
	cond, ctrlResult, err := db.CreateOrPatchDB(
		ctx,
		helper,
	)
	instance.Status.Conditions.UpdateCurrentCondition(cond)

	if err != nil {
		return ctrl.Result{}, err
	}
	if (ctrlResult != ctrl.Result{}) {
		r.Log.Info(cond.Message)
		return ctrlResult, nil
	}
	// wait for the DB to be setup
	cond, ctrlResult, err = db.WaitForDBCreated(ctx, helper)
	instance.Status.Conditions.UpdateCurrentCondition(cond)
	if err != nil {
		return ctrlResult, err
	}
	if (ctrlResult != ctrl.Result{}) {
		r.Log.Info(cond.Message)
		return ctrlResult, nil
	}
	// update Status.DatabaseHostname, used to config the service
	instance.Status.DatabaseHostname = db.GetDatabaseHostname()
	// create service DB - end

	//
	// create users and endpoints - https://docs.openstack.org/Glance/latest/install/install-rdo.html#configure-user-and-endpoints
	// TODO: rework this, and also...place in cinder-api controller instead?
	//

	//
	// run Cinder db sync
	//
	dbSyncHash := instance.Status.Hash[cinderv1beta1.DbSyncHash]
	jobDef := cinder.DbSyncJob(instance, serviceLabels)
	dbSyncjob := common.NewJob(
		jobDef,
		cinderv1beta1.DbSyncHash,
		instance.Spec.PreserveJobs,
		5,
		dbSyncHash,
	)
	ctrlResult, err = dbSyncjob.DoJob(
		ctx,
		helper,
	)
	if (ctrlResult != ctrl.Result{}) {
		return ctrlResult, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if dbSyncjob.HasChanged() {
		instance.Status.Hash[cinderv1beta1.DbSyncHash] = dbSyncjob.GetHash()
		if err := r.Client.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
		r.Log.Info(fmt.Sprintf("Job %s hash added - %s", jobDef.Name, instance.Status.Hash[cinderv1beta1.DbSyncHash]))
	}

	// run Glance db sync - end

	r.Log.Info("Reconciled Service init successfully")
	return ctrl.Result{}, nil
}

func (r *CinderReconciler) reconcileNormal(ctx context.Context, instance *cinderv1beta1.Cinder, helper *helper.Helper) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service")

	// If the service object doesn't have our finalizer, add it.
	controllerutil.AddFinalizer(instance, helper.GetFinalizer())
	// Register the finalizer immediately to avoid orphaning resources on delete
	//if err := patchHelper.Patch(ctx, openStackCluster); err != nil {
	if err := r.Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	// ConfigMap
	configMapVars := make(map[string]common.EnvSetter)

	//
	// check for required OpenStack secret holding passwords for service/admin user and add hash to the vars map
	//
	ospSecret, hash, err := common.GetSecret(ctx, helper, instance.Spec.Secret, instance.Namespace)
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: time.Second * 10}, fmt.Errorf("OpenStack secret %s not found", instance.Spec.Secret)
		}
		return ctrl.Result{}, err
	}
	configMapVars[ospSecret.Name] = common.EnvValue(hash)
	// run check OpenStack secret - end

	//
	// Create ConfigMaps and Secrets required as input for the Service and calculate an overall hash of hashes
	//

	//
	// create Configmap required for cinder input
	// - %-scripts configmap holding scripts to e.g. bootstrap the service
	// - %-config configmap holding minimal cinder config required to get the service up, user can add additional files to be added to the service
	// - parameters which has passwords gets added from the OpenStack secret via the init container
	//
	err = r.generateServiceConfigMaps(ctx, helper, instance, &configMapVars)
	if err != nil {
		return ctrl.Result{}, err
	}

	//
	// create hash over all the different input resources to identify if any those changed
	// and a restart/recreate is required.
	//
	_, err = r.createHashOfInputHashes(ctx, instance, configMapVars)
	if err != nil {
		return ctrl.Result{}, err
	}
	// Create ConfigMaps and Secrets - end

	//
	// TODO check when/if Init, Update, or Upgrade should/could be skipped
	//

	serviceLabels := map[string]string{
		common.AppSelector: cinder.ServiceName,
	}

	// Handle service init
	ctrlResult, err := r.reconcileInit(ctx, instance, helper, serviceLabels)
	if err != nil {
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		return ctrlResult, nil
	}

	// Handle service update
	ctrlResult, err = r.reconcileUpdate(ctx, instance, helper)
	if err != nil {
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		return ctrlResult, nil
	}

	// Handle service upgrade
	ctrlResult, err = r.reconcileUpgrade(ctx, instance, helper)
	if err != nil {
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		return ctrlResult, nil
	}

	//
	// normal reconcile tasks
	//

	// TODO: create all sub-CRs, etc

	// deploy cinder-api
	op, err := r.apiDeploymentCreateOrUpdate(instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	if op != controllerutil.OperationResultNone {
		r.Log.Info(fmt.Sprintf("Deployment %s successfully reconciled - operation: %s", instance.Name, string(op)))
	}

	// TODO: These will not work without rabbit (and nova?) yet
	// // deploy cinder-scheduler
	// op, err = r.schedulerDeploymentCreateOrUpdate(instance)
	// if err != nil {
	// 	return ctrl.Result{}, err
	// }
	// if op != controllerutil.OperationResultNone {
	// 	r.Log.Info(fmt.Sprintf("Deployment %s successfully reconciled - operation: %s", instance.Name, string(op)))
	// }

	// // deploy cinder-backup
	// op, err = r.backupDeploymentCreateOrUpdate(instance)
	// if err != nil {
	// 	return ctrl.Result{}, err
	// }
	// if op != controllerutil.OperationResultNone {
	// 	r.Log.Info(fmt.Sprintf("Deployment %s successfully reconciled - operation: %s", instance.Name, string(op)))
	// }

	// TODO: This requires some sort of backend (and rabbit and nova?)
	// // deploy cinder-volumes
	// for _, volume := range instance.Spec.CinderVolumes {
	// 	op, err = r.volumeDeploymentCreateOrUpdate(instance, &volume)
	// 	if err != nil {
	// 		return ctrl.Result{}, err
	// 	}
	// 	if op != controllerutil.OperationResultNone {
	// 		r.Log.Info(fmt.Sprintf("Deployment %s successfully reconciled - operation: %s", instance.Name, string(op)))
	// 	}
	// }

	r.Log.Info("Reconciled Service successfully")
	return ctrl.Result{}, nil
}

func (r *CinderReconciler) reconcileUpdate(ctx context.Context, instance *cinderv1beta1.Cinder, helper *helper.Helper) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service update")

	// TODO: should have minor update tasks if required
	// - delete dbsync hash from status to rerun it?

	r.Log.Info("Reconciled Service update successfully")
	return ctrl.Result{}, nil
}

func (r *CinderReconciler) reconcileUpgrade(ctx context.Context, instance *cinderv1beta1.Cinder, helper *helper.Helper) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service upgrade")

	// TODO: should have major version upgrade tasks
	// -delete dbsync hash from status to rerun it?

	r.Log.Info("Reconciled Service upgrade successfully")
	return ctrl.Result{}, nil
}

//
// generateServiceConfigMaps - create create configmaps which hold scripts and service configuration
// TODO add DefaultConfigOverwrite
//
func (r *CinderReconciler) generateServiceConfigMaps(
	ctx context.Context,
	h *helper.Helper,
	instance *cinderv1beta1.Cinder,
	envVars *map[string]common.EnvSetter,
) error {
	//
	// create Configmap/Secret required for cinder input
	// - %-scripts configmap holding scripts to e.g. bootstrap the service
	// - %-config configmap holding minimal cinder config required to get the service up, user can add additional files to be added to the service
	// - parameters which has passwords gets added from the ospSecret via the init container
	//

	cmLabels := common.GetLabels(instance, common.GetGroupLabel(cinder.ServiceName), map[string]string{})

	// customData hold any customization for the service.
	// custom.conf is going to /etc/<service>/<service>.conf.d
	// all other files get placed into /etc/<service> to allow overwrite of e.g. logging.conf or policy.json
	// TODO: make sure custom.conf can not be overwritten
	customData := map[string]string{common.CustomServiceConfigFileName: instance.Spec.CustomServiceConfig}

	for key, data := range instance.Spec.DefaultConfigOverwrite {
		customData[key] = data
	}

	// Each individual Volume service might have custom config, so add a key/value to the Cinder configuration
	// config map for each volume
	for _, volume := range instance.Spec.CinderVolumes {
		customData[fmt.Sprintf("%s-volume-%s-%s", instance.Name, volume.Name, common.CustomServiceConfigFileName)] = volume.CustomServiceConfig
	}

	keystoneAPI, err := keystone.GetKeystoneAPI(ctx, h, instance.Namespace, map[string]string{})
	if err != nil {
		return err
	}
	authURL, err := keystoneAPI.GetEndpoint(common.EndpointPublic)
	if err != nil {
		return err
	}
	templateParameters := make(map[string]interface{})
	templateParameters["ServiceUser"] = instance.Spec.ServiceUser
	templateParameters["KeystonePublicURL"] = authURL

	cms := []common.Template{
		// ScriptsConfigMap
		{
			Name:               fmt.Sprintf("%s-scripts", instance.Name),
			Namespace:          instance.Namespace,
			Type:               common.TemplateTypeScripts,
			InstanceType:       instance.Kind,
			AdditionalTemplate: map[string]string{"common.sh": "/common/common.sh"},
			Labels:             cmLabels,
		},
		// ConfigMap
		{
			Name:          fmt.Sprintf("%s-config-data", instance.Name),
			Namespace:     instance.Namespace,
			Type:          common.TemplateTypeConfig,
			InstanceType:  instance.Kind,
			CustomData:    customData,
			ConfigOptions: templateParameters,
			Labels:        cmLabels,
		},
	}

	err = common.EnsureConfigMaps(ctx, r, instance, cms, envVars)
	if err != nil {
		return nil
	}

	return nil
}

//
// createHashOfInputHashes - creates a hash of hashes which gets added to the resources which requires a restart
// if any of the input resources change, like configs, passwords, ...
//
func (r *CinderReconciler) createHashOfInputHashes(
	ctx context.Context,
	instance *cinderv1beta1.Cinder,
	envVars map[string]common.EnvSetter,
) (string, error) {
	mergedMapVars := common.MergeEnvs([]corev1.EnvVar{}, envVars)
	hash, err := common.ObjectHash(mergedMapVars)
	if err != nil {
		return hash, err
	}
	if hashMap, changed := common.SetHash(instance.Status.Hash, common.InputHashName, hash); changed {
		instance.Status.Hash = hashMap
		if err := r.Client.Status().Update(ctx, instance); err != nil {
			return hash, err
		}
		r.Log.Info(fmt.Sprintf("Input maps hash %s - %s", common.InputHashName, hash))
	}
	return hash, nil
}

func (r *CinderReconciler) apiDeploymentCreateOrUpdate(instance *cinderv1beta1.Cinder) (controllerutil.OperationResult, error) {
	deployment := &cinderv1beta1.CinderAPI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-api", instance.Name),
			Namespace: instance.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, deployment, func() error {
		deployment.Spec = cinderv1beta1.CinderAPISpec{
			ManagingCrName:    instance.Name,
			ServiceUser:       instance.Spec.ServiceUser,
			Replicas:          instance.Spec.CinderAPIReplicas,
			ContainerImage:    instance.Spec.CinderAPIContainerImage,
			DatabaseHostname:  instance.Status.DatabaseHostname,
			DatabaseUser:      instance.Spec.DatabaseUser,
			Secret:            instance.Spec.Secret,
			PasswordSelectors: instance.Spec.PasswordSelectors,
			NodeSelector:      instance.Spec.CinderAPINodeSelector,
		}

		err := controllerutil.SetControllerReference(instance, deployment, r.Scheme)
		if err != nil {
			return err
		}

		return nil
	})

	return op, err
}

func (r *CinderReconciler) schedulerDeploymentCreateOrUpdate(instance *cinderv1beta1.Cinder) (controllerutil.OperationResult, error) {
	deployment := &cinderv1beta1.CinderScheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-scheduler", instance.Name),
			Namespace: instance.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, deployment, func() error {
		deployment.Spec = cinderv1beta1.CinderSchedulerSpec{
			ManagingCrName:    instance.Name,
			ServiceUser:       instance.Spec.ServiceUser,
			Replicas:          instance.Spec.CinderSchedulerReplicas,
			ContainerImage:    instance.Spec.CinderSchedulerContainerImage,
			DatabaseHostname:  instance.Status.DatabaseHostname,
			DatabaseUser:      instance.Spec.DatabaseUser,
			Secret:            instance.Spec.Secret,
			PasswordSelectors: instance.Spec.PasswordSelectors,
			NodeSelector:      instance.Spec.CinderSchedulerNodeSelector,
		}

		err := controllerutil.SetControllerReference(instance, deployment, r.Scheme)
		if err != nil {
			return err
		}

		return nil
	})

	return op, err
}

func (r *CinderReconciler) backupDeploymentCreateOrUpdate(instance *cinderv1beta1.Cinder) (controllerutil.OperationResult, error) {
	deployment := &cinderv1beta1.CinderBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-backup", instance.Name),
			Namespace: instance.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, deployment, func() error {
		deployment.Spec = cinderv1beta1.CinderBackupSpec{
			ManagingCrName:    instance.Name,
			ServiceUser:       instance.Spec.ServiceUser,
			Replicas:          instance.Spec.CinderBackupReplicas,
			ContainerImage:    instance.Spec.CinderBackupContainerImage,
			DatabaseHostname:  instance.Status.DatabaseHostname,
			DatabaseUser:      instance.Spec.DatabaseUser,
			Secret:            instance.Spec.Secret,
			PasswordSelectors: instance.Spec.PasswordSelectors,
			NodeSelector:      instance.Spec.CinderBackupNodeSelector,
		}

		err := controllerutil.SetControllerReference(instance, deployment, r.Scheme)
		if err != nil {
			return err
		}

		return nil
	})

	return op, err
}

func (r *CinderReconciler) volumeDeploymentCreateOrUpdate(instance *cinderv1beta1.Cinder, volume *cinderv1beta1.Volume) (controllerutil.OperationResult, error) {
	deployment := &cinderv1beta1.CinderVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-volume-%s", instance.Name, volume.Name),
			Namespace: instance.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, deployment, func() error {
		deployment.Spec = cinderv1beta1.CinderVolumeSpec{
			ManagingCrName:    instance.Name,
			ServiceUser:       instance.Spec.ServiceUser,
			Replicas:          volume.Replicas,
			ContainerImage:    volume.ContainerImage,
			DatabaseHostname:  instance.Status.DatabaseHostname,
			DatabaseUser:      instance.Spec.DatabaseUser,
			Secret:            instance.Spec.Secret,
			PasswordSelectors: instance.Spec.PasswordSelectors,
			NodeSelector:      volume.NodeSelector,
		}

		err := controllerutil.SetControllerReference(instance, deployment, r.Scheme)
		if err != nil {
			return err
		}

		return nil
	})

	return op, err
}
