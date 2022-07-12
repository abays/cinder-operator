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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	cinderv1beta1 "github.com/openstack-k8s-operators/cinder-operator/api/v1beta1"
	"github.com/openstack-k8s-operators/cinder-operator/pkg/cinder"
	cinderapi "github.com/openstack-k8s-operators/cinder-operator/pkg/cinderapi"
	keystonev1beta1 "github.com/openstack-k8s-operators/keystone-operator/api/v1beta1"
	"github.com/openstack-k8s-operators/lib-common/pkg/common"
	"github.com/openstack-k8s-operators/lib-common/pkg/condition"
	"github.com/openstack-k8s-operators/lib-common/pkg/helper"
)

// GetClient -
func (r *CinderAPIReconciler) GetClient() client.Client {
	return r.Client
}

// GetKClient -
func (r *CinderAPIReconciler) GetKClient() kubernetes.Interface {
	return r.Kclient
}

// GetLogger -
func (r *CinderAPIReconciler) GetLogger() logr.Logger {
	return r.Log
}

// GetScheme -
func (r *CinderAPIReconciler) GetScheme() *runtime.Scheme {
	return r.Scheme
}

// CinderAPIReconciler reconciles a CinderAPI object
type CinderAPIReconciler struct {
	client.Client
	Kclient kubernetes.Interface
	Log     logr.Logger
	Scheme  *runtime.Scheme
}

//+kubebuilder:rbac:groups=cinder.openstack.org,resources=cinderapis,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cinder.openstack.org,resources=cinderapis/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cinder.openstack.org,resources=cinderapis/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;create;update;delete;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;create;update;delete;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;create;update;delete;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;create;update;delete;watch

// Reconcile -
func (r *CinderAPIReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// Fetch the CinderAPI instance
	instance := &cinderv1beta1.CinderAPI{}
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
	if instance.Status.APIEndpoints == nil {
		instance.Status.APIEndpoints = map[string]string{}
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
func (r *CinderAPIReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// watch for configmap where the CM owner label AND the CR.Spec.ManagingCrName label matches
	configMapFn := func(o client.Object) []reconcile.Request {
		result := []reconcile.Request{}

		// get all API CRs
		apis := &cinderv1beta1.CinderAPIList{}
		listOpts := []client.ListOption{
			client.InNamespace(o.GetNamespace()),
		}
		if err := r.Client.List(context.Background(), apis, listOpts...); err != nil {
			r.Log.Error(err, "Unable to retrieve API CRs %v")
			return nil
		}

		label := o.GetLabels()
		// TODO: Just trying to verify that the CM is owned by this CR's managing CR
		if l, ok := label["cinder.openstack.org/name"]; ok {
			for _, cr := range apis.Items {
				// return reconcil event for the CR where the CM owner label AND the CR.Spec.ManagingCrName matches
				if l == cr.Spec.ManagingCrName {
					// return namespace and Name of CR
					name := client.ObjectKey{
						Namespace: o.GetNamespace(),
						Name:      cr.Name,
					}
					r.Log.Info(fmt.Sprintf("ConfigMap object %s and CR %s marked with label: %s", o.GetName(), cr.Name, l))
					result = append(result, reconcile.Request{NamespacedName: name})
				}
			}
		}
		if len(result) > 0 {
			return result
		}
		return nil
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&cinderv1beta1.CinderAPI{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Secret{}).
		// watch the config CMs we don't own
		Watches(&source.Kind{Type: &corev1.ConfigMap{}},
			handler.EnqueueRequestsFromMapFunc(configMapFn)).
		Complete(r)
}

func (r *CinderAPIReconciler) reconcileDelete(ctx context.Context, instance *cinderv1beta1.CinderAPI, helper *helper.Helper) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service delete")

	// Service is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(instance, helper.GetFinalizer())
	r.Log.Info("Reconciled Service delete successfully")
	if err := r.Update(ctx, instance); err != nil && !k8s_errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *CinderAPIReconciler) reconcileInit(
	ctx context.Context,
	instance *cinderv1beta1.CinderAPI,
	helper *helper.Helper,
	serviceLabels map[string]string,
) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service init")

	//
	// expose the service (create service, route and return the created endpoint URLs)
	//
	var ports = map[common.Endpoint]int32{
		common.EndpointAdmin:    cinder.CinderAdminPort,
		common.EndpointPublic:   cinder.CinderPublicPort,
		common.EndpointInternal: cinder.CinderInternalPort,
	}

	apiEndpoints, ctrlResult, err := common.ExposeEndpoints(
		ctx,
		helper,
		cinder.ServiceName,
		serviceLabels,
		ports,
	)
	if err != nil {
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		return ctrlResult, nil
	}

	//
	// Update instance status with service endpoint url from route host information
	//
	// TODO: need to support https default here
	if instance.Status.APIEndpoints == nil {
		instance.Status.APIEndpoints = map[string]string{}
	}
	instance.Status.APIEndpoints = apiEndpoints

	// expose service - end

	//
	// create users and endpoints - https://docs.openstack.org/Cinder/latest/install/install-rdo.html#configure-user-and-endpoints
	// TODO: rework this
	//
	ospSecret, _, err := common.GetSecret(ctx, helper, instance.Spec.Secret, instance.Namespace)
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: time.Second * 10}, fmt.Errorf("OpenStack secret %s not found", instance.Spec.Secret)
		}
		return ctrl.Result{}, err
	}

	cinderKeystoneServiceV2 := &keystonev1beta1.KeystoneService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%sv2", instance.Name),
			Namespace: instance.Namespace,
		},
	}

	_, err = controllerutil.CreateOrPatch(context.TODO(), r.Client, cinderKeystoneServiceV2, func() error {
		cinderKeystoneServiceV2.Spec.Username = instance.Spec.ServiceUser
		cinderKeystoneServiceV2.Spec.Password = string(ospSecret.Data[instance.Spec.PasswordSelectors.Service])
		cinderKeystoneServiceV2.Spec.ServiceType = cinder.ServiceTypeV2
		cinderKeystoneServiceV2.Spec.ServiceName = cinder.ServiceNameV2
		cinderKeystoneServiceV2.Spec.ServiceDescription = cinder.ServiceName
		cinderKeystoneServiceV2.Spec.Enabled = true
		// TODO: get from keystone object
		cinderKeystoneServiceV2.Spec.Region = "regionOne"
		cinderKeystoneServiceV2.Spec.AdminURL = apiEndpoints["admin"]
		cinderKeystoneServiceV2.Spec.PublicURL = apiEndpoints["public"]
		cinderKeystoneServiceV2.Spec.InternalURL = apiEndpoints["internal"]

		return nil
	})

	if err != nil {
		return ctrl.Result{}, err
	}

	cinderKeystoneServiceV3 := &keystonev1beta1.KeystoneService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%sv3", instance.Name),
			Namespace: instance.Namespace,
		},
	}

	_, err = controllerutil.CreateOrPatch(context.TODO(), r.Client, cinderKeystoneServiceV3, func() error {
		// don't pass the cinder user again as update is right now not handled in KeystoneService
		// atm we'll get a cinderv3 user which is not used.
		cinderKeystoneServiceV3.Spec.ServiceType = cinder.ServiceTypeV3
		cinderKeystoneServiceV3.Spec.ServiceName = cinder.ServiceNameV3
		cinderKeystoneServiceV3.Spec.ServiceDescription = cinder.ServiceName
		cinderKeystoneServiceV3.Spec.Enabled = true
		// TODO: get from keystone object
		cinderKeystoneServiceV3.Spec.Region = "regionOne"
		cinderKeystoneServiceV3.Spec.AdminURL = apiEndpoints["admin"]
		cinderKeystoneServiceV3.Spec.PublicURL = apiEndpoints["public"]
		cinderKeystoneServiceV3.Spec.InternalURL = apiEndpoints["internal"]

		return nil
	})

	if err != nil {
		return ctrl.Result{}, err
	}

	r.Log.Info("Reconciled Service init successfully")
	return ctrl.Result{}, nil
}

