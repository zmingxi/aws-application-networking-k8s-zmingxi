/*
Copyright 2021.

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

	"github.com/golang/glog"
	"github.com/pkg/errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gateway_api "sigs.k8s.io/gateway-api/apis/v1beta1"
	mcs_api "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"github.com/aws/aws-application-networking-k8s/controllers/eventhandlers"
	"github.com/aws/aws-application-networking-k8s/pkg/aws"
	"github.com/aws/aws-application-networking-k8s/pkg/config"
	"github.com/aws/aws-application-networking-k8s/pkg/deploy"
	"github.com/aws/aws-application-networking-k8s/pkg/deploy/lattice"
	"github.com/aws/aws-application-networking-k8s/pkg/gateway"
	"github.com/aws/aws-application-networking-k8s/pkg/k8s"
	"github.com/aws/aws-application-networking-k8s/pkg/latticestore"
	"github.com/aws/aws-application-networking-k8s/pkg/model/core"
	latticemodel "github.com/aws/aws-application-networking-k8s/pkg/model/lattice"
	lattice_runtime "github.com/aws/aws-application-networking-k8s/pkg/runtime"
)

// HTTPRouteReconciler reconciles a HTTPRoute object
type HTTPRouteReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	gwReconciler      *GatewayReconciler
	gwClassReconciler *GatewayClassReconciler
	finalizerManager  k8s.FinalizerManager
	eventRecorder     record.EventRecorder
	modelBuilder      gateway.LatticeServiceBuilder
	stackDeployer     deploy.StackDeployer
	latticeDataStore  *latticestore.LatticeDataStore
	stackMashaller    deploy.StackMarshaller
}

const (
	httpRouteFinalizer        = "httproute.k8s.aws/resources"
	LatticeAssignedDomainName = "application-networking.k8s.aws/lattice-assigned-domain-name"
)

func NewHttpRouteReconciler(cloud aws.Cloud, client client.Client, scheme *runtime.Scheme, eventRecorder record.EventRecorder,
	gwReconciler *GatewayReconciler, gwClassReconciler *GatewayClassReconciler, finalizerManager k8s.FinalizerManager,
	latticeDataStore *latticestore.LatticeDataStore) *HTTPRouteReconciler {
	modelBuilder := gateway.NewLatticeServiceBuilder(client, latticeDataStore, cloud)
	stackDeployer := deploy.NewLatticeServiceStackDeploy(cloud, client, latticeDataStore)
	stackMarshaller := deploy.NewDefaultStackMarshaller()

	return &HTTPRouteReconciler{
		Client:            client,
		Scheme:            scheme,
		gwReconciler:      gwReconciler,
		gwClassReconciler: gwClassReconciler,
		finalizerManager:  finalizerManager,
		modelBuilder:      modelBuilder,
		stackDeployer:     stackDeployer,
		eventRecorder:     eventRecorder,
		latticeDataStore:  latticeDataStore,
		stackMashaller:    stackMarshaller,
	}
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the HTTPRoute object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *HTTPRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return lattice_runtime.HandleReconcileError(r.reconcile(ctx, req))
}

func (r *HTTPRouteReconciler) reconcile(ctx context.Context, req ctrl.Request) error {
	httpLog := log.FromContext(ctx)

	// TODO(user): your logic here
	httpLog.Info("HTTPRouteReconciler")

	httpRoute := &gateway_api.HTTPRoute{}

	if err := r.Client.Get(ctx, req.NamespacedName, httpRoute); err != nil {
		return client.IgnoreNotFound(err)
	}

	if !r.isHTTPRouteRelevant(ctx, httpRoute) {
		// not relevalnt
		return nil
	}

	if !httpRoute.DeletionTimestamp.IsZero() {
		httpLog.Info("Deleting")
		r.eventRecorder.Event(httpRoute, corev1.EventTypeNormal,
			k8s.HTTPRouteeventReasonReconcile, "Deleting Reconcile")
		if err := r.cleanupHTTPRouteResources(ctx, httpRoute); err != nil {
			glog.V(6).Infof("Failed to cleanup HTTPRoute %v err %v\n", httpRoute, err)
			return err
		}
		UpdateHTTPRouteListenerStatus(ctx, r.Client, httpRoute)
		r.finalizerManager.RemoveFinalizers(ctx, httpRoute, httpRouteFinalizer)

		// TODO delete metrics
		return nil
	} else {
		httpLog.Info("Adding/Updating")
		r.eventRecorder.Event(httpRoute, corev1.EventTypeNormal,
			k8s.HTTPRouteeventReasonReconcile, "Adding/Updating Reconcile")
		err := r.reconcileHTTPRouteResource(ctx, httpRoute)
		// TODO add/update metrics
		return err
	}

}

func (r *HTTPRouteReconciler) cleanupHTTPRouteResources(ctx context.Context, httpRoute *gateway_api.HTTPRoute) error {

	_, _, err := r.buildAndDeployModel(ctx, httpRoute)

	return err
}

func (r *HTTPRouteReconciler) isHTTPRouteRelevant(ctx context.Context, httpRoute *gateway_api.HTTPRoute) bool {

	if len(httpRoute.Spec.ParentRefs) == 0 {
		glog.V(6).Infof("Ignore HTTPRoute which has no ParentRefs gateway %v \n ", httpRoute.Spec)
		return false
	}

	gw := &gateway_api.Gateway{}

	// TODO handle multiple parentRefs
	gwNamespace := httpRoute.Namespace
	if httpRoute.Spec.ParentRefs[0].Namespace != nil {
		gwNamespace = string(*httpRoute.Spec.ParentRefs[0].Namespace)
	}
	gwName := types.NamespacedName{
		Namespace: gwNamespace,
		// TODO assume one parent for now and point to service network
		Name: string(httpRoute.Spec.ParentRefs[0].Name),
	}

	if err := r.gwReconciler.Client.Get(ctx, gwName, gw); err != nil {
		glog.V(6).Infof("Could not find gateway %s: %s\n", gwName.String(), err.Error())
		glog.V(6).Infof("Ignore HTTPRoute whose ParentRef gatway object has NOT defined yet for %v\n", httpRoute.Spec)
		return false
	}

	// make sure gateway is a aws-vpc-lattice
	gwClass := &gateway_api.GatewayClass{}
	gwClassName := types.NamespacedName{
		Namespace: "default",
		Name:      string(gw.Spec.GatewayClassName),
	}

	if err := r.gwClassReconciler.Client.Get(ctx, gwClassName, gwClass); err != nil {
		glog.V(6).Infof("Ignore HTTPRoute that NOT controlled by any GatewayClass for %v\n", httpRoute.Spec)
		return false
	}

	if gwClass.Spec.ControllerName == config.LatticeGatewayControllerName {
		glog.V(6).Infof("Found aws-vpc-lattice for HTTPRoute for %v\n", httpRoute.Spec)

		return true
	} else {
		glog.V(6).Infof("Ignore non aws-vpc-lattice HTTPRoute !!! %v\n", httpRoute.Spec)
		return false
	}
}

func (r *HTTPRouteReconciler) buildAndDeployModel(ctx context.Context, httproute *gateway_api.HTTPRoute) (core.Stack, *latticemodel.Service, error) {
	httpLog := log.FromContext(ctx)

	stack, latticeService, err := r.modelBuilder.Build(ctx, httproute)

	if err != nil {

		r.eventRecorder.Event(httproute, corev1.EventTypeWarning,
			k8s.HTTPRouteEventReasonFailedBuildModel, fmt.Sprintf("Failed build model due to %v", err))
		glog.V(6).Infof("buildAndDeployModel, Failed build model for %v due to %v\n", httproute.Name, err)

		// Build failed
		// TODO continue deploy to trigger reconsile of stale HTTProute and policy
		return nil, nil, err
	}

	stackJSON, err := r.stackMashaller.Marshal(stack)
	if err != nil {
		//TODO
		glog.V(6).Infof("error on r.stackMashaller.Marshal error %v \n", err)
	}

	httpLog.Info("Successfully built model:", stackJSON, "")

	if err := r.stackDeployer.Deploy(ctx, stack); err != nil {
		glog.V(6).Infof("HTTPRouteReconciler: Failed deploy %s due to err %v \n", httproute.Name, err)

		var retryErr = errors.New(lattice.LATTICE_RETRY)

		if errors.As(err, &retryErr) {
			r.eventRecorder.Event(httproute, corev1.EventTypeNormal,
				k8s.HTTPRouteEventReasonRetryReconcile, "retry reconcile...")

		} else {
			r.eventRecorder.Event(httproute, corev1.EventTypeWarning,
				k8s.HTTPRouteEventReasonFailedDeployModel, fmt.Sprintf("Failed deploy model due to %v", err))
		}
		return nil, nil, err
	}

	httpLog.Info("Successfully deployed model")

	return stack, latticeService, err
}

func (r *HTTPRouteReconciler) reconcileHTTPRouteResource(ctx context.Context, httproute *gateway_api.HTTPRoute) error {
	glog.V(6).Infof("Beginning -- reconcileHTTPRouteResource, [%v]\n", httproute)

	if err := r.finalizerManager.AddFinalizers(ctx, httproute, httpRouteFinalizer); err != nil {
		r.eventRecorder.Event(httproute, corev1.EventTypeWarning, k8s.HTTPRouteventReasonFailedAddFinalizer, fmt.Sprintf("Failed add finalizer due to %v", err))
	}

	_, _, err := r.buildAndDeployModel(ctx, httproute)

	//TODO add metric

	if err == nil {
		r.eventRecorder.Event(httproute, corev1.EventTypeNormal,
			k8s.HTTPRouteeventReasonDeploySucceed, "Adding/Updating reconcile Done!")

		serviceStatus, err1 := r.latticeDataStore.GetLatticeService(httproute.Name, httproute.Namespace)

		if err1 == nil {
			r.updateHTTPRouteStatus(ctx, serviceStatus.DNS, httproute)
		}
	}

	return err

}

func (r *HTTPRouteReconciler) updateHTTPRouteStatus(ctx context.Context, dns string, httproute *gateway_api.HTTPRoute) error {
	glog.V(6).Infof("updateHTTPRouteStatus: httproute %v, dns %v\n", httproute, dns)
	httprouteOld := httproute.DeepCopy()

	if len(httproute.ObjectMeta.Annotations) == 0 {
		httproute.ObjectMeta.Annotations = make(map[string]string)
	}

	httproute.ObjectMeta.Annotations[LatticeAssignedDomainName] = dns

	if err := r.Client.Patch(ctx, httproute, client.MergeFrom(httprouteOld)); err != nil {
		glog.V(2).Infof("updateHTTPRouteStatus: Patch() received err %v \n", err)
		return errors.Wrapf(err, "failed to update httproute status")
	}

	httprouteOld = httproute.DeepCopy()
	if len(httproute.Status.RouteStatus.Parents) == 0 {
		httproute.Status.RouteStatus.Parents = make([]gateway_api.RouteParentStatus, 1)
		httproute.Status.RouteStatus.Parents[0].Conditions = make([]metav1.Condition, 1)
		httproute.Status.RouteStatus.Parents[0].Conditions[0].LastTransitionTime = eventhandlers.ZeroTransitionTime
	}

	httproute.Status.RouteStatus.Parents[0].ControllerName = config.LatticeGatewayControllerName

	httproute.Status.RouteStatus.Parents[0].Conditions[0].Type = string(gateway_api.RouteConditionAccepted)
	httproute.Status.RouteStatus.Parents[0].Conditions[0].Status = metav1.ConditionTrue
	httproute.Status.RouteStatus.Parents[0].Conditions[0].Message = fmt.Sprintf("DNS Name: %s", dns)
	httproute.Status.RouteStatus.Parents[0].Conditions[0].Reason = string(gateway_api.RouteReasonAccepted)
	httproute.Status.RouteStatus.Parents[0].Conditions[0].ObservedGeneration = httproute.Generation

	if httproute.Status.RouteStatus.Parents[0].Conditions[0].LastTransitionTime == eventhandlers.ZeroTransitionTime {
		httproute.Status.RouteStatus.Parents[0].Conditions[0].LastTransitionTime = metav1.NewTime(time.Now())
	}

	httproute.Status.RouteStatus.Parents[0].ParentRef.Group = httproute.Spec.ParentRefs[0].Group
	httproute.Status.RouteStatus.Parents[0].ParentRef.Kind = httproute.Spec.ParentRefs[0].Kind
	httproute.Status.RouteStatus.Parents[0].ParentRef.Name = httproute.Spec.ParentRefs[0].Name

	// Update listener Status
	UpdateHTTPRouteListenerStatus(ctx, r.Client, httproute)

	if err := r.Client.Status().Patch(ctx, httproute, client.MergeFrom(httprouteOld)); err != nil {
		glog.V(2).Infof("updateHTTPRouteStatus: Patch() received err %v \n", err)
		return errors.Wrapf(err, "failed to update httproute status")
	}

	glog.V(6).Infof("updateHTTPRouteStatus patched dns %v \n", dns)

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HTTPRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	gwEventHandler := eventhandlers.NewEnqueueRequestGatewayEvent(r.Client)
	svcEventHandler := eventhandlers.NewEqueueHTTPRequestServiceEvent(r.Client)
	svcImportEventHandler := eventhandlers.NewEqueueRequestServiceImportEvent(r.Client)
	return ctrl.NewControllerManagedBy(mgr).
		// Uncomment the following line adding a pointer to an instance of the controlled resource as an argument
		For(&gateway_api.HTTPRoute{}).
		Watches(&source.Kind{Type: &gateway_api.Gateway{}}, gwEventHandler).
		Watches(&source.Kind{Type: &corev1.Service{}}, svcEventHandler).
		Watches(&source.Kind{Type: &mcs_api.ServiceImport{}}, svcImportEventHandler).
		Complete(r)
}
