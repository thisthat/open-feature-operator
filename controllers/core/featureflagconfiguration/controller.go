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

package featureflagconfiguration

import (
	"context"

	"github.com/go-logr/logr"
	corev1alpha1 "github.com/thisthat/open-feature-operator/apis/core/v1alpha1"
	"github.com/thisthat/open-feature-operator/controllers/common"
	"github.com/thisthat/open-feature-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// FeatureFlagConfigurationReconciler reconciles a FeatureFlagConfiguration object
type FeatureFlagConfigurationReconciler struct {
	client.Client

	// Scheme contains the scheme of this controller
	Scheme *runtime.Scheme
	// ReqLogger contains the Logger of this controller
	Log logr.Logger
}

//+kubebuilder:rbac:groups=core.openfeature.dev,resources=featureflagconfigurations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.openfeature.dev,resources=featureflagconfigurations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.openfeature.dev,resources=featureflagconfigurations/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the FeatureFlagConfiguration object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile

const CrdName = "FeatureFlagConfiguration"

//nolint:gocognit,gocyclo
func (r *FeatureFlagConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling" + CrdName)

	ffconf := &corev1alpha1.FeatureFlagConfiguration{}
	if err := r.Client.Get(ctx, req.NamespacedName, ffconf); err != nil {
		if errors.IsNotFound(err) {
			// taking down all associated K8s resources is handled by K8s
			r.Log.Info(CrdName + " resource not found. Ignoring since object must be deleted")
			return r.finishReconcile(nil, false)
		}
		r.Log.Error(err, "Failed to get the "+CrdName)
		return r.finishReconcile(err, false)
	}

	if ffconf.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !utils.ContainsString(ffconf.GetFinalizers(), common.FinalizerName) {
			controllerutil.AddFinalizer(ffconf, common.FinalizerName)
			if err := r.Update(ctx, ffconf); err != nil {
				return r.finishReconcile(err, false)
			}
		}
	} else {
		// The object is being deleted
		if utils.ContainsString(ffconf.GetFinalizers(), common.FinalizerName) {
			controllerutil.RemoveFinalizer(ffconf, common.FinalizerName)
			if err := r.Update(ctx, ffconf); err != nil {
				return ctrl.Result{}, err
			}
		}
		// Stop reconciliation as the item is being deleted
		return r.finishReconcile(nil, false)
	}

	// Check the provider on the FeatureFlagConfiguration
	if !ffconf.Spec.ServiceProvider.IsSet() {
		r.Log.Info("No service provider specified for FeatureFlagConfiguration, using FlagD")
		ffconf.Spec.ServiceProvider = &corev1alpha1.FeatureFlagServiceProvider{
			Name: "flagd",
		}
		if err := r.Update(ctx, ffconf); err != nil {
			r.Log.Error(err, "Failed to update FeatureFlagConfiguration service provider")
			return r.finishReconcile(err, false)
		}
	}

	// Get list of configmaps
	configMapList := &corev1.ConfigMapList{}
	var ffConfigMapList []corev1.ConfigMap
	if err := r.List(ctx, configMapList); err != nil {
		return r.finishReconcile(err, false)
	}

	// Get list of configmaps with annotation
	for _, cm := range configMapList.Items {
		val, ok := cm.GetAnnotations()["openfeature.dev/featureflagconfiguration"]
		if ok && val == ffconf.Name {
			ffConfigMapList = append(ffConfigMapList, cm)
		}
	}

	for _, cm := range ffConfigMapList {
		// Append OwnerReference if not set
		if !r.featureFlagResourceIsOwner(ffconf, cm) {
			r.Log.Info("Setting owner reference for " + cm.Name)
			cm.OwnerReferences = append(cm.OwnerReferences, ffconf.GetReference())
			err := r.Client.Update(ctx, &cm)
			if err != nil {
				return r.finishReconcile(err, true)
			}
		} else if len(cm.OwnerReferences) == 1 {
			// Delete ConfigMap if the Controller is the only reference
			r.Log.Info("Deleting configmap " + cm.Name)
			err := r.Client.Delete(ctx, &cm)
			return r.finishReconcile(err, true)
		}
		// Update ConfigMap Spec
		r.Log.Info("Updating ConfigMap Spec " + cm.Name)
		cm.Data = map[string]string{
			utils.FeatureFlagConfigurationConfigMapKey(cm.Namespace, cm.Name): ffconf.Spec.FeatureFlagSpec,
		}
		err := r.Client.Update(ctx, &cm)
		if err != nil {
			return r.finishReconcile(err, true)
		}
	}

	return r.finishReconcile(nil, false)
}

func (r *FeatureFlagConfigurationReconciler) finishReconcile(err error, requeueImmediate bool) (ctrl.Result, error) {
	if err != nil {
		interval := common.ReconcileErrorInterval
		if requeueImmediate {
			interval = 0
		}
		r.Log.Error(err, "Finished Reconciling "+CrdName+" with error: %w")
		return ctrl.Result{Requeue: true, RequeueAfter: interval}, err
	}
	interval := common.ReconcileSuccessInterval
	if requeueImmediate {
		interval = 0
	}
	r.Log.Info("Finished Reconciling " + CrdName)
	return ctrl.Result{Requeue: true, RequeueAfter: interval}, nil
}

func (r *FeatureFlagConfigurationReconciler) featureFlagResourceIsOwner(ff *corev1alpha1.FeatureFlagConfiguration, cm corev1.ConfigMap) bool {
	for _, cmOwner := range cm.OwnerReferences {
		if cmOwner.UID == ff.GetReference().UID {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *FeatureFlagConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.FeatureFlagConfiguration{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