func (r *CinderAPIReconciler) reconcileNormal(ctx context.Context, instance *cinderv1beta1.CinderAPI, helper *helper.Helper) (ctrl.Result, error) {
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
	// check for required Cinder config maps that should have been created by ManagingCr
	//

	configMaps := []string{
		fmt.Sprintf("%s-scripts", instance.Spec.ManagingCrName),     //ScriptsConfigMap
		fmt.Sprintf("%s-config-data", instance.Spec.ManagingCrName), //ConfigMap
	}

	_, err = common.GetConfigMaps(ctx, r, instance, configMaps, instance.Namespace, &configMapVars)
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: time.Second * 10}, fmt.Errorf("Could not find all Cinder config maps for %s", instance.Spec.ManagingCrName)
		}
		return ctrl.Result{}, err
	}

	// run check Cinder ManagingCr config maps - end

	//
	// create hash over all the different input resources to identify if any those changed
	// and a restart/recreate is required.
	//
	inputHash, err := r.createHashOfInputHashes(ctx, instance, configMapVars)
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

	// Define a new Deployment object
	depl := common.NewDeployment(
		cinderapi.Deployment(instance, inputHash, serviceLabels),
		5,
	)

	ctrlResult, err = depl.CreateOrPatch(ctx, helper)
	if err != nil {
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		return ctrlResult, nil
	}
	// create Deployment - end

	r.Log.Info("Reconciled Service successfully")
	return ctrl.Result{}, nil
}

func (r *CinderAPIReconciler) reconcileUpdate(ctx context.Context, instance *cinderv1beta1.CinderAPI, helper *helper.Helper) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service update")

	// TODO: should have minor update tasks if required
	// - delete dbsync hash from status to rerun it?

	r.Log.Info("Reconciled Service update successfully")
	return ctrl.Result{}, nil
}

func (r *CinderAPIReconciler) reconcileUpgrade(ctx context.Context, instance *cinderv1beta1.CinderAPI, helper *helper.Helper) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service upgrade")

	// TODO: should have major version upgrade tasks
	// -delete dbsync hash from status to rerun it?

	r.Log.Info("Reconciled Service upgrade successfully")
	return ctrl.Result{}, nil
}

//
// createHashOfInputHashes - creates a hash of hashes which gets added to the resources which requires a restart
// if any of the input resources change, like configs, passwords, ...
//
func (r *CinderAPIReconciler) createHashOfInputHashes(
	ctx context.Context,
	instance *cinderv1beta1.CinderAPI,
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
